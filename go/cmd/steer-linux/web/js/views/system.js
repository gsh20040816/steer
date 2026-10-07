/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 系统：规则数据、运行时版本、路径与访问说明。 */
'use strict';
(function () {
  const S = window.S;
  const { h } = S;
  const ui = S.ui;

  function runtimeValue(value) {
    if (value && typeof value === 'object') {
      if (value.version) return value.version;
      return value.error ? '不可用' : '—';
    }
    return value == null || value === '' ? '—' : String(value);
  }

  function webAccess(listenValue) {
    const listen = String(listenValue || '');
    const match = listen.match(/^\[[^\]]+\]:(\d+)$/) || listen.match(/^[^:]+:(\d+)$/);
    if (!match) return {
      description: '运行时未返回可确认的 Web 监听地址；无法生成安全的 SSH 转发示例。',
      command: '监听地址不可用'
    };
    const port = match[1];
    return {
      description: '控制台仅允许本机访问。需要从其他设备打开时，可通过 SSH 建立临时转发：',
      command: `ssh -L ${port}:${listen} host\n# 然后访问 http://127.0.0.1:${port}`
    };
  }

  const view = {
    name: 'system',
    async render(root) {
      const isCurrent = ui.beginRender(root);
      const [geosite, geoip] = await Promise.all([S.api.geodata('geosite'), S.api.geodata('geoip')]);
      const runtime = S.store.runtime || {};
      const status = S.store.overview?.status || {};
      const geo = runtime.geodata || {};
      const access = webAccess(runtime.web_listen);
      const lastApply = status.last_apply;
      const lastApplyResult = lastApply?.result;

      const versions = ui.section('版本', {}, ui.facts([
        ['Steer', runtimeValue(runtime.steer)],
        ['sing-box', runtimeValue(runtime.sing_box)],
        ['上次应用', lastApply ? `${ui.applyTime(lastApply)} · ${lastApplyResult?.ok ? '成功' : '失败'}` : '—']
      ], { single: true }));

      const geoCard = ui.section('GeoSite 与 GeoIP', {
        sub: '规则数据随 Steer 提供，并会定期检查更新。',
        aside: h('span', { class: geo.error ? 'badge badge--err' : 'badge badge--ok' }, geo.error ? '不可用' : '可用')
      }, [
        ui.facts([
          ['数据版本', runtimeValue(geo)],
          ['规则总数', geo.rule_count == null ? '—' : String(geo.rule_count)],
          ['GeoSite 规则数', geosite.readable ? String(geosite.count) : '不可用'],
          ['GeoIP 规则数', geoip.readable ? String(geoip.count) : '不可用']
        ], { single: true }),
        geo.error ? h('pre', { class: 'command-block u-mt-10' }, geo.error) : null
      ]);

      const platformCard = ui.section('组件与路径', {}, ui.facts([
        ['服务', 'Steer · Web 控制台 · 订阅更新'],
        ['配置', '/etc/steer/config.json', { mono: true }],
        ['运行目录', '/run/steer', { mono: true }],
        ['状态目录', '/var/lib/steer', { mono: true }],
        ['规则数据', '/usr/share/steer/geodata-seed', { mono: true }]
      ], { single: true }));

      const accessCard = ui.section('从其他设备访问', { sub: access.description },
        h('pre', { class: 'command-block' }, access.command));

      if (!isCurrent()) return;
      root.append(
        ui.viewHead('系统', '版本、规则数据、系统路径与访问方式'),
        h('div', { class: 'grid-2' }, [versions, geoCard]),
        h('div', { class: 'grid-2' }, [platformCard, accessCard])
      );
    }
  };

  S.views = S.views || {};
  S.views.system = view;
})();
