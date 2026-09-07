/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 节点：订阅分组 + 行内/批量测速 + 协议抽屉编辑 + 分享链接导入/导出。 */
'use strict';
(function () {
  const S = window.S;
  const { h, fmtLatestProbe, asList } = S;
  const ui = S.ui;

  const MANUAL = '_manual';
  let activeGroup = MANUAL;
  let displaySortMode = 'default';
  let displaySortDirection = S.uiSpec.node_display_sorting?.default_direction || 'best_first';
  const rowButtons = new Map(); /* nodeId -> {conn, down} */

  function probeMetric(result, mode) {
    if (!result || result.scope !== 'nodes' || result.kind !== mode || result.ok !== true || result.stale === true)
      return null;
    const value = result.metric_value;
    return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
  }

  function sortNodesForDisplay(nodes, mode, direction, probeResults) {
    const values = asList(nodes);
    if (mode === 'default') return values.slice();
    const latest = asList(probeResults?.latest_results);
    const decorated = values.map((node, index) => {
      const result = latest.find((candidate) =>
        candidate.scope === 'nodes' && candidate.object_id === node.id && candidate.kind === mode);
      const metric = probeMetric(result, mode);
      return { node, index, metric, ranked: metric != null };
    });
    const contract = S.uiSpec.node_display_sorting || {};
    const goodDirection = mode === 'connect' ? contract.connect_direction : contract.download_direction;
    const metricDirection = direction === 'worst_first'
      ? (goodDirection === 'ascending' ? 'descending' : 'ascending')
      : goodDirection;
    decorated.sort((left, right) => {
      if (left.ranked !== right.ranked) return left.ranked ? -1 : 1;
      if (!left.ranked) return left.index - right.index;
      const compared = metricDirection === 'ascending'
        ? left.metric - right.metric
        : right.metric - left.metric;
      return compared || left.index - right.index;
    });
    return decorated.map((entry) => entry.node);
  }

  function sortHeader(label, mode, root) {
    const active = displaySortMode === mode;
    const metricMode = mode !== 'default';
    const direction = displaySortDirection === 'best_first' ? '↑' : '↓';
    return h('button', {
      class: `table-sort ${active ? 'is-active' : ''}`,
      type: 'button', title: label, 'aria-pressed': String(active),
      'aria-label': active ? `${label} ${metricMode ? direction : '↑'}` : label,
      onclick: () => {
        if (active && displaySortDirection === 'worst_first') {
          displaySortMode = 'default';
          displaySortDirection = S.uiSpec.node_display_sorting?.default_direction || 'best_first';
        } else if (active) {
          displaySortDirection = 'worst_first';
        } else {
          displaySortMode = mode;
          displaySortDirection = S.uiSpec.node_display_sorting?.default_direction || 'best_first';
        }
        view.render(root);
      }
    }, [
      h('span', {}, label),
      active ? h('span', { class: 'table-sort__direction', 'aria-hidden': 'true' }, metricMode ? direction : '↑') : null
    ]);
  }

  function syncTestButtons() {
    rowButtons.forEach((pair) => [pair.conn, pair.down].forEach((button) => {
      if (button.classList.contains('spinning')) return;
      button.disabled = !button._eligible || S.store.dirty;
      button.title = !button._eligible ? '已停用节点不能测试' : (S.store.dirty ? '请先保存或放弃工作副本修改' : button._label);
      syncLatest(button);
    }));
  }

  function latestResult(nodeId, download) {
    const kind = download ? 'download' : 'connect';
    return (S.store.probeResults?.latest_results || []).find((result) =>
      result.scope === 'nodes' && result.object_id === nodeId && result.kind === kind);
  }

  function syncLatest(button, result = latestResult(button._nodeId, button._download)) {
    if (!button._latest) return;
    const latest = fmtLatestProbe(result);
    button._latest.className = `probe-latest ${latest.stale ? 'is-stale' : (latest.ok === false ? 'is-err' : '')}`;
    button._latest.replaceChildren(latest.text);
  }

  function probeAction(button) {
    return h('div', { class: 'probe-action' }, button, button._latest);
  }

  const PROTOCOL_LABEL = Object.fromEntries(S.uiSpec.node_types.map((item) => [item.value, item.label]));
  const FIELD_LABEL = {
    uuid: 'UUID', username: '用户名', password: '密码', method: '加密方法', plugin: '插件', plugin_options: '插件参数',
    security: '加密', alter_id: 'Alter ID', network: 'Network', packet_encoding: 'UDP 包编码', flow: 'Flow',
    transport: '传输', transport_path: '传输路径', transport_host: '传输主机', service_name: 'gRPC 服务名',
    server_ports: '端口跳跃区间', hop_interval: '跳跃间隔', obfs_type: '混淆', obfs_password: '混淆密码',
    up_mbps: '上行 Mbps', down_mbps: '下行 Mbps', version: '版本', congestion_control: '拥塞控制',
    udp_relay_mode: 'UDP 中继', udp_over_stream: 'UDP over stream', zero_rtt_handshake: '0-RTT 握手', heartbeat: '心跳',
    quic: 'QUIC', quic_congestion_control: 'QUIC 拥塞控制', insecure_concurrency: '不安全并发', private_key: '私钥',
    host_key: 'Host key', host_key_algorithms: 'Host key algorithms', executable_path: '可执行文件', extra_args: '额外参数',
    data_directory: '数据目录', tls_server_name: 'TLS 服务器名', alpn: 'ALPN', utls_fingerprint: 'uTLS 指纹',
    insecure: '跳过证书校验', reality_public_key: 'REALITY 公钥', reality_short_id: 'REALITY Short ID'
  };
  const NODE_OPTION_KEYS = new Set(S.uiSpec.node_fields.map((field) => field.key));
  function fieldsFor(type) { return S.uiSpec.node_fields.filter((field) => field.types.includes(type)); }
  function requiresRemoteEndpoint(type) {
    const keys = new Set(fieldsFor(type).map((field) => field.key));
    return keys.has('server') && keys.has('server_port');
  }

  function formatEndpoint(hostValue, portValue) {
    const host = String(hostValue || '').trim();
    const port = Number(portValue);
    if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return '端点不可用';
    const displayHost = host.includes(':') && !(host.startsWith('[') && host.endsWith(']')) ? `[${host}]` : host;
    return `${displayHost}:${port}`;
  }

  function nodeEndpoint(node) {
    if (!requiresRemoteEndpoint(node.type)) {
      if (node.type === 'tor') return node.executable_path ? `本地 Tor · ${node.executable_path}` : '本地 Tor';
      return '本地执行 · 无远端端点';
    }
    return formatEndpoint(node.server, node.server_port);
  }

  function groupOf(node) { return node.source_subscription || MANUAL; }
  function groups(intent) {
    const subs = new Map(intent.subscriptions.map((s) => [s.id, s]));
    const list = [{ id: MANUAL, label: '手动节点', count: 0 }];
    const index = { [MANUAL]: list[0] };
    intent.nodes.forEach((node) => {
      const id = groupOf(node);
      if (!index[id]) {
        const sub = subs.get(id);
        index[id] = { id, label: sub ? (sub.name || id) : `缺失订阅：${id}`, count: 0 };
        list.push(index[id]);
      }
      index[id].count++;
    });
    return list;
  }

  function testButton(label, download, nodeId, eligible) {
    const btn = h('button', { class: 'btn btn--sm', onclick: async () => {
      if (!eligible) {
        ui.toast('已停用节点不能测试；请先启用并保存', 'warn');
        return;
      }
      if (S.store.dirty) {
        ui.toast('请先保存或放弃工作副本修改，再测试节点', 'warn');
        return;
      }
      btn.disabled = true;
      btn.classList.add('spinning');
      btn.textContent = '测试中…';
      try {
        const result = await S.api.speedtestNode(nodeId, download);
        S.store.installProbeResult?.(result);
        btn.classList.remove('spinning');
        btn.textContent = result.ok ? (result.summary || '成功') : '失败';
        btn.title = result.ok ? '成功' : (result.error_summary || '详细原因请查看诊断日志');
        btn.classList.toggle('is-ok', result.ok);
        btn.classList.toggle('is-err', !result.ok);
        syncLatest(btn, result);
      } catch (e) {
        btn.classList.remove('spinning');
        btn.textContent = '失败';
        btn.title = '详细原因请查看诊断日志';
        btn.classList.add('is-err');
        try { await S.store.refreshProbeResults(); syncLatest(btn); } catch (_) { /* keep explicit request failure */ }
      }
      syncTestButtons();
      if (displaySortMode !== 'default') view.render(document.querySelector('#view'));
    }, disabled: !eligible || S.store.dirty, title: !eligible ? '已停用节点不能测试' : (S.store.dirty ? '请先保存或放弃工作副本修改' : label) }, label);
    btn._eligible = eligible;
    btn._label = label;
    btn._nodeId = nodeId;
    btn._download = download;
    btn._latest = h('small', { class: 'probe-latest' });
    syncLatest(btn);
    return btn;
  }

  function renderBatch(nodes) {
    if (!nodes.length) return null;
    const bar = h('div', { class: 'batch-bar' });
    const make = (label, download) => {
      const btn = h('button', { class: 'btn', onclick: () => runBatch(nodes, download, btn, label) }, label);
      bar.append(btn);
      return btn;
    };
    make('批量连接测试', false);
    make('批量下载测试', true);
    return bar;
  }

  async function runBatch(ids, download, button, title) {
    if (S.store.dirty) {
      ui.toast('请先保存或放弃工作副本修改，再批量测试节点', 'warn');
      return;
    }
    const rows = ids.map((id) => rowButtons.get(id)).filter(Boolean);
    rows.forEach((pair) => { pair.conn.disabled = true; pair.down.disabled = true; });
    let cursor = 0, succeeded = 0;
    const worker = async () => {
      while (cursor < ids.length) {
        const id = ids[cursor++];
        const pair = rowButtons.get(id);
        const btn = download ? pair?.down : pair?.conn;
        if (btn) {
          btn.classList.add('spinning');
          btn.textContent = '…';
          try {
            const result = await S.api.speedtestNode(id, download);
            S.store.installProbeResult?.(result);
            btn.textContent = result.ok ? (result.summary || '成功') : '失败';
            btn.title = result.ok ? '成功' : (result.error_summary || '详细原因请查看诊断日志');
            btn.classList.toggle('is-ok', result.ok);
            btn.classList.toggle('is-err', !result.ok);
            if (result.ok) succeeded++;
            syncLatest(btn, result);
          } catch (e) {
            btn.textContent = '失败'; btn.title = '详细原因请查看诊断日志'; btn.classList.add('is-err');
            try { await S.store.refreshProbeResults(); syncLatest(btn); } catch (_) { /* keep explicit request failure */ }
          }
          btn.classList.remove('spinning');
        }
        button.textContent = `${title} · ${cursor}/${ids.length}`;
      }
    };
    await Promise.all(Array.from({ length: Math.min(4, ids.length) }, worker));
    button.textContent = `${title} · 成功 ${succeeded}/${ids.length}`;
    rows.forEach((pair) => { pair.conn.disabled = false; pair.down.disabled = false; });
    if (displaySortMode !== 'default') view.render(document.querySelector('#view'));
  }

  /* ---------- 分享链接导入 ---------- */
  function openImport() {
    const textarea = h('textarea', { class: 'textarea', rows: 4, placeholder: 'ss://…  vmess://…  vless://…  trojan://…  hysteria2://…  tuic://…' });
    const preview = h('div', { class: 'import-preview' });

    async function review() {
      try {
        const parsed = await S.api.importNodes(textarea.value);
        const nodes = parsed.nodes || [];
        if (!nodes.length) throw new Error('没有可导入的有效节点');
        const node = nodes[0];
        const nameInput = nodes.length === 1 ? ui.input({ value: node.name, placeholder: '节点名称' }) : null;
        const facts = [
          ['协议', PROTOCOL_LABEL[node.type] || node.type], ['服务器', node.server], ['端口', String(node.server_port)],
          ['凭据', '已解析（预览隐藏）'], ['证书校验', node.insecure ? '已禁用' : '启用']
        ];
        preview.replaceChildren(
          h('h4', { class: 'import-title' }, `确认导入 ${nodes.length} 个节点`),
          ...(parsed.skipped ? [h('div', { class: 'alert' }, `已跳过 ${parsed.skipped} 个无效条目`)] : []),
          ...(parsed.warnings.length ? [h('div', { class: 'alert' }, h('strong', {}, '解析警告'), h('ul', { class: 'import-warnings' }, parsed.warnings.map((w) => h('li', {}, w.detail))))] : []),
          ...(nameInput ? [ui.field('名称', nameInput)] : []),
          h('div', { class: 'facts' }, facts.map(([k, v]) => h('div', { class: 'fact' }, h('dt', {}, k), h('dd', {}, v)))),
          h('p', { class: 'muted' }, '上方只预览第一个节点；凭据始终隐藏。'),
          h('div', { class: 'dialog-inline-actions' }, h('button', {
            class: 'btn btn--primary', onclick: () => {
              if (nameInput) node.name = nameInput.value.trim() || node.name;
              nodes.forEach((candidate) => {
                candidate.id = ui.creationDraft('nodes').id;
                delete candidate.source_subscription;
                delete candidate.source_fingerprint;
                delete candidate.pinned_stale;
                S.store.intent.nodes.push(candidate);
              });
              S.store.touch();
              ui.toast(`已导入 ${nodes.length} 个节点到工作副本 · 未保存`, 'info');
              activeGroup = MANUAL;
              view.render(document.querySelector('#view'));
              close();
            }
          }, '添加到工作副本'))
        );
      } catch (e) {
        preview.replaceChildren(h('div', { class: 'alert alert--err' }, e.message));
      }
    }

    const { close } = ui.dialog({
      title: '导入节点',
      body: h('div', {}, [
        h('p', { class: 'muted' }, '每行粘贴一个节点分享链接，也支持 Base64 订阅内容；解析后可预览并添加到工作副本。'),
        textarea, preview,
        h('div', { class: 'dialog-inline-actions u-mt-10' }, [
          h('button', { class: 'btn', onclick: () => close() }, '取消'),
          h('button', { class: 'btn', onclick: review }, '解析并预览')
        ])
      ])
    });
  }

  function exportUnavailableReason(node) {
    if (node.type === 'tor') return 'Tor 节点没有分享链接格式';
    if (node.type === 'ssh' && !node.password) return '仅使用私钥的 SSH 节点不能导出为分享链接';
    return '';
  }

  function openExport(node) {
    const result = h('div', {}, h('p', { class: 'spinning' }, '正在生成分享链接…'));
    const { close } = ui.dialog({
      title: '导出节点链接',
      body: h('div', {}, [
        h('div', { class: 'alert alert--warn' }, '分享链接包含完整节点凭据，请仅通过可信渠道保存或分享。'),
        result,
        h('div', { class: 'dialog-inline-actions u-mt-10' }, h('button', { class: 'btn', onclick: () => close() }, '关闭'))
      ])
    });
    Promise.resolve(S.api.exportNode(node)).then((exported) => {
      if (!exported?.uri) throw new Error('后端没有返回分享链接');
      const textarea = h('textarea', {
        class: 'textarea export-link', rows: 5, readonly: true,
        autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false'
      });
      textarea.value = exported.uri;
      const copy = h('button', { class: 'btn btn--primary', onclick: async () => {
        try {
          if (!window.navigator?.clipboard?.writeText) throw new Error('clipboard unavailable');
          await window.navigator.clipboard.writeText(textarea.value);
          ui.toast('节点分享链接已复制到剪贴板', 'ok');
        } catch (_) {
          textarea.focus?.();
          textarea.select?.();
          ui.toast('无法自动复制，请手动复制已选中的链接', 'warn');
        }
      } }, '复制链接');
      result.replaceChildren(
        h('p', { class: 'muted' }, node.name || node.id),
        textarea,
        h('div', { class: 'dialog-inline-actions u-mt-10' }, copy)
      );
    }).catch((error) => {
      result.replaceChildren(h('div', { class: 'alert alert--err' }, `导出失败：${error.message}`));
    });
  }

  /* ---------- 节点编辑抽屉 ---------- */
  function openNodeEditor(node, focusOption) {
    const isNew = !S.store.intent.nodes.includes(node);
    const opened = ui.drawer({
      eyebrow: isNew ? '新建节点' : '编辑节点', title: node.name || '未命名', submitLabel: '保存到工作副本',
      renderBody(body) {
        const draft = JSON.parse(JSON.stringify(node));
        const name = ui.input({ value: draft.name || '', placeholder: '节点名称' });
        const enabled = ui.toggle(draft.enabled, (v) => { draft.enabled = v; });
        const typeOptions = S.uiSpec.node_types.map((item) => [item.value, item.label]);
        if (!draft.type) typeOptions.unshift(['', '缺失（需修复）']);
        const typeSel = ui.select(typeOptions, draft.type, (v) => {
          draft.type = v;
          for (const field of fieldsFor(v)) {
            if (draft[field.key] == null && field.default !== undefined)
              draft[field.key] = JSON.parse(JSON.stringify(field.default));
          }
          rebuildSpec();
        });
        const server = ui.input({ value: draft.server || '', placeholder: 'example.com 或 IP' });
        const port = ui.input({ type: 'number', value: draft.server_port || '', placeholder: '443' });
        const endpoint = h('div', { class: 'field--row' });
        const specBox = h('div', {});
        let listControls = [];

        function fieldValue(field) {
          const value = draft[field.key];
          if ((value == null || value === '') && field.default !== undefined)
            return JSON.parse(JSON.stringify(field.default));
          return value;
        }

        function effectiveValue(key) {
          const field = fieldsFor(draft.type).find((candidate) => candidate.key === key);
          return field ? fieldValue(field) : draft[key];
        }

        function specControl(field) {
          const key = field.key;
          if (field.control === 'boolean') return ui.toggleRow(FIELD_LABEL[key] || field.label, !!fieldValue(field), (v) => { draft[key] = v; });
          if (field.control === 'string-list') {
            const control = ui.chips(asList(draft[key]), { placeholder: field.placeholder || '', onchange: (v) => { draft[key] = v; } });
            listControls.push(control);
            return control;
          }
          if (field.control === 'select' || field.control === 'select-integer') {
            const options = field.options.map((item) => [item.value, item.label]);
            const selected = fieldValue(field);
            if ((selected == null || selected === '') && !options.some(([value]) => value === '')) options.unshift(['', '缺失（需修复）']);
            return ui.select(
              options, String(selected ?? ''),
              (v) => { draft[key] = field.control === 'select-integer' && v !== '' ? Number(v) : v; rebuildSpec(); }
            );
          }
          if (field.control === 'integer') return ui.input({ type: 'number', value: fieldValue(field) ?? '', placeholder: field.placeholder || '', oninput: (e) => { draft[key] = e.target.value === '' ? undefined : Number(e.target.value); } });
          if (field.multiline) return ui.textarea({
            value: draft[key] ?? '', placeholder: field.placeholder || '', sensitive: !!field.sensitive,
            oninput: (e) => { draft[key] = e.target.value; }
          });
          return ui.input({
            type: field.control === 'password' ? 'password' : 'text', value: draft[key] ?? '',
            placeholder: field.placeholder || '', oninput: (e) => { draft[key] = e.target.value; }
          });
        }

        function rebuildSpec() {
          listControls.forEach((control) => control.commitPending());
          listControls = [];
          specBox.replaceChildren();
          endpoint.replaceChildren();
          endpoint.hidden = !requiresRemoteEndpoint(draft.type);
          if (requiresRemoteEndpoint(draft.type)) endpoint.append(ui.field('服务器', server, null, 'server'), ui.field('端口', port, null, 'server_port'));
          for (const field of fieldsFor(draft.type).filter((candidate) => !['enabled', 'name', 'server', 'server_port'].includes(candidate.key))) {
            if (field.when && !field.when.values.includes(String(effectiveValue(field.when.field) ?? ''))) continue;
            specBox.append(ui.field(
              FIELD_LABEL[field.key] || field.label,
              specControl(field),
              field.sensitive ? '敏感字段；留空表示不设置' : null,
              field.key
            ));
          }
        }
        rebuildSpec();

        body.append(
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '基本信息'), [
            ui.field('名称', name, null, 'name'), ui.field('启用', enabled, null, 'enabled'),
            ui.field('协议', typeSel, null, 'type'),
            endpoint
          ]),
          h('div', { class: 'drawer-section' }, h('div', { class: 'drawer-section__title' }, '协议参数'), specBox)
        );

        return {
          submit() {
            listControls.forEach((control) => control.commitPending());
            const trim = (v) => String(v ?? '').trim();
            draft.name = trim(name.value) || undefined;
            if (!requiresRemoteEndpoint(draft.type)) {
              delete draft.server;
              delete draft.server_port;
            } else {
              if (!trim(server.value)) { ui.toast('服务器不能为空', 'err'); return false; }
              const p = Number(port.value);
              if (!port.value || p < 1 || p > 65535) { ui.toast('端口必须是 1–65535', 'err'); return false; }
              draft.server = trim(server.value);
              draft.server_port = p;
            }
            const allowed = new Set(fieldsFor(draft.type).map((field) => field.key));
            for (const key of NODE_OPTION_KEYS) if (!allowed.has(key)) delete draft[key];
            for (const key of Object.keys(draft)) {
              if (draft[key] === '' || draft[key] == null) delete draft[key];
            }
            Object.assign(node, draft);
            return node;
          }
        };
      },
      onSubmit(node) {
        if (isNew) S.store.intent.nodes.push(node);
        S.store.touch();
        ui.toast(`节点 ${node.name || node.id} 已${isNew ? '创建' : '更新'} · 未保存`, 'info');
        view.render(document.querySelector('#view'));
        return true;
      }
    });
    ui.focusDrawerOption(focusOption);
    return opened;
  }

  /* ---------- 视图 ---------- */
  const view = {
    name: 'nodes',
    render(root) {
      const isCurrent = ui.beginRender(root);
      const intent = S.store.intent;
      const pendingFocus = S.pendingObjectFocus?.object_type === 'node' ? S.pendingObjectFocus : null;
      const pendingNode = pendingFocus && intent.nodes.find((node) => node.id === pendingFocus.object_id);
      if (pendingNode) activeGroup = groupOf(pendingNode);
      rowButtons.clear();
      const groupList = groups(intent);
      if (!groupList.some((g) => g.id === activeGroup)) activeGroup = MANUAL;
      const active = groupList.find((g) => g.id === activeGroup);
      const sourceNodes = intent.nodes.filter((n) => groupOf(n) === activeGroup);
      const nodes = sortNodesForDisplay(sourceNodes, displaySortMode, displaySortDirection, S.store.probeResults);
      const orderingDisabledReason = displaySortMode === 'default' ? '' : '顺序';
      const editable = activeGroup === MANUAL;

      const groupNav = h('div', { class: 'node-groups' }, groupList.map((g) => h('button', {
        class: `chip ${g.id === activeGroup ? 'is-active' : ''}`,
        onclick: () => { activeGroup = g.id; view.render(root); }
      }, h('span', {}, g.label), h('span', { class: 'count' }, String(g.count)))));

      const table = h('table', { class: 'table' }, [
        h('thead', {}, h('tr', {}, [
          h('th', {}, '顺序'),
          h('th', {}, '状态'), h('th', {}, '节点'), h('th', {}, '协议'), h('th', {}, '端点'),
          h('th', { class: 'is-sortable' }, sortHeader('连接测速', 'connect', root)),
          h('th', { class: 'is-sortable' }, sortHeader('下载测速', 'download', root)),
          h('th', {}, '操作')
        ])),
        h('tbody', {}, nodes.map((node) => {
          const eligible = node.enabled !== false;
          const conn = testButton('连接', false, node.id, eligible);
          const down = testButton('下载', true, node.id, eligible);
          rowButtons.set(node.id, { conn, down });
          const edit = editable ? h('button', { class: 'btn btn--sm', onclick: () => openNodeEditor(node) }, '编辑') : h('span', { class: 'badge' }, '订阅');
          const exportReason = exportUnavailableReason(node);
          const exportLink = h('button', {
            class: 'btn btn--sm', disabled: !!exportReason, title: exportReason || '导出包含完整凭据的节点分享链接',
            onclick: () => openExport(node)
          }, '导出链接');
          const del = editable ? h('button', { class: 'btn btn--sm btn--danger', onclick: () => {
            if (!ui.guardCollectionDeletion('nodes', node.id, node.name || node.id)) return;
            S.store.intent.nodes = S.store.intent.nodes.filter((n) => n.id !== node.id);
            S.store.touch();
            ui.toast(`已删除 ${node.name} · 未保存`, 'warn');
            view.render(root);
          } }, '删除') : null;
          return h('tr', ui.collectionRowAttributes(
            'nodes', node, nodes, () => view.render(root), node.enabled === false ? 'is-disabled' : ''
          ), [
            h('td', { class: 'collection-drag-column' }, ui.collectionDragHandle(
              'nodes', node, sourceNodes, () => view.render(root), { disabledReason: orderingDisabledReason }
            )),
            h('td', {}, (() => {
              const enabled = ui.toggle(node.enabled, (v) => { node.enabled = v; S.store.touch(); view.render(root); });
              enabled.disabled = !editable;
              enabled.title = editable ? '启用或停用节点' : '订阅节点状态由订阅管理';
              return enabled;
            })()),
            h('td', {}, h('div', {}, h('div', {}, h('strong', {}, node.name || node.id)), node.pinned_stale ? h('span', { class: 'badge badge--stale' }, '已失效') : null)),
            h('td', {}, h('span', { class: `badge protocol-badge protocol--${node.type}` }, PROTOCOL_LABEL[node.type] || node.type)),
            h('td', { class: 'mono' }, nodeEndpoint(node)),
            h('td', { class: 'probe-sort-cell' }, probeAction(conn)),
            h('td', { class: 'probe-sort-cell' }, probeAction(down)),
            h('td', {}, h('div', { class: 'row-actions row-actions--wrap' }, edit, exportLink, del))
          ]);
        }))
      ]);

      const batch = renderBatch(sourceNodes.filter((node) => node.enabled !== false).map((node) => node.id));
	  syncTestButtons();
	  if (typeof S.store.subscribe === 'function') {
		const unsubscribe = S.store.subscribe(syncTestButtons);
		isCurrent.onDispose(unsubscribe);
	  }
      root.append(
        ui.viewHead('节点', editable ? '手动添加与维护的节点列表' : '订阅节点列表（只读）', [
          ui.collectionOrderToolbar('nodes', sourceNodes, () => view.render(root), { disabledReason: orderingDisabledReason }),
          h('button', { class: 'btn', onclick: openImport }, '导入节点'),
          editable ? h('button', { class: 'btn btn--primary', onclick: () => openNodeEditor(ui.creationDraft('nodes')) }, '添加节点') : null
        ]),
        groupNav
      );
      if (batch) root.append(batch);
      root.append(
        nodes.length ? h('div', { class: 'card table-card' }, h('div', { class: 'table-wrap' }, table)) : h('div', { class: 'empty' }, '该分组没有节点')
      );

      const focus = ui.takeObjectFocus('node');
      const focusedNode = focus && intent.nodes.find((node) => node.id === focus.object_id);
      if (focusedNode && !focusedNode.source_subscription) openNodeEditor(focusedNode, focus.option);
      else if (focusedNode) ui.toast(`已定位订阅节点 ${focusedNode.name || focusedNode.id}；该对象只读，请修改订阅源或高级配置`, 'warn');
    }
  };

  view.probeMetric = probeMetric;
  view.sortNodesForDisplay = sortNodesForDisplay;

  S.views = S.views || {};
  S.views.nodes = view;
})();
