/*
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

'use strict';
'require form';
'require uci';
'require view';
'require steer as steer';
'require steer.ui-spec as uiSpec';

return view.extend({
	load: function() {
		return uci.load('steer');
	},

	render: function() {
		let m, s, o;
		steer.loadStyle(this);

		m = new form.Map('steer', _('Local proxies'));

		s = m.section(form.GridSection, 'local_proxy', _('Named proxy entry points'));
		steer.configureNamedSection(s, steer.creationDefaults('local_proxies'));
		steer.configureOrdering(s, 'local_proxies');
		steer.configureRemovalGuard(s, (sectionId) => steer.collectionReferences('local_proxies', sectionId),
			_('Local proxy is still referenced'));
		s.addremove = true;
		s.nodescriptions = true;
		s.addbtntitle = _('Add local proxy');

		o = s.option(form.Flag, 'enabled', _('Enabled'));
		o.default = '1';
		o.editable = true;

		o = s.option(form.Value, 'name', _('Name'));
		o.rmempty = true;
		o.optional = true;
		o.modalonly = true;

		o = s.option(form.ListValue, 'protocol', _('Protocol'));
		uiSpec.local_proxy_protocols.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label)));
		o.rmempty = false;
		o.editable = true;

		o = s.option(form.Value, 'listen', _('Listen address'));
		o.datatype = 'ipaddr';
		o.placeholder = '127.0.0.1';
		o.rmempty = false;
		o.editable = true;
		o.description = E('strong', { 'class': 'steer-exposure-warning' },
			_('Exposure warning: this address may be reachable from LAN or public networks and requires authentication.'));

		o = s.option(form.Value, 'listen_port', _('Port'));
		o.datatype = 'port';
		o.rmempty = false;
		o.editable = true;

		o = s.option(form.Value, 'username', _('Username'));
		o.modalonly = true;

		o = s.option(form.Value, 'password', _('Password'));
		o.password = true;
		o.modalonly = true;
		o.description = _('Username and password must either both be set or both be empty.');

		return m.render().then((formNode) => steer.focusSection(s, 'local_proxy').then(() => formNode));
	},

	handleSaveApply: function(ev, mode) {
		return steer.apply(this, ev, mode);
	}
});
