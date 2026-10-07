/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 外壳与共享组件：侧栏、状态条、通知、抽屉、对话框、表单件、Match 编辑器、问题列表。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon, asList } = S;

  const renderTokens = new WeakMap();
  const renderLifecycles = new WeakMap();
  const routeTokens = new WeakMap();
  let routeSequence = 0;
  let enabledToggleBusy = false;
  const collectionSelections = new Map();
  let collectionDragState = null;

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !collectionDragState) return;
    event.preventDefault();
    cancelCollectionDrag();
  });
  document.addEventListener('mouseup', (event) => {
    if (collectionDragState?.input !== 'mouse') return;
    commitCollectionDrag();
  });
  window.addEventListener('pointermove', handleCollectionPointerMove, { passive: false });
  window.addEventListener('pointerup', handleCollectionPointerUp, { passive: false });
  window.addEventListener('pointercancel', handleCollectionPointerCancel);
  window.addEventListener('blur', () => cancelCollectionDrag());

  function disposeRender(root) {
    const lifecycle = renderLifecycles.get(root);
    if (!lifecycle) return;
    renderLifecycles.delete(root);
    if (renderTokens.get(root) === lifecycle.token) renderTokens.delete(root);
    lifecycle.disposers.splice(0).forEach((dispose) => dispose());
  }

  function beginRender(root) {
    disposeRender(root);
    const token = {};
    renderTokens.set(root, token);
    const lifecycle = { token, disposers: [] };
    renderLifecycles.set(root, lifecycle);
    root.replaceChildren();
    const isCurrent = () => renderTokens.get(root) === token;
    isCurrent.onDispose = (dispose) => {
      if (renderLifecycles.get(root) === lifecycle) lifecycle.disposers.push(dispose);
      else dispose();
    };
    return isCurrent;
  }

  function beginRoute(root) {
    disposeRender(root);
    const token = ++routeSequence;
    routeTokens.set(root, token);
    root.replaceChildren();
    return token;
  }

  function isCurrentRoute(root, token) {
    return routeTokens.get(root) === token;
  }

  const GROUP_LABEL = { status: '状态', configuration: '配置', services: '服务', advanced: '高级' };
  const VIEW_LABEL = {
    overview: '总览', general: '基础设置', nodes: '节点', routes: '路由', dns: 'DNS 配置', proxies: '本地代理',
    rules: '规则', subscriptions: '订阅', diagnostics: '诊断', system: '系统', advanced: '高级配置'
  };
  const VIEW_ICON = {
    overview: 'gauge', general: 'sliders', nodes: 'server', routes: 'route', dns: 'globe', proxies: 'plug', rules: 'list',
    subscriptions: 'refresh', diagnostics: 'activity', system: 'cpu', advanced: 'braces'
  };
  const NAV = S.uiSpec.navigation.map((group) => ({
    label: GROUP_LABEL[group.key] || group.label,
    items: group.items.map((item) => [item.key, VIEW_ICON[item.key], VIEW_LABEL[item.key] || item.label])
  }));

  /* 行内菜单：<details> 保证菜单项始终在 DOM 中；点击外部或选择后收起。 */
  const closeMenus = (event) => {
    document.querySelectorAll?.('details.menu[open]').forEach((menu) => {
      if (!event || !menu.contains?.(event.target)) menu.removeAttribute('open');
    });
  };
  document.addEventListener('click', closeMenus);
  window.addEventListener('resize', () => closeMenus());
  window.addEventListener('scroll', () => closeMenus(), true);

  /* ---------- 侧栏 ---------- */
  function renderShell(router) {
    const side = document.querySelector('#side');
    const brand = h('div', { class: 'brand' }, [
      h('span', { class: 'brand__mark' }, icon('steer', 16)),
      h('div', { class: 'brand__text' }, h('strong', {}, 'Steer'), h('span', { class: 'brand__sub' }, 'Linux 控制台'))
    ]);
    const nav = h('nav', { 'aria-label': '页面' });
    for (const group of NAV) {
      nav.append(h('div', { class: 'nav-group__label' }, group.label));
      for (const [view, ic, label] of group.items) {
        nav.append(h('button', {
          class: 'nav-item', dataset: { view },
          onclick: () => router(view)
        }, icon(ic, 16), h('span', {}, label)));
      }
    }
    const foot = h('div', { class: 'side-foot' }, [
      h('button', {
        class: 'btn btn--sm btn--ghost',
        onclick: () => S.auth.logout()
      }, icon('logout', 15), '退出登录'),
      h('div', { class: 'side-foot__note' }, '仅允许本机访问\n令牌只保存在当前标签页')
    ]);
    side.append(brand, nav, foot);
  }

  /* ---------- 状态条 ---------- */
  function showValidation(v, title = '配置校验') {
    dialog({
      title,
      body: h('div', {}, [
        h('p', { class: 'muted' }, `${v.errors.length} 错误 · ${v.warnings.length} 警告 · 校验的是当前工作副本`),
        issueList(v.errors, jumpToObject),
        issueList(v.warnings, jumpToObject, true)
      ]),
      actions: [['关闭', null]]
    });
  }

  async function onValidate() {
    if (S.store.draftValid === false) {
      toast(`当前配置格式有误：${S.store.draftError}`, 'err');
      return;
    }
    const v = await S.api.validate(S.store.intent);
    if (!v.errors.length && !v.warnings.length) { toast('校验通过 · 0 错误 · 0 警告', 'ok'); return; }
    showValidation(v);
  }

  function jumpToObject(issue) {
    const target = { node: 'nodes', route: 'routes', rule: 'rules', dns_profile: 'dns', local_proxy: 'proxies', subscription: 'subscriptions' }[issue.object_type];
    if (!target || !S.router) return false;
    S.pendingObjectFocus = { ...issue };
    document.querySelectorAll('.dialog-overlay').forEach((overlay) => {
      const close = overlay.querySelector('.dialog__close');
      if (typeof close?.click === 'function') close.click(); else overlay.remove();
    });
    S.router(target);
    return true;
  }

  function takeObjectFocus(objectType) {
    if (S.pendingObjectFocus?.object_type !== objectType) return null;
    const focus = S.pendingObjectFocus;
    S.pendingObjectFocus = null;
    return focus;
  }

  function focusDrawerOption(option) {
    if (!option) return;
    setTimeout(() => {
      const selector = `[data-option="${option}"]`;
      const target = document.querySelector('#drawer-root')?.querySelector(selector) || document.querySelector(selector);
      if (!target) return;
      target.classList.add('is-focused');
      target.querySelector('input, select, textarea, button')?.focus();
      target.scrollIntoView?.({ block: 'center' });
    }, 0);
  }

  function collectionReferences(intent, targetCollection, targetId) {
    if (!intent || !targetId) return [];
    if (targetCollection === 'subscriptions') {
      const owned = new Set(asList(intent.nodes).filter((node) => node.source_subscription === targetId).map((node) => node.id));
      return asList(intent.routes).filter((route) => owned.has(route.node)).map((route) => ({
        source_collection: 'routes', source_object_type: 'route', source_id: route.id,
        source_label: route.name || route.id, field: 'node'
      }));
    }
    const references = [];
    for (const relation of S.uiSpec.collection_references || []) {
      if (relation.target_collection !== targetCollection) continue;
      for (const source of asList(intent[relation.source_collection])) {
        const value = source?.[relation.field];
        const matched = relation.multiple ? asList(value).includes(targetId) : value === targetId;
        if (!matched) continue;
        references.push({
          source_collection: relation.source_collection,
          source_object_type: relation.source_object_type,
          source_id: source.id,
          source_label: source.name || source.id,
          field: relation.field
        });
      }
    }
    return references;
  }

  function guardCollectionDeletion(targetCollection, targetId, label) {
    const references = collectionReferences(S.store.intent, targetCollection, targetId);
    if (!references.length) return true;
    dialog({
      title: `不能删除 · ${label || targetId}`,
      body: h('div', {}, [
        h('p', {}, '以下对象仍在引用它。请先修改这些引用；Steer 不会自动级联改写规则或路由。'),
        h('ul', { class: 'issue-list' }, references.map((reference) => h('li', { class: 'issue issue--warning' }, [
          h('span', { class: 'issue__where' }, reference.source_label),
          h('button', { class: 'btn btn--sm', onclick: () => jumpToObject({
            object_type: reference.source_object_type, object_id: reference.source_id, option: reference.field
          }) }, '前往引用')
        ])))
      ]),
      actions: [['关闭', null]]
    });
    return false;
  }

  async function onSave(apply) {
    try {
      const res = await S.store.save(apply);
      if (res.ok) {
        const writeValidation = res.res.apply_result?.validation || res.res.validation;
        if ((writeValidation?.errors?.length || 0) + (writeValidation?.warnings?.length || 0) > 0) {
          showValidation(writeValidation, apply && res.res.applied === false ? '保存和应用校验结果' : '保存校验结果');
        }
        const overviewWarning = res.overviewError ? `；状态刷新失败：${res.overviewError.message}` : '';
        if (apply && res.res.applied === false) {
          toast(`配置已保存但应用失败：${applyFailureSummary(res.res.apply_result || res.res)}${res.staleDraft ? '；期间新修改仍未保存' : ''}${overviewWarning}`, 'err');
        } else if (res.staleDraft) {
          toast(`${apply ? '请求时的工作副本已保存并应用；期间新增修改仍未保存' : '请求时的工作副本已保存；期间新增修改仍未保存'}${overviewWarning}。`, 'warn');
        } else if (res.overviewError) {
          toast(`${apply ? '已保存并应用' : '已保存'}${overviewWarning}`, 'warn');
        } else {
          toast(apply ? '已保存并应用。' : '已保存。', 'ok');
        }
      } else if (res.conflict) {
        conflictDialog(res.conflict, null, apply);
      } else if (res.busy) {
        toast('已有保存或重新载入操作正在进行，请等待完成。', 'warn');
      }
    } catch (error) {
      if (error.details?.validation) showValidation(error.details.validation, '保存前校验失败');
      else toast(`保存失败：${error.message}`, 'err');
    }
  }

  async function reloadSavedDraft(close, message) {
    try {
      const result = await S.store.reload();
      if (!result?.ok) {
        if (result?.staleDraft) toast('重新载入期间工作副本又发生变化；已保留这些新修改。', 'warn');
        else if (result?.busy) toast('保存、应用或重新载入正在进行；工作副本未丢弃，请稍后重试。', 'warn');
        return false;
      }
      close?.();
      toast(message || '已放弃全部修改并重新载入已保存配置。', 'info');
      S.renderCurrent?.();
      return true;
    } catch (error) {
      toast(`重新载入失败：${error.message}`, 'err');
      return false;
    }
  }

  function onDiscard() {
    if (!S.store.dirty) return;
    dialog({
      title: '放弃当前全部修改？',
      body: h('div', {}, [
        h('p', {}, '这会丢弃当前工作副本中的全部修改，并重新载入服务器上已保存的配置。'),
        h('p', { class: 'muted' }, '高级配置中尚未保存的文本也会被丢弃；当前运行配置不会改变。')
      ]),
      actions: [
        ['取消', null],
        ['放弃修改并重新载入', (close) => reloadSavedDraft(close), 'btn--danger']
      ]
    });
  }

  async function onApplySaved() {
    try {
      const result = await S.store.applySaved();
      if (result.busy) {
        toast('已有保存、应用或重新载入操作正在进行，请等待完成。', 'warn');
      } else if (result.ok) {
        if (result.validation?.warnings?.length) showValidation(result.validation, '应用已保存配置校验结果');
        if (result.overviewError) toast(`已应用已保存配置；状态刷新失败：${result.overviewError.message}`, 'warn');
        else toast('已应用已保存配置。', 'ok');
      } else {
        if (result.validation) showValidation(result.validation, '应用已保存配置校验失败');
        else toast(`应用已保存配置失败：${applyFailureSummary(result)}${result.overviewError ? `；状态刷新失败：${result.overviewError.message}` : ''}`, 'err');
      }
    } catch (error) {
      toast(`应用已保存配置失败：${error.message}`, 'err');
    }
  }

  async function onRefreshState() {
    try {
      const result = await S.store.refreshServerState();
      if (result?.busy) {
        toast('保存、应用或重新载入正在进行；稍后再刷新。', 'warn');
      } else if (result?.changed) {
        toast(S.store.dirty ? '服务器上的配置已变化；当前工作副本已保留，保存前请先处理冲突。' : '服务器上的配置已变化；可重新载入最新配置。', 'warn');
      } else if (result?.ok) {
        toast('服务器配置与运行状态已刷新。', 'info');
      }
      if (result?.ok) S.renderCurrent?.();
    } catch (error) {
      toast(`状态刷新失败：${error.message}`, 'err');
    }
  }

  function onReloadExternal() {
    if (S.store.dirty) {
      toast('服务器上的配置已变化；当前工作副本未被覆盖。请先保存、放弃或处理冲突。', 'warn');
      return false;
    }
    return reloadSavedDraft(null, '已重新载入服务器上的最新配置。');
  }

  async function onToggleEnabled(next) {
    if (enabledToggleBusy) return;
    if (S.store.saving === true || S.store.reloading === true || S.store.applying === true) {
      toast('已有保存、应用或重新载入操作正在进行，请等待完成。', 'warn');
      return;
    }

    enabledToggleBusy = true;
    renderStatusStrip();
    try {
      const res = await S.store.setEnabled(next);
      if (res.ok) {
        const overviewWarning = res.overviewError ? `；状态刷新失败：${res.overviewError.message}` : '';
        if (res.res.applied === false) {
          toast(`已保存为${next ? '启用' : '禁用'}，但应用失败：${applyFailureSummary(res.res.apply_result || res.res)}${res.staleDraft ? '；期间新修改仍未保存' : ''}${overviewWarning}`, 'err');
        } else if (res.staleDraft) {
          toast(`已保存并应用${next ? '启用' : '禁用'}状态；期间新增修改仍未保存${overviewWarning}。`, 'warn');
        } else if (res.overviewError) {
          toast(`${next ? 'Steer 已启用并应用' : 'Steer 已禁用并清理运行资源'}${overviewWarning}`, 'warn');
        } else {
          toast(next ? 'Steer 已启用并应用。' : 'Steer 已禁用并清理运行资源。', 'ok');
        }
      }
    } catch (error) {
      toast(`切换 Steer 状态失败：${error.message}`, 'err');
    } finally {
      enabledToggleBusy = false;
      renderStatusStrip();
    }
  }

  function generationLabel(value) {
    const parts = String(value || '').split('/').filter(Boolean);
    return parts[parts.length - 1] || '—';
  }

  function applyTime(record) {
    let date = record?.timestamp ? new Date(record.timestamp) : null;
    if ((!date || Number.isNaN(date.getTime())) && /^\d{13,}$/.test(String(record?.sequence || ''))) {
      date = new Date(Number(String(record.sequence).slice(0, 13)));
    }
    if (!date || Number.isNaN(date.getTime())) return '时间未知';
    return date.toLocaleString();
  }

  function applyFailureSummary(result) {
    const error = typeof result?.error === 'string' ? result.error : (result?.error?.message || '运行态未切换');
    return error;
  }

  function applyRecord(status, includeTechnicalDetail = false) {
    const record = status?.last_apply || null;
    if (!record) return h('p', { class: 'muted' }, '尚无应用记录。');
    const result = record.result || record;
    const failedBeforeActivation = !!result.candidate_generation && !result.activated;
    return h('div', { class: `apply-record ${result.ok ? 'is-ok' : 'is-err'}` }, [
      h('div', { class: 'apply-record__head' }, [
        h('span', { class: `badge badge--dot ${result.ok ? 'badge--ok' : 'badge--err'}` }, result.ok ? '成功' : '失败'),
        h('strong', {}, result.ok ? '应用成功' : '应用失败'),
        h('span', { class: 'muted' }, applyTime(record))
      ]),
      failedBeforeActivation
        ? h('p', { class: 'alert alert--err' }, '新配置未启用，当前运行配置保持不变。')
        : (!result.ok && result.activated
            ? h('p', { class: 'alert alert--err' }, '运行配置已变化，但应用过程未完整；请检查诊断信息。')
            : null),
      !result.ok ? h('p', { class: `apply-record__error ${includeTechnicalDetail && result.error ? 'is-technical' : ''}` }, includeTechnicalDetail && result.error
        ? result.error
        : (failedBeforeActivation
            ? '运行配置未切换；已保存配置仍可重试应用。'
            : '应用未完整成功；请打开诊断查看恢复步骤。')) : null
    ]);
  }

  async function forceSave(apply) {
    try {
      const res = await S.store.save(!!apply, true);
      if (res.ok) {
        const overviewWarning = res.overviewError ? `；状态刷新失败：${res.overviewError.message}` : '';
        if (apply && res.res.applied === false) toast(`已覆盖保存，但应用失败：${applyFailureSummary(res.res.apply_result || res.res)}${res.staleDraft ? '；期间新修改仍未保存' : ''}${overviewWarning}`, 'err');
        else if (res.staleDraft) toast(`请求时的工作副本已覆盖保存；期间新增修改仍未保存${overviewWarning}。`, 'warn');
        else if (res.overviewError) toast(`${apply ? '已覆盖保存并应用' : '已覆盖保存'}${overviewWarning}`, 'warn');
        else toast(apply ? '已覆盖保存并应用。' : '已覆盖保存。', 'ok');
      } else if (res.busy) {
        toast('已有保存或重新载入操作正在进行，请等待完成。', 'warn');
      }
    } catch (error) {
      toast(`覆盖保存失败：${error.message}`, 'err');
    }
  }

  function runState(status, savedEnabled) {
    const active = !!status.generation;
    if (active && status.healthy) return { tone: 'ok', label: '正常运行', title: '分流服务运行正常' };
    if (active) return { tone: 'err', label: '运行异常', title: '分流服务运行异常；请打开诊断' };
    if (savedEnabled) return { tone: 'err', label: '已停止', title: '已保存为启用，但分流服务未运行' };
    return { tone: 'off', label: '已停止', title: '分流服务已停止' };
  }

  function stripFact(label, value, title) {
    return h('div', { class: 'strip__fact', title: title || null },
      h('span', { class: 'strip__fact-label' }, label), h('span', { class: 'strip__fact-value' }, value));
  }

  function renderStatusStrip() {
    const strip = document.querySelector('#strip');
    strip.replaceChildren();
    const ov = S.store.overview || {};
    const status = ov.status || {};
    const lastApply = status.last_apply || null;
    const lastResult = lastApply?.result || lastApply;
    const savedEnabled = ov.saved_enabled === true;
    const state = runState(status, savedEnabled);
    const dirty = S.store.dirty;
    const draftValid = S.store.draftValid !== false;
    const pendingApply = S.store.pendingApply === true;
    const applying = S.store.applying === true;
    const busy = S.store.saving === true || S.store.reloading === true || applying;
    const externalChange = S.store.hasExternalChange === true;
    const invalidTitle = !draftValid ? '请先修复或放弃格式有误的配置' : '';

    const flags = [
      pendingApply ? h('span', { class: 'badge badge--warn', title: '已保存配置尚未应用到运行环境' }, '待应用') : null,
      !draftValid ? h('span', { class: 'badge badge--err', title: S.store.draftError }, '配置格式有误') : null,
      externalChange ? h('span', { class: 'badge badge--err', title: '服务器配置与当前工作副本不同；工作副本未被覆盖' }, '服务器配置已变化') : null
    ].filter(Boolean);

    const pending = dirty || !draftValid ? h('div', { class: 'strip__pending', role: 'group', 'aria-label': '未保存的修改' }, [
      h('span', { class: 'strip__pending-label' }, '工作副本已修改'),
      dirty ? h('button', { class: 'btn btn--sm btn--ghost btn--danger', onclick: onDiscard, disabled: busy }, '放弃修改') : null,
      h('button', { class: 'btn btn--sm', onclick: () => onSave(false), disabled: !dirty || !draftValid || busy, title: invalidTitle }, '保存'),
      h('button', { class: 'btn btn--sm btn--primary', onclick: () => onSave(true), disabled: !dirty || !draftValid || busy, title: invalidTitle }, '保存并应用')
    ]) : null;

    strip.append(
      h('div', { class: 'strip__group' }, [
        h('div', { class: 'strip__state', title: state.title }, [
          h('span', { class: `state-dot is-${state.tone}`, 'aria-hidden': 'true' }),
          stripFact('运行状态', state.label)
        ]),
        h('span', { class: 'strip__divider', 'aria-hidden': 'true' }),
        h('div', { class: 'strip__toggle' }, [
          toggle(savedEnabled, (next) => onToggleEnabled(next), '启用或禁用 Steer'),
          h('div', {}, h('span', { class: 'strip__fact-label' }, '配置开关'), h('span', { class: 'strip__fact-value' }, savedEnabled ? '启用' : '禁用'))
        ]),
        stripFact('上次应用', lastApply ? `${applyTime(lastApply)} · ${lastResult?.ok ? '成功' : '失败'}` : '—',
          lastResult?.ok === false ? '应用未完整成功；请打开诊断。' : ''),
        flags.length ? h('div', { class: 'strip__flags' }, flags) : null
      ]),
      h('div', { class: 'strip__actions' }, [
        h('button', {
          class: 'btn btn--sm btn--ghost', onclick: onRefreshState, disabled: busy,
          title: S.store.lastRefreshedAt ? `上次刷新 ${S.fmtTime(S.store.lastRefreshedAt)}` : '刷新状态'
        }, icon('refresh', 15), '刷新'),
        h('button', { class: 'btn btn--sm btn--ghost', onclick: onValidate }, icon('check', 15), '校验'),
        externalChange ? h('button', { class: `btn btn--sm ${dirty ? 'btn--danger' : 'btn--primary'}`, onclick: onReloadExternal, disabled: busy }, dirty ? '处理配置冲突' : '重新载入') : null,
        pendingApply || applying ? h('button', {
          class: `btn btn--sm ${!dirty && !busy ? 'btn--primary' : ''}`, onclick: onApplySaved, disabled: busy,
          title: '应用当前已保存配置'
        }, '应用已保存配置') : null,
        pending
      ])
    );
    strip.querySelector('.strip__toggle .switch').disabled = enabledToggleBusy || S.store.saving || S.store.reloading || S.store.applying;
  }

  /* ---------- 通知 ---------- */
  function toast(message, kind = 'info') {
    const box = document.querySelector('#toasts');
    const el = h('div', { class: `toast toast--${kind}` }, h('span', {}, message));
    box.append(el);
    setTimeout(() => {
      el.classList.add('is-leaving');
      setTimeout(() => el.remove(), 320);
    }, 4200);
  }

  /* ---------- 对话框 ---------- */
  function dialog({ title, body, actions = [], width }) {
    const overlay = h('div', { class: 'dialog-overlay' });
    const close = () => {
      overlay.remove();
      document.removeEventListener('keydown', onEsc);
    };
    const onEsc = (e) => { if (e.key === 'Escape') close(); };
    overlay.append(h('div', { class: `dialog ${width ? 'dialog--wide' : ''}` }, [
      h('div', { class: 'dialog__head' }, h('strong', {}, title), h('button', { class: 'dialog__close', onclick: close, 'aria-label': '关闭' }, '×')),
      h('div', { class: 'dialog__body' }, body),
      actions.length ? h('div', { class: 'dialog__foot' }, actions.map(([label, handler, cls]) =>
        h('button', { class: `btn ${cls || ''}`, onclick: () => (handler ? handler(close) : close()) }, label))) : null
    ]));
    overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
    document.addEventListener('keydown', onEsc);
    document.body.append(overlay);
    return { close };
  }

  function conflictDialog(conflict, beforeForceSave, apply = false) {
    dialog({
      title: '配置冲突',
      body: h('div', {}, [
        h('p', { class: 'muted' }, '服务器上的配置已被其他会话修改。请选择保留哪一份配置。'),
        h('div', { class: 'conflict-grid' }, [
          h('div', { class: 'conflict-col' }, [
            h('span', { class: 'badge badge--warn' }, '未保存'),
            h('strong', {}, '本地工作副本'),
            h('p', { class: 'muted' }, '包含你的未保存修改')
          ]),
          h('div', { class: 'conflict-col' }, [
            h('span', { class: 'badge' }, '最新配置'),
            h('strong', {}, '服务器'),
            h('p', { class: 'muted' }, '包含其他会话保存的最新修改')
          ])
        ])
      ]),
      actions: [
        ['以服务器为准（丢弃本地修改）', (close) => reloadSavedDraft(close, '已丢弃本地修改并重载服务器配置'), 'btn--danger'],
        ['覆盖保存（保留本地修改）', (close) => { close(); beforeForceSave?.(); forceSave(apply); }],
        ['取消', null]
      ]
    });
  }

  /* ---------- 抽屉（右侧编辑面板，非 LuCI 式 modalonly） ---------- */
  function drawer({ eyebrow, title, renderBody, onSubmit, submitLabel = '保存到工作副本', width }) {
    const root = document.querySelector('#drawer-root');
    const close = () => {
      root.classList.remove('is-open');
      document.removeEventListener('keydown', onEsc);
      setTimeout(() => root.replaceChildren(), 240);
    };
    const onEsc = (e) => { if (e.key === 'Escape') close(); };
    const overlay = h('div', { class: 'drawer-overlay', onclick: close });
    const panel = h('div', { class: `drawer ${width ? 'drawer--wide' : ''}` }, [
      h('div', { class: 'drawer__head' }, [
        h('div', {}, h('span', { class: 'drawer__eyebrow' }, eyebrow), h('strong', { class: 'drawer__title' }, title)),
        h('button', { class: 'drawer__close', onclick: close, 'aria-label': '关闭' }, '×')
      ]),
      h('div', { class: 'drawer__body' }),
      h('div', { class: 'drawer__foot' }, [
        h('button', { class: 'btn', onclick: close }, '取消'),
        h('button', { class: 'btn btn--primary', onclick: submit }, submitLabel)
      ])
    ]);
    root.append(overlay, panel);
    const spec = renderBody(panel.querySelector('.drawer__body'));
    function submit() {
      const payload = spec.submit();
      if (payload === false) return;
      Promise.resolve(onSubmit(payload)).then((ok) => { if (ok !== false) close(); });
    }
    document.addEventListener('keydown', onEsc);
    requestAnimationFrame(() => root.classList.add('is-open'));
    return { close };
  }

  /* ---------- 表单件 ---------- */
  function field(label, control, hint, option) {
    const id = `f-${Math.random().toString(36).slice(2, 7)}`;
    const el = h('div', { class: 'field', dataset: option ? { option } : null });
    if (label) {
      el.append(h('label', { for: id }, label));
      if (control.inputControl) control.inputControl.setAttribute('id', id);
      else if (control.setAttribute) control.setAttribute('id', id);
    }
    el.append(control);
    if (hint) el.append(h('div', { class: 'field__hint' }, hint));
    return el;
  }

  function input({ value = '', placeholder = '', type = 'text', disabled = false, mono = false, oninput }) {
    const control = h('input', { class: `input ${mono ? 'input--mono' : ''}`, type, value, placeholder, disabled, oninput, spellcheck: mono ? 'false' : null });
    if (type !== 'password') return control;

    const button = h('button', {
      class: 'input-reveal__button', type: 'button', 'aria-pressed': 'false',
      onclick: () => {
        const visible = control.type === 'text';
        control.type = visible ? 'password' : 'text';
        button.setAttribute('aria-pressed', String(!visible));
        button.textContent = visible ? '显示' : '隐藏';
      }
    }, '显示');
    const wrapper = h('div', { class: 'input-reveal' }, control, button);
    Object.defineProperty(wrapper, 'value', {
      configurable: false,
      enumerable: true,
      get: () => control.value,
      set: (next) => { control.value = next; }
    });
    wrapper.inputControl = control;
    return wrapper;
  }

  function textarea({ value = '', placeholder = '', rows = 6, disabled = false, sensitive = false, oninput }) {
    const control = h('textarea', {
      class: `textarea ${sensitive ? 'sensitive-textarea is-masked' : ''}`,
      placeholder, rows, disabled, spellcheck: 'false', autocomplete: sensitive ? 'off' : null, oninput
    });
    /* textarea does not reflect a value attribute like input; assign the value property to preserve newlines. */
    control.value = String(value ?? '');
    if (!sensitive) return control;

    const button = h('button', {
      class: 'input-reveal__button', type: 'button', 'aria-pressed': 'false', 'aria-label': '显示敏感内容',
      onclick: () => {
        const visible = !control.classList.contains('is-masked');
        control.classList.toggle('is-masked', visible);
        button.setAttribute('aria-pressed', String(!visible));
        button.setAttribute('aria-label', visible ? '显示敏感内容' : '隐藏敏感内容');
        button.textContent = visible ? '显示' : '隐藏';
      }
    }, '显示');
    const wrapper = h('div', { class: 'input-reveal input-reveal--multiline' }, control, button);
    Object.defineProperty(wrapper, 'value', {
      configurable: false,
      enumerable: true,
      get: () => control.value,
      set: (next) => { control.value = String(next ?? ''); }
    });
    wrapper.inputControl = control;
    return wrapper;
  }

  function select(options, value, onchange) {
    return h('select', { class: 'select', onchange: (e) => onchange?.(e.target.value) },
      options.map(([v, label]) => h('option', { value: v, selected: String(v) === String(value) }, label)));
  }

  function multiChoice(options, values = [], { onchange, missingLabel = '缺失' } = {}) {
    const selected = new Set(asList(values).map(String));
    const choices = options.slice();
    for (const value of selected) {
      if (!choices.some(([candidate]) => String(candidate) === value)) choices.push([value, `${missingLabel}：${value}`]);
    }
    const root = h('div', { class: 'choice-grid' });
    const emit = () => onchange?.([...selected]);
    for (const [rawValue, label] of choices) {
      const value = String(rawValue);
      const button = h('button', {
        class: 'choice-toggle', type: 'button', role: 'checkbox', 'aria-checked': String(selected.has(value)),
        onclick: () => {
          if (selected.has(value)) selected.delete(value); else selected.add(value);
          button.setAttribute('aria-checked', String(selected.has(value)));
          emit();
        }
      }, label);
      root.append(button);
    }
    Object.defineProperty(root, 'value', { enumerable: true, get: () => [...selected] });
    root.commitPending = () => {};
    return root;
  }

  function toggle(checked, onchange, label) {
    const btn = h('button', {
      class: 'switch', role: 'switch', 'aria-checked': String(!!checked), 'aria-label': label || '',
      onclick: () => {
        const next = btn.getAttribute('aria-checked') !== 'true';
        btn.setAttribute('aria-checked', String(next));
        onchange?.(next);
      }
    });
    return btn;
  }

  function toggleRow(label, checked, onchange) {
    const row = h('div', { class: 'toggle-row' });
    row.append(toggle(checked, onchange, label), h('span', {}, label));
    return row;
  }

  function parseIPv4Literal(value) {
    const pieces = String(value).split('.');
    if (pieces.length !== 4) return null;
    const octets = [];
    for (const piece of pieces) {
      if (!/^(0|[1-9]\d{0,2})$/.test(piece)) return null;
      const octet = Number(piece);
      if (octet > 255) return null;
      octets.push(octet);
    }
    return octets;
  }

  function parseIPv6Literal(value) {
    let address = String(value);
    const zoneIndex = address.indexOf('%');
    if (zoneIndex >= 0) {
      if (zoneIndex === 0 || zoneIndex === address.length - 1 || address.indexOf('%', zoneIndex + 1) >= 0) return null;
      address = address.slice(0, zoneIndex);
    }
    if (!address.includes(':') || address.indexOf('::') !== address.lastIndexOf('::')) return null;

    const compressed = address.includes('::');
    const halves = compressed ? address.split('::') : [address];
    const parseWords = (part, allowIPv4Tail) => {
      if (!part) return [];
      const tokens = part.split(':');
      const words = [];
      for (let index = 0; index < tokens.length; index++) {
        const token = tokens[index];
        if (token.includes('.')) {
          if (!allowIPv4Tail || index !== tokens.length - 1) return null;
          const octets = parseIPv4Literal(token);
          if (!octets) return null;
          words.push((octets[0] << 8) | octets[1], (octets[2] << 8) | octets[3]);
        } else {
          if (!/^[0-9a-f]{1,4}$/i.test(token)) return null;
          words.push(Number.parseInt(token, 16));
        }
      }
      return words;
    };

    const left = parseWords(halves[0], !compressed);
    const right = compressed ? parseWords(halves[1], true) : [];
    if (!left || !right) return null;
    if (!compressed) return left.length === 8 ? left : null;
    const omitted = 8 - left.length - right.length;
    return omitted >= 1 ? [...left, ...Array(omitted).fill(0), ...right] : null;
  }

  function classifyLocalProxyListen(value) {
    const address = String(value ?? '');
    const ipv4 = parseIPv4Literal(address);
    if (ipv4) return ipv4[0] === 127 ? 'loopback' : 'non_loopback';
    const ipv6 = parseIPv6Literal(address);
    if (!ipv6) return 'invalid';
    return ipv6.slice(0, 7).every((word) => word === 0) && ipv6[7] === 1 ? 'loopback' : 'non_loopback';
  }

  /* chips：动态列表（回车 / 失焦 / 显式添加提交 pending token，× 删除） */
  function chips(values = [], { placeholder = '', onchange } = {}) {
    let current = asList(values).map(String);
    const box = h('div', { class: 'chips' });
    const list = h('span', { class: 'contents' });
    const inp = h('input', {
      class: 'chips__input', placeholder,
      onkeydown: (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          commitPending();
        }
      },
      onblur: commitPending
    });
    const add = h('button', {
      class: 'chips__add', type: 'button', 'aria-label': '添加当前项', onclick: commitPending
    }, '添加');
    function commitPending() {
      const value = inp.value.trim();
      inp.value = '';
      if (!value || current.includes(value)) return false;
      current.push(value);
      render();
      onchange?.(current.slice());
      return true;
    }
    function render() {
      list.replaceChildren();
      current.forEach((v, i) => list.append(h('span', { class: 'chip' }, v, h('button', {
        class: 'chip__x', type: 'button', 'aria-label': `移除 ${v}`,
        onclick: () => { current.splice(i, 1); render(); onchange?.(current.slice()); }
      }, '×'))));
    }
    render();
    box.append(list, inp, add);
    box.commitPending = commitPending;
    return box;
  }

  /* ---------- Match 编辑器（每行一条表达式 · 行内 OR · geo 前缀补全） ---------- */
  function matchEditor({ value = [], kind, catalog = [], placeholder = '' }) {
    const ta = h('textarea', { class: 'match-editor__ta', spellcheck: 'false', autocomplete: 'off', placeholder });
    ta.value = asList(value).join('\n');
    const list = h('div', { class: 'match-editor__list', hidden: true, role: 'listbox' });
    const status = h('div', { class: 'match-editor__status' });
    const wrap = h('div', { class: 'match-editor' }, ta, list, status);

    const prefixes = kind === 'domain' ? S.uiSpec.domain_prefixes : S.uiSpec.ip_prefixes;
    const geoPrefix = kind === 'domain' ? 'geosite:' : 'geoip:';
    let matches = [];
    let active = 0;

    function currentLine() {
      const pos = ta.selectionStart ?? ta.value.length;
      const start = ta.value.lastIndexOf('\n', pos - 1) + 1;
      let end = ta.value.indexOf('\n', pos);
      if (end < 0) end = ta.value.length;
      return { start, end, value: ta.value.slice(start, end).trim() };
    }

    function render() {
      list.replaceChildren();
      if (!matches.length) { list.hidden = true; status.textContent = ''; return; }
      matches.forEach((m, i) => list.append(h('button', {
        type: 'button', class: 'match-editor__item' + (i === active ? ' is-active' : ''),
        role: 'option', 'aria-selected': String(i === active),
        onmousedown: (e) => e.preventDefault(),
        onclick: () => accept(m)
      }, m)));
      status.textContent = `${matches.length} 个匹配`;
      list.hidden = false;
    }

    function suggest() {
      const line = currentLine().value.toLowerCase();
      matches = [];
      if (line.startsWith(geoPrefix)) {
        const needle = line.slice(geoPrefix.length);
        matches = catalog.filter((n) => String(n).toLowerCase().includes(needle)).slice(0, 50).map((n) => geoPrefix + n);
      } else if (line === '' || prefixes.some((p) => p.startsWith(line))) {
        matches = prefixes.filter((p) => p.startsWith(line));
      }
      active = 0;
      render();
    }

    function accept(value) {
      const line = currentLine();
      ta.value = ta.value.slice(0, line.start) + value + ta.value.slice(line.end);
      const pos = line.start + value.length;
      ta.setSelectionRange(pos, pos);
      ta.focus();
      suggest();
    }

    ta.addEventListener('input', suggest);
    ta.addEventListener('click', suggest);
    ta.addEventListener('focus', suggest);
    ta.addEventListener('blur', () => setTimeout(() => { list.hidden = true; status.textContent = ''; }, 130));
    ta.addEventListener('keydown', (e) => {
      if (list.hidden || !matches.length) return;
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        active = (active + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length;
        render();
      } else if (e.key === 'Tab') {
        e.preventDefault();
        accept(matches[active]);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        list.hidden = true;
        status.textContent = '';
      }
    });

    return {
      el: wrap,
      get value() { return ta.value.split('\n').map((s) => s.trim()).filter(Boolean); }
    };
  }

  /* ---------- 校验问题列表 ---------- */
  function issueMessage(issue) {
    return {
      REQUIRED: '必填字段尚未填写',
      DANGLING_NODE: '所选节点不存在',
      DANGLING_DETOUR: '所选前置路由不存在',
      DANGLING_DNS_PROFILE: '所选 DNS 配置不存在',
      DANGLING_ROUTE: '所选路由不存在',
      DANGLING_LOCAL_PROXY: '所选本地代理入口不存在',
      DISABLED_NODE: '所选节点已停用',
      DISABLED_DETOUR: '所选前置路由已停用',
      DISABLED_DNS_PROFILE: '所选 DNS 配置已停用',
      DISABLED_ROUTE: '所选路由已停用',
      DISABLED_LOCAL_PROXY: '所选本地代理入口已停用',
      ROUTE_DETOUR_CYCLE: '前置代理链存在循环引用',
      LOCAL_PROXY_AUTH_REQUIRED: '该监听地址允许其他设备连接，必须设置用户名和密码',
      INVALID_DURATION: '更新间隔必须是大于零的时长',
      DNS_PROJECTION_EMPTY: '该规则仅含连接阶段条件，不影响 DNS 上游选择'
    }[issue.code] || issue.message;
  }

  function issueList(issues, jump, warning = false) {
    if (!issues.length) return null;
    return h('ul', { class: 'issue-list' },
      issues.map((issue) => h('li', { class: `issue ${warning ? 'issue--warning' : ''}` }, [
        h('span', { class: 'issue__msg' }, issueMessage(issue)),
        jump && issue.object_type ? h('button', { class: 'btn btn--sm', onclick: () => jump(issue) }, '前往') : null
      ])));
  }

  /* ---------- 视图页头与页面结构 ---------- */
  function viewHead(title, sub, actions = []) {
    const visible = asList(actions).filter(Boolean);
    return h('div', { class: 'view-head' }, [
      h('div', {}, h('h1', { class: 'view-title' }, title), sub ? h('p', { class: 'view-sub' }, sub) : null),
      visible.length ? h('div', { class: 'view-head__actions' }, visible) : null
    ]);
  }

  /* 区块：标题 + 可选说明 + 右侧附加内容；attrs 透传到 <section>。 */
  function section(title, { sub, aside, class: cls = '', attrs = {} } = {}, ...children) {
    return h('section', { ...attrs, class: `card ${cls}`.trim() }, [
      title || aside ? h('div', { class: 'card__head' }, [
        h('div', {}, title ? h('h2', { class: 'card__title' }, title) : null, sub ? h('p', { class: 'card__sub' }, sub) : null),
        aside ? h('div', { class: 'card__aside' }, aside) : null
      ]) : null,
      ...children
    ]);
  }

  function fact(label, value, { mono = false } = {}) {
    return h('div', { class: 'fact' }, h('dt', {}, label), h('dd', { class: mono ? 'mono' : null }, value));
  }

  function facts(entries, { single = false } = {}) {
    return h('dl', { class: `facts ${single ? 'facts--single' : ''}`.trim() },
      entries.filter(Boolean).map((entry) => (Array.isArray(entry) ? fact(entry[0], entry[1], entry[2]) : entry)));
  }

  function emptyState(message, actions = [], iconName = 'inbox') {
    const visible = asList(actions).filter(Boolean);
    return h('div', { class: 'empty' }, [
      icon(iconName, 22),
      h('p', {}, message),
      visible.length ? h('div', { class: 'empty__actions' }, visible) : null
    ]);
  }

  /* 行尾“更多”菜单：items = [{ label, onclick, danger, disabled, title }] | 'separator' */
  function rowMenu(items, label = '更多操作') {
    const menu = h('details', { class: 'menu' });
    const summary = h('summary', { class: 'btn btn--sm btn--ghost btn--icon', 'aria-label': label, title: label }, icon('more', 16));
    const list = h('div', { class: 'menu__list', role: 'menu' });
    for (const item of items.filter(Boolean)) {
      if (item === 'separator') { list.append(h('div', { class: 'menu__sep', role: 'separator' })); continue; }
      list.append(h('button', {
        class: `menu__item ${item.danger ? 'menu__item--danger' : ''}`, type: 'button', role: 'menuitem',
        disabled: !!item.disabled, title: item.title || null,
        onclick: (event) => { menu.removeAttribute('open'); item.onclick?.(event); }
      }, item.label));
    }
    /* 菜单列表用 fixed 定位，避免被表格的滚动容器裁剪；滚动或缩放时收起。 */
    menu.addEventListener('toggle', () => {
      if (!menu.open) return;
      const anchor = summary.getBoundingClientRect?.();
      if (!anchor || !list.style) return;
      list.style.position = 'fixed';
      list.style.right = `${Math.max(8, (window.innerWidth || 0) - anchor.right)}px`;
      const below = (window.innerHeight || 0) - anchor.bottom;
      const height = list.getBoundingClientRect?.().height || 0;
      if (below < height + 12 && anchor.top > height + 12) {
        list.style.top = 'auto';
        list.style.bottom = `${(window.innerHeight || 0) - anchor.top + 4}px`;
      } else {
        list.style.bottom = 'auto';
        list.style.top = `${anchor.bottom + 4}px`;
      }
    });
    menu.append(summary, list);
    return menu;
  }

  function collectionItemMovable(collection, item) {
    const policy = S.uiSpec.collection_ordering?.[collection];
    if (!policy || !item?.[policy.stable_id_field || 'id']) return false;
    if (policy.movable_kinds?.length && !policy.movable_kinds.includes(item.kind)) return false;
    return !policy.pinned_last_boolean_field || item[policy.pinned_last_boolean_field] !== true;
  }

  function selectedCollectionID(collection, items) {
    const selected = collectionSelections.get(collection) || '';
    return asList(items).some((item) => item?.id === selected) ? selected : '';
  }

  function selectCollectionItem(collection, itemID) {
    if (itemID) collectionSelections.set(collection, itemID);
    else collectionSelections.delete(collection);
  }

  function syncCollectionRowSelection(collection, selectedRow) {
    collectionRows(collection).forEach((row) => {
      const selected = row === selectedRow;
      row.classList.toggle('is-selected', selected);
      row.setAttribute?.('aria-selected', String(selected));
    });
  }

  function collectionRows(collection) {
    return Array.from(document.querySelectorAll?.('[data-collection-id]') || [])
      .filter((row) => row.dataset?.collection === collection);
  }

  function captureCollectionRowPositions(collection) {
    return new Map(collectionRows(collection).map((row) => [
      row.dataset.collectionId, row.getBoundingClientRect()
    ]));
  }

  function animateCollectionRows(collection, positions) {
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
    collectionRows(collection).forEach((row) => {
      const previous = positions.get(row.dataset.collectionId);
      if (!previous) return;
      const current = row.getBoundingClientRect();
      const offsetX = previous.left - current.left;
      const offsetY = previous.top - current.top;
      if (Math.abs(offsetX) < 1 && Math.abs(offsetY) < 1) return;
      row.animate?.([
        { transform: `translate3d(${offsetX}px, ${offsetY}px, 0)` },
        { transform: 'translate3d(0, 0, 0)' }
      ], { duration: 160, easing: 'cubic-bezier(.2, .8, .2, 1)' });
    });
  }

  function moveCollectionWithAnimation(collection, mutate, rerender) {
    const positions = captureCollectionRowPositions(collection);
    if (!mutate()) return false;
    rerender();
    animateCollectionRows(collection, positions);
    return true;
  }

  function collectionDragCompatible(state, item) {
    if (!state || !collectionItemMovable(state.collection, item)) return false;
    const policy = S.uiSpec.collection_ordering?.[state.collection];
    if (!policy?.group_field) return true;
    const source = state.items.find((candidate) => candidate.id === state.itemID);
    return (source?.[policy.group_field] || '') === (item?.[policy.group_field] || '');
  }

  function clearCollectionDragVisuals(state = collectionDragState) {
    const scope = state?.sourceRow?.parentElement || document;
    Array.from(scope?.querySelectorAll?.('.is-dragging, .is-placeholder, .is-drop-before, .is-drop-after') || [])
      .forEach((row) => row.classList.remove('is-dragging', 'is-placeholder', 'is-drop-before', 'is-drop-after'));
    state?.sourceRow?.classList?.remove('is-dragging', 'is-placeholder');
    state?.handle?.setAttribute?.('aria-grabbed', 'false');
    state?.preview?.remove?.();
  }

  function restoreCollectionDragPreview(state) {
    if (!state?.sourceRow || !state.originalParent) return;
    const reference = state.originalNextSibling?.parentNode === state.originalParent
      ? state.originalNextSibling : null;
    if (state.sourceRow.parentNode === state.originalParent && state.sourceRow.nextElementSibling === reference)
      return;
    const positions = captureCollectionRowPositions(state.collection);
    state.originalParent.insertBefore(state.sourceRow, reference);
    animateCollectionRows(state.collection, positions);
  }

  function cancelCollectionDrag() {
    const state = collectionDragState;
    restoreCollectionDragPreview(state);
    clearCollectionDragVisuals(state);
    collectionDragState = null;
  }

  function previewCollectionDrop(state, row, after) {
    const source = state?.sourceRow;
    const parent = row?.parentNode;
    if (!source || !parent || source === row) return false;
    const reference = after ? row.nextElementSibling : row;
    if (reference === source || (!after && source.nextElementSibling === row)) return false;
    const positions = captureCollectionRowPositions(state.collection);
    parent.insertBefore(source, reference);
    animateCollectionRows(state.collection, positions);
    return true;
  }

  function markCollectionDropTarget(state, row, item, after) {
    if (!collectionDragCompatible(state, item) || item.id === state.itemID) {
      state.targetRow?.classList?.remove('is-drop-before', 'is-drop-after');
      state.targetRow = null;
      state.targetID = '';
      return false;
    }
    if (state.targetRow !== row)
      state.targetRow?.classList?.remove('is-drop-before', 'is-drop-after');
    row.classList.remove('is-drop-before', 'is-drop-after');
    row.classList.add(after ? 'is-drop-after' : 'is-drop-before');
    state.targetRow = row;
    state.targetID = item.id;
    state.after = after;
    previewCollectionDrop(state, row, after);
    return true;
  }

  function updateCollectionDropTargetFromPoint(state, event) {
    let row = document.elementFromPoint?.(event.clientX, event.clientY)?.closest?.('[data-collection-id]');
    if (row === state.sourceRow)
      return true;
    if (!row || row.dataset?.collection !== state.collection) {
      row = collectionRows(state.collection).filter((candidate) => candidate !== state.sourceRow)
        .find((candidate) => {
          const rect = candidate.getBoundingClientRect();
          const bottom = rect.bottom ?? rect.top + rect.height;
          return event.clientY >= rect.top && event.clientY <= bottom;
        });
    }
    const target = row && state.items.find((candidate) => candidate.id === row.dataset.collectionId);
    if (target) {
      const rect = row.getBoundingClientRect();
      markCollectionDropTarget(state, row, target, event.clientY >= rect.top + rect.height / 2);
      return true;
    }
    const scopeRect = state.sourceRow?.parentElement?.getBoundingClientRect?.();
    if (scopeRect && event.clientX >= scopeRect.left && event.clientX <= (scopeRect.right ?? scopeRect.left + scopeRect.width) &&
        event.clientY >= scopeRect.top && event.clientY <= (scopeRect.bottom ?? scopeRect.top + scopeRect.height))
      return true;
    state.targetRow?.classList?.remove('is-drop-before', 'is-drop-after');
    state.targetRow = null;
    state.targetID = '';
    return false;
  }

  function handleCollectionPointerMove(event) {
    const state = collectionDragState;
    if (!state || state.pointerId !== event.pointerId) return;
    event.preventDefault();
    if (state.preview) {
      state.preview.style.left = `${event.clientX - state.grabOffsetX}px`;
      state.preview.style.top = `${event.clientY - state.grabOffsetY}px`;
    }
    updateCollectionDropTargetFromPoint(state, event);
    const edge = 48;
    if (event.clientY < edge) window.scrollBy?.({ top: -18, behavior: 'auto' });
    else if (event.clientY > (window.innerHeight || 0) - edge) window.scrollBy?.({ top: 18, behavior: 'auto' });
  }

  function handleCollectionPointerUp(event) {
    const state = collectionDragState;
    if (!state || state.pointerId !== event.pointerId) return;
    event.preventDefault();
    event.stopPropagation();
    commitCollectionDrag();
  }

  function handleCollectionPointerCancel(event) {
    if (!collectionDragState || collectionDragState.pointerId !== event.pointerId) return;
    cancelCollectionDrag();
  }

  function orderingToast(collection) {
    const labels = {
      nodes: '节点', routes: '路由', dns_profiles: 'DNS 配置', local_proxies: '本地代理',
      rules: '规则', subscriptions: '订阅'
    };
    toast(`${labels[collection] || '项目'}顺序已调整 · 未保存`, 'info');
  }

  function commitCollectionDrag() {
    const state = collectionDragState;
    if (!state?.targetID) {
      cancelCollectionDrag();
      return false;
    }
    selectCollectionItem(state.collection, state.itemID);
    const moved = S.store.moveCollectionItemTo(
      state.collection, state.itemID, state.targetID, state.after, state.visibleIDs
    );
    if (!moved) {
      cancelCollectionDrag();
      return false;
    }
    collectionDragState = null;
    state.targetRow?.classList?.remove('is-drop-before', 'is-drop-after');
    state.handle?.setAttribute?.('aria-grabbed', 'false');
    state.sourceRow?.classList?.remove('is-dragging');
    const finish = () => {
      state.sourceRow?.classList?.remove('is-placeholder');
      state.preview?.remove?.();
      state.rerender();
    };
    if (state.preview && state.sourceRow && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
      const previewRect = state.preview.getBoundingClientRect();
      const targetRect = state.sourceRow.getBoundingClientRect();
      state.preview.animate?.([
        { transform: 'translate3d(0, 0, 0)' },
        { transform: `translate3d(${targetRect.left - previewRect.left}px, ${targetRect.top - previewRect.top}px, 0)` }
      ], { duration: 160, easing: 'cubic-bezier(.2, .8, .2, 1)', fill: 'forwards' });
      setTimeout(finish, 160);
    } else {
      finish();
    }
    orderingToast(state.collection);
    return true;
  }

  function startCollectionDrag(collection, item, items, rerender, row, handle, event) {
    if (!collectionItemMovable(collection, item) || !row) return false;
    cancelCollectionDrag();
    selectCollectionItem(collection, item.id);
    syncCollectionRowSelection(collection, row);
    row.classList.add('is-dragging');
    handle?.setAttribute?.('aria-grabbed', 'true');
    const rect = row.getBoundingClientRect();
    const previewRow = row.cloneNode(true);
    previewRow.removeAttribute('draggable');
    delete previewRow.dataset.collection;
    delete previewRow.dataset.collectionId;
    previewRow.querySelectorAll?.('[id]').forEach((element) => element.removeAttribute('id'));
    const preview = h('div', { class: 'collection-drag-preview', 'aria-hidden': 'true' }, previewRow);
    Object.assign(preview.style, {
      left: `${rect.left}px`, top: `${rect.top}px`, width: `${rect.width}px`, height: `${rect.height}px`
    });
    document.body.append(preview);
    collectionDragState = {
      collection, itemID: item.id, items: asList(items),
      visibleIDs: asList(items).map((candidate) => candidate.id),
      rerender, sourceRow: row, handle, input: event.pointerType || 'mouse', preview,
      pointerId: event.pointerId,
      originalParent: row.parentNode, originalNextSibling: row.nextElementSibling,
      grabOffsetX: event.clientX - rect.left, grabOffsetY: event.clientY - rect.top,
      targetRow: null, targetID: '', after: false
    };
    requestAnimationFrame(() => {
      if (collectionDragState?.sourceRow === row) row.classList.add('is-placeholder');
    });
    return true;
  }

  function collectionDragHandle(collection, item, items, rerender, options = {}) {
    const disabledReason = options.disabledReason || '';
    const movable = !disabledReason && collectionItemMovable(collection, item);
    const label = item.name || item.id;
    return h('button', {
      class: 'collection-drag-handle', type: 'button',
      disabled: !movable,
      title: movable ? '拖动或按 ↑/↓ 调整顺序' : (disabledReason || '此项目顺序固定'),
      'aria-label': movable ? `调整 ${label} 的顺序（拖动或按上下方向键）` : `${label} ${disabledReason || '顺序固定'}`,
      'aria-grabbed': 'false',
      onclick: (event) => { event.preventDefault(); event.stopPropagation(); },
      onkeydown: (event) => {
        if (!movable || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return;
        event.preventDefault();
        const offset = event.key === 'ArrowUp' ? -1 : 1;
        const values = asList(items);
        selectCollectionItem(collection, item.id);
        if (!moveCollectionWithAnimation(collection, () =>
          S.store.moveCollectionItem(collection, item.id, offset, values.map((candidate) => candidate.id)), rerender)) return;
        orderingToast(collection);
        collectionRows(collection).find((row) => row.dataset.collectionId === item.id)
          ?.querySelector?.('.collection-drag-handle')?.focus?.();
      },
      onpointerdown: (event) => {
        if (!movable || event.button > 0) return;
        const row = event.currentTarget.closest?.('[data-collection-id]');
        if (!startCollectionDrag(collection, item, items, rerender, row, event.currentTarget, event)) return;
        event.preventDefault();
        event.stopPropagation();
      }
    }, '⠿');
  }

  function collectionRowAttributes(collection, item, items, rerender, baseClass = '') {
    const selected = selectedCollectionID(collection, items) === item.id;
    return {
      class: [baseClass, 'entity-row', selected ? 'is-selected' : ''].filter(Boolean).join(' '),
      dataset: { collection, collectionId: item.id },
      'aria-selected': String(selected),
      onclick: (event) => {
        if (event.target?.closest?.('button, input, select, textarea, a, summary, details') || collectionDragState) return;
        selectCollectionItem(collection, item.id);
        rerender();
      }
    };
  }

  /* 下拉补全：把悬空引用保留为可修复选项（与 LuCI 版一致的修复语义） */
  function selectWithMissing(options, current, label) {
    const values = options.slice();
    if (current && !values.some(([v]) => v === current)) values.push([current, `${label || '缺失'}：${current}`]);
    return values;
  }

  function creationDraft(collection, overrides = {}) {
    const defaults = JSON.parse(JSON.stringify(S.uiSpec.creation_defaults?.[collection] || {}));
    const prefix = S.uiSpec.id_policy?.collection_prefixes?.[collection] || 'item';
    const used = new Set(Object.values(S.store?.intent || {}).flatMap((value) =>
      Array.isArray(value) ? value.map((item) => item?.id).filter(Boolean) : []));
    let id = S.uid(prefix);
    let collision = 2;
    while (used.has(id)) id = `${prefix}-${collision++}`;
    return { ...defaults, id, ...overrides };
  }

  function referenceOptions(collection, items) {
    const values = asList(items);
    const systemRouteLabel = (item) => (collection === 'routes' ? ({ direct: '直连', block: '拒绝' })[item.kind] : '');
    const baseLabels = values.map((item) => item.name || systemRouteLabel(item) || item.id);
    const counts = new Map();
    const ordinals = new Map();
    baseLabels.forEach((label) => counts.set(label, (counts.get(label) || 0) + 1));
    const endpoint = (host, port) => host && port ? `${host}:${port}` : '';
    return values.map((item, index) => {
      const base = baseLabels[index];
      if ((counts.get(base) || 0) < 2) return [item.id, base];
      const ordinal = (ordinals.get(base) || 0) + 1;
      ordinals.set(base, ordinal);
      let detail = '';
      if (collection === 'nodes') {
        detail = endpoint(item.server, item.server_port) || item.type || '';
        if (item.source_subscription) {
          const source = asList(S.store.intent?.subscriptions).find((subscription) => subscription.id === item.source_subscription);
          detail += `${detail ? ' · ' : ''}订阅 ${source?.name || '未命名订阅'}`;
        }
      } else if (collection === 'routes') {
        const node = asList(S.store.intent?.nodes).find((candidate) => candidate.id === item.node);
        detail = item.kind === 'single' ? `节点 ${node?.name || '未选择'}` : (item.kind || '');
      } else if (collection === 'dns_profiles') {
        detail = endpoint(item.server, item.server_port) || item.protocol || '';
      } else if (collection === 'local_proxies') {
        detail = endpoint(item.listen, item.listen_port) || item.protocol || '';
      }
      return [item.id, `${base}${detail ? ` · ${detail}` : ''} · 同名项 ${ordinal}`];
    });
  }

  Object.assign(S, { ui: { beginRender, beginRoute, isCurrentRoute, renderShell, renderStatusStrip, runState, toast, dialog, conflictDialog, drawer, field, input, textarea, select, multiChoice, toggle, toggleRow, chips, matchEditor, issueList, viewHead, section, fact, facts, emptyState, rowMenu, collectionItemMovable, selectedCollectionID, selectCollectionItem, collectionDragHandle, collectionRowAttributes, selectWithMissing, creationDraft, referenceOptions, classifyLocalProxyListen, applyRecord, applyTime, generationLabel, onValidate, onSave, onDiscard, onToggleEnabled, onRefreshState, jumpToObject, takeObjectFocus, focusDrawerOption, collectionReferences, guardCollectionDeletion } });
})();
