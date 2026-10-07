/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 配置 · 高级：Canonical JSON 原文编辑器 + 校验问题映射 + geo 补全。 */
'use strict';
(function () {
  const S = window.S;
  const { h } = S;
  const ui = S.ui;

  function currentGeoToken(value, cursor) {
    const before = String(value || '').slice(0, cursor == null ? String(value || '').length : cursor);
    const match = before.match(/(geosite|geoip):[a-z0-9_!.@-]*$/);
    return match ? { start: before.length - match[0].length, end: before.length, kind: match[1], value: match[0] } : null;
  }

  const view = {
    name: 'advanced',
    async render(root) {
      const isCurrent = ui.beginRender(root);
      const [geosite, geoip] = await Promise.all([S.api.geodata('geosite'), S.api.geodata('geoip')]);
      const catalogs = { geosite, geoip };

      const editor = h('textarea', { class: 'textarea editor-tall', spellcheck: 'false', autocomplete: 'off' });
      editor.value = S.store.draftText || JSON.stringify(S.store.intent, null, 2);
      const syntax = h('div', { class: 'match-editor__status' });
      const suggestions = h('div', { class: 'editor-suggestions' });
      const issues = h('div', {});
      const dirtyBadge = h('span', { class: 'badge badge--warn', hidden: !S.store.dirty }, '工作副本已修改');
      const invalidBadge = h('span', { class: 'badge badge--err', hidden: S.store.draftValid !== false }, '配置格式有误');
      const saveButton = h('button', { class: 'btn', onclick: () => ui.onSave(false) }, '保存');
      const validateButton = h('button', { class: 'btn btn--ghost', onclick: validate }, '校验');
      const saveApplyButton = h('button', { class: 'btn btn--primary', onclick: () => ui.onSave(true) }, '保存并应用');

      let matches = [];
	  let validationEpoch = null;
      const syncState = () => {
        const valid = S.store.draftValid !== false;
        if (editor.value !== S.store.draftText) editor.value = S.store.draftText;
        syntax.textContent = valid
          ? 'JSON 语法正常'
          : `JSON 格式有误，已阻止保存与结构化页面：${S.store.draftError}`;
        syntax.classList.toggle('is-err', !valid);
        dirtyBadge.hidden = !S.store.dirty;
        invalidBadge.hidden = valid;
        invalidBadge.title = S.store.draftError || '';
        const busy = S.store.saving === true || S.store.reloading === true || S.store.applying === true;
        saveButton.disabled = !S.store.dirty || !valid || busy;
        saveApplyButton.disabled = !S.store.dirty || !valid || busy;
      };
      const renderSuggestions = () => {
        const token = currentGeoToken(editor.value, editor.selectionStart);
        matches = [];
        suggestions.replaceChildren();
        if (!token) return;
        const needle = token.value.slice(token.kind.length + 1);
        matches = (catalogs[token.kind]?.names || []).filter((n) => String(n).includes(needle)).slice(0, 30).map((n) => `${token.kind}:${n}`);
        matches.forEach((value) => suggestions.append(h('button', {
          class: 'chip', type: 'button',
          onclick: () => {
            editor.value = editor.value.slice(0, token.start) + value + editor.value.slice(token.end);
            const pos = token.start + value.length;
            editor.setSelectionRange(pos, pos);
            S.store.editJSON(editor.value);
            renderSuggestions();
            editor.focus();
          }
        }, value)));
      };
      editor.addEventListener('input', () => {
        S.store.editJSON(editor.value);
        renderSuggestions();
      });
      ['click', 'focus', 'keyup'].forEach((ev) => editor.addEventListener(ev, () => {
        renderSuggestions();
      }));

      async function validate() {
        if (S.store.draftValid === false) {
          issues.replaceChildren(h('div', { class: 'alert alert--err' }, `当前 JSON 格式无效：${S.store.draftError}`));
          return;
        }
		const requestedEpoch = S.store.draftEpoch;
		const v = await S.api.validate(S.store.intent);
		if (requestedEpoch !== S.store.draftEpoch) {
			issues.replaceChildren(h('div', { class: 'alert' }, '校验期间工作副本已变化；旧结果已丢弃。'));
			validationEpoch = null;
			return;
		}
		validationEpoch = requestedEpoch;
        issues.replaceChildren(
          h('div', { class: 'card__head validation-head' }, h('h2', { class: 'card__title' }, `校验结果：${v.errors.length} 个错误 · ${v.warnings.length} 个警告`)),
          ui.issueList(v.errors, ui.jumpToObject),
          ui.issueList(v.warnings, ui.jumpToObject, true)
        );
      }

      if (!isCurrent()) return;
      const unsubscribe = S.store.subscribe(() => {
        if (!isCurrent()) return;
        syncState();
		if (validationEpoch != null && validationEpoch !== S.store.draftEpoch) {
			issues.replaceChildren(h('div', { class: 'alert' }, '工作副本已变化；先前校验结果已过期，请重新校验。'));
			validationEpoch = null;
		}
      });
      isCurrent.onDispose(unsubscribe);
      syncState();

      root.append(
        ui.viewHead('高级配置', '直接查看和编辑 Canonical JSON。与结构化页面共用同一份工作副本。', [dirtyBadge, invalidBadge]),
        h('section', { class: 'card' }, [
          h('div', { class: 'editor-actions' }, [
            validateButton,
            h('button', {
              class: 'btn btn--ghost', onclick: () => {
                try {
                  editor.value = JSON.stringify(JSON.parse(editor.value), null, 2);
                  S.store.editJSON(editor.value);
                  ui.toast('已格式化', 'info');
                }
                catch (e) { ui.toast(`无法格式化：${e.message}`, 'err'); }
              }
            }, '格式化'),
            h('span', { class: 'spacer' }),
            saveButton,
            saveApplyButton
          ]),
          editor,
          syntax,
          suggestions,
          issues
        ])
      );
    }
  };

  S.views = S.views || {};
  S.views.advanced = view;
})();
