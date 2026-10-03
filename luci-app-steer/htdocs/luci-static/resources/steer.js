/* SPDX-License-Identifier: GPL-3.0-or-later */

'use strict';
'require baseclass';
'require rpc';
'require uci';
'require ui';
'require steer.ui-spec as uiSpec';

const callStatus = rpc.declare({ object: 'luci.steer', method: 'status', expect: { '': {} } });
const callOverviewState = rpc.declare({ object: 'luci.steer', method: 'overview_state', expect: { '': {} } });
const callSetEnabled = rpc.declare({ object: 'luci.steer', method: 'set_enabled', params: [ 'enabled' ], expect: { '': {} } });
const callApplySaved = rpc.declare({ object: 'luci.steer', method: 'apply_saved', expect: { '': {} } });
const callValidate = rpc.declare({ object: 'luci.steer', method: 'validate', expect: { '': {} } });
const callCommitCandidate = rpc.declare({ object: 'luci.steer', method: 'commit_candidate', expect: { '': {} } });
const callDiscardCandidate = rpc.declare({ object: 'luci.steer', method: 'discard_candidate', expect: { '': {} } });
const callIntentPreview = rpc.declare({ object: 'luci.steer', method: 'intent_preview', params: [ 'reveal' ], expect: { '': {} } });
const callRuntime = rpc.declare({ object: 'luci.steer', method: 'runtime', expect: { '': {} } });
const callLogs = rpc.declare({ object: 'luci.steer', method: 'logs', expect: { '': {} } });
const callDiagnostics = rpc.declare({ object: 'luci.steer', method: 'diagnostics', expect: { '': {} } });
const callProbeResults = rpc.declare({ object: 'luci.steer', method: 'probe_results', expect: { '': {} } });
const callGeodataCatalog = rpc.declare({ object: 'luci.steer', method: 'geodata_catalog', expect: { '': {} } });
const callSubscriptions = rpc.declare({ object: 'luci.steer', method: 'subscriptions', expect: { '': {} } });
const callSubscriptionUpdate = rpc.declare({ object: 'luci.steer', method: 'subscription_update', params: [ 'id' ], expect: { '': {} } });
const callSubscriptionClean = rpc.declare({ object: 'luci.steer', method: 'subscription_clean', params: [ 'id', 'node' ], expect: { '': {} } });
const callNodeSpeedtest = rpc.declare({ object: 'luci.steer', method: 'node_speedtest', params: [ 'node', 'download' ], expect: { '': {} } });
const callRouteSpeedtest = rpc.declare({ object: 'luci.steer', method: 'route_speedtest', params: [ 'route', 'download' ], expect: { '': {} } });
const callOverviewProbe = rpc.declare({ object: 'luci.steer', method: 'overview_probe', params: [ 'kind' ], expect: { '': {} } });
const callNodeImport = rpc.declare({ object: 'luci.steer', method: 'node_import', params: [ 'document' ], expect: { '': {} } });
const callNodeExport = rpc.declare({ object: 'luci.steer', method: 'node_export', params: [ 'node' ], expect: { '': {} } });
const callSessionAccess = rpc.declare({
	object: 'session', method: 'access', params: [ 'scope', 'object', 'function' ], expect: { access: false }
});
const sectionIDPattern = new RegExp(uiSpec.id_policy.pattern);
const collectionBySectionType = {
	node: 'nodes', route: 'routes', dns_profile: 'dns_profiles', local_proxy: 'local_proxies',
	rule: 'rules', subscription: 'subscriptions'
};

function uciCreationDefaults(collection, overrides) {
	const values = Object.assign({}, uiSpec.creation_defaults?.[collection] || {}, overrides || {});
	return Object.fromEntries(Object.entries(values).map(([ key, value ]) => {
		if (typeof(value) == 'boolean') return [ key, value ? '1' : '0' ];
		if (typeof(value) == 'number') return [ key, String(value) ];
		return [ key, value ];
	}));
}

function nextSectionID(collection) {
	const prefix = uiSpec.id_policy?.collection_prefixes?.[collection] || 'item';
	let index = 1;
	while (uci.get('steer', prefix + '_' + index)) index++;
	return prefix + '_' + index;
}

function disambiguateReferences(references) {
	const counts = {};
	const ordinals = {};
	references.forEach((reference) => { counts[reference.label] = (counts[reference.label] || 0) + 1; });
	return references.map((reference) => Object.assign({}, reference, {
		label: counts[reference.label] > 1
			? '%s%s · %s'.format(
				reference.label,
				reference.detail ? ' · ' + reference.detail : '',
				_('Duplicate %d').format(ordinals[reference.label] = (ordinals[reference.label] || 0) + 1))
			: reference.label
	}));
}

const validationMessages = {
	DANGLING_NODE: _('The referenced Node does not exist.'),
	DANGLING_DETOUR: _('The referenced detour Route does not exist.'),
	DANGLING_DNS_PROFILE: _('The referenced DNS Profile does not exist.'),
	DANGLING_ROUTE: _('The referenced Route does not exist.'),
	DANGLING_LOCAL_PROXY: _('The referenced Local Proxy does not exist.'),
	DISABLED_NODE: _('The referenced Node is disabled.'),
	DISABLED_DETOUR: _('The referenced detour Route is disabled.'),
	DISABLED_DNS_PROFILE: _('The referenced DNS Profile is disabled.'),
	DISABLED_ROUTE: _('The referenced Route is disabled.'),
	DISABLED_LOCAL_PROXY: _('The referenced Local Proxy is disabled.'),
	ROUTE_DETOUR_CYCLE: _('The Route detour chain contains a cycle.'),
	PORT_COLLISION: _('The listener conflicts with another configured listener.'),
	DEFAULT_COUNT: _('Exactly one enabled Default Rule is required.'),
	DIRECT_ROUTE_COUNT: _('Exactly one enabled Direct Route is required.'),
	RULE_AFTER_DEFAULT: _('An enabled Rule appears after the Default Rule.'),
	DNS_PROJECTION_EMPTY: _('This Rule has only connection-stage conditions, so DNS continues to later Rules.'),
	DNS_REJECT_PROJECTION_SKIPPED: _('DNS cannot evaluate this Rule’s connection-stage conditions, so DNS continues to later Rules.'),
	INSECURE_TLS: _('TLS certificate verification is disabled.'),
	SUBSCRIPTION_NODE_STALE: _('The subscription no longer advertises this Node; remove Route references as soon as possible.'),
	PLATFORM_UNSUPPORTED_SOURCE_MAC: _('The current platform cannot match the original source MAC address.'),
	CANDIDATE_READ_FAILED: _('The pending Steer configuration could not be read.'),
	CANDIDATE_VALIDATE_FAILED: _('The pending Steer configuration could not be validated.'),
	CANDIDATE_CHANGED: _('The pending Steer configuration changed during validation. Review it and retry.')
};

const issueObjectLabels = {
	steer: _('Steer'),
	bootstrap: _('Bootstrap DNS'),
	node: _('Node'),
	route: _('Route'),
	dns_profile: _('DNS profile'),
	local_proxy: _('Local proxy'),
	rule: _('Rule'),
	subscription: _('Subscription')
};

const issueOptionLabels = {
	insecure: _('Certificate verification'),
	pinned_stale: _('Subscription status'),
	dns_profile: _('DNS profile'),
	route: _('Route'),
	inbound: _('Local proxy entry'),
	node: _('Node')
};

const rpcErrorMessages = {
	PENDING_STATE_UNAVAILABLE: _('The pending Steer configuration cannot be inspected.'),
	PENDING_CHANGES: _('Apply or discard pending Steer changes before operating on the committed configuration.'),
	CONTROL_START_FAILED: _('The Steer control program could not be started.'),
	CONTROL_OUTPUT_INVALID: _('The Steer control program returned an invalid response.'),
	CONTROL_EXIT_FAILED: _('The Steer control program failed.'),
	IMPORT_TEMP_FAILED: _('A private import workspace could not be created.'),
	IMPORT_PROGRAM_MISSING: _('The Steer node parser is not installed.'),
	IMPORT_PROGRAM_NOT_EXECUTABLE: _('The Steer node parser is not executable.'),
	IMPORT_START_FAILED: _('The Steer node parser could not be started.'),
	IMPORT_PARSE_FAILED: _('The node share-link document could not be parsed.'),
	IMPORT_OUTPUT_INVALID: _('The Steer node parser returned an invalid response.'),
	CANDIDATE_READ_FAILED: _('The pending Steer configuration could not be read.'),
	CANDIDATE_CHANGED: _('The pending Steer configuration changed during validation. Review it and retry.'),
	CANDIDATE_COMMIT_FAILED: _('The validated Steer configuration could not be committed.'),
	CANDIDATE_REVERT_FAILED: _('The pending Steer configuration could not be discarded.'),
	LIFECYCLE_PENDING_READ_FAILED: _('The pending lifecycle state could not be read.'),
	LIFECYCLE_STATE_FAILED: _('The Steer lifecycle state could not be read.'),
	LIFECYCLE_CHANGED: _('The pending Steer configuration changed while lifecycle state was read. Retry.'),
	PREVIEW_DECODE_FAILED: _('The Steer configuration preview could not be decoded.'),
	LOG_READ_FAILED: _('Recent Steer logs could not be read.'),
	MISSING_SUBSCRIPTION_ID: _('A Subscription ID is required.'),
	MISSING_SUBSCRIPTION_NODE_ID: _('A Subscription ID and Node ID are required.'),
	MISSING_NODE_ID: _('A Node ID is required.'),
	MISSING_ROUTE_ID: _('A Route ID is required.'),
	INVALID_PROBE_KIND: _('Choose the direct, proxy, or speed-test probe.'),
	MISSING_NODE_DOCUMENT: _('Paste at least one node share link.'),
	NODE_NOT_FOUND: _('The requested Node does not exist.'),
	NODE_EXPORT_UNSUPPORTED: _('This Node cannot be represented as a supported share link.'),
	NODE_EXPORT_FAILED: _('The Node share link could not be exported.')
};

function validationMessage(issue) {
	const code = String(issue?.code || 'VALIDATION');
	if (validationMessages[code]) return validationMessages[code];
	if (code == 'REQUIRED' || code.startsWith('REQUIRED_') || code.startsWith('INCOMPLETE_'))
		return _('A required value is missing or incomplete.');
	if (code.startsWith('INVALID_')) return _('The field value is invalid.');
	if (code.startsWith('UNSUPPORTED_')) return _('This value is not supported.');
	if (code.startsWith('UNEXPECTED_')) return _('This field is not applicable here.');
	return _('Configuration validation failed.');
}

function rpcErrorText(result) {
	const code = result?.error_code;
	if (code && rpcErrorMessages[code]) return rpcErrorMessages[code];
	return _('Operation failed.');
}

function uiSpecLabel(label) {
	return label == null ? '' : _(String(label));
}

function validateInput(formatName, value) {
	const raw = String(value || '');
	if (raw == '') return true;
	const spec = uiSpec.input_formats?.[formatName];
	if (!spec) return true;
	let valid = raw == raw.trim();
	if (valid && spec.kind == 'url') {
		try {
			const parsed = new URL(raw);
			const scheme = parsed.protocol.replace(/:$/, '').toLowerCase();
			valid = (!spec.absolute || !!parsed.hostname) && (!spec.schemes?.length || spec.schemes.includes(scheme));
			if (spec.forbid_credentials && (parsed.username || parsed.password)) valid = false;
			if (spec.forbid_fragment && parsed.hash) valid = false;
		}
		catch (_) { valid = false; }
	}
	else if (valid && spec.kind == 'duration') {
		valid = !spec.pattern || new RegExp(spec.pattern).test(raw);
	}
	else if (valid && spec.kind == 'string' && spec.prefix) {
		valid = raw.startsWith(spec.prefix);
	}
	if (valid) return true;
	return {
		probe_url: _('Enter an absolute HTTPS URL without credentials or a fragment.'),
		subscription_url: _('Enter an absolute HTTP or HTTPS URL.'),
		positive_duration: _('Enter a positive duration such as 30m, 6h, or 250ms.'),
		dns_http_path: _('The DNS HTTP path must start with /.')
	}[formatName] || _('The field value is invalid.');
}

function issueText(issue) {
	let target = issueObjectLabels[issue?.object_type] || _('Configuration');
	if (issueOptionLabels[issue?.option])
		target += ' / %s'.format(issueOptionLabels[issue.option]);
	return '%s: %s'.format(target, validationMessage(issue));
}

function issueDestination(issue) {
	return {
		steer: 'general', bootstrap: 'general', node: 'nodes', route: 'routes',
		dns_profile: 'dns', local_proxy: 'proxies', rule: 'rules', subscription: 'subscriptions'
	}[issue?.object_type];
}

function navigateIssue(issue) {
	const page = issueDestination(issue);
	if (!page) return false;
	const focus = [ issue.object_type, issue.object_id || '', issue.option || '' ].join(':');
	window.location.href = L.url('admin/services/steer/' + page) + '?steer-focus=' + encodeURIComponent(focus);
	return true;
}

function collectionReferences(targetCollection, targetId) {
	if (targetCollection == 'subscriptions') {
		const owned = Object.fromEntries(uci.sections('steer', 'node')
			.filter((node) => node.source_subscription == targetId)
			.map((node) => [ node['.name'], true ]));
		return uci.sections('steer', 'route').filter((route) => owned[route.node]).map((route) => ({
			objectType: 'route', id: route['.name'], label: route.name || route['.name'], option: 'node'
		}));
	}
	const references = [];
	(uiSpec.collection_references || []).filter((relation) => relation.target_collection == targetCollection)
		.forEach((relation) => {
			uci.sections('steer', relation.source_object_type).forEach((source) => {
				const value = source[relation.field];
				const matched = relation.multiple
					? (Array.isArray(value) ? value : (value ? [ value ] : [])).includes(targetId)
					: value == targetId;
				if (matched) references.push({
					objectType: relation.source_object_type, id: source['.name'],
					label: source.name || source['.name'], option: relation.field
				});
			});
		});
	return references;
}

function resultMessage(result) {
	const issues = [
		...(result?.validation?.errors || []).map((issue) => ({ issue, warning: false })),
		...(result?.validation?.warnings || []).map((issue) => ({ issue, warning: true }))
	];
	if (issues.length)
		return E('ul', { 'class': 'steer-validation-issues' }, issues.map((entry) => E('li', { 'class': entry.warning ? 'warning' : 'error' }, [
			E('span', {}, issueText(entry.issue)),
			issueDestination(entry.issue) ? E('button', { 'class': 'btn cbi-button-action', 'click': () => navigateIssue(entry.issue) }, _('Go to field')) : ''
		])));
	return E('p', {}, rpcErrorText(result));
}

function waitForApply(sequence, attempts) {
	return L.resolveDefault(callStatus(), null).then((status) => {
		if (status?.last_apply?.sequence && status.last_apply.sequence !== sequence)
			return { result: status.last_apply.result, status };
		if (attempts <= 0)
			return { result: { ok: false, error: _('Timed out waiting for the committed Steer configuration to reload.') }, status };
		return new Promise((resolve) => window.setTimeout(resolve, 250))
			.then(() => waitForApply(sequence, attempts - 1));
	});
}

function notifyCandidateRejected(commit) {
	const result = { ok: false, saved: false, committed: false, validation: commit?.validation, error: commit?.error, error_code: commit?.error_code };
	ui.addNotification(_('Configuration was not saved'), E('div', {}, [
		E('p', {}, _('Correct the reported problem and try again. The saved and running configurations were not changed.')),
		resultMessage(result)
	]), 'danger');
	return result;
}

function finishCommittedApply(owner, result, status, validation, previousGeneration) {
	if (status) owner.refreshStatus(status, validation);
	ui.hideModal();
	if (!result?.ok) {
		const currentGeneration = status?.generation || '';
		const unchanged = currentGeneration == (previousGeneration || '');
		const failed = { ...result, saved: true, committed: true, active_unchanged: unchanged };
		ui.addNotification(_('Configuration saved; application failed'), E('div', {}, [
			E('p', {}, unchanged
				? _('The service continues using the previous running configuration.')
				: _('The running configuration may have changed. Check Diagnostics before relying on traffic stability.')),
			resultMessage(failed),
			E('p', {}, _('Fix the reported problem, then choose Apply Saved configuration.'))
		]), 'danger');
		return failed;
	}
	const disabled = uci.get('steer', 'main', 'enabled') != '1';
	ui.addNotification(null, E('p', {}, disabled
		? _('Configuration saved; Steer was disabled and its runtime resources were cleaned up.')
		: _('Steer configuration saved and activated.')), 'info');
	if (validation?.warnings?.length)
		ui.addNotification(_('Validation warnings'), resultMessage({ validation }), 'warning');
	return { ...result, saved: true, committed: true };
}

return baseclass.extend({
	loadStyle: function(view) {
		if (!document.getElementById('steer-stylesheet'))
			document.head.appendChild(E('link', { id: 'steer-stylesheet', rel: 'stylesheet', href: L.resource('steer/steer.css') }));
		if (view) {
			this._lifecycleView = view;
			if (typeof(view.handleSave) == 'function' && view._steerLifecycleSaveWrapped !== true) {
				const owner = this;
				const handleSave = view.handleSave;
				view.handleSave = function() {
					return Promise.resolve(handleSave.apply(this, arguments)).then((result) =>
						owner.mountLifecycleBar(view).then(() => result));
				};
				view._steerLifecycleSaveWrapped = true;
			}
		}
		this.mountLifecycleBar(this._lifecycleView);
	},

	mountLifecycleBar: function(view) {
		if (view) this._lifecycleView = view;
		const mountSequence = this._lifecycleMountSequence = (this._lifecycleMountSequence || 0) + 1;
		const existing = document.getElementById('steer-lifecycle-global');
		existing?.remove();
		const host = document.getElementById('maincontent');
		if (!host?.prepend) return;
		return Promise.all([
			this.overviewState(),
			this.permissions([ 'commit_candidate', 'discard_candidate', 'apply_saved', 'set_enabled' ], true)
		]).then(([ state, permissions ]) => {
			if (mountSequence != this._lifecycleMountSequence) return;
			if (state?.ok !== true) return;
			const desired = state.desired || {};
			const saved = state.saved || {};
			const active = state.active || {};
			const desiredEnabled = saved.enabled === true;
			const desiredValid = desired.validation?.ok === true;
			const canApplyPending = permissions.commit_candidate === true && permissions.uci_write === true;
			const canApplySaved = permissions.apply_saved === true;
			const operationBusy = this._lifecycleOperationBusy === true;
			const toggleAllowed = permissions.set_enabled === true && !operationBusy && this._globalEnableBusy !== true;
			const actions = [];
			const controls = [];
			const run = (button, operation) => {
				if (this._lifecycleOperationBusy === true) return Promise.resolve({ busy: true });
				this._lifecycleOperationBusy = true;
				controls.forEach((control) => { control.disabled = true; });
				return Promise.resolve().then(operation).then((result) => {
					this._lifecycleOperationBusy = false;
					return this.mountLifecycleBar(this._lifecycleView).then(() => result);
				}).catch((error) => {
					this._lifecycleOperationBusy = false;
					ui.addNotification(_('Operation failed'), E('p', {}, error?.error_code ? rpcErrorText(error) : _('Operation failed.')), 'danger');
					return this.mountLifecycleBar(this._lifecycleView).then(() => ({ ok: false, error }));
				});
			};
			if (state.pending) {
				const apply = E('button', { 'class': 'btn cbi-button-positive', 'disabled': canApplyPending && !operationBusy ? null : true }, _('Save & Apply pending changes'));
				const discard = E('button', { 'class': 'btn cbi-button-negative', 'disabled': permissions.discard_candidate === true && permissions.uci_write === true && !operationBusy ? null : true }, _('Discard pending changes'));
				apply.addEventListener('click', () => run(apply, () => this.applyPending()));
				discard.addEventListener('click', () => run(discard, () => this.discardPending()));
				actions.push(apply, discard);
				controls.push(apply, discard);
			}
			else if (state.pending_apply) {
				const applySaved = E('button', { 'class': 'btn cbi-button-positive', 'disabled': canApplySaved && !operationBusy ? null : true }, _('Apply Saved configuration'));
				applySaved.addEventListener('click', () => run(applySaved, () => this.applySaved().then((result) => {
					if (result?.ok === false) throw result;
					return result;
				})));
				actions.push(applySaved);
				controls.push(applySaved);
			}
			const toggle = E('button', {
				'class': 'btn steer-global-status__toggle',
				'role': 'switch',
				'aria-checked': desiredEnabled ? 'true' : 'false',
				'disabled': toggleAllowed ? null : true,
				'title': permissions.set_enabled !== true ? _('You do not have permission to change Steer.') : _('Apply Saved configuration')
			}, desiredEnabled ? _('Enabled') : _('Disabled'));
			controls.push(toggle);
			toggle.addEventListener('click', () => run(toggle, () => this.setGlobalEnabled(!desiredEnabled, this._lifecycleView)));
			const actualState = active.generation
				? (active.healthy ? _('Running normally') : _('Running abnormally'))
				: _('Stopped');
			const fact = (label, value) => E('div', {}, [ E('dt', {}, label), E('dd', {}, value) ]);
			const bar = E('section', { id: 'steer-lifecycle-global', 'class': 'steer-global-status' }, [
				E('div', { 'class': 'steer-global-status__lead' }, [
					E('span', { 'class': 'steer-status__eyebrow' }, _('Steer status')),
					E('strong', { 'class': active.healthy ? 'is-running' : (active.generation ? 'is-starting' : 'is-stopped') }, actualState)
				]),
				E('div', { 'class': 'steer-global-status__enable' }, [ E('span', {}, _('Enable Steer')), toggle ]),
				E('dl', { 'class': 'steer-global-status__facts' }, [
					fact(_('Working copy'), state.pending ? _('Unsaved changes') : _('All changes saved')),
					fact(_('Saved configuration'), saved.enabled ? _('Enabled') : _('Disabled')),
					fact(_('Active configuration'), actualState),
					fact(_('Pending apply'), state.pending_apply ? _('Yes') : _('No'))
				]),
				actions.length ? E('div', { 'class': 'steer-global-status__actions' }, actions) : ''
			]);
			host.prepend(bar);
		});
	},

	setGlobalEnabled: function(enabled, view) {
		if (this._globalEnableBusy === true) return Promise.resolve({ busy: true });
		this._globalEnableBusy = true;
        return callSetEnabled(enabled).then((result) => {
            if (result?.ok !== true) {
                if (result?.saved === true) {
                    ui.addNotification(_('Configuration saved; application failed'), resultMessage(result), 'danger');
                    return result;
                }
                throw result;
            }
            return result;
        }).finally(() => { this._globalEnableBusy = false; });
	},

	status: function() { return L.resolveDefault(callStatus(), {}); },
	focusIssue: function(issue) { return navigateIssue(issue); },
	issueText: function(issue) { return issueText(issue); },
	rpcErrorText: function(result) { return rpcErrorText(result); },
	uiSpecLabel: function(label) { return uiSpecLabel(label); },
	validateInput: function(formatName, value) { return validateInput(formatName, value); },
	permissions: function(methods, includeUCIWrite) {
		const requests = (methods || []).map((method) =>
			L.resolveDefault(callSessionAccess('ubus', 'luci.steer', method), false));
		if (includeUCIWrite)
			requests.push(L.resolveDefault(callSessionAccess('uci', 'steer', 'write'), false));
		return Promise.all(requests).then((allowed) => {
			const result = Object.fromEntries((methods || []).map((method, index) => [ method, allowed[index] === true ]));
			if (includeUCIWrite) result.uci_write = allowed[allowed.length - 1] === true;
			return result;
		});
	},
	collectionReferences: function(targetCollection, targetId) { return collectionReferences(targetCollection, targetId); },
	overviewState: function() { return L.resolveDefault(callOverviewState(), {}); },
	applySaved: function() { return callApplySaved(); },
	validate: function() { return L.resolveDefault(callValidate(), {}); },
	commitCandidate: function() { return L.resolveDefault(callCommitCandidate(), {}); },
	intentPreview: function(reveal) { return L.resolveDefault(callIntentPreview(reveal === true), {}); },
	runtime: function() { return L.resolveDefault(callRuntime(), {}); },
	logs: function() { return L.resolveDefault(callLogs(), {}); },
	diagnostics: function() { return L.resolveDefault(callDiagnostics(), {}); },
	probeResults: function() { return L.resolveDefault(callProbeResults(), { latest_results: [], warnings: [] }); },
	geodataCatalog: function() { return L.resolveDefault(callGeodataCatalog(), {}); },
	subscriptions: function() { return L.resolveDefault(callSubscriptions(), {}); },
	updateSubscription: function(id) { return callSubscriptionUpdate(id); },
	cleanSubscription: function(id, node) { return callSubscriptionClean(id, node); },
	speedtest: function(node, download) { return callNodeSpeedtest(node, download); },
	routeSpeedtest: function(route, download) { return callRouteSpeedtest(route, download); },
	overviewProbe: function(kind) { return callOverviewProbe(kind); },
	importNodes: function(document) { return callNodeImport(document); },
	exportNode: function(node) { return callNodeExport(node); },
	creationDefaults: function(collection, overrides) { return uciCreationDefaults(collection, overrides); },
	disambiguateReferences: function(references) { return disambiguateReferences(references); },
	nextSectionID: function(collection) { return nextSectionID(collection); },

	configureNamedSection: function(section, defaults, beforeSectionId) {
		const handleAdd = section.handleAdd;
		const collection = collectionBySectionType[section.sectiontype || section.sectionType];
		const resolvedDefaults = Object.assign(uciCreationDefaults(collection), defaults || {});
		section.anonymous = false;
		section.autoIDs = true;
		section.sectiontitle = function(sectionId) {
			return uci.get('steer', sectionId, 'name') || _('Unnamed');
		};
		const renderSectionAdd = section.renderSectionAdd;
		section.renderSectionAdd = function(extraClass) {
			const row = renderSectionAdd.call(this, extraClass);
			const input = row.querySelector?.('.cbi-section-create-name');
			const button = row.querySelector?.('.cbi-button-add');
			if (!input || !button)
				return row;
			input.value = nextSectionID(collection);
			input.type = 'hidden';
			input.hidden = true;
			button.disabled = this.map?.readonly === true ? true : null;
			return row;
		};
		section.handleAdd = function(ev, sectionId) {
			if (typeof(sectionId) != 'string' || !sectionIDPattern.test(sectionId)) {
				ui.addNotification(_('Invalid section ID'), E('p', {}, _('Use the shared 1–32 character lowercase ID policy.')), 'danger');
				return;
			}
			/* Keep GridSection's native provisional-section lifecycle so Add opens
			 * the editor modal and Cancel removes the unsaved section. Inject the
			 * Steer defaults synchronously at the native data.add() boundary so the
			 * first modal render sees them. */
			const data = this.map.data;
			const nativeAdd = data.add;
			const hadOwnAdd = Object.prototype.hasOwnProperty.call(data, 'add');
			data.add = function(config, type, name) {
				const addedSection = nativeAdd.call(this, config, type, name);
				try {
					Object.entries(resolvedDefaults).forEach((entry) => this.set(config, addedSection, entry[0], entry[1]));
					if (beforeSectionId)
						this.move(config, addedSection, beforeSectionId, false);
					return addedSection;
				}
				catch (error) {
					this.remove(config, addedSection);
					throw error;
				}
			};
			try {
				return handleAdd.call(this, ev, sectionId);
			}
			finally {
				if (hadOwnAdd)
					data.add = nativeAdd;
				else
					delete data.add;
			}
		};
		return section;
	},

	configureOrdering: function(section, collection, options) {
		const policy = uiSpec.collection_ordering?.[collection];
		if (!policy) return section;
		const disabledReason = options?.disabledReason || '';
		section.sortable = !disabledReason;
		const renderRowActions = section.renderRowActions;
		const movable = function(sectionId) {
			if (disabledReason || section.map?.readonly === true) return false;
			const object = uci.get('steer', sectionId) || {};
			if (policy.movable_kinds?.length && !policy.movable_kinds.includes(object.kind)) return false;
			return !policy.pinned_last_boolean_field || object[policy.pinned_last_boolean_field] != '1';
		};
		const compatible = function(sourceId, targetId) {
			if (!movable(sourceId) || !movable(targetId) || sourceId == targetId) return false;
			if (!policy.group_field) return true;
			const source = uci.get('steer', sourceId) || {};
			const target = uci.get('steer', targetId) || {};
			return (source[policy.group_field] || '') == (target[policy.group_field] || '');
		};
		const peers = function(sectionId) {
			const source = uci.get('steer', sectionId) || {};
			const group = policy.group_field ? (source[policy.group_field] || '') : '';
			return section.cfgsections().filter((candidateId) => {
				const candidate = uci.get('steer', candidateId) || {};
				return movable(candidateId) && (!policy.group_field || (candidate[policy.group_field] || '') == group);
			});
		};
		const captureRows = function(table) {
			return new Map(Array.from(table?.querySelectorAll?.('tr[data-sid]') || []).map((row) => [
				row.getAttribute('data-sid'), row.getBoundingClientRect()
			]));
		};
		const animateRows = function(table, positions) {
			if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
			Array.from(table?.querySelectorAll?.('tr[data-sid]') || []).forEach((row) => {
				const previous = positions.get(row.getAttribute('data-sid'));
				if (!previous) return;
				const current = row.getBoundingClientRect();
				const offsetX = previous.left - current.left;
				const offsetY = previous.top - current.top;
				if (Math.abs(offsetX) < 1 && Math.abs(offsetY) < 1) return;
				row.animate?.([
					{ transform: `translate3d(${offsetX}px, ${offsetY}px, 0)` },
					{ transform: 'translate3d(0, 0, 0)' }
				], { duration: 150, easing: 'ease' });
			});
		};
		const clearDragVisuals = function(table) {
			Array.from((table || document)?.querySelectorAll?.(
				'.steer-order-dragging, .steer-order-placeholder, .steer-order-over-before, .steer-order-over-after, .drag-over-above, .drag-over-below'
			) || []).forEach((row) => row.classList.remove(
				'steer-order-dragging', 'steer-order-placeholder', 'steer-order-over-before', 'steer-order-over-after',
				'drag-over-above', 'drag-over-below'
			));
		};
		const moveSection = function(sectionId, targetId, after) {
			if (!compatible(sectionId, targetId)) return false;
			const ordered = peers(sectionId);
			const sourcePosition = ordered.indexOf(sectionId);
			const targetPosition = ordered.indexOf(targetId);
			if (sourcePosition < 0 || targetPosition < 0 ||
				(!after && targetPosition == sourcePosition + 1) ||
				(after && targetPosition == sourcePosition - 1)) return false;
			const row = document.getElementById('cbi-%s-%s'.format(section.uciconfig || section.map.config, sectionId));
			const target = document.getElementById('cbi-%s-%s'.format(section.uciconfig || section.map.config, targetId));
			const table = row?.closest?.('table');
			const positions = captureRows(table);
			section.map.data.move(section.uciconfig || section.map.config, sectionId, targetId, after === true);
			if (row && target)
				target.parentNode.insertBefore(row, after ? target.nextElementSibling : target);
			animateRows(table, positions);
			window.dispatchEvent?.(new Event('steer-uci-state-changed'));
			return true;
		};
		const previewDesktopMove = function(drag, target, after) {
			const row = drag?.row;
			const parent = target?.parentNode;
			if (!row || !parent || row == target) return false;
			const reference = after ? target.nextElementSibling : target;
			if (reference == row || (!after && row.nextElementSibling == target)) return false;
			const positions = captureRows(drag.table);
			parent.insertBefore(row, reference);
			animateRows(drag.table, positions);
			return true;
		};
		const restoreDesktopMove = function(drag) {
			if (!drag?.row || !drag.originalParent) return;
			const positions = captureRows(drag.table);
			const reference = drag.originalNextSibling?.parentNode == drag.originalParent ? drag.originalNextSibling : null;
			drag.originalParent.insertBefore(drag.row, reference);
			animateRows(drag.table, positions);
		};

		let desktopDrag = null;
		section.handleDragStart = function(ev, row) {
			const sectionId = row?.getAttribute?.('data-sid');
			if (!sectionId || !movable(sectionId)) {
				ev.preventDefault();
				return false;
			}
			desktopDrag = {
				sectionId: sectionId, row: row, table: row.closest?.('table'),
				originalParent: row.parentNode, originalNextSibling: row.nextElementSibling,
				targetRow: null, targetId: '', after: false
			};
			ev.dataTransfer.effectAllowed = 'move';
			ev.dataTransfer.setData('text/plain', sectionId);
			const rect = row.getBoundingClientRect();
			ev.dataTransfer.setDragImage?.(row,
				Math.max(0, Math.min(rect.width, ev.clientX - rect.left)),
				Math.max(0, Math.min(rect.height, ev.clientY - rect.top)));
			row.classList.add('steer-order-dragging');
			window.requestAnimationFrame(() => {
				if (desktopDrag?.row == row) row.classList.add('steer-order-placeholder');
			});
		};
		section.handleDragEnter = function(ev) {
			const row = ev.currentTarget;
			const targetId = row?.getAttribute?.('data-sid');
			if (!desktopDrag || !compatible(desktopDrag.sectionId, targetId)) return false;
			desktopDrag.targetRow = row;
			desktopDrag.targetId = targetId;
		};
		section.handleDragOver = function(ev) {
			const row = ev.currentTarget;
			const targetId = row?.getAttribute?.('data-sid');
			if (!desktopDrag || !compatible(desktopDrag.sectionId, targetId)) return false;
			const rect = row.getBoundingClientRect();
			const after = ev.clientY >= rect.top + rect.height / 2;
			desktopDrag.targetRow?.classList?.remove('steer-order-over-before', 'steer-order-over-after');
			row.classList.add(after ? 'steer-order-over-after' : 'steer-order-over-before');
			desktopDrag.targetRow = row;
			desktopDrag.targetId = targetId;
			desktopDrag.after = after;
			previewDesktopMove(desktopDrag, row, after);
			ev.dataTransfer.dropEffect = 'move';
			ev.preventDefault();
			return false;
		};
		section.handleDragLeave = function(ev) {
			if (ev.currentTarget?.contains?.(ev.relatedTarget)) return;
			ev.currentTarget?.classList?.remove('steer-order-over-before', 'steer-order-over-after');
		};
		section.handleDrop = function(ev) {
			if (!desktopDrag) return false;
			moveSection(desktopDrag.sectionId, desktopDrag.targetId, desktopDrag.after);
			clearDragVisuals(desktopDrag.row?.closest?.('table'));
			desktopDrag = null;
			ev.stopPropagation();
			ev.preventDefault();
			return false;
		};
		section.handleDragEnd = function(_ev, row) {
			if (desktopDrag) restoreDesktopMove(desktopDrag);
			clearDragVisuals(row?.closest?.('table'));
			desktopDrag = null;
		};
		let touchDrag = null;
		const touchMoveListener = function(ev) { return section.handleTouchMove(ev); };
		const touchEndListener = function(ev) { return section.handleTouchEnd(ev); };
		const touchCancelListener = function(ev) { return section.handleTouchCancel(ev); };
		const removeTouchListeners = function() {
			window.removeEventListener?.('pointermove', touchMoveListener);
			window.removeEventListener?.('pointerup', touchEndListener);
			window.removeEventListener?.('pointercancel', touchCancelListener);
		};
		section.handleTouchStart = function(ev, row, handle) {
			if (touchDrag || (ev.button != null && ev.button !== 0)) return false;
			const sectionId = row?.getAttribute?.('data-sid');
			if (!sectionId || !movable(sectionId)) return false;
			const rect = row.getBoundingClientRect();
			const clone = row.cloneNode(true);
			clone.removeAttribute('id');
			clone.querySelectorAll('[id]').forEach((element) => element.removeAttribute('id'));
			clone.querySelectorAll('button, input, select, textarea, a').forEach((element) => {
				element.disabled = true;
				element.removeAttribute('href');
			});
			const preview = E('div', { 'class': 'touchsort-element steer-touchsort-whole-row', 'aria-hidden': 'true' }, clone);
			Object.assign(preview.style, {
				position: 'fixed', left: `${rect.left}px`, top: `${rect.top}px`,
				width: `${rect.width}px`, height: `${rect.height}px`
			});
			document.body.append(preview);
			touchDrag = {
				sectionId: sectionId, row: row, table: row.closest?.('table'), handle: handle,
				pointerId: ev.pointerId, preview: preview,
				originalParent: row.parentNode, originalNextSibling: row.nextElementSibling,
				grabOffsetX: ev.clientX - rect.left, grabOffsetY: ev.clientY - rect.top,
				targetRow: null, targetId: '', after: false
			};
			row.classList.add('steer-order-dragging');
			window.requestAnimationFrame(() => {
				if (touchDrag?.row == row) row.classList.add('steer-order-placeholder');
			});
			window.addEventListener?.('pointermove', touchMoveListener, { passive: false });
			window.addEventListener?.('pointerup', touchEndListener, { passive: false });
			window.addEventListener?.('pointercancel', touchCancelListener);
			ev.preventDefault();
			ev.stopPropagation();
			return true;
		};
		section.handleTouchMove = function(ev) {
			if (!touchDrag || touchDrag.pointerId != ev.pointerId) return false;
			touchDrag.preview.style.left = `${ev.clientX - touchDrag.grabOffsetX}px`;
			touchDrag.preview.style.top = `${ev.clientY - touchDrag.grabOffsetY}px`;
			let target = document.elementFromPoint?.(ev.clientX, ev.clientY)?.closest?.('tr[data-sid], .tr[data-sid]');
			if (!target || target == touchDrag.row) {
				target = Array.from(touchDrag.table?.querySelectorAll?.('tr[data-sid]') || [])
					.filter((candidate) => candidate != touchDrag.row)
					.find((candidate) => {
						const rect = candidate.getBoundingClientRect();
						return ev.clientY >= rect.top && ev.clientY <= (rect.bottom ?? rect.top + rect.height);
					});
			}
			const targetId = target?.getAttribute?.('data-sid');
			if (!targetId || !compatible(touchDrag.sectionId, targetId)) return false;
			const rect = target.getBoundingClientRect();
			const after = ev.clientY >= rect.top + rect.height / 2;
			touchDrag.targetRow?.classList?.remove('steer-order-over-before', 'steer-order-over-after');
			target.classList.add(after ? 'steer-order-over-after' : 'steer-order-over-before');
			touchDrag.targetRow = target;
			touchDrag.targetId = targetId;
			touchDrag.after = after;
			previewDesktopMove(touchDrag, target, after);
			ev.preventDefault();
			return false;
		};
		section.handleTouchEnd = function(ev) {
			if (!touchDrag || touchDrag.pointerId != ev.pointerId) return false;
			const drag = touchDrag;
			touchDrag = null;
			removeTouchListeners();
			if (drag.targetId)
				moveSection(drag.sectionId, drag.targetId, drag.after);
			else
				restoreDesktopMove(drag);
			drag.preview?.remove();
			clearDragVisuals(drag.table);
			ev.stopPropagation();
			ev.preventDefault();
			return false;
		};
		section.handleTouchCancel = function(ev) {
			if (!touchDrag || touchDrag.pointerId != ev.pointerId) return false;
			const drag = touchDrag;
			touchDrag = null;
			removeTouchListeners();
			restoreDesktopMove(drag);
			drag.preview?.remove();
			clearDragVisuals(drag.table);
			return false;
		};
		section.renderRowActions = function(sectionId) {
			let cell = options?.baseActions === false ? null : renderRowActions.call(this, sectionId);
			let container = cell?.querySelector?.('div');
			if (!container) {
				container = E('div');
				cell = E('td', { 'class': 'td cbi-section-table-cell nowrap cbi-section-actions' }, container);
			}
			let dragHandle = container.querySelector('.drag-handle');
			if (!dragHandle) {
				dragHandle = E('button', {
					'class': 'cbi-button drag-handle center', 'type': 'button',
					'title': disabledReason || _('Drag to reorder'),
					'disabled': disabledReason || this.map?.readonly === true ? true : null,
					'pointerdown': !disabledReason && this.map?.readonly !== true
						? (ev) => this.handleTouchStart(ev, ev.currentTarget.closest('.tr'), ev.currentTarget) : null
				}, '⠿');
				dragHandle._steerPointerOrderingBound = true;
				container.append(dragHandle);
			}
			else {
				dragHandle.title = disabledReason || _('Drag to reorder');
				dragHandle.disabled = !!disabledReason || this.map?.readonly === true;
				dragHandle.textContent = '⠿';
				dragHandle.draggable = false;
				dragHandle.removeAttribute?.('draggable');
				if (!disabledReason && this.map?.readonly !== true && !dragHandle._steerPointerOrderingBound) {
					dragHandle.addEventListener('pointerdown', (ev) => this.handleTouchStart(ev, ev.currentTarget.closest('.tr'), ev.currentTarget));
					dragHandle._steerPointerOrderingBound = true;
				}
			}
			return cell;
		};
		return section;
	},

	configureRemovalGuard: function(section, referencesFor, title) {
		const handleRemove = section.handleRemove;
		section.handleRemove = function(sectionId, ev) {
			const references = referencesFor(sectionId) || [];
			if (references.length) {
				ui.addNotification(title || _('Object is still referenced'), E('div', {}, [
					E('p', {}, _('Remove or change these references first; Steer will not cascade configuration changes.')),
					E('ul', {}, references.map((reference) => E('li', {}, [
						E('span', {}, '%s / %s'.format(reference.label || reference.id, reference.option)),
						E('button', { 'class': 'btn cbi-button-action', 'click': () => navigateIssue({
							object_type: reference.objectType, object_id: reference.id, option: reference.option
						}) }, _('Go to reference'))
					])))
				]), 'danger');
				return;
			}
			return handleRemove.call(this, sectionId, ev);
		};
		return section;
	},

	focusSection: function(section, objectType) {
		const encoded = new URLSearchParams(window.location.search || '').get('steer-focus');
		if (!encoded) return Promise.resolve(false);
		const [ requestedType, sectionId, option ] = encoded.split(':');
		if (requestedType != objectType || !sectionId) return Promise.resolve(false);
		const open = typeof(section.renderMoreOptionsModal) == 'function'
			? section.renderMoreOptionsModal(sectionId)
			: Promise.resolve();
		return Promise.resolve(open).then(() => {
			const target = document.getElementById('cbid.steer.%s.%s'.format(sectionId, option));
			const control = target?.querySelector?.('input, select, textarea, button') || target;
			control?.focus?.();
			control?.scrollIntoView?.({ block: 'center' });
			return true;
		});
	},

	apply: function(view, ev, mode) {
		let previousSequence = '';
		let previousGeneration = '';
		return L.resolveDefault(callStatus(), {})
			.then((status) => {
				previousSequence = status?.last_apply?.sequence || '';
				previousGeneration = status?.generation || '';
			})
			.then(() => view.handleSave(ev))
			.then(() => this.commitCandidate())
			.then((commit) => {
				const validation = commit?.validation;
				if (commit?.committed !== true) {
					return uci.changes()
						.then((changes) => ui.changes.renderChangeIndicator(changes))
						.then(() => notifyCandidateRejected(commit));
				}
				return uci.changes()
					.then((changes) => ui.changes.renderChangeIndicator(changes))
					.then(() => {
						window.dispatchEvent?.(new Event('steer-uci-state-changed'));
						ui.showModal(_('Applying Steer'), [ E('p', { 'class': 'spinning' }, _('Preparing and applying the configuration.')) ]);
						return waitForApply(previousSequence, 240);
					})
					.then(({ result, status }) => finishCommittedApply(this, result, status, validation, previousGeneration));
			});
	},

	applyPending: function() {
		let previousSequence = '';
		let previousGeneration = '';
		return L.resolveDefault(callStatus(), {})
			.then((status) => {
				previousSequence = status?.last_apply?.sequence || '';
				previousGeneration = status?.generation || '';
			})
			.then(() => this.commitCandidate())
			.then((commit) => {
				const validation = commit?.validation;
				if (commit?.committed !== true)
					return notifyCandidateRejected(commit);
				return uci.changes()
					.then((changes) => ui.changes.renderChangeIndicator(changes))
					.then(() => {
						window.dispatchEvent?.(new Event('steer-uci-state-changed'));
						ui.showModal(_('Applying Steer'), [ E('p', { 'class': 'spinning' }, _('Preparing and applying the configuration.')) ]);
						return waitForApply(previousSequence, 240);
					})
					.then(({ result, status }) => finishCommittedApply(this, result, status, validation, previousGeneration));
			});
	},

	discardPending: function() {
		return L.resolveDefault(callDiscardCandidate(), {})
			.then((result) => {
				if (result?.ok !== true) throw result;
				return result;
			})
			.then(() => uci.unload('steer'))
			.then(() => uci.load('steer'))
			.then(() => uci.changes())
			.then((changes) => ui.changes.renderChangeIndicator(changes))
			.then(() => window.dispatchEvent?.(new Event('steer-uci-state-changed')));
	},

	refreshStatus: function(status, validation) {
		this.mountLifecycleBar(this._lifecycleView);
	}
});
