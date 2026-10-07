/* SPDX-License-Identifier: GPL-3.0-or-later */
/* DNS 配置：六种传输 + TLS + 规则引用计数。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon } = S;
  const ui = S.ui;
  const PROTOCOL_LABEL = Object.fromEntries(S.uiSpec.dns_protocols.map((item) => [item.value, item.label]));
  const DNS_FIELDS = new Set(S.uiSpec.dns_protocols.flatMap((item) => item.fields));

  function protocolSpec(value) {
    return S.uiSpec.dns_protocols.find((item) => item.value === value);
  }

  function cleanupProtocolFields(draft) {
    const spec = protocolSpec(draft.protocol);
    if (!spec) return;
    for (const field of DNS_FIELDS) if (!spec.fields.includes(field)) delete draft[field];
  }

  function applyProtocol(draft, nextValue) {
    const previous = protocolSpec(draft.protocol);
    const next = protocolSpec(nextValue);
    if (!next) return draft;
    const currentPort = Number(draft.server_port);
    if (!Number.isInteger(currentPort) || currentPort < 1 || (previous && currentPort === previous.default_port)) {
      draft.server_port = next.default_port;
    }
    draft.protocol = next.value;
    cleanupProtocolFields(draft);
    return draft;
  }

  function commitProfile(profile, draft) {
    for (const field of DNS_FIELDS) if (!(field in draft)) delete profile[field];
    Object.assign(profile, draft);
    return profile;
  }

  function refCount(intent, profileId) {
    return intent.rules.filter((r) => r.dns_profile === profileId).length;
  }

  function openEditor(profile, focusOption) {
    const isNew = !S.store.intent.dns_profiles.includes(profile);
    const opened = ui.drawer({
      eyebrow: isNew ? '新建 DNS 配置' : '编辑 DNS 配置', title: profile.name || '未命名', submitLabel: '保存到工作副本',
      renderBody(body) {
        const draft = JSON.parse(JSON.stringify(profile));
        const initialSpec = protocolSpec(draft.protocol);
        cleanupProtocolFields(draft);
        const name = ui.input({ value: draft.name || '', placeholder: '名称' });
        const enabled = ui.toggle(draft.enabled, (v) => { draft.enabled = v; });
        const server = ui.input({ value: draft.server || '', placeholder: 'dns.example.com 或 IP', mono: true });
        const port = ui.input({ type: 'number', value: draft.server_port || '', placeholder: '53 / 853 / 443' });
        const protocolFields = h('div', {});
        const protocolOptions = S.uiSpec.dns_protocols.map((item) => [item.value, item.label]);
        if (!initialSpec) protocolOptions.unshift(['', '缺失（需修复）']);
        const protocol = ui.select(protocolOptions, draft.protocol || '', (v) => {
          draft.server_port = Number(port.value);
          applyProtocol(draft, v);
          port.value = draft.server_port;
          renderProtocolFields();
        });

        function renderProtocolFields() {
          const spec = protocolSpec(draft.protocol);
          const fields = [];
          for (const field of spec?.fields || []) {
            switch (field) {
            case 'tls_server_name':
              fields.push(ui.field('TLS 服务器名', ui.input({
                value: draft.tls_server_name || '', placeholder: 'dns.example.com', mono: true,
                oninput: (event) => { draft.tls_server_name = event.target.value; }
              }), '加密 DNS 必填', 'tls_server_name'));
              break;
            case 'path':
              fields.push(ui.field('HTTP 路径', ui.input({
                value: draft.path || '', placeholder: '/dns-query', mono: true,
                oninput: (event) => { draft.path = event.target.value; }
              }), 'DoH / DoH3 使用', 'path'));
              break;
            case 'insecure':
              fields.push(ui.field('跳过证书校验', ui.toggle(draft.insecure, (v) => { draft.insecure = v; }), null, 'insecure'));
              break;
            }
          }
          protocolFields.replaceChildren(...fields);
        }
        renderProtocolFields();

        body.append(
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '上游'), [
            ui.field('名称', name, null, 'name'),
            ui.field('启用', enabled, null, 'enabled'),
            ui.field('协议', protocol, null, 'protocol'),
            h('div', { class: 'field--row field--endpoint' }, [ui.field('服务器', server, null, 'server'), ui.field('端口', port, null, 'server_port')]),
            protocolFields
          ])
        );
        return {
          submit() {
            if (!server.value.trim()) { ui.toast('服务器不能为空', 'err'); return false; }
            draft.name = name.value.trim() || undefined;
            draft.server = server.value.trim();
            draft.server_port = Number(port.value);
            if (!protocolSpec(draft.protocol)) { ui.toast('请选择 DNS 协议', 'err'); return false; }
            if (!Number.isInteger(draft.server_port) || draft.server_port < 1 || draft.server_port > 65535) { ui.toast('端口必须是 1–65535', 'err'); return false; }
            for (const field of ['path', 'tls_server_name']) {
              if (typeof draft[field] === 'string') draft[field] = draft[field].trim() || undefined;
            }
            cleanupProtocolFields(draft);
            const spec = protocolSpec(draft.protocol);
            const missing = spec.required_fields.find((field) => !String(draft[field] || '').trim());
            if (missing) { ui.toast('加密 DNS 需要 TLS 服务器名', 'err'); return false; }
            for (const key of Object.keys(draft)) if (draft[key] == null) delete draft[key];
            return commitProfile(profile, draft);
          }
        };
      },
      onSubmit(profile) {
        if (isNew) S.store.intent.dns_profiles.push(profile);
        S.store.touch();
        ui.toast(`DNS 配置 ${profile.name || profile.id} 已${isNew ? '创建' : '更新'} · 未保存`, 'info');
        view.render(document.querySelector('#view'));
        return true;
      }
    });
    ui.focusDrawerOption(focusOption);
    return opened;
  }

  const view = {
    name: 'dns',
    render(root) {
      ui.beginRender(root);
      const intent = S.store.intent;
      const remove = (p) => {
        if (!ui.guardCollectionDeletion('dns_profiles', p.id, p.name || p.id)) return;
        S.store.intent.dns_profiles = S.store.intent.dns_profiles.filter((x) => x.id !== p.id);
        S.store.touch();
        ui.toast(`已删除 ${p.name || p.id} · 未保存`, 'warn');
        view.render(root);
      };
      const table = h('table', { class: 'table' }, [
        h('thead', {}, h('tr', {}, [
          h('th', { class: 'collection-drag-column', 'aria-label': '顺序' }),
          h('th', {}, '启用'), h('th', {}, '名称'), h('th', {}, '协议'), h('th', {}, '服务器'),
          h('th', { class: 'num' }, '规则引用'), h('th', { class: 'col-actions', 'aria-label': '操作' })
        ])),
        h('tbody', {}, intent.dns_profiles.map((p) => h('tr', ui.collectionRowAttributes(
          'dns_profiles', p, intent.dns_profiles, () => view.render(root), p.enabled === false ? 'is-disabled' : ''
        ), [
          h('td', { class: 'collection-drag-column' }, ui.collectionDragHandle('dns_profiles', p, intent.dns_profiles, () => view.render(root))),
          h('td', {}, ui.toggle(p.enabled, (v) => { p.enabled = v; S.store.touch(); }, `启用 ${p.name || p.id}`)),
          h('td', {}, h('strong', {}, p.name || p.id)),
          h('td', {}, h('span', { class: 'tag' }, PROTOCOL_LABEL[p.protocol] || p.protocol)),
          h('td', { class: 'mono' }, `${p.server}:${p.server_port}${p.path ? p.path : ''}`),
          h('td', { class: 'num' }, String(refCount(intent, p.id))),
          h('td', { class: 'col-actions' }, h('div', { class: 'row-actions' }, [
            h('button', { class: 'btn btn--sm btn--ghost', onclick: () => openEditor(p) }, '编辑'),
            ui.rowMenu([{ label: '删除', danger: true, onclick: () => remove(p) }])
          ]))
        ])))
      ]);
      const addButton = () => h('button', { class: 'btn btn--primary', onclick: () => openEditor(ui.creationDraft('dns_profiles')) }, icon('plus', 15), '添加 DNS 配置');

      root.append(
        ui.viewHead('DNS 配置', '规则按 DNS 配置选择上游解析器；支持 UDP、TCP、TLS、HTTPS (DoH)、QUIC (DoQ) 与 HTTP/3。', [addButton()]),
        intent.dns_profiles.length
          ? h('section', { class: 'card table-card' }, h('div', { class: 'table-wrap' }, table))
          : ui.emptyState('还没有 DNS 配置', [addButton()], 'globe')
      );
      const focus = ui.takeObjectFocus('dns_profile');
      const focused = focus && intent.dns_profiles.find((profile) => profile.id === focus.object_id);
      if (focused) openEditor(focused, focus.option);
    }
  };

  view.applyProtocol = applyProtocol;
  view.commitProfile = commitProfile;

  S.views = S.views || {};
  S.views.dns = view;
})();
