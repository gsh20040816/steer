/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 总览：运行状态、配置状态、对象数量、校验摘要与最近一次应用。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon } = S;
  const ui = S.ui;

  function warningGroupLabel(group) {
    if (group.code === 'INSECURE_TLS' && group.object_type === 'dns_profile') return 'DNS 证书校验已关闭';
    if (group.code === 'INSECURE_TLS') return 'TLS 证书校验已关闭';
    if (group.code === 'SUBSCRIPTION_NODE_STALE') return '订阅已不再提供此节点';
    if (group.code === 'DNS_REJECT_PROJECTION_SKIPPED') return 'DNS 拒绝条件无法在解析前完整执行';
    if (group.code === 'DNS_PROJECTION_EMPTY') return 'DNS 将继续匹配后续规则';
    return group.summary || '配置警告';
  }

  function warningGroupScope(group) {
    return ({ node: '节点', route: '路由', dns_profile: 'DNS 配置', local_proxy: '本地代理', rule: '规则' })[group.object_type] || '对象';
  }

  function warningGroups(validation) {
    const groups = Array.isArray(validation?.warning_groups) ? validation.warning_groups : [];
    if (!groups.length) return null;
    return h('div', { class: 'warning-groups', 'aria-label': '警告摘要' }, groups.map((group) => h('article', { class: 'warning-group' }, [
      h('div', { class: 'warning-group__copy' }, [
        h('strong', {}, warningGroupLabel(group)),
        h('span', {}, `${group.count || 0} 个正在使用的${warningGroupScope(group)}`)
      ]),
      group.destination ? h('button', { class: 'btn btn--sm', onclick: () => S.router?.(group.destination) }, `查看${warningGroupScope(group)}`) : null
    ])));
  }

  /* 一句话说明当前状态和下一步。 */
  function headline({ savedEnabled, active, healthy, pendingApply, lastResult }) {
    if (active && healthy) {
      return {
        tone: pendingApply ? 'warn' : 'ok', icon: 'check', title: '当前运行正常',
        text: pendingApply ? '已保存的配置尚未应用；应用后运行配置才会更新。' : '运行配置与已保存配置一致。'
      };
    }
    if (active) return { tone: 'err', icon: 'error', title: '运行异常', text: '分流服务正在运行但检查未通过；请打开诊断查看原因。' };
    if (savedEnabled) {
      return {
        tone: 'err', icon: 'error', title: '已启用，但没有运行',
        text: lastResult?.ok === false ? '上次应用没有成功，运行配置未切换。' : '已保存配置为启用，但分流服务没有运行。'
      };
    }
    return { tone: 'off', icon: 'pause', title: 'Steer 已停用', text: '流量不经过 Steer。使用顶部开关启用后会立即保存并应用。' };
  }

  function stat(label, value, destination) {
    return h('button', { class: 'stat', type: 'button', onclick: () => S.router?.(destination), title: `打开${label}` }, [
      h('span', { class: 'stat__label' }, label),
      h('span', { class: 'stat__value' }, String(value))
    ]);
  }

  const view = {
    name: 'overview',
    async render(root) {
      const isCurrent = ui.beginRender(root);
      const intent = S.store.intent;
      const ov = S.store.overview || {};
      const status = ov.status || {};
      const lastApply = status.last_apply || null;
      const lastResult = lastApply?.result || lastApply;
      const validation = await S.api.validate(intent);
      const healthy = !!status.healthy;
      const active = !!status.generation;
      const savedEnabled = ov.saved_enabled === true;
      const pendingApply = ov.pending_apply === true;
      const externalChange = S.store.hasExternalChange === true;
      const errors = validation.errors?.length || 0;
      const warnings = validation.warnings?.length || 0;
      const consistent = !pendingApply && savedEnabled === active;

      const externalNotice = externalChange ? h('div', { class: 'alert alert--err', role: 'status' }, [
        h('strong', {}, '服务器配置已变化'),
        h('span', {}, S.store.dirty
          ? '当前工作副本已保留且不会自动覆盖。请先保存、放弃或在顶部处理配置冲突。'
          : '点击顶部“重新载入”即可更新为服务器上的最新配置。')
      ]) : null;

      const state = headline({ savedEnabled, active, healthy, pendingApply, lastResult });
      const flow = h('div', { class: 'flow', title: '规则按顺序首条命中即停' }, [
        h('span', { class: 'flow__step' }, '规则', h('strong', {}, String(intent.rules.length))),
        h('span', { class: 'flow__arrow', 'aria-hidden': 'true' }, '→'),
        h('span', { class: 'flow__step' }, 'DNS 配置', h('strong', {}, String(intent.dns_profiles.length))),
        h('span', { class: 'flow__arrow', 'aria-hidden': 'true' }, '→'),
        h('span', { class: 'flow__step' }, '路由', h('strong', {}, String(intent.routes.length))),
        h('span', { class: 'flow__arrow', 'aria-hidden': 'true' }, '→'),
        h('span', { class: 'flow__step' }, '出口')
      ]);
      const hero = h('section', { class: `card hero is-${state.tone}`, 'data-overview-region': 'execution_model' }, [
        h('span', { class: 'hero__icon', 'aria-hidden': 'true' }, icon(state.icon, 20)),
        h('div', { class: 'hero__body' }, [
          h('h2', { class: 'hero__title' }, state.title),
          h('p', { class: 'hero__text' }, state.text),
          flow
        ]),
        state.tone === 'err' ? h('div', { class: 'hero__actions' },
          h('button', { class: 'btn', onclick: () => S.router?.('diagnostics') }, '查看诊断')) : null
      ]);

      const lifecycle = ui.section('配置状态', {
        attrs: { 'data-overview-region': 'configuration_lifecycle' },
        aside: consistent ? h('span', { class: 'badge badge--ok' }, '状态一致') : h('span', { class: 'badge badge--warn' }, '状态不一致')
      }, ui.facts([
        ['工作副本', S.store.dirty ? '有未保存修改' : '与已保存配置一致'],
        ['已保存配置', savedEnabled ? '启用' : '禁用'],
        ['当前运行', active ? (healthy ? '正常' : '异常') : '已停止'],
        ['等待应用', pendingApply ? '是' : '否']
      ], { single: true }));

      const scale = ui.section('配置规模', {
        sub: '当前工作副本中的对象数量',
        attrs: { 'data-overview-region': 'object_scale' }
      }, h('div', { class: 'stats stats--inset' }, [
        stat('节点', intent.nodes.length, 'nodes'),
        stat('路由', intent.routes.length, 'routes'),
        stat('DNS 配置', intent.dns_profiles.length, 'dns'),
        stat('本地代理', intent.local_proxies.length, 'proxies'),
        stat('规则', intent.rules.length, 'rules'),
        stat('订阅', intent.subscriptions.length, 'subscriptions')
      ]));

      const validationSummary = ui.section('校验', {
        sub: errors || warnings ? `${errors} 个错误 · ${warnings} 个警告` : '当前工作副本校验通过',
        attrs: { 'data-overview-region': 'validation_summary' },
        aside: [
          validation.ok ? h('span', { class: 'badge badge--ok' }, '合法') : h('span', { class: 'badge badge--err' }, `${errors} 错误`),
          errors || warnings ? h('button', { class: 'btn btn--sm', onclick: ui.onValidate }, '查看详情') : null
        ]
      }, warningGroups(validation));

      const lastApplySummary = ui.section('最近一次应用', {
        attrs: { 'data-overview-region': 'last_apply_and_actions' },
        aside: [
          h('button', { class: 'btn btn--sm', onclick: ui.onRefreshState }, '刷新'),
          h('button', { class: 'btn btn--sm', onclick: () => S.router?.('diagnostics') }, '打开诊断'),
          h('button', { class: 'btn btn--sm', onclick: () => S.router?.('system') }, '系统信息')
        ]
      }, ui.applyRecord(status));

      if (!isCurrent()) return;
      root.append(...[
        ui.viewHead('总览', 'Steer 的运行状态与当前配置概况'),
        externalNotice,
        hero,
        h('div', { class: 'grid-2 overview-grid' }, [lifecycle, scale]),
        h('div', { class: 'grid-2 overview-grid' }, [validationSummary, lastApplySummary])
      ].filter(Boolean));
    }
  };

  S.views = S.views || {};
  S.views.overview = view;
})();
