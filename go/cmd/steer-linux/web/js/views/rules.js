/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 规则：有序 first-match 列表（拖拽排序）+ 系统直连固定卡 + Default 固定卡 + Match 编辑器。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon, asList } = S;
  const ui = S.ui;

  let catalogPromise = null;
  function catalogs() {
    if (!catalogPromise) {
      catalogPromise = Promise.all([S.api.geodata('geosite'), S.api.geodata('geoip')])
        .then(([geosite, geoip]) => ({ geosite, geoip }));
    }
    return catalogPromise;
  }

  function routeLabel(intent, id) {
    const r = intent.routes.find((x) => x.id === id);
    if (!r) return id;
    return r.name || ({ direct: '直连', block: '拒绝' })[r.kind] || id;
  }
  function dnsLabel(intent, id) {
    const p = intent.dns_profiles.find((x) => x.id === id);
    return p ? (p.name || id) : id;
  }

  function summaryTokens(rule) {
    if (rule.default) return ['default'];
    const tokens = [];
    for (const field of S.uiSpec.rule_match_fields) {
      const values = asList(rule[field]);
      if (!values.length) continue;
      tokens.push((field === 'network' || field === 'protocol')
        ? `${field}:${values.join('/')}` : `${field}:${values.length}`);
    }
    return tokens;
  }

  function dnsContinues(rule) {
    const populated = S.uiSpec.rule_match_fields.filter((field) => asList(rule[field]).length > 0);
    return populated.length > 0 && populated.every((field) => S.uiSpec.rule_connection_only_fields.includes(field));
  }

  function summary(rule) {
    const labels = {
      inbound: '本地代理', domain_match: '域名', ip_match: 'IP', source_ip_cidr: '源 CIDR',
      source_mac_address: '源 MAC', network: '网络', protocol: '协议', port: '端口'
    };
    const parts = summaryTokens(rule).map((token) => {
      if (token === 'default') return '兜底';
      const separator = token.indexOf(':');
      const field = token.slice(0, separator);
      const value = token.slice(separator + 1);
      if (field === 'network') return value.toUpperCase();
      if (field === 'protocol') return `协议 ${value.replace(/\//g, ',')}`;
      return `${labels[field] || field} ×${value}`;
    });
    if (!parts.length) parts.push('无匹配条件');
    if (dnsContinues(rule)) parts.push('DNS 继续匹配后续规则');
    return parts.join(' · ');
  }

  async function openRuleEditor(rule, focusOption) {
    const isNew = !S.store.intent.rules.includes(rule);
    const geo = await catalogs();
    const opened = ui.drawer({
      eyebrow: isNew ? '新建规则' : '编辑规则', title: rule.name || '未命名', submitLabel: '保存到工作副本', width: 560,
      renderBody(body) {
        const intent = S.store.intent;
        const draft = JSON.parse(JSON.stringify(rule));
        const name = ui.input({ value: draft.name || '', placeholder: '规则名称' });
        const enabled = ui.toggle(draft.enabled, (v) => { draft.enabled = v; });
        const dnsOpts = ui.referenceOptions('dns_profiles', intent.dns_profiles);
        const dnsSel = ui.select(ui.selectWithMissing(dnsOpts, draft.dns_profile, '缺失 DNS 配置'), draft.dns_profile, (v) => { draft.dns_profile = v; });
        const routeOpts = ui.referenceOptions('routes', intent.routes);
        const routeSel = ui.select(ui.selectWithMissing(routeOpts, draft.route, '缺失路由'), draft.route, (v) => { draft.route = v; });

        const inboundChoices = ui.referenceOptions('local_proxies', intent.local_proxies);
        const inboundControl = ui.multiChoice(inboundChoices, asList(draft.inbound), {
          onchange: (v) => { draft.inbound = v; }, missingLabel: '缺失本地代理'
        });
        const domain = ui.matchEditor({
          value: draft.domain_match, kind: 'domain', catalog: geo.geosite?.names || [],
          placeholder: 'domain:example.com\nfull:www.example.com\nkeyword\ngeosite:geolocation-!cn\nregexp:^api\\d+\\.example\\.com$'
        });
        const ip = ui.matchEditor({
          value: draft.ip_match, kind: 'ip', catalog: geo.geoip?.names || [],
          placeholder: '1.1.1.1\n10.0.0.0/8\ngeoip:cn'
        });
        const srcCidr = ui.chips(asList(draft.source_ip_cidr), { placeholder: '192.168.50.0/24', onchange: (v) => { draft.source_ip_cidr = v; } });
        const srcMac = ui.chips(asList(draft.source_mac_address), { placeholder: '02:00:00:00:00:10', onchange: (v) => { draft.source_mac_address = v; } });
        const nets = ui.multiChoice(S.uiSpec.rule_networks.map((item) => [item.value, item.label]), asList(draft.network), { onchange: (v) => { draft.network = v; } });
        const protos = ui.multiChoice(S.uiSpec.rule_protocols.map((item) => [item.value, item.label]), asList(draft.protocol), { onchange: (v) => { draft.protocol = v; } });
        const ports = ui.chips(asList(draft.port).map(String), { placeholder: '443 / 27015', onchange: (v) => { draft.port = v.map((x) => (/^\d+$/.test(x) ? Number(x) : x)); } });

        body.append(
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '规则效果'), [
            ui.field('名称', name, null, 'name'),
            ui.field('启用', enabled, null, 'enabled'),
            h('div', { class: 'field--row' }, [ui.field('DNS 配置', dnsSel, null, 'dns_profile'), ui.field('路由', routeSel, null, 'route')])
          ]),
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '匹配条件'), h('p', { class: 'field__hint rule-match-hint' }, '同一字段内满足任一项即可，多个字段需要同时满足。'), [
            ui.field('本地代理入口', inboundControl, '可将规则限定到一个或多个本地代理入口', 'inbound'),
            ui.field('域名匹配', domain.el, `每行一条 · 支持域名与 GeoSite 规则${geo.geosite?.readable ? ` (${geo.geosite.count} 个 GeoSite 可补全)` : ''}`, 'domain_match'),
            ui.field('目标 IP 匹配', ip.el, `每行一条 · 支持 IP/CIDR 与 GeoIP 规则${geo.geoip?.readable ? ` (${geo.geoip.count} 个 GeoIP 可补全)` : ''} · 仅连接阶段`, 'ip_match'),
            ui.field('源 IP/CIDR', srcCidr, '按客户端 IP 地址或网段匹配', 'source_ip_cidr'),
            ui.field('源 MAC', srcMac, '按客户端 MAC 地址匹配；不能与本地代理入口同时使用', 'source_mac_address'),
            h('div', { class: 'field--row' }, [
              ui.field('网络', nets, '仅连接阶段', 'network'),
              ui.field('检测协议', protos, '仅连接阶段', 'protocol')
            ]),
            ui.field('目标端口', ports, '精确端口 · 仅连接阶段', 'port'),
            h('div', { class: 'alert alert--info' }, '只包含目标 IP、网络、协议或端口的规则不影响 DNS 配置的选择。')
          ])
        );
        return {
          submit() {
            [srcCidr, srcMac, ports].forEach((control) => control.commitPending());
            draft.name = name.value.trim() || undefined;
            draft.dns_profile = dnsSel.value;
            draft.route = routeSel.value;
            draft.domain_match = domain.value;
            draft.ip_match = ip.value;
            for (const key of Object.keys(draft)) if (draft[key] == null || (Array.isArray(draft[key]) && draft[key].length === 0)) delete draft[key];
            Object.assign(rule, draft);
            return rule;
          }
        };
      },
      onSubmit(rule) {
        if (isNew) {
          const rules = S.store.intent.rules;
          const defaultIndex = rules.findIndex((candidate) => candidate.default);
          rules.splice(defaultIndex < 0 ? rules.length : defaultIndex, 0, rule);
        }
        S.store.touch();
        ui.toast(`规则 ${rule.name || rule.id} 已${isNew ? '创建' : '更新'} · 未保存`, 'info');
        view.render(document.querySelector('#view'));
        return true;
      }
    });
    ui.focusDrawerOption(focusOption);
    return opened;
  }

  function intentTags(intent, rule) {
    return h('div', { class: 'rule-row__intent' }, [
      h('span', { class: 'faint' }, 'DNS'),
      h('span', { class: 'tag', title: 'DNS 配置' }, dnsLabel(intent, rule.dns_profile)),
      h('span', { class: 'faint' }, '路由'),
      h('span', { class: 'tag', title: '路由' }, routeLabel(intent, rule.route))
    ]);
  }

  function renderRow(rule, index, ordered, root) {
    const intent = S.store.intent;
    const rowAttributes = ui.collectionRowAttributes(
      'rules', rule, ordered, () => view.render(root), `rule-row ${rule.enabled === false ? 'is-disabled' : ''}`
    );
    rowAttributes.dataset = { ...rowAttributes.dataset, ruleId: rule.id };
    const remove = () => {
      if (!ui.guardCollectionDeletion('rules', rule.id, rule.name || rule.id)) return;
      S.store.intent.rules = S.store.intent.rules.filter((r) => r.id !== rule.id);
      S.store.touch();
      ui.toast(`已删除规则 ${rule.name || rule.id} · 未保存`, 'warn');
      view.render(document.querySelector('#view'));
    };
    return h('div', rowAttributes, [
      ui.collectionDragHandle('rules', rule, ordered, () => view.render(root)),
      h('span', { class: 'rule-row__order' }, String(index + 1)),
      h('div', { class: 'rule-row__name' }, [
        h('strong', {}, rule.name || rule.id),
        h('span', { class: 'rule-row__summary' }, `${rule.enabled === false ? '已停用 · ' : ''}${summary(rule)}`)
      ]),
      intentTags(intent, rule),
      h('div', { class: 'rule-row__actions' }, [
        h('button', { class: 'btn btn--sm btn--ghost', onclick: () => openRuleEditor(rule) }, '编辑'),
        ui.rowMenu([{ label: '删除', danger: true, onclick: remove }])
      ])
    ]);
  }

  function renderDefault(defaultRule, position) {
    const intent = S.store.intent;
    return h('div', { class: 'rule-default', dataset: { ruleId: defaultRule.id } }, [
      h('span', { class: 'rule-default__lock', title: '默认规则固定在最后，始终启用' }, icon('lock', 14)),
      h('span', { class: 'rule-row__order' }, String(position)),
      h('div', { class: 'rule-row__name' }, [
        h('strong', {}, '默认规则'),
        h('span', { class: 'rule-row__summary' }, '其余所有流量 · 只能修改 DNS 配置和路由')
      ]),
      h('div', { class: 'default-fields' }, [
        ui.field('DNS 配置', ui.select(ui.referenceOptions('dns_profiles', intent.dns_profiles), defaultRule.dns_profile, (v) => { defaultRule.dns_profile = v; S.store.touch(); }), null, 'dns_profile'),
        ui.field('路由', ui.select(ui.referenceOptions('routes', intent.routes), defaultRule.route, (v) => { defaultRule.route = v; S.store.touch(); }), null, 'route')
      ])
    ]);
  }

  const view = {
    name: 'rules',
    render(root) {
      ui.beginRender(root);
      const intent = S.store.intent;
      const ordered = intent.rules.filter((r) => !r.default);
      const defaultRule = intent.rules.find((r) => r.default);

      const list = h('div', { class: 'rule-list', role: 'list', 'aria-label': '规则（按匹配顺序）' }, ordered.map((rule, i) => renderRow(rule, i, ordered, root)));
      if (!ordered.length) list.append(h('div', { class: 'rule-empty' }, '还没有普通规则，所有流量都由默认规则处理。'));
      if (defaultRule) list.append(renderDefault(defaultRule, ordered.length + 1));

      root.append(
        ui.viewHead('规则', '从上到下依次匹配，命中第一条即停止；拖动左侧手柄或按 ↑/↓ 调整顺序。', [
          h('button', { class: 'btn btn--primary', onclick: () => {
            const direct = intent.routes.find((route) => route.kind === 'direct' && route.enabled !== false)
              || intent.routes.find((route) => route.enabled !== false);
            const rule = ui.creationDraft('rules', {
              dns_profile: intent.dns_profiles.find((profile) => profile.enabled !== false)?.id || '',
              route: direct?.id || ''
            });
            openRuleEditor(rule);
          } }, icon('plus', 15), '添加规则')
        ]),
        list
      );

      const focus = ui.takeObjectFocus('rule');
      const focusedRule = focus && intent.rules.find((rule) => rule.id === focus.object_id);
      if (focusedRule?.default) ui.focusDrawerOption(focus.option);
      else if (focusedRule) void openRuleEditor(focusedRule, focus.option);
    }
  };

  view.summaryTokens = summaryTokens;
  view.dnsContinues = dnsContinues;

  S.views = S.views || {};
  S.views.rules = view;
})();
