/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 真实 Linux Web API 适配层：Bearer 鉴权、ETag 并发控制与结构化错误。 */
'use strict';
(function () {
  const S = window.S;
  const TOKEN_KEY = 'steer.web.token';
  let token = sessionStorage.getItem(TOKEN_KEY) || '';

  function messageOf(data, fallback) {
    if (typeof data?.error === 'string') return data.error;
    if (data?.error?.message) return data.error.message;
    return fallback;
  }

  function responseError(response, data) {
    const error = new Error(messageOf(data, response.statusText || `HTTP ${response.status}`));
    error.status = response.status;
    error.details = data;
    if (response.status === 401) error.code = 'AUTH_REQUIRED';
    else if (response.status === 409) error.code = 'CONFLICT';
    else error.code = data?.error?.code || `HTTP_${response.status}`;
    return error;
  }

  async function fetchJSON(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (token) headers.set('Authorization', `Bearer ${token}`);
    if (options.body != null && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const response = await fetch(path, { ...options, headers });
    const data = await response.json().catch(() => ({}));
    return { response, data };
  }

  async function request(path, options) {
    const result = await fetchJSON(path, options);
    if (!result.response.ok) throw responseError(result.response, result.data);
    return result;
  }

  async function config() {
    const { response, data } = await request('/api/v1/config');
    data.revision = response.headers.get('ETag') || data.revision || '';
    return data;
  }

  async function putConfig(intent, expectedRevision, apply) {
    const revision = expectedRevision || (await config()).revision;
    const { response, data } = await fetchJSON('/api/v1/config', {
      method: 'PUT',
      headers: { 'If-Match': revision },
      body: JSON.stringify({ intent, apply: !!apply })
    });
    data.revision = response.headers.get('ETag') || data.revision || revision;
    if (response.status === 409) {
      const latest = await config();
      const error = responseError(response, data);
      error.serverRevision = latest.revision;
      error.external = { note: '服务器配置已被其他会话修改。', changes: ['服务器修订已变化'] };
      throw error;
    }
    /* Apply 失败时后端仍已持久化配置；把这个真实的部分成功状态交给 store。 */
    if (!response.ok && data.saved !== true) throw responseError(response, data);
    data.request_ok = response.ok;
    return data;
  }

  async function applySaved() {
    const { response, data } = await fetchJSON('/api/v1/apply', { method: 'POST' });
    /* Apply 的 422 是有持久结果的业务失败，不丢掉后端错误详情。 */
    if (!response.ok && typeof data?.ok !== 'boolean') throw responseError(response, data);
    data.request_ok = response.ok;
    return data;
  }

  const api = {
    async overview() { return (await request('/api/v1/overview')).data; },
    async runtime() { return (await request('/api/v1/runtime')).data; },
    async logs() { return (await request('/api/v1/logs')).data; },
    async diagnostics() { return (await request('/api/v1/diagnostics')).data; },
    async probeResults() { return (await request('/api/v1/probe-results')).data; },
    config,
    putConfig,
    applySaved,
    async setEnabled(enabled) {
      const { response, data } = await fetchJSON('/api/v1/enabled', {
        method: 'POST', body: JSON.stringify({ enabled: !!enabled })
      });
      if (!response.ok && data.saved !== true) throw responseError(response, data);
      return data;
    },
    async validate(intent) {
      return (await request('/api/v1/validate', { method: 'POST', body: JSON.stringify({ intent }) })).data;
    },
    async probe(kind) {
      return (await request('/api/v1/probes/overview', {
        method: 'POST', body: JSON.stringify({ kind, download: kind === 'speedtest' })
      })).data;
    },
    async speedtestNode(id, download) {
      return (await request(`/api/v1/probes/nodes/${encodeURIComponent(id)}`, {
        method: 'POST', body: JSON.stringify({ download: !!download })
      })).data;
    },
    async speedtestRoute(id, download) {
      return (await request(`/api/v1/probes/routes/${encodeURIComponent(id)}`, {
        method: 'POST', body: JSON.stringify({ download: !!download })
      })).data;
    },
    async subscriptions() { return (await request('/api/v1/subscriptions')).data; },
    async updateSubscription(id) {
      return (await request(`/api/v1/subscriptions/${encodeURIComponent(id)}/update`, { method: 'POST' })).data;
    },
    async cleanNode(subscriptionId, nodeId) {
      return (await request(`/api/v1/subscriptions/${encodeURIComponent(subscriptionId)}/nodes/${encodeURIComponent(nodeId)}`, { method: 'DELETE' })).data;
    },
    async geodata(kind) { return (await request(`/api/v1/geodata/${encodeURIComponent(kind)}`)).data; },
    async importNodes(document) {
      return (await request('/api/v1/nodes/import', { method: 'POST', body: JSON.stringify({ document }) })).data;
    },
    async exportNode(node) {
      return (await request('/api/v1/nodes/export', { method: 'POST', body: JSON.stringify({ node }) })).data;
    }
  };

  function showLogin(onAuthenticated) {
    document.querySelector('.auth-overlay')?.remove();
    const errorText = S.h('div', { class: 'auth-error', role: 'alert' });
    const input = S.h('input', {
      class: 'input auth-token', type: 'password', autocomplete: 'current-password',
      placeholder: '粘贴 steer web-token 输出的令牌', value: token
    });
    const submit = S.h('button', { class: 'btn btn--primary', type: 'submit' }, '连接控制台');
    const form = S.h('form', {
      class: 'auth-form',
      onsubmit: async (event) => {
        event.preventDefault();
        const value = input.value.trim();
        if (!value) { errorText.textContent = '请输入 Web 令牌。'; return; }
        token = value;
        sessionStorage.setItem(TOKEN_KEY, value);
        submit.disabled = true;
        submit.textContent = '正在连接…';
        errorText.textContent = '';
        try {
          await config();
          document.querySelector('.auth-overlay')?.remove();
          await onAuthenticated();
        } catch (error) {
          if (error.code === 'AUTH_REQUIRED') {
            errorText.textContent = '令牌无效，请重新运行 steer web-token 后再试。';
            sessionStorage.removeItem(TOKEN_KEY);
            token = '';
          } else {
            errorText.textContent = `连接失败：${error.message}`;
          }
          submit.disabled = false;
          submit.textContent = '连接控制台';
        }
      }
    }, [
      S.h('span', { class: 'eyebrow' }, '本机管理控制台'),
      S.h('h1', { class: 'auth-title' }, '连接 Steer'),
      S.h('p', { class: 'muted' }, '控制面仅监听本机回环地址。令牌只保存在当前浏览器标签页中。'),
      S.h('label', { class: 'field' }, S.h('span', {}, 'Web 令牌'), input),
      errorText,
      S.h('div', { class: 'auth-actions' }, submit),
      S.h('p', { class: 'auth-hint mono' }, 'sudo steer web-token')
    ]);
    const overlay = S.h('div', { class: 'dialog-overlay auth-overlay' },
      S.h('div', { class: 'dialog auth-dialog' }, form));
    document.body.append(overlay);
    setTimeout(() => input.focus(), 0);
  }

  const auth = {
    show: showLogin,
    logout() {
      sessionStorage.removeItem(TOKEN_KEY);
      token = '';
      location.reload();
    }
  };

  Object.assign(S, { api, auth });
})();
