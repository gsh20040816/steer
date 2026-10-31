/* SPDX-License-Identifier: GPL-3.0-or-later */

'use strict';
'require form';
'require uci';
'require view';
'require steer as steer';
'require steer.ui-spec as uiSpec';

const dnsFields = Array.from(new Set(uiSpec.dns_protocols.flatMap((item) => item.fields)));

function dnsProtocol(value) {
	return uiSpec.dns_protocols.find((item) => item.value == value);
}

function clearUnsupportedDNSFields(sectionId, protocol) {
	if (!protocol)
		return;
	dnsFields.forEach((field) => {
		if (!protocol.fields.includes(field))
			uci.unset('steer', sectionId, field);
	});
}

function dependOnDNSField(option, field) {
	uiSpec.dns_protocols
		.filter((protocol) => protocol.fields.includes(field))
		.forEach((protocol) => option.depends('protocol', protocol.value));
	if (uiSpec.dns_protocols.some((protocol) => protocol.required_fields.includes(field)))
		option.rmempty = false;
	if (field == 'path')
		option.validate = (sectionId, value) => steer.validateInput('dns_http_path', value);
	option.write = function(sectionId, value) {
		const protocol = dnsProtocol(uci.get('steer', sectionId, 'protocol'));
		if (!protocol?.fields.includes(field))
			return uci.unset('steer', sectionId, field);
		uci.set('steer', sectionId, field, value);
	};
}

return view.extend({
	load: function() { return uci.load('steer'); },

	render: function() {
		let m, s, o;
		steer.loadStyle(this);
		m = new form.Map('steer', _('DNS profiles'));
		s = m.section(form.GridSection, 'dns_profile', _('DNS profiles'));
		steer.configureNamedSection(s, steer.creationDefaults('dns_profiles'));
		steer.configureOrdering(s, 'dns_profiles');
		steer.configureRemovalGuard(s, (sectionId) => steer.collectionReferences('dns_profiles', sectionId),
			_('DNS profile is still referenced'));
		s.addremove = true;
		s.nodescriptions = true;
		s.addbtntitle = _('Add DNS profile');
		s.tab('general', _('Upstream'));
		s.tab('tls', _('TLS'));

		o = s.taboption('general', form.Flag, 'enabled', _('Enabled')); o.default = '1'; o.editable = true;
		o = s.taboption('general', form.Value, 'name', _('Name')); o.rmempty = true; o.optional = true; o.modalonly = true;
		o = s.taboption('general', form.ListValue, 'protocol', _('Protocol'));
		uiSpec.dns_protocols.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label)));
		o.rmempty = false; o.editable = true;
		o.write = function(sectionId, value) {
			const previous = dnsProtocol(uci.get('steer', sectionId, 'protocol'));
			const next = dnsProtocol(value);
			const currentPort = Number(uci.get('steer', sectionId, 'server_port'));
			uci.set('steer', sectionId, 'protocol', value);
			if (next && (!Number.isInteger(currentPort) || currentPort < 1 || (previous && currentPort == previous.default_port)))
				uci.set('steer', sectionId, 'server_port', String(next.default_port));
			clearUnsupportedDNSFields(sectionId, next);
		};
		o = s.taboption('general', form.Value, 'server', _('Server')); o.rmempty = false; o.editable = true;
		o = s.taboption('general', form.Value, 'server_port', _('Port')); o.datatype = 'port'; o.rmempty = false; o.modalonly = true;
		o = s.taboption('general', form.Value, 'path', _('HTTP path')); o.placeholder = '/dns-query'; o.modalonly = true;
		dependOnDNSField(o, 'path');

		o = s.taboption('tls', form.Value, 'tls_server_name', _('TLS server name')); o.modalonly = true;
		dependOnDNSField(o, 'tls_server_name');
		o = s.taboption('tls', form.Flag, 'insecure', _('Skip certificate verification')); o.default = '0'; o.modalonly = true;
		dependOnDNSField(o, 'insecure');

		return m.render().then((formNode) => steer.focusSection(s, 'dns_profile').then(() => formNode));
	},

	handleSaveApply: function(ev, mode) { return steer.apply(this, ev, mode); }
});
