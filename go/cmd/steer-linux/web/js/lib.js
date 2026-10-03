/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 共享小工具：DOM 构造、图标、格式化。 */
'use strict';
(function () {
  const S = (window.S = window.S || {});

  /* 轻量 DOM 构造器：h('div', {class, onclick, ...}, child…) */
  function h(tag, attrs = {}, ...children) {
    const el = document.createElement(tag);
    for (const [key, value] of Object.entries(attrs)) {
      if (value == null || value === false) continue;
      if (key === 'class') el.className = value;
      else if (key === 'dataset') Object.assign(el.dataset, value);
      else if (key.startsWith('on')) el.addEventListener(key.slice(2), value);
      else if (key === 'html') el.innerHTML = value; /* 仅用于受信任的内联 SVG */
      else el.setAttribute(key, value === true ? '' : String(value));
    }
    for (const child of children.flat(Infinity)) {
      if (child == null || child === false) continue;
      el.append(child instanceof Node ? child : document.createTextNode(String(child)));
    }
    return el;
  }

  const ICONS = {
    gauge: '<circle cx="12" cy="12" r="8.5"/><path d="M12 12 16.4 7.6"/><path d="M12 14.4a2.4 2.4 0 1 0 0-4.8"/>',
    server: '<rect x="3" y="4.5" width="18" height="6.5" rx="1.6"/><rect x="3" y="13" width="18" height="6.5" rx="1.6"/><path d="M7 7.75h.01M7 16.25h.01"/>',
    route: '<circle cx="6" cy="6" r="2.3"/><circle cx="18" cy="18" r="2.3"/><path d="M8.3 6h3.9a3.8 3.8 0 0 1 3.8 3.8v4.4a3.8 3.8 0 0 0 3.8 3.8h-3.9" transform="translate(2 -2)"/>',
    globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.4 2.3 3.6 5.2 3.6 8.5s-1.2 6.2-3.6 8.5c-2.4-2.3-3.6-5.2-3.6-8.5s1.2-6.2 3.6-8.5z"/>',
    plug: '<path d="M9 7.5V3.5m6 4V3.5M7 7.5h10v4.2a5 5 0 0 1-10 0z"/><path d="M12 16.7v3.8"/>',
    list: '<path d="M8.5 6h11M8.5 12h11M8.5 18h11"/><circle cx="4.2" cy="6" r="1"/><circle cx="4.2" cy="12" r="1"/><circle cx="4.2" cy="18" r="1"/>',
    refresh: '<path d="M19.6 11.5a7.6 7.6 0 1 0-2.2 5.4"/><path d="M19.8 4.2v7.3h-7.3"/>',
    activity: '<path d="M3 12h4l3-8 4 16 3-8h4"/>',
    sliders: '<path d="M4 8h10M18 8h2M4 16h4M12 16h8"/><circle cx="16" cy="8" r="2"/><circle cx="10" cy="16" r="2"/>',
    braces: '<path d="M8.5 4.5c-2 0-3 .9-3 2.6v2.3c0 1.6-.9 2-2 2.6 1.1.6 2 1 2 2.6v2.3c0 1.7 1 2.6 3 2.6M15.5 4.5c2 0 3 .9 3 2.6v2.3c0 1.6.9 2 2 2.6-1.1.6-2 1-2 2.6v2.3c0 1.7-1 2.6-3 2.6"/>'
  };

  const icon = (name, size = 15) => h('span', {
    class: 'icon', 'aria-hidden': 'true',
    html: `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${ICONS[name] || ''}</svg>`
  });

  const asList = (value) => (value == null ? [] : (Array.isArray(value) ? value : [value]));

  const fmtTime = (iso) => (iso ? new Date(iso).toLocaleString('zh-CN', { hour12: false }) : '—');

  function fmtLatestProbe(result) {
    if (!result) return { text: '尚未测试', ok: null, stale: false };
    const metric = result.ok ? result.summary : result.error_summary;
    return {
      text: `上次 ${fmtTime(result.tested_at)} · ${result.stale ? '已过期 · ' : ''}${result.ok ? '成功' : '失败'}${metric ? ` · ${metric}` : ''}`,
      ok: result.ok,
      stale: result.stale === true
    };
  }

  const uid = (prefix) => `${prefix}-${Math.random().toString(36).slice(2, 8)}`;

  Object.assign(S, {
    h, icon, asList, fmtTime, fmtLatestProbe, uid
  });
})();
