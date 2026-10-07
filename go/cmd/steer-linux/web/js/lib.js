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
    braces: '<path d="M8.5 4.5c-2 0-3 .9-3 2.6v2.3c0 1.6-.9 2-2 2.6 1.1.6 2 1 2 2.6v2.3c0 1.7 1 2.6 3 2.6M15.5 4.5c2 0 3 .9 3 2.6v2.3c0 1.6.9 2 2 2.6-1.1.6-2 1-2 2.6v2.3c0 1.7-1 2.6-3 2.6"/>',
    cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9.5 9.5h5v5h-5zM9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>',
    steer: '<path d="M4 12h4.5l3-6 3 12 3-6H20"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    more: '<circle cx="5.5" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="18.5" cy="12" r="1.2"/>',
    check: '<circle cx="12" cy="12" r="8.5"/><path d="m8.5 12.2 2.4 2.4 4.6-4.9"/>',
    alert: '<path d="M12 4.2 2.9 19.5h18.2z"/><path d="M12 10v4.2M12 16.9h.01"/>',
    error: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.8v5M12 16.2h.01"/>',
    pause: '<circle cx="12" cy="12" r="8.5"/><path d="M10 9v6M14 9v6"/>',
    upload: '<path d="M12 15.5V4.5M7.5 9 12 4.5 16.5 9"/><path d="M4.5 15v3.5a1 1 0 0 0 1 1h13a1 1 0 0 0 1-1V15"/>',
    arrow: '<path d="M5 12h14M13.5 6.5 19 12l-5.5 5.5"/>',
    ban: '<circle cx="12" cy="12" r="8.5"/><path d="m6 6 12 12"/>',
    lock: '<rect x="5" y="10.5" width="14" height="9.5" rx="2"/><path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5"/>',
    logout: '<path d="M14.5 4.5h3a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-3M10 16.5 5.5 12 10 7.5M5.5 12H15"/>',
    inbox: '<path d="M3.5 13.5 6 5.5h12l2.5 8v5a1 1 0 0 1-1 1h-15a1 1 0 0 1-1-1z"/><path d="M3.5 13.5h5l1.5 2.5h4l1.5-2.5h5"/>'
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
