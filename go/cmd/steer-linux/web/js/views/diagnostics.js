/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 诊断：连通性测试、配置校验、应用结果与相关服务日志。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon, fmtLatestProbe } = S;
  const ui = S.ui;

  const overviewBoundary = '使用设备当前网络环境访问已保存的测试地址；即使 Steer 未启用也可以测试。成功仅表示该地址在测试时可达。';

  function probeCard(kind, title, description, probeResults) {
    const resultForKind = () => (probeResults.latest_results || [])
      .find((result) => result.scope === 'overview' && result.kind === kind);
    const out = h('div', { class: 'test-slot' });
    const showLatest = (result = resultForKind()) => {
      const latest = fmtLatestProbe(result);
      out.replaceChildren(h('div', {
        class: `test-result ${latest.stale ? 'is-stale' : (latest.ok === false ? 'is-err' : (latest.ok ? 'is-ok' : ''))}`
      }, h('strong', {}, latest.text)));
    };
    showLatest();
    const run = h('button', { class: 'btn btn--sm', onclick: async () => {
      run.disabled = true;
      out.replaceChildren(h('span', { class: 'muted spinning' }, '测试中…'));
      try {
        const result = await S.api.probe(kind);
        S.store.installProbeResult?.(result);
        showLatest(result);
      } catch (error) {
        try {
          const refreshed = await S.store.refreshProbeResults();
          probeResults.latest_results = refreshed.latest_results;
          showLatest();
        } catch (_) {
          out.replaceChildren(h('div', { class: 'test-result is-err' }, h('strong', {}, '本次请求失败 · 请查看诊断日志')));
        }
      }
      run.disabled = false;
    } }, '运行测试');
    return h('section', { class: 'card test-card' }, [
      h('div', { class: 'test-card__head' }, h('h2', { class: 'card__title' }, title), run),
      h('p', {}, description),
      out
    ]);
  }

  const view = {
    name: 'diagnostics',
    async render(root) {
      const isCurrent = ui.beginRender(root);
      let logs = { output: '' }, diagnostics = { warnings: [] }, probeResults = { latest_results: [], warnings: [] }, validation = { errors: [], warnings: [] };
      [logs, diagnostics, probeResults, validation] = await Promise.all([
        S.api.logs().catch((error) => ({ output: `日志刷新失败：${error.message}` })),
        (typeof S.store.refreshDiagnostics === 'function' ? S.store.refreshDiagnostics() : S.api.diagnostics())
          .catch((error) => ({ warnings: [`诊断状态刷新失败：${error.message}`] })),
        (typeof S.store.refreshProbeResults === 'function' ? S.store.refreshProbeResults() : S.api.probeResults())
          .catch((error) => ({ latest_results: [], warnings: [`最近结果刷新失败：${error.message}`] })),
        S.api.validate(S.store.intent).catch((error) => ({ errors: [{ code: 'VALIDATION_UNAVAILABLE', message: error.message }], warnings: [] }))
      ]);
      const status = S.store.overview?.status || {};
      const dnsCapture = diagnostics.dns_capture || {};
      if (!isCurrent()) return;
      const refresh = h('button', { class: 'btn', onclick: async () => {
        refresh.disabled = true;
        try { await S.store.refreshOverview(); } finally { if (isCurrent()) view.render(root); }
      } }, icon('refresh', 15), '刷新诊断');
      const dnsConfigured = dnsCapture.detail === 'the published Active generation contains the expected port-53 capture artifacts';
      const errors = validation.errors || [];
      const warnings = validation.warnings || [];
      root.append(
        ui.viewHead('诊断', overviewBoundary, [refresh]),
        h('div', { class: 'grid-3 diagnostics-probes' }, [
          probeCard('direct', '直连目标', '在当前网络环境中测试直连地址', probeResults),
          probeCard('proxy', '代理目标', '在当前网络环境中测试代理地址', probeResults),
          probeCard('speedtest', '下载目标', '在当前网络环境中测试下载速度', probeResults)
        ]),
        ...(diagnostics.warnings || []).slice(0, 3).map((warning) => h('p', { class: 'alert' }, warning)),
        ...(probeResults.warnings || []).slice(0, 3).map((warning) => h('p', { class: 'alert' }, warning)),
        h('div', { class: 'grid-2' }, [
          ui.section('系统 DNS 接管检查', {
            sub: '检查当前运行配置是否接管了 53 端口的 DNS 请求。',
            aside: h('span', { class: `badge ${dnsCapture.configured ? 'badge--ok' : 'badge--warn'}` }, dnsCapture.configured ? '已配置' : '未确认')
          }, ui.facts([['结果', dnsConfigured ? '系统 DNS 接管已配置' : (dnsCapture.detail || '尚未检查')]], { single: true })),
          ui.section('配置校验', {
            sub: errors.length || warnings.length ? `${errors.length} 个错误 · ${warnings.length} 个警告` : '当前工作副本',
            aside: errors.length ? h('span', { class: 'badge badge--err' }, '有错误') : h('span', { class: 'badge badge--ok' }, '通过')
          }, errors.length || warnings.length
            ? h('div', {}, [ui.issueList(errors, ui.jumpToObject), ui.issueList(warnings, ui.jumpToObject, true)])
            : h('p', { class: 'muted' }, '当前工作副本校验通过'))
        ]),
        ui.section('最近应用结果', {
          aside: h('span', { class: 'muted' }, `运行状态：${status.healthy ? '正常' : (status.generation ? '异常' : '已停止')}`)
        }, ui.applyRecord(status, true)),
        ui.section(null, {}, h('details', { class: 'fold' }, [
          h('summary', {}, 'Steer 服务日志'),
          h('pre', { class: 'diagnostics-log' }, logs.output || '当前没有日志输出')
        ]))
      );
    }
  };

  S.views = S.views || {};
  S.views.diagnostics = view;
})();
