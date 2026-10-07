/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 基础设置：Main、探测、DNS cache 与 Bootstrap 的原生表单。 */
'use strict';
(function () {
  const S = window.S;
  const { h } = S;
  const ui = S.ui;

  function textField(object, key, placeholder) {
    return ui.input({
      value: object[key] ?? '', placeholder: placeholder || '', mono: true,
      oninput: (event) => { object[key] = event.target.value; S.store.touch(); }
    });
  }

  function numberField(object, key, placeholder, zeroAsEmpty = false) {
    const configured = object[key];
    return ui.input({
      type: 'number', value: zeroAsEmpty && Number(configured) === 0 ? '' : (configured ?? ''), placeholder: placeholder || '',
      oninput: (event) => {
        const value = event.target.value;
        if (value === '' || (zeroAsEmpty && Number(value) === 0)) delete object[key]; else object[key] = Number(value);
        S.store.touch();
      }
    });
  }

  const view = {
    name: 'general',
    render(root) {
      ui.beginRender(root);
      const main = S.store.intent.main;
      const bootstrap = S.store.intent.bootstrap;
      const logLevel = ui.select(S.uiSpec.log_levels.map((item) => [item.value, item.label]), main.log_level, (value) => {
        main.log_level = value; S.store.touch();
      });
      const bootstrapProtocol = ui.select(S.uiSpec.bootstrap_protocols.map((item) => [item.value, item.label]), bootstrap.protocol, (value) => {
        bootstrap.protocol = value; S.store.touch();
      });
      const strategy = ui.select(S.uiSpec.bootstrap_strategies.map((item) => [item.value, item.label]), bootstrap.strategy, (value) => {
        bootstrap.strategy = value; S.store.touch();
      });

      const short = (field) => { field.classList.add('field--short'); return field; };
      root.append(
        ui.viewHead('基础设置', '全局运行参数、测试网址、DNS 缓存与引导 DNS'),
        ui.section('运行', { class: 'settings' }, [
          short(ui.field('日志级别', logLevel)),
          S.uiSpec.platform_capabilities.linux.direct_bypass ? ui.field('IPv6 直连方式', ui.select(S.uiSpec.direct_bypass_modes.map((item) => [item.value, item.label]), main.direct_bypass || 'off', (value) => { main.direct_bypass = value; S.store.touch(); }), '提前判定为直连时保留客户端地址；DNS 辅助模式接受共享 IP 的域名歧义，其余连接继续正常分流。') : null,
          h('p', { class: 'card__note' }, '启用或停用 Steer 请使用顶部开关；切换后会立即保存并应用。')
        ]),
        ui.section('测试网址', { class: 'settings', sub: '必须是 HTTPS 地址，且不能包含账号密码或 # 片段。' }, [
          ui.field('直连测试', textField(main, 'probe_direct', 'https://www.example.com/')),
          ui.field('代理测试', textField(main, 'probe_proxy', 'https://www.example.com/')),
          ui.field('下载测速', textField(main, 'speedtest_proxy', 'https://speed.example.com/file'))
        ]),
        ui.section('DNS 缓存', { class: 'settings', sub: '所有 DNS 配置共用的解析缓存。' }, [
          short(ui.field('缓存容量', numberField(main, 'dns_cache_capacity', '4096', true), '留空使用默认值；自定义范围 1,024–10,000,000')),
          ui.field('持久化缓存', ui.toggle(main.dns_cache_persist, (value) => { main.dns_cache_persist = value; S.store.touch(); })),
          ui.field('乐观缓存', ui.toggle(main.dns_optimistic_cache, (value) => { main.dns_optimistic_cache = value; S.store.touch(); }))
        ]),
        ui.section('引导 DNS', { class: 'settings', sub: '用来解析节点和 DNS 服务器域名的解析器，固定直连。' }, [
          short(ui.field('协议', bootstrapProtocol)),
          ui.field('服务器 IP', textField(bootstrap, 'server', '1.1.1.1'), '必须填写 IP 地址，避免解析环路'),
          short(ui.field('端口', numberField(bootstrap, 'server_port', '53'))),
          short(ui.field('地址策略', strategy))
        ])
      );
    }
  };

  S.views = S.views || {};
  S.views.general = view;
})();
