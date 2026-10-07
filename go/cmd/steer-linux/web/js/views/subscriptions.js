/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 订阅：状态表 + 立即更新 + stale 节点清理（引用安全由后端拒绝）+ 编辑抽屉。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon, fmtTime } = S;
  const ui = S.ui;

  let statuses = null;

  function latestFailure(status) {
    const failure = status.last_failure;
    if (!failure) return null;
    if (!status.last_success || !failure.at) return failure;
    return new Date(failure.at).getTime() > new Date(status.last_success).getTime() ? failure : null;
  }

  function statusLabel(status) {
    if (!status.enabled) return ['已停用', ''];
    if (latestFailure(status)) return [status.never_fetched ? '抓取失败' : '最近失败', 'badge--err'];
    if (status.never_fetched) return ['未抓取', ''];
    if (status.skipped) return ['成功 · 有跳过', 'badge--warn'];
    return ['成功', 'badge--ok'];
  }

  function updateSummary(status) {
    return `新增 ${status.added || 0} · 当前 ${status.current || 0} · 已失效 ${(status.stale || []).length} · 已跳过 ${status.skipped || 0}`;
  }

  function updateNotice(status) {
    const counts = status ? ` ${updateSummary(status)}。` : '';
    return `订阅节点已更新，当前运行配置未改变；无引用的消失节点已自动删除，仍被路由使用的节点已自动保留并应尽快解除引用。${counts}`;
  }

  function requestDelete(subscription) {
    const intent = S.store.intent;
    const owned = intent.nodes.filter((node) => node.source_subscription === subscription.id);
    if (!ui.guardCollectionDeletion('subscriptions', subscription.id, subscription.name || subscription.id)) return;
    ui.dialog({
      title: `删除订阅 · ${subscription.name || subscription.id}`,
      body: h('p', {}, `将从工作副本删除订阅和 ${owned.length} 个订阅节点。保存前可重新加载恢复。`),
      actions: [
        ['取消', null],
        ['删除', (close) => {
          intent.subscriptions = intent.subscriptions.filter((item) => item.id !== subscription.id);
          intent.nodes = intent.nodes.filter((node) => node.source_subscription !== subscription.id);
          S.store.touch();
          ui.toast(`已删除 ${subscription.name || subscription.id} · 未保存`, 'warn');
          close();
          view.render(document.querySelector('#view'));
        }, 'btn--danger']
      ]
    });
  }

  function openEditor(sub, focusOption) {
    const isNew = !sub;
    const opened = ui.drawer({
      eyebrow: isNew ? '新建订阅' : '编辑订阅', title: sub?.name || '新订阅', submitLabel: '保存到工作副本',
      renderBody(body) {
        const defaultInterval = S.uiSpec.subscription_update_interval_default;
        const draft = sub ? JSON.parse(JSON.stringify(sub)) : ui.creationDraft('subscriptions');
        const name = ui.input({ value: draft.name || '', placeholder: '订阅名称' });
        const enabled = ui.toggle(draft.enabled, (v) => { draft.enabled = v; });
        const url = ui.input({ value: draft.url || '', placeholder: 'https://sub.example.com/all', mono: true });
        const interval = ui.input({ value: draft.update_interval || '', placeholder: defaultInterval, mono: true });
        body.append(
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '订阅'), [
            ui.field('名称', name, null, 'name'),
            ui.field('启用', enabled, null, 'enabled'),
            ui.field('订阅 URL', url, null, 'url'),
            ui.field('更新间隔', interval, '定时调度使用 · 留空表示仅手动更新', 'update_interval')
          ])
        );
        return {
          submit() {
            if (!/^https?:\/\//.test(url.value.trim())) { ui.toast('订阅 URL 必须是 http(s)://', 'err'); return false; }
            draft.name = name.value.trim() || undefined;
            draft.url = url.value.trim();
            draft.update_interval = interval.value.trim() || undefined;
            if (isNew) S.store.intent.subscriptions.push(draft);
            else Object.assign(sub, draft);
            return draft;
          }
        };
      },
      onSubmit(sub) {
        S.store.touch();
        ui.toast(`订阅 ${sub.name || sub.id} 已${isNew ? '创建' : '更新'} · 未保存`, 'info');
        view.render(document.querySelector('#view'));
        return true;
      }
    });
    ui.focusDrawerOption(focusOption);
    return opened;
  }

  function openCleanup(subscription, nodes, isCurrent, root) {
    const box = h('div', { class: 'u-mt-10' });
    const renderList = () => {
      box.replaceChildren();
      const remaining = nodes.filter((n) => n.pinned_stale);
      if (!remaining.length) {
        box.replaceChildren(h('p', { class: 'muted' }, '没有已失效节点。'));
        return;
      }
      box.append(h('ul', { class: 'issue-list' }, remaining.map((node) => h('li', { class: 'issue issue--warning' }, [
        h('span', { class: 'issue__msg' }, node.name || node.id),
        h('button', { class: 'btn btn--sm btn--danger', onclick: async () => {
          if (!ui.guardCollectionDeletion('nodes', node.id, node.name || node.id)) return;
          if (S.store.dirty) {
            ui.toast('请先保存或放弃工作副本修改，再清理已失效节点', 'warn');
            return;
          }
          try {
            const startedEpoch = S.store.draftEpoch;
            await S.api.cleanNode(subscription.id, node.id);
            let reloaded = false;
            if (S.store.draftEpoch !== startedEpoch && S.store.dirty) {
              await S.store.refreshOverview();
              ui.toast(`已清理 ${node.name || '节点'}；期间工作副本已变化，已保留本地修改`, 'warn');
            } else {
              const reload = await S.store.reload();
              reloaded = reload?.ok === true;
              if (!reloaded) ui.toast(`已清理 ${node.name || '节点'}；工作副本未能自动重新载入`, 'warn');
            }
            if (reloaded) {
              const index = nodes.indexOf(node);
              if (index >= 0) nodes.splice(index, 1);
              ui.toast(`已清理 ${node.name || '失效节点'}`, 'ok');
            }
            renderList();
            if (isCurrent()) view.render(root);
          } catch (e) {
            ui.toast(e.message, 'err');
          }
        } }, '移除')
      ]))));
    };
    renderList();
    ui.dialog({
      title: `清理已失效节点 · ${subscription.name || subscription.id}`,
      body: h('div', {}, [
        h('p', { class: 'muted' }, '已失效节点指订阅更新后不再存在的节点。若仍有路由正在引用该节点，将无法直接清理。'),
        box
      ]),
      actions: [['关闭', null]]
    });
  }

  const view = {
    name: 'subscriptions',
    async render(root) {
      const isCurrent = ui.beginRender(root);
      const intent = S.store.intent;
      const statusByID = new Map((await S.api.subscriptions()).subscriptions.map((status) => [status.id, status]));
      statuses = intent.subscriptions.map((subscription) => statusByID.get(subscription.id) || {
        ...subscription, never_fetched: true, node_count: 0, current: 0, added: 0, skipped: 0, stale: []
      });

      const table = h('table', { class: 'table' }, [
        h('thead', {}, h('tr', {}, [
          h('th', { class: 'collection-drag-column', 'aria-label': '顺序' }),
          h('th', {}, '启用'), h('th', {}, '订阅'), h('th', {}, '状态'), h('th', {}, '最近更新'),
          h('th', { class: 'num' }, '节点'), h('th', { class: 'num' }, '跳过 / 失效'),
          h('th', { class: 'col-actions', 'aria-label': '操作' })
        ])),
        h('tbody', {}, statuses.map((s) => {
          const subscription = intent.subscriptions.find((item) => item.id === s.id) || s;
          const state = statusLabel(s);
          const updateBtn = h('button', { class: 'btn btn--sm', onclick: async () => {
            if (!s.enabled) {
              ui.toast('已停用的订阅不能更新；请先启用并保存配置', 'warn');
              return;
            }
            if (S.store.dirty) {
              ui.toast('请先保存或放弃工作副本修改，再更新订阅', 'warn');
              return;
            }
            updateBtn.disabled = true;
            updateBtn.classList.add('spinning');
            updateBtn.textContent = '更新中…';
            try {
              const startedEpoch = S.store.draftEpoch;
              const res = await S.api.updateSubscription(s.id);
              const updated = (res.subscriptions || []).find((item) => item.id === s.id);
              let reloaded = false;
              if (S.store.draftEpoch !== startedEpoch && S.store.dirty) {
                await S.store.refreshOverview();
                ui.toast('节点列表已更新；期间工作副本已变化，已保留本地修改且未自动重新载入', 'warn');
              } else {
                const reload = await S.store.reload();
                reloaded = reload?.ok === true;
                if (!reloaded) ui.toast('节点列表已更新；工作副本未自动重新载入，请稍后重试', 'warn');
              }
              if (reloaded) ui.toast(updateNotice(updated), 'ok');
              updateBtn.textContent = '立即更新';
              if (isCurrent()) await view.render(root);
            } catch (e) {
              ui.toast(e.message, 'err');
              updateBtn.textContent = '立即更新';
              if (isCurrent()) view.render(root);
            }
            updateBtn.classList.remove('spinning');
            updateBtn.disabled = !s.enabled;
          }, disabled: !s.enabled, title: s.enabled ? '' : '已停用的订阅不能更新' }, '立即更新');
          const failure = latestFailure(s) ? s.last_failure : null;
          const url = String(s.url || '');
          const menu = ui.rowMenu([
            s.stale?.length ? {
              label: `清理失效节点 ×${s.stale.length}`,
              onclick: () => openCleanup(s, intent.nodes.filter((n) => n.source_subscription === s.id), isCurrent, root)
            } : null,
            s.stale?.length ? 'separator' : null,
            { label: '删除', danger: true, onclick: () => requestDelete(s) }
          ]);
          return h('tr', ui.collectionRowAttributes(
            'subscriptions', subscription, intent.subscriptions, () => view.render(root), s.enabled === false ? 'is-disabled' : ''
          ), [
            h('td', { class: 'collection-drag-column' }, ui.collectionDragHandle('subscriptions', subscription, intent.subscriptions, () => view.render(root))),
            h('td', {}, ui.toggle(s.enabled, (v) => { const sub = S.store.intent.subscriptions.find((x) => x.id === s.id); sub.enabled = v; S.store.touch(); }, `启用 ${s.name || s.id}`)),
            h('td', {}, h('div', { class: 'cell-title' }, [
              h('strong', {}, s.name || s.id),
              h('small', { class: 'mono', title: url }, url.length > 48 ? `${url.slice(0, 48)}…` : url),
              h('small', {}, s.update_interval ? `每 ${s.update_interval} 自动更新` : '仅手动更新')
            ])),
            h('td', {}, h('span', { class: `badge ${state[1]}` }, state[0])),
            h('td', {}, h('div', { class: 'cell-title' }, [
              h('span', {}, s.last_success ? fmtTime(s.last_success) : h('span', { class: 'faint' }, '从未成功')),
              s.last_failure ? h('small', { class: failure ? 'text-err' : '', title: s.last_failure.summary },
                `${failure ? '失败' : '曾失败'} ${s.last_failure.at ? fmtTime(s.last_failure.at) : ''} · ${s.last_failure.summary}`) : null
            ])),
            h('td', { class: 'num' }, `${s.node_count}（当前 ${s.current}）`),
            h('td', { class: 'num' }, `${s.skipped || 0} / ${(s.stale || []).length}`),
            h('td', { class: 'col-actions' }, h('div', { class: 'row-actions' }, [
              updateBtn,
              h('button', { class: 'btn btn--sm btn--ghost', onclick: () => openEditor(S.store.intent.subscriptions.find((x) => x.id === s.id)) }, '编辑'),
              menu
            ]))
          ]);
        }))
      ]);

      if (!isCurrent()) return;
      const addButton = () => h('button', { class: 'btn btn--primary', onclick: () => openEditor(null) }, icon('plus', 15), '添加订阅');
      root.append(
        ui.viewHead('订阅', '从远程订阅获取节点。更新只改变节点列表，不会自动应用到运行配置。', [addButton()]),
        statuses.length
          ? h('section', { class: 'card table-card' }, h('div', { class: 'table-wrap' }, table))
          : ui.emptyState('还没有订阅', [addButton()], 'refresh')
      );
      const focus = ui.takeObjectFocus('subscription');
      const focused = focus && intent.subscriptions.find((subscription) => subscription.id === focus.object_id);
      if (focused) openEditor(focused, focus.option);
    }
  };

  S.views = S.views || {};
  S.views.subscriptions = view;
})();
