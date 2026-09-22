/* SPDX-License-Identifier: GPL-3.0-or-later */

'use strict';
'require form';
'require uci';
'require ui';
'require view';
'require steer as steer';
'require steer.ui-spec as uiSpec';

function diagnosticDetail(detail) {
	const value = String(detail || '');
	if (value == 'the published Active generation contains the expected port-53 capture artifacts')
		return _('System DNS capture is configured.');
	return detail;
}

function overviewProbePresentation(result) {
	if (!result) return { text: _('Not tested'), ok: null, stale: false };
	const tested = new Date(result.tested_at);
	const testedAt = Number.isNaN(tested.getTime()) ? '—' : tested.toLocaleString();
	return {
		text: [ _('Tested at'), testedAt, result.stale ? _('Outdated') : '',
			result.ok ? _('Succeeded') : _('Failed'), result.ok ? result.summary : result.error_summary ].filter(Boolean).join(' · '),
		ok: result.ok,
		stale: result.stale === true
	};
}

function renderOverviewProbe(output, result) {
	const latest = overviewProbePresentation(result);
	output.replaceChildren(E('div', {
		'class': 'steer-test-card__result' + (latest.stale ? ' is-stale' : (latest.ok === false ? ' is-error' : (latest.ok ? ' is-success' : '')))
	}, E('strong', {}, latest.text)));
}

function findOverviewResult(probeResults, kind) {
	return (probeResults?.latest_results || []).find((result) => result.scope == 'overview' && result.kind == kind);
}

function renderTestCard(kind, title, description, allowed, probeResults) {
	const output = E('div', { 'class': 'steer-test-card__output' });
	renderOverviewProbe(output, findOverviewResult(probeResults, kind));
	const button = E('button', {
		'class': 'btn cbi-button-action',
		'disabled': allowed ? null : true,
		'title': allowed ? '' : _('You do not have permission to run Overview probes.'),
		'click': function(ev) {
			ev.preventDefault();
			if (!allowed) {
				ui.addNotification(_('Probe is not permitted'), E('p', {}, _('Your session does not have permission to run Overview probes.')), 'warning');
				return;
			}
			const current = ev.currentTarget;
			current.disabled = true;
			output.replaceChildren(E('span', { 'class': 'spinning' }, _('Testing…')));
			return steer.overviewProbe(kind).then((result) => {
				renderOverviewProbe(output, result);
				current.disabled = false;
				return result;
			}).catch((error) => {
				return steer.probeResults().then((refreshed) => {
					renderOverviewProbe(output, findOverviewResult(refreshed, kind));
				}).catch(() => output.replaceChildren(E('div', { 'class': 'steer-test-card__result is-error' },
					E('strong', {}, '%s · %s'.format(_('Failed'), _('See diagnostic logs for details.'))))))
					.finally(() => { current.disabled = false; });
			});
		}
	}, _('Run test'));
	return E('section', { 'class': 'steer-test-card' }, [
		E('div', {}, [ E('h3', {}, title), E('p', {}, description) ]),
		button,
		output
	]);
}

function renderOverviewTests(allowed, probeResults) {
	return E('div', { 'class': 'steer-test-grid' }, [
		renderTestCard('direct', _('Direct target'), _('Tests the direct target in the current network environment.'), allowed, probeResults),
		renderTestCard('proxy', _('Proxy target'), _('Tests the proxy target in the current network environment.'), allowed, probeResults),
		renderTestCard('speedtest', _('Download target'), _('Tests download speed in the current network environment.'), allowed, probeResults)
	]);
}

function diagnosticFact(label, value) {
	return E('div', {}, [ E('dt', {}, label), E('dd', {}, value == null || value === '' ? '—' : String(value)) ]);
}

function hasPendingSteerChanges(changes) {
	if (Array.isArray(changes)) return changes.length > 0;
	if (Array.isArray(changes?.steer)) return changes.steer.length > 0;
	return changes?.steer != null && Object.keys(changes.steer).length > 0;
}

function renderDiagnostics(status, validation, diagnostics, probeResults, changes, permissions) {
	const lastApply = status?.last_apply;
	const result = lastApply?.result;
	const dnsCapture = diagnostics?.dns_capture || {};
	return E([], [
		E('section', { 'class': 'cbi-section' }, [
			E('h3', {}, _('Connectivity targets')),
			E('p', {}, _('The tests use the current network environment and remain available while Steer is disabled. Success only means the target was reachable at that time.')),
			renderOverviewTests(permissions?.overview_probe === true, probeResults)
		]),
		E('section', { 'class': 'cbi-section' }, [
			E('h3', {}, _('System DNS capture check')),
			E('dl', { 'class': 'steer-status__facts' }, [
				diagnosticFact(_('Configured'), dnsCapture.configured ? _('Yes') : _('No')),
				diagnosticFact(_('Result'), diagnosticDetail(dnsCapture.detail))
			])
		]),
		...(diagnostics?.warnings || []).slice(0, 3).map((warning) => E('p', { 'class': 'alert-message warning' }, warning)),
		...(probeResults?.warnings || []).slice(0, 3).map((warning) => E('p', { 'class': 'alert-message warning' }, warning)),
		E('section', { 'class': 'cbi-section' }, [
			E('h3', {}, _('Validation')),
			...((validation?.errors || []).map((issue) => E('p', { 'class': 'alert-message danger' }, steer.issueText(issue)))),
			...((validation?.warnings || []).map((issue) => E('p', { 'class': 'alert-message warning' }, steer.issueText(issue)))),
			validation?.ok ? E('p', {}, _('The saved configuration is valid.')) : ''
		]),
		E('section', { 'class': 'cbi-section' }, [
			E('h3', {}, _('Recent application result')),
			E('dl', { 'class': 'steer-status__facts' }, [
				diagnosticFact(_('Result'), result?.ok ? _('Succeeded') : (lastApply ? _('Failed') : '—')),
				diagnosticFact(_('Error'), result?.error)
			])
		]),
		E('section', { 'class': 'cbi-section' }, [
			E('h3', {}, _('Recent logs')),
			E('pre', { 'class': 'steer-canonical-preview' }, diagnostics?.logs || diagnostics?.log_error || _('No Steer log entries were returned.'))
		]),
		E('button', { 'class': 'btn cbi-button-action', click: function() { window.location.reload(); } }, _('Refresh diagnostics'))
	]);
}

function warningGroupLabel(group) {
	if (group?.code == 'INSECURE_TLS' && group?.object_type == 'dns_profile') return _('DNS certificate verification is disabled');
	if (group?.code == 'INSECURE_TLS') return _('TLS certificate verification is disabled');
	if (group?.code == 'SUBSCRIPTION_NODE_STALE') return _('Subscription node is no longer advertised');
	if (group?.code == 'DNS_REJECT_PROJECTION_SKIPPED') return _('DNS reject conditions cannot be applied before resolution');
	if (group?.code == 'DNS_PROJECTION_EMPTY') return _('DNS continues matching later rules');
	return group?.summary || _('Configuration warning');
}

function warningGroupScope(group) {
	return ({ node: _('node(s)'), route: _('route(s)'), dns_profile: _('DNS Profile(s)'),
		local_proxy: _('local proxy entry/entries'), rule: _('rule(s)') })[group?.object_type] || _('object(s)');
}

function warningGroupDestination(group) {
	return ({ nodes: 'nodes', routes: 'routes', dns: 'dns', proxies: 'proxies', rules: 'rules',
		subscriptions: 'subscriptions', general: 'general' })[group?.destination];
}

function renderWarningGroups(validation, title) {
	const groups = validation?.warning_groups || [];
	if (!groups.length) return '';
	return E('div', { 'class': 'steer-warning-summary' }, [
		E('h4', {}, title),
		E('div', { 'class': 'steer-warning-groups' }, groups.map((group) => {
			const destination = warningGroupDestination(group);
			return E('article', { 'class': 'steer-warning-group' }, [
				E('div', {}, [
					E('strong', {}, warningGroupLabel(group)),
					E('span', {}, _('%d affected in-use %s').format(group.count || 0, warningGroupScope(group)))
				]),
				destination ? E('a', { 'class': 'btn cbi-button-action', 'href': destination }, _('View affected items')) : ''
			]);
		}))
	]);
}

function localizedApplyTime(record) {
	let date = record?.timestamp ? new Date(record.timestamp) : null;
	if ((!date || Number.isNaN(date.getTime())) && /^\d{13,}$/.test(String(record?.sequence || '')))
		date = new Date(Number(String(record.sequence).slice(0, 13)));
	return !date || Number.isNaN(date.getTime()) ? _('Time unknown') : date.toLocaleString();
}

function overviewPipeline(counts) {
	counts = counts || {};
	const step = (kind, value, title, detail) => E('div', { 'class': 'steer-overview-step steer-overview-step--' + kind }, [
		E('strong', {}, title), value == null ? '' : E('span', {}, value), E('small', {}, detail)
	]);
	const arrow = () => E('span', { 'class': 'steer-overview-arrow', 'aria-hidden': 'true' }, '→');
	return E('div', { 'class': 'steer-overview-pipeline' }, [
		step('match', counts.rules || 0, _('Match rules'), _('First matching Rule wins')),
		arrow(),
		step('dns', counts.dns_profiles || 0, _('DNS Profile'), _('Independent resolution path')),
		arrow(),
		step('route', counts.routes || 0, _('Routes'), _('Direct, Reject, or Node chain')),
		arrow(),
		step('network', null, _('Network egress'), _('Establish the final connection'))
	]);
}

function overviewCounts(counts) {
	counts = counts || {};
	return E('dl', { 'class': 'steer-overview-metrics' }, [
		diagnosticFact(_('Nodes'), counts.nodes || 0),
		diagnosticFact(_('Routes'), counts.routes || 0),
		diagnosticFact(_('DNS Profiles'), counts.dns_profiles || 0),
		diagnosticFact(_('Local Proxies'), counts.local_proxies || 0),
		diagnosticFact(_('Rules'), counts.rules || 0),
		diagnosticFact(_('Subscriptions'), counts.subscriptions || 0)
	]);
}

function renderLifecycleOverview(state) {
	const desired = state?.desired || {};
	const saved = state?.saved || {};
	const active = state?.active || {};
	const pending = state?.pending === true;
	const lastApply = active.last_apply;
	const lastResult = lastApply?.result;
	const overviewValidation = pending ? desired.validation : saved.validation;
	const activeEnabled = !!active.generation;
	const lifecycleDifference = state?.pending_apply === true || saved.enabled !== activeEnabled;
	const validationErrors = overviewValidation?.errors || [];
	const validationWarnings = overviewValidation?.warnings || [];
	const validationGroups = overviewValidation?.warning_groups || [];
	return E('section', { 'class': 'cbi-section steer-overview-shell' }, [
		E('div', { 'class': 'steer-overview-region', 'data-overview-region': 'execution_model' }, [
			E('h3', {}, _('Execution model')),
			overviewPipeline(desired.counts)
		]),
		E('div', { 'class': 'steer-overview-region', 'data-overview-region': 'configuration_lifecycle' }, [
			E('h3', {}, _('Configuration lifecycle')),
			E('dl', { 'class': 'steer-status__facts' }, [
				diagnosticFact(_('Working copy'), pending ? _('Unsaved changes') : _('Saved')),
				diagnosticFact(_('Saved configuration'), saved.enabled ? _('Enabled') : _('Disabled')),
				diagnosticFact(_('Pending apply'), state?.pending_apply ? _('Yes') : _('No')),
				diagnosticFact(_('Active status'), activeEnabled ? (active.healthy ? _('Normal') : _('Abnormal')) : _('Stopped')),
				diagnosticFact(_('Saved / Active'), lifecycleDifference ? _('Different; action required') : _('Consistent'))
			])
		]),
		E('div', { 'class': 'steer-overview-region', 'data-overview-region': 'object_scale' }, [
			E('h3', {}, _('Working copy scale')),
			overviewCounts(desired.counts)
		]),
		E('div', { 'class': 'steer-overview-region', 'data-overview-region': 'validation_summary' }, [
			E('h3', {}, _('Validation and warning summary')),
			E('dl', { 'class': 'steer-status__facts' }, [
				diagnosticFact(_('Errors'), validationErrors.length),
				diagnosticFact(_('Warnings'), validationWarnings.length),
				diagnosticFact(_('Warning groups'), validationGroups.length)
			]),
			renderWarningGroups(overviewValidation, pending ? _('Working copy warning summary') : _('Saved configuration warning summary')),
			!validationErrors.length && !validationWarnings.length ? E('p', {}, _('The current working copy is valid.')) : ''
		]),
		E('div', { 'class': 'steer-overview-region', 'data-overview-region': 'last_apply_and_actions' }, [
			E('h3', {}, _('Recent application and shortcuts')),
			E('dl', { 'class': 'steer-status__facts' }, [
				diagnosticFact(_('Time'), lastApply ? localizedApplyTime(lastApply) : '—'),
				diagnosticFact(_('Result'), lastResult?.ok ? _('Succeeded') : (lastApply ? _('Failed') : '—'))
			]),
			lastResult?.ok === false
				? E('p', { 'class': 'alert-message warning' }, lastResult.activated
					? _('The running configuration changed but Apply did not finish. Open Diagnostics for recovery steps.')
					: _('The running configuration did not change. The Saved configuration can be applied again.'))
				: E('p', {}, lastApply ? _('The Saved configuration was applied successfully.') : _('No application has been recorded yet.')),
			E('div', { 'class': 'steer-overview-actions' }, [
				E('button', { 'class': 'btn cbi-button-action', 'click': () => window.location.reload() }, _('Refresh')),
				E('a', { 'class': 'btn cbi-button-action', 'href': 'diagnostics' }, _('Open Diagnostics')),
				E('a', { 'class': 'btn cbi-button-action', 'href': 'system' }, _('System information'))
			]),
			E('p', { 'class': 'steer-overview-note' }, _('Save, Apply Saved, Save and Apply, and Discard are available in the global status area above.'))
		])
	]);
}

return view.extend({
	load: function() {
		return Promise.all([
			uci.load('steer'), steer.overviewState(), steer.validate(), steer.diagnostics(), steer.probeResults(), uci.changes(),
			steer.permissions([ 'overview_probe' ])
		]);
	},

	render: function(data) {
		let m, s, o;
		const lifecycle = data[1] || {};
		const status = lifecycle.active || {};
		const validation = data[2];
		const page = (window.location.pathname || '').split('/').pop();
		steer.loadStyle(this);
		if (page == 'overview' || page == 'steer')
			return renderLifecycleOverview(lifecycle);
		if (page == 'diagnostics')
			return renderDiagnostics(status, validation, data[3] || {}, data[4] || { latest_results: [] }, data[5], data[6] || {});

		m = new form.Map('steer', _('Steer'));
		s = m.section(form.NamedSection, 'main', 'steer', _('General'));

		o = s.option(form.ListValue, 'log_level', _('Log level'));
		uiSpec.log_levels.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label)));
		o.default = 'warn';

		if (uiSpec.platform_capabilities.openwrt.direct_bypass) {
			o = s.option(form.ListValue, 'direct_bypass', _('IPv6 TCP direct mode'));
			uiSpec.direct_bypass_modes.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label)));
			o.default = 'off';
			o.description = _('Kernel direct preserves the client address when rules can be decided early. DNS-assisted mode accepts shared-IP ambiguity; other connections use normal routing.');
		}

		o = s.option(form.Value, 'dns_cache_capacity', _('Cache capacity'));
		o.datatype = 'range(1024,10000000)';
		o.placeholder = '4096';
		o.rmempty = true;
		o.description = _('Leave empty to use the default value; custom range is 1,024–10,000,000.');
		o.cfgvalue = function(sectionId) {
			const value = uci.get('steer', sectionId, 'dns_cache_capacity');
			return value === '0' || value === 0 ? '' : value;
		};

		o = s.option(form.Flag, 'dns_cache_persist', _('Persistent cache'));
		o.default = '0';
		o.description = _('Persist the shared DNS cache in the Steer cache database.');

		o = s.option(form.Flag, 'dns_optimistic_cache', _('Optimistic cache'));
		o.default = '0';
		o.description = _('Serve recently expired DNS answers while refreshing them in the background.');

		o = s.option(form.Value, 'probe_direct', _('Direct connectivity probe URL'));
		o.validate = function(_sectionId, value) { return steer.validateInput('probe_url', value); };
		o.rmempty = false;
		o.placeholder = 'https://www.example.com/';

		o = s.option(form.Value, 'probe_proxy', _('Proxy connectivity probe URL'));
		o.validate = function(_sectionId, value) { return steer.validateInput('probe_url', value); };
		o.rmempty = false;
		o.placeholder = 'https://www.example.com/';

		o = s.option(form.Value, 'speedtest_proxy', _('Proxy speed-test URL'));
		o.validate = function(_sectionId, value) { return steer.validateInput('probe_url', value); };
		o.rmempty = false;
		o.placeholder = 'https://speed.cloudflare.com/__down?bytes=1000000';

		s = m.section(form.NamedSection, 'bootstrap', 'bootstrap', _('Bootstrap DNS'));
		o = s.option(form.ListValue, 'protocol', _('Protocol'));
		uiSpec.bootstrap_protocols.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label))); o.rmempty = false;
		o = s.option(form.Value, 'server', _('Server IP')); o.datatype = 'ipaddr'; o.rmempty = false;
		o.description = _('Server IP used to resolve encrypted DNS upstream domains.');
		o = s.option(form.Value, 'server_port', _('Port')); o.datatype = 'port'; o.rmempty = false;
		o = s.option(form.ListValue, 'strategy', _('Address strategy'));
		uiSpec.bootstrap_strategies.forEach((item) => o.value(item.value, steer.uiSpecLabel(item.label)));
		o.rmempty = false;

		return m.render();
	},

	handleSaveApply: function(ev, mode) { return steer.apply(this, ev, mode); }
});
