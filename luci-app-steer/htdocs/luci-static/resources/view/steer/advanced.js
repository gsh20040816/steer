/* SPDX-License-Identifier: GPL-3.0-or-later */

'use strict';
'require view';
'require steer as steer';

const redactedValue = '[REDACTED]';

function secretKey(key) {
	return key == 'uuid' || key == 'private_key' || key == 'url' || key == 'extra_args' || key == 'plugin_options' ||
		/(^|_)(password|token)$/.test(key);
}

function redactSecrets(value) {
	if (Array.isArray(value))
		return value.map(redactSecrets);
	if (value == null || typeof(value) != 'object')
		return value;
	return Object.fromEntries(Object.entries(value).map(([ key, item ]) => [
		key,
		secretKey(key) && item != null && String(item) != '' ? redactedValue : redactSecrets(item)
	]));
}

return view.extend({
	load: function() {
		return steer.intentPreview(false);
	},

	render: function(preview) {
		steer.loadStyle(this);
		if (preview?.ok !== true) {
			return E('section', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Canonical Preview unavailable')),
				E('p', { 'class': 'alert-message danger' }, _('Unable to export the Canonical preview.'))
			]);
		}

		let current = preview;
		let revealed = false;
		const heading = E('h3');
		const notice = E('p');
		const document = E('pre', { 'class': 'steer-canonical-preview' });
		const reveal = E('button', {
			'type': 'button',
			'class': 'cbi-button cbi-button-action',
			'aria-pressed': 'false',
			'click': async function() {
				if (revealed) {
					current = Object.assign({}, current, {
						redacted: true,
						intent: redactSecrets(current.intent || {})
					});
					revealed = false;
					document.replaceChildren(JSON.stringify(current.intent, null, 2));
					reveal.setAttribute('aria-pressed', 'false');
					reveal.textContent = _('Reveal secrets temporarily');
					return;
				}

				reveal.disabled = true;
				const revealedPreview = await steer.intentPreview(true);
				reveal.disabled = false;
				if (revealedPreview?.ok !== true) {
					revealError.hidden = false;
					return;
				}
				revealError.hidden = true;
				current = revealedPreview;
				if (!updateMetadata()) {
					revealed = false;
					document.replaceChildren();
					return;
				}
				revealed = true;
				document.replaceChildren(JSON.stringify(revealedPreview.intent || {}, null, 2));
				reveal.setAttribute('aria-pressed', 'true');
				reveal.textContent = _('Hide secrets');
			}
		}, _('Reveal secrets temporarily'));
		const revealError = E('p', { 'class': 'alert-message danger', 'hidden': '' }, _('Secret reveal failed.'));
		const secretNotice = E('p', {}, _('Secrets are hidden by default. Reveal is temporary and resets when you leave this page.'));

		function updateMetadata() {
			if (current.pending === true || current.available === false) {
				heading.replaceChildren(_('Pending candidate preview unavailable'));
				notice.className = 'alert-message warning';
				notice.replaceChildren(_('Pending UCI changes exist. No Canonical JSON is shown because a trustworthy preview of the Apply input is unavailable.'));
				secretNotice.hidden = true;
				reveal.hidden = true;
				document.hidden = true;
				return false;
			}
			heading.replaceChildren(_('Committed snapshot'));
			notice.className = '';
			notice.replaceChildren(_('No pending Steer changes exist. This preview is the committed UCI snapshot.'));
			secretNotice.hidden = false;
			reveal.hidden = false;
			document.hidden = false;
			return true;
		}

		if (updateMetadata())
			document.replaceChildren(JSON.stringify(preview.intent || {}, null, 2));
		return E('section', { 'class': 'cbi-section' }, [
			E('h2', {}, _('Canonical Preview')),
			heading,
			notice,
			secretNotice,
			E('div', { 'class': 'right' }, reveal),
			revealError,
			document
		]);
	}
});
