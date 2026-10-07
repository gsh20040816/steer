/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 本地代理：规则 inbound 的入口端点。 */
'use strict';
(function () {
  const S = window.S;
  const { h, icon, asList } = S;
  const ui = S.ui;
  const PROTOCOL_LABEL = Object.fromEntries(S.uiSpec.local_proxy_protocols.map((item) => [item.value, item.label]));

  function refCount(intent, proxyId) {
    return intent.rules.filter((r) => asList(r.inbound).includes(proxyId)).length;
  }

  function openEditor(proxy, focusOption) {
    const isNew = !S.store.intent.local_proxies.includes(proxy);
    const opened = ui.drawer({
      eyebrow: isNew ? '新建本地代理' : '编辑本地代理', title: proxy.name || '未命名', submitLabel: '保存到工作副本',
      renderBody(body) {
        const draft = JSON.parse(JSON.stringify(proxy));
        const hasSavedPassword = typeof draft.password === 'string' && draft.password !== '';
        let authAction = hasSavedPassword ? 'keep' : 'remove';
        const name = ui.input({ value: draft.name || '', placeholder: '名称' });
        const enabled = ui.toggle(draft.enabled, (v) => { draft.enabled = v; });
        const protocolOptions = S.uiSpec.local_proxy_protocols.map((item) => [item.value, item.label]);
        if (!draft.protocol) protocolOptions.unshift(['', '缺失（需修复）']);
        const protocol = ui.select(protocolOptions, draft.protocol || '', (v) => { draft.protocol = v; });
        const listen = ui.input({ value: draft.listen || '', placeholder: '127.0.0.1', mono: true, oninput: updateExposure });
        const port = ui.input({ type: 'number', value: draft.listen_port || '', placeholder: '1080' });
        const username = ui.input({ value: draft.username || '', placeholder: '（可选）' });
        const password = ui.input({ type: 'password', value: '', placeholder: '输入新密码' });
        const authStatus = h('div', { class: 'field__hint' });
        const auth = ui.select(hasSavedPassword ? [
          ['keep', '保持已保存密码'], ['replace', '替换密码'], ['remove', '移除认证']
        ] : [
          ['remove', '不设置认证'], ['replace', '设置用户名和密码']
        ], authAction, (value) => { authAction = value; updateAuthControls(); });
        const exposure = h('div', { class: 'alert local-proxy-exposure', role: 'alert' }, [
          h('strong', {}, '该监听地址会扩大访问范围'),
          h('div', {}, '该地址可能允许局域网或公网客户端连接；必须同时设置用户名和密码。')
        ]);

        function updateAuthControls() {
          const editingUsername = authAction === 'keep' || authAction === 'replace';
          username.disabled = !editingUsername;
          password.disabled = authAction !== 'replace';
          authStatus.replaceChildren(authAction === 'keep'
            ? '密码不会回显；保存时保留已保存值。'
            : (authAction === 'replace' ? '用户名与新密码必须同时填写。' : '保存时会同时清空用户名和密码。'));
        }

        function updateExposure() {
          exposure.hidden = ui.classifyLocalProxyListen(listen.value.trim()) !== 'non_loopback';
        }

        updateAuthControls();
        updateExposure();

        body.append(
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '端点'), [
            ui.field('名称', name, null, 'name'),
            ui.field('启用', enabled, null, 'enabled'),
            h('div', { class: 'field--row' }, [ui.field('协议', protocol, null, 'protocol'), ui.field('监听端口', port, null, 'listen_port')]),
            ui.field('监听地址', listen, '通常保持环回地址', 'listen'),
            exposure,
            ui.field('认证操作', auth),
            h('div', { class: 'field--row' }, [
              ui.field('用户名', username, null, 'username'),
              ui.field('新密码', password, null, 'password')
            ]),
            authStatus
          ])
        );
        return {
          submit() {
            if (!listen.value.trim()) { ui.toast('监听地址不能为空', 'err'); return false; }
            if (!S.uiSpec.local_proxy_protocols.some((item) => item.value === draft.protocol)) { ui.toast('请选择代理协议', 'err'); return false; }
            const listenAddress = listen.value.trim();
            const listenClass = ui.classifyLocalProxyListen(listenAddress);
            if (listenClass === 'invalid') { ui.toast('监听地址必须是有效的 IP 地址，不能填写域名', 'err'); return false; }
            const p = Number(port.value);
            if (!port.value || p < 1 || p > 65535) { ui.toast('端口必须是 1–65535', 'err'); return false; }

            let nextUsername = '';
            let nextPassword = '';
            if (authAction === 'keep') {
              nextUsername = username.value.trim();
              nextPassword = hasSavedPassword ? draft.password : '';
            } else if (authAction === 'replace') {
              nextUsername = username.value.trim();
              nextPassword = password.value;
            }
            if ((nextUsername === '') !== (nextPassword === '')) {
              ui.toast('用户名和密码必须同时填写；替换密码时不能留空', 'err');
              return false;
            }
            if (listenClass === 'non_loopback' && nextUsername === '') {
              ui.toast('该监听地址可能允许其他设备连接，必须设置用户名和密码', 'err');
              return false;
            }

            draft.name = name.value.trim() || undefined;
            draft.listen = listenAddress;
            draft.listen_port = p;
            if (nextUsername) {
              draft.username = nextUsername;
              draft.password = nextPassword;
            } else {
              delete draft.username;
              delete draft.password;
            }
            delete proxy.username;
            delete proxy.password;
            Object.assign(proxy, draft);
            return proxy;
          }
        };
      },
      onSubmit(proxy) {
        if (isNew) S.store.intent.local_proxies.push(proxy);
        S.store.touch();
        ui.toast(`本地代理 ${proxy.name || proxy.id} 已${isNew ? '创建' : '更新'} · 未保存`, 'info');
        view.render(document.querySelector('#view'));
        return true;
      }
    });
    ui.focusDrawerOption(focusOption);
    return opened;
  }

  const view = {
    name: 'proxies',
    render(root) {
      ui.beginRender(root);
      const intent = S.store.intent;
      const remove = (p) => {
        if (!ui.guardCollectionDeletion('local_proxies', p.id, p.name || p.id)) return;
        S.store.intent.local_proxies = S.store.intent.local_proxies.filter((x) => x.id !== p.id);
        S.store.touch();
        ui.toast(`已删除 ${p.name || p.id} · 未保存`, 'warn');
        view.render(root);
      };
      const table = h('table', { class: 'table' }, [
        h('thead', {}, h('tr', {}, [
          h('th', { class: 'collection-drag-column', 'aria-label': '顺序' }),
          h('th', {}, '启用'), h('th', {}, '名称'), h('th', {}, '协议'), h('th', {}, '监听'),
          h('th', {}, '认证'), h('th', { class: 'num' }, '规则引用'), h('th', { class: 'col-actions', 'aria-label': '操作' })
        ])),
        h('tbody', {}, intent.local_proxies.map((p) => h('tr', ui.collectionRowAttributes(
          'local_proxies', p, intent.local_proxies, () => view.render(root), p.enabled === false ? 'is-disabled' : ''
        ), [
          h('td', { class: 'collection-drag-column' }, ui.collectionDragHandle('local_proxies', p, intent.local_proxies, () => view.render(root))),
          h('td', {}, ui.toggle(p.enabled, (v) => { p.enabled = v; S.store.touch(); }, `启用 ${p.name || p.id}`)),
          h('td', {}, h('strong', {}, p.name || p.id)),
          h('td', {}, h('span', { class: 'tag' }, PROTOCOL_LABEL[p.protocol] || p.protocol)),
          h('td', { class: 'mono' }, `${p.listen}:${p.listen_port}`),
          h('td', {}, p.username ? '用户名和密码' : h('span', { class: 'faint' }, '无')),
          h('td', { class: 'num' }, String(refCount(intent, p.id))),
          h('td', { class: 'col-actions' }, h('div', { class: 'row-actions' }, [
            h('button', { class: 'btn btn--sm btn--ghost', onclick: () => openEditor(p) }, '编辑'),
            ui.rowMenu([{ label: '删除', danger: true, onclick: () => remove(p) }])
          ]))
        ])))
      ]);
      const addButton = () => h('button', { class: 'btn btn--primary', onclick: () => openEditor(ui.creationDraft('local_proxies')) }, icon('plus', 15), '添加本地代理');

      root.append(
        ui.viewHead('本地代理', '在本机开放 SOCKS5、HTTP 或 Mixed 代理入口；规则可以按入口分流。', [addButton()]),
        intent.local_proxies.length
          ? h('section', { class: 'card table-card' }, h('div', { class: 'table-wrap' }, table))
          : ui.emptyState('还没有本地代理入口', [addButton()], 'plug')
      );
      const focus = ui.takeObjectFocus('local_proxy');
      const focused = focus && intent.local_proxies.find((proxy) => proxy.id === focus.object_id);
      if (focused) openEditor(focused, focus.option);
    }
  };

  S.views = S.views || {};
  S.views.proxies = view;
})();
