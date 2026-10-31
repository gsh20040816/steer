#!/usr/bin/env node

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

if (typeof String.prototype.format != 'function') {
	Object.defineProperty(String.prototype, 'format', {
		value: function(...values) {
			let index = 0;
			return String(this).replace(/%[sd]/g, () => String(values[index++]));
		}
	});
}

const root = path.resolve(__dirname, '../..');
const validationIssueFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/validation-issue-fixtures.json'), 'utf8'));
const formInputFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/form-input-fixtures.json'), 'utf8'));
const creationPolicyFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/creation-policy-fixtures.json'), 'utf8'));
const uiSpec = JSON.parse(fs.readFileSync(path.join(root, 'ui/steer-ui-spec.json'), 'utf8'));

function element(tag, attributes, children) {
	if (Array.isArray(tag)) {
		children = attributes;
		attributes = {};
	}
	else if (arguments.length == 2 &&
		(Array.isArray(attributes) || typeof attributes != 'object' || attributes == null)) {
		children = attributes;
		attributes = {};
	}

	return {
		tag,
		attributes: attributes || {},
		children: children == null ? [] : (Array.isArray(children) ? children : [ children ]),
		style: {},
		addEventListener(name, listener) { this.listeners = this.listeners || {}; this.listeners[name] = listener; },
		append(child) { this.children.push(child); },
		prepend(child) { this.children.unshift(child); },
		querySelector(selector) {
			return findElement(this, (candidate) =>
				(selector == 'div' && candidate.tag == 'div') ||
				(selector.startsWith('.') && String(candidate.attributes?.class || '').split(/\s+/).includes(selector.slice(1)))
			);
		},
		remove() { this.removed = true; }
	};
}

function textContent(value) {
	if (value == null)
		return '';
	if (typeof value != 'object')
		return String(value);
	return (value.children || []).map(textContent).join(' ');
}

function findElement(value, predicate) {
	if (value == null || typeof value != 'object') return null;
	if (predicate(value)) return value;
	for (const child of value.children || []) {
		const found = findElement(child, predicate);
		if (found) return found;
	}
	return null;
}

function loadHelper(runtime) {
	const source = fs.readFileSync(path.join(root,
		'luci-app-steer/htdocs/luci-static/resources/steer.js'), 'utf8');
	const baseclass = { extend: (value) => value };
	const rpc = {
		declare: ({ object, method }) => (...args) => {
			if (method == 'commit')
				throw new Error('LuCI must validate and commit through one backend transaction');
			if (method == 'discard_candidate') {
				runtime.revertedConfigs.push('steer');
				return Promise.resolve({ ok: true });
			}
			if (method == 'status') {
				runtime.statusCalls++;
				const status = Object.assign({}, runtime.status, {
					last_apply: runtime.commitCalls > 0 ?
						{ sequence: '11', result: runtime.applyResult } :
						{ sequence: '10', result: { ok: true } }
				});
				if (runtime.commitCalls > 0 && runtime.afterApplyGeneration !== undefined)
					status.generation = runtime.afterApplyGeneration;
				return Promise.resolve(status);
			}
			if (method == 'validate') {
				runtime.validationCalls++;
				return Promise.resolve(runtime.validation);
			}
			if (method == 'access')
				return Promise.resolve(runtime.permissions?.[args[2]] !== false);
			if (method == 'commit_candidate') {
				runtime.sequence.push('validate');
				runtime.validationCalls++;
				const code = runtime.candidateValidation.ok === true ? runtime.commitStatus : null;
				const reply = runtime.commitReply;
				const committed = code === 0;
				if (committed) {
					runtime.sequence.push('commit');
					runtime.commitCalls++;
				}
				return Promise.resolve({
					committed,
					validation: runtime.candidateValidation,
					error: code == null || code === 0 ? null : `commit status ${code}; reply ${JSON.stringify(reply)}`
				});
			}
			if (method == 'intent_preview') {
				runtime.previewCalls.push(args[0]);
				return Promise.resolve(runtime.previewResponse);
			}
			if (method == 'overview_state')
				return Promise.resolve(runtime.lifecycleState);
			if (method == 'set_enabled') { runtime.sequence.push('set-enabled-rpc'); runtime.enabled = args[0] ? '1' : '0'; return Promise.resolve(runtime.setEnabledResult || { ok: true }); }
			if (method == 'subscription_nodes' || method == 'subscription_node_set') {
				runtime.inventoryCalls.push({ method, args });
				return Promise.resolve(runtime.inventoryResult);
			}
			if (method == 'apply_saved') {
				runtime.applySavedCalls++;
				return Promise.resolve(runtime.applySavedResult);
			}
			if (method == 'overview_probe' || method == 'route_speedtest' || method == 'node_speedtest') {
				runtime.testCalls.push({ method, args });
				return Promise.resolve({ ok: true });
			}
			if (method == 'apply')
				throw new Error('LuCI must not start a second Apply after UCI commit triggered procd');
			return Promise.resolve({});
		}
	};
	const ui = {
		changes: {
			apply: () => { throw new Error('ui.changes.apply() is non-awaitable and must not be used'); },
			renderChangeIndicator: () => { runtime.indicatorRefreshed = true; }
		},
		showModal: (title, content) => { runtime.modalTitle = title; runtime.modalContent = content; },
		hideModal: () => { runtime.modalHidden = true; },
		addNotification: (title, content, level) => runtime.notifications.push({ title, content, level }),
		createHandlerFn: (context, handler) => (...args) => handler.apply(context, args)
	};
	const L = {
		resolveDefault: (promise, fallback) => Promise.resolve(promise).catch(() => fallback),
		resource: (value) => value
	};
	const document = {
		body: { append: (value) => { runtime.bodyElement = value; } },
		head: { appendChild: () => {} },
		elementFromPoint: () => runtime.elementFromPoint || null,
		getElementById: (id) => id == 'steer-lifecycle-global' ? runtime.lifecycleBar
			: (id == 'maincontent' ? runtime.mainContent : (runtime.domById?.[id] || null))
	};
	const translate = (value) => String(value);
	const uci = {
		changes: () => Promise.resolve({}),
		sections: (config, type) => config == 'steer' ? (runtime.uciSectionsByType?.[type] || []) : [],
		save: () => { runtime.sequence.push('save-candidate'); return Promise.resolve(); },
		unload: (config) => { runtime.unloadedConfigs.push(config); },
		load: (config) => { runtime.loadedConfigs.push(config); return Promise.resolve(); },
		get: (config, section, option) => {
			if (config != 'steer') return null;
			if (section == 'main' && option == 'enabled') return runtime.enabled;
			const value = runtime.uciSections?.[section];
			return option == null ? value : value?.[option];
		},
		set: (config, section, option, value) => {
			if (config == 'steer' && section == 'main' && option == 'enabled') {
				runtime.enabled = value;
				runtime.lifecycleState.desired.enabled = value == '1';
				runtime.lifecycleState.pending = true;
				runtime.sequence.push('set-enabled');
			}
		}
	};
	const window = {
		addEventListener: (name, listener) => { (runtime.windowListeners ||= {})[name] = listener; },
		removeEventListener: (name) => { delete (runtime.windowListeners ||= {})[name]; },
		setTimeout: (callback) => callback(),
		requestAnimationFrame: (callback) => callback(),
		location: { pathname: '/cgi-bin/luci/admin/services/steer/nodes', reload: () => { runtime.reloaded = true; } }
	};

	return new Function('baseclass', 'rpc', 'uci', 'ui', 'uiSpec', 'E', '_', 'L', 'document', 'window', source)(
		baseclass, rpc, uci, ui, uiSpec, element, translate, L, document, window);
}

async function main() {
	const runtime = {
		status: {},
		validation: { ok: true, errors: [], warnings: [] },
		candidateValidation: { ok: true, errors: [], warnings: [] },
		commitStatus: 0,
		commitReply: { result: 'standard UCI commit reply' },
		enabled: '1',
		applyResult: { ok: true, output: 'applied' },
		applySavedResult: { ok: true, generation: 'generation-saved' },
		applySavedCalls: 0,
		statusCalls: 0,
		validationCalls: 0,
		commitCalls: 0,
		previewCalls: [],
			previewResponse: { ok: true, source: 'committed', redacted: true, intent: {} },
			permissions: {},
		testCalls: [],
		inventoryCalls: [],
		inventoryResult: { subscriptions: [] },
		sequence: [],
		notifications: [],
		revertedConfigs: [],
		unloadedConfigs: [],
		loadedConfigs: [],
		mainContent: { prepend: (value) => { runtime.lifecycleBar = value; } },
		lifecycleState: {
			ok: true, pending: true, pending_apply: false,
			desired: { enabled: true, validation: { ok: true, errors: [], warnings: [] } },
			saved: { enabled: true, digest: 'saved-a' }, active: { generation: 'generation-a', healthy: true }
		}
	};
	const helper = loadHelper(runtime);
	for (const fixture of formInputFixtures.cases) {
		assert.equal(helper.validateInput(fixture.format, fixture.value) === true, fixture.valid,
			`${fixture.format} must classify ${JSON.stringify(fixture.value)} from the shared fixture`);
	}
	for (const fixture of creationPolicyFixtures.cases) {
		const expected = { ...fixture.expected };
		delete expected.id;
		const expectedUCI = Object.fromEntries(Object.entries(expected).map(([key, value]) => [
			key, typeof value == 'boolean' ? (value ? '1' : '0') : String(value)
		]));
		assert.deepEqual(helper.creationDefaults(fixture.collection, fixture.overrides), expectedUCI,
			`${fixture.collection} helper defaults must match the shared Canonical fixture`);
	}
	const ambiguous = helper.disambiguateReferences(creationPolicyFixtures.ambiguous_references.nodes.map((node) => ({
		id: node.id, label: node.name, detail: `${node.server}:${node.server_port}`
	})));
	assert.equal(ambiguous.find((item) => item.id == 'node-unique').label, 'Unique');
	assert.match(ambiguous.find((item) => item.id == 'node-a1b2c3').label, /Same · a\.example:1080 · Duplicate 1/);
	runtime.permissions.node_import = false;
	assert.deepEqual(await helper.permissions([ 'node_import', 'node_speedtest' ]), {
		node_import: false, node_speedtest: true
	}, 'each handwritten action checks its own ubus method permission');
	runtime.permissions.node_import = true;
	runtime.permissions.write = false;
	assert.deepEqual(await helper.permissions([ 'node_import' ], true), { node_import: true, uci_write: false },
		'Node import separately requires UCI write access in addition to parser RPC access');
	runtime.permissions.write = true;
	assert.equal(helper.rpcErrorText({ error_code: 'MISSING_NODE_ID', error: 'raw backend detail' }), 'A Node ID is required.',
		'RPC failures are localized by stable code before raw detail');
	assert.equal(helper.rpcErrorText({ error_code: 'IMPORT_PROGRAM_MISSING' }), 'The Steer node parser is not installed.');
	assert.equal(helper.rpcErrorText({ error_code: 'IMPORT_PROGRAM_NOT_EXECUTABLE' }), 'The Steer node parser is not executable.');
	const sharedNodes = uiSpec.subscription_nodes;
	runtime.uciSections = {
		proxy: { '.type': 'route', '.name': 'proxy', name: 'Proxy route', kind: 'single', node: 'feed_a' },
		direct: { '.type': 'route', '.name': 'direct', kind: 'direct' }
	};
	for (const code of sharedNodes.error_codes.filter((code) => code != 'NODE_STILL_REFERENCED')) {
		assert.equal(helper.subscriptionErrorText({ error_code: code, error: `${code}: raw backend detail` }), sharedNodes.errors[code].label,
			`${code} uses the shared subscription node text, not a LuCI-only copy`);
	}
	const stillReferencedLabel = sharedNodes.errors.NODE_STILL_REFERENCED.label;
	const routesText = (names) => stillReferencedLabel.replace(sharedNodes.routes_placeholder, names.join(sharedNodes.messages.list_separator.label));
	assert.equal(helper.subscriptionErrorText({
		ok: false, error_code: 'NODE_STILL_REFERENCED', error: 'NODE_STILL_REFERENCED: route "proxy"',
		validation: { ok: false, warnings: [], errors: [
			{ code: 'DISABLED_NODE', object_type: 'route', object_id: 'proxy', option: 'node' },
			{ code: 'DISABLED_NODE', object_type: 'route', object_id: 'proxy', option: 'node' },
			{ code: 'DISABLED_NODE', object_type: 'route', object_id: 'direct', option: 'node' },
			{ code: 'DISABLED_NODE', object_type: 'route', object_id: 'saved_only', option: 'node' },
			{ code: 'OTHER', object_type: 'rule', object_id: 'rule_a', option: 'route' }
		] }
	}), routesText([ 'Proxy route', 'Direct', 'saved_only' ]),
	'NODE_STILL_REFERENCED names each route once: UCI name, system route label, else the route ID');
	assert.equal(helper.subscriptionErrorText({ error_code: 'NODE_STILL_REFERENCED', error: 'NODE_STILL_REFERENCED: raw' }),
		'NODE_STILL_REFERENCED: raw', 'without route issues the backend message is shown');
	assert.equal(helper.subscriptionErrorText({ error_code: 'NEW_BACKEND_CODE', error: 'new backend detail' }), 'new backend detail',
		'unknown codes fall back to the backend message');
	assert.equal(helper.subscriptionErrorText({ error_code: 'MISSING_SUBSCRIPTION_ID', error: 'raw' }), 'A Subscription ID is required.',
		'LuCI transport codes keep their LuCI message');
	assert.equal(helper.subscriptionErrorText(new Error('rpc failure')), 'Operation failed.');
	for (const code of sharedNodes.error_codes)
		assert.equal(helper.rpcErrorText({ error_code: code }), 'Operation failed.',
			`${code} has no LuCI-only copy in the generic RPC message table`);
	assert.equal(helper.subscriptionNodeText('read_only_note'), sharedNodes.messages.read_only_note.label);
	assert.equal(helper.subscriptionStillReferencedText([ { id: 'proxy', name: 'Saved name' }, { id: 'gone', name: 'Saved only' } ]),
		routesText([ 'Proxy route', 'Saved only' ]), 'the UCI view names a route before the Saved name');
	runtime.uciSectionsByType = { route: [
		{ '.name': 'proxy', name: 'Proxy route', kind: 'single', node: 'feed_stale' },
		{ '.name': 'other', name: 'Other', kind: 'single', node: 'feed_other' }
	] };
	assert.deepEqual(helper.subscriptionNodeCleanBlockers('feed_stale', [
		{ object_type: 'route', id: 'proxy', name: 'Proxy route (saved)' },
		{ object_type: 'route', id: 'saved_route', name: 'Saved route' }
	]), [ { id: 'proxy', name: 'Proxy route' }, { id: 'saved_route', name: 'Saved route' } ],
	'clean-up is blocked by routes in the UCI view and in the Saved configuration, each listed once');
	assert.deepEqual(helper.subscriptionNodeCleanBlockers('feed_free', []), [], 'an unreferenced stale node can be cleaned');
	delete runtime.uciSectionsByType;
	delete runtime.uciSections;
	assert.ok(helper.issueText({ code: 'SUBSCRIPTION_NODE_IN_CONFIG', object_type: 'node', object_id: 'feed_a', option: 'source_subscription' })
		.endsWith(sharedNodes.validation_issues.SUBSCRIPTION_NODE_IN_CONFIG.label),
		'the SUBSCRIPTION_NODE_IN_CONFIG validation issue uses the shared text');
	runtime.inventoryResult = { subscriptions: [ { id: 'feed', nodes: [ { id: 'feed_a', enabled: true } ] } ] };
	assert.deepEqual(await helper.subscriptionNodes(), runtime.inventoryResult);
	await helper.subscriptionNodes('feed');
	await helper.setSubscriptionNodeEnabled('feed', 'feed_a', false);
	await helper.setSubscriptionNodeEnabled('feed', 'feed_a', 1);
	assert.deepEqual(runtime.inventoryCalls, [
		{ method: 'subscription_nodes', args: [ '' ] },
		{ method: 'subscription_nodes', args: [ 'feed' ] },
		{ method: 'subscription_node_set', args: [ 'feed', 'feed_a', false ] },
		{ method: 'subscription_node_set', args: [ 'feed', 'feed_a', false ] }
	], 'subscription node RPCs forward IDs and only an explicit true enables a node');
	runtime.uciSectionsByType = {
		node: [ { '.name': 'manual' } ],
		route: [
			{ '.name': 'via_feed', name: 'Via feed', kind: 'single', node: 'feed_a' },
			{ '.name': 'via_manual', kind: 'single', node: 'manual' },
			{ '.name': 'via_other', kind: 'single', node: 'other_b' }
		]
	};
	const storedNodes = { subscriptions: [
		{ id: 'feed', nodes: [ { id: 'feed_a' }, { id: 'feed_b' } ] },
		{ id: 'other', nodes: [ { id: 'other_b' } ] }
	] };
	assert.deepEqual(helper.collectionReferences('subscriptions', 'feed', storedNodes), [
		{ objectType: 'route', id: 'via_feed', label: 'Via feed', option: 'node' }
	], 'subscription removal is guarded by routes that use nodes from the subscription node store');
	assert.deepEqual(helper.collectionReferences('subscriptions', 'missing', storedNodes), []);
	assert.deepEqual(helper.collectionReferences('subscriptions', 'feed'), [],
		'UCI node sections never define subscription ownership');
	delete runtime.uciSectionsByType;
	const unchangedLabel = uiSpec.apply_result.unchanged_notice.label;
	assert.equal(helper.applyUnchangedNotice({ ok: true, activated: true, restarted: false }), unchangedLabel);
	for (const result of [ { ok: true, activated: true, restarted: true }, { ok: true, activated: false, restarted: false },
		{ ok: false, activated: true, restarted: false }, { ok: true, activated: true } ])
		assert.equal(helper.applyUnchangedNotice(result), '', `no unchanged notice for ${JSON.stringify(result)}`);
	const currentView = { handleSave: () => { runtime.sequence.push('capture-form'); return Promise.resolve(); } };
	helper.loadStyle(currentView);
	await new Promise((resolve) => setImmediate(resolve));
	assert.ok(textContent(runtime.lifecycleBar).includes('Steer status'));
	assert.ok(textContent(runtime.lifecycleBar).includes('Enable Steer'));
	assert.ok(textContent(runtime.lifecycleBar).includes('Working copy'));
	assert.ok(textContent(runtime.lifecycleBar).includes('Save & Apply pending changes'));
	assert.equal(findElement(runtime.lifecycleBar, (node) => node.attributes?.role == 'switch')?.attributes?.['aria-checked'], 'true',
		'the global status area exposes the desired Enable state as an accessible switch');
	runtime.lifecycleState = {
		ok: true, pending: false, pending_apply: true,
		desired: { enabled: true, validation: { ok: true, errors: [], warnings: [] } },
		saved: { enabled: true, digest: 'saved-b' }, active: { generation: 'generation-old', healthy: true }
	};
	helper.loadStyle(currentView);
	await new Promise((resolve) => setImmediate(resolve));
	assert.ok(textContent(runtime.lifecycleBar).includes('Apply Saved configuration'),
		'every configuration page exposes Apply Saved when the runtime projection is pending');
	runtime.sequence = [];
	runtime.lifecycleState.pending = false;
	runtime.lifecycleState.pending_apply = false;
	await helper.setGlobalEnabled(false, currentView);
	assert.deepEqual(runtime.sequence, [ 'set-enabled-rpc' ],
		'global Enable only changes the committed enabled field and preserves pending edits');
	assert.equal(runtime.enabled, '0');
	runtime.lifecycleState = {
		ok: true, pending: true, pending_apply: false,
		desired: { enabled: false, validation: { ok: false, errors: [ { code: 'DANGLING_ROUTE' } ], warnings: [] } },
		saved: { enabled: true }, active: { generation: 'generation-a', healthy: true }
	};
	helper.loadStyle(currentView);
	await new Promise((resolve) => setImmediate(resolve));
	const invalidToggle = findElement(runtime.lifecycleBar, (node) => node.attributes?.role == 'switch');
	assert.equal(invalidToggle.attributes.disabled, null);
	assert.equal(invalidToggle.attributes.title, 'Apply Saved configuration',
		'an invalid candidate blocks global Enable with a stable reason');
	runtime.lifecycleState = {
		ok: true, pending: false, pending_apply: true,
		desired: { enabled: false, validation: { ok: true, errors: [], warnings: [] } },
		saved: { enabled: false }, active: { generation: 'generation-a', healthy: true }
	};
	helper.loadStyle(currentView);
	await new Promise((resolve) => setImmediate(resolve));
	const partialText = textContent(runtime.lifecycleBar);
	assert.ok(partialText.includes('Saved configuration') && partialText.includes('Disabled') &&
		partialText.includes('Active configuration') && partialText.includes('Running normally'),
		'the global status area keeps Saved Enable separate from the actual Active runtime');
	runtime.sequence = [];
	runtime.commitCalls = 0;
	runtime.statusCalls = 0;
	runtime.validationCalls = 0;
	runtime.enabled = '1';
	runtime.lifecycleState = {
		ok: true, pending: false, pending_apply: false,
		desired: { enabled: true, validation: { ok: true, errors: [], warnings: [] } },
		saved: { enabled: true }, active: { generation: 'generation-a', healthy: true }
	};
	await helper.intentPreview(false);
	await helper.intentPreview(true);
	assert.deepEqual(runtime.previewCalls, [ false, true ],
		'Canonical Preview forwards only the explicit temporary reveal decision');
	const addedSections = [];
	const removedSections = [];
	const modalSections = [];
	const sectionValues = {};
	const namedData = {
		add: (...args) => { addedSections.push(args); return args[2]; },
		set: (_config, sectionId, option, value) => { (sectionValues[sectionId] ||= {})[option] = value; },
		remove: (_config, sectionId) => { removedSections.push(sectionId); delete sectionValues[sectionId]; }
	};
	const namedSection = {
		sectiontype: 'rule',
		map: {
			config: 'steer',
			data: namedData,
			save: () => { throw new Error('Add must not persist a provisional section before modal Save'); }
		},
		handleAdd: function(_ev, sectionId) {
			const added = this.map.data.add(this.map.config, this.sectiontype, sectionId);
			this.map.addedSection = added;
			return this.renderMoreOptionsModal(added);
		},
		renderMoreOptionsModal: function(sectionId) {
			modalSections.push({ sectionId, values: { ...(sectionValues[sectionId] || {}) } });
			return Promise.resolve(sectionId);
		},
		handleModalCancel: function(_modalMap, _ev, isSaving) {
			if (this.map.addedSection != null && !isSaving)
				this.map.data.remove(this.map.config, this.map.addedSection);
			delete this.map.addedSection;
		}
	};
	helper.configureNamedSection(namedSection, { enabled: '1' });
	assert.equal(namedSection.anonymous, false, 'Steer-owned sections retain stable named IDs');
	assert.equal(namedSection.autoIDs, true, 'regular Add generates the stable ID instead of asking the user');
	assert.equal(namedSection.handleAdd(null, 'Rule-A'), undefined,
		'Invalid UCI section IDs fail before saving');
	assert.equal(runtime.notifications.at(-1).level, 'danger');
	await namedSection.handleAdd(null, 'laptop_direct');
	assert.deepEqual(addedSections, [ [ 'steer', 'rule', 'laptop_direct' ] ],
		'Valid section IDs create one provisional named UCI section');
	assert.deepEqual(modalSections, [ { sectionId: 'laptop_direct', values: {
		enabled: '1', default: '0', dns_profile: '', route: 'direct'
	} } ],
		'Grid Add opens the native editor modal with defaults already available');
	namedSection.handleModalCancel(null, null, false);
	assert.deepEqual(removedSections, [ 'laptop_direct' ],
		'Cancelling the native editor removes the provisional section without a pending row');

	const ruleOrder = [ 'default' ];
	const savedRuleOrders = [];
	const moveCalls = [];
	const orderedRuleSection = {
		sectiontype: 'rule',
		map: {
			config: 'steer',
			data: {
				add: (config, type, sectionId) => {
					assert.deepEqual([ config, type ], [ 'steer', 'rule' ]);
					ruleOrder.push(sectionId);
					return sectionId;
				},
				set: () => {},
				move: (config, sectionId, targetId, after) => {
					moveCalls.push([ config, sectionId, targetId, after ]);
					const from = ruleOrder.indexOf(sectionId);
					if (from < 0) return false;
					ruleOrder.splice(from, 1);
					const target = ruleOrder.indexOf(targetId);
					if (target < 0) return false;
					ruleOrder.splice(target + (after ? 1 : 0), 0, sectionId);
					return true;
				}
			},
			save: () => { throw new Error('Add must wait for the modal Save action'); }
		},
		handleAdd: function(_ev, sectionId) {
			const added = this.map.data.add(this.map.config, this.sectiontype, sectionId);
			this.map.addedSection = added;
			return this.renderMoreOptionsModal(added);
		},
		renderMoreOptionsModal: function(sectionId) {
			savedRuleOrders.push([ ...ruleOrder ]);
			return Promise.resolve(sectionId);
		}
	};
	helper.configureNamedSection(orderedRuleSection, null, 'default');
	await orderedRuleSection.handleAdd(null, 'first_rule');
	await orderedRuleSection.handleAdd(null, 'second_rule');
	assert.deepEqual(savedRuleOrders, [
		[ 'first_rule', 'default' ],
		[ 'first_rule', 'second_rule', 'default' ]
	], 'Each new Rule opens its modal only after being provisionally ordered before Default');
	assert.deepEqual(moveCalls, [
		[ 'steer', 'first_rule', 'default', false ],
		[ 'steer', 'second_rule', 'default', false ]
	], 'Named rule creation explicitly moves each new UCI section before Default');

	runtime.uciSections = {
		node_a: { '.name': 'node_a' },
		node_b: { '.name': 'node_b' }
	};
	const nodeOrder = [ 'node_a', 'node_b' ];
	const orderingMoves = [];
	const orderedNodeSection = {
		sectiontype: 'node',
		map: {
			config: 'steer',
			data: {
				move: (config, sectionId, targetId, after) => {
					orderingMoves.push([ config, sectionId, targetId, after ]);
					const sourceIndex = nodeOrder.indexOf(sectionId);
				nodeOrder.splice(sourceIndex, 1);
				const targetIndex = nodeOrder.indexOf(targetId);
				nodeOrder.splice(targetIndex + (after ? 1 : 0), 0, sectionId);
			}
			}
		},
		cfgsections: () => nodeOrder.slice(),
		renderRowActions: () => element('td', {}, element('div'))
	};
	helper.configureOrdering(orderedNodeSection, 'nodes');
	const nodeBActions = orderedNodeSection.renderRowActions('node_b');
	const nodeBDragHandle = findElement(nodeBActions, (candidate) => candidate.attributes?.class?.includes('drag-handle'));
	assert.ok(nodeBDragHandle,
		'a movable row exposes one direct drag handle');
	assert.equal(nodeBDragHandle.attributes.disabled, null,
		'an enabled LuCI drag handle omits the HTML boolean disabled attribute');
	assert.equal(findElement(nodeBActions, (candidate) => candidate.attributes?.['data-steer-order'] != null), null,
		'LuCI ordering does not add separate up/down arrow buttons');
	const nativeHandle = element('button', { 'class': 'cbi-button drag-handle', draggable: true }, '☰');
	const nativeSection = {
		sectiontype: 'node', map: orderedNodeSection.map, cfgsections: orderedNodeSection.cfgsections,
		renderRowActions: () => element('td', {}, element('div', {}, nativeHandle))
	};
	helper.configureOrdering(nativeSection, 'nodes');
	nativeSection.renderRowActions('node_b');
	assert.equal(nativeHandle.draggable, false);
	assert.equal(nativeHandle.textContent, '⠿');
	assert.equal(typeof nativeHandle.listeners?.pointerdown, 'function',
		'existing LuCI GridSection handles are rebound to the unified pointer drag path');
	const animations = [];
	const tbody = {
		children: [],
		insertBefore(child, reference) {
			const previous = this.children.indexOf(child);
			if (previous >= 0) this.children.splice(previous, 1);
			const next = reference == null ? this.children.length : this.children.indexOf(reference);
			this.children.splice(next < 0 ? this.children.length : next, 0, child);
			this.children.forEach((row, index) => {
				row.parentNode = this;
				row.nextElementSibling = this.children[index + 1] || null;
			});
		}
	};
	const table = {
		querySelectorAll: (selector) => selector == 'tr[data-sid]' ? tbody.children : []
	};
	const dragRow = (sectionId) => {
		const classes = new Set();
		return {
			cloneNode: () => ({ removeAttribute() {}, querySelectorAll: () => [] }),
			getAttribute: (name) => name == 'data-sid' ? sectionId : null,
			getBoundingClientRect: function() {
				const top = tbody.children.indexOf(this) * 40;
				return { top, bottom: top + 40, left: 0, width: 600, height: 40 };
			},
			closest: (selector) => selector == 'table' ? table : null,
			contains: () => false,
			animate: (frames, options) => animations.push({ sectionId, frames, options }),
			classList: {
				add: (...names) => names.forEach((name) => classes.add(name)),
				remove: (...names) => names.forEach((name) => classes.delete(name)),
				contains: (name) => classes.has(name)
			}
		};
	};
	const rowA = dragRow('node_a');
	const rowB = dragRow('node_b');
	tbody.children = [ rowA, rowB ];
	tbody.children.forEach((row, index) => { row.parentNode = tbody; row.nextElementSibling = tbody.children[index + 1] || null; });
	runtime.domById = { 'cbi-steer-node_a': rowA, 'cbi-steer-node_b': rowB };
	const pointerEvent = (overrides = {}) => ({
		button: 0, pointerId: 7, clientX: 10, clientY: 50,
		preventDefault() {}, stopPropagation() {}, ...overrides
	});
	orderedNodeSection.handleTouchStart(pointerEvent(), rowB, {});
	runtime.elementFromPoint = rowA;
	orderedNodeSection.handleTouchMove(pointerEvent({ clientY: 5 }));
	assert.deepEqual(tbody.children, [ rowB, rowA ], 'dragover moves the whole row before Drop so peers yield in real time');
	assert.equal(orderingMoves.length, 0, 'drag preview does not mutate UCI');
	assert.ok(new Set(animations.map((entry) => entry.sectionId)).has('node_a') &&
		new Set(animations.map((entry) => entry.sectionId)).has('node_b'),
		'all displaced rows receive the same FLIP animation');
	orderedNodeSection.handleTouchEnd(pointerEvent({ clientY: 5 }));
	assert.deepEqual(orderingMoves, [ [ 'steer', 'node_b', 'node_a', false ] ],
		'pointer Drop writes exactly one stable-ID UCI move');
	assert.deepEqual(nodeOrder, [ 'node_b', 'node_a' ]);
	const movesBeforeCancel = orderingMoves.length;
	orderedNodeSection.handleTouchStart(pointerEvent({ clientY: 10 }), rowB, {});
	runtime.elementFromPoint = rowA;
	orderedNodeSection.handleTouchMove(pointerEvent({ clientY: 75 }));
	orderedNodeSection.handleTouchCancel(pointerEvent({ clientY: 75 }));
	assert.equal(orderingMoves.length, movesBeforeCancel, 'a cancelled LuCI drag never mutates pending UCI');
	assert.deepEqual(tbody.children, [ rowB, rowA ], 'a cancelled LuCI drag restores the original DOM order');
	assert.deepEqual(runtime.windowListeners, {}, 'pointer completion and cancellation remove global drag listeners');
	const sortedNodeSection = {
		sectiontype: 'node', map: orderedNodeSection.map,
		cfgsections: orderedNodeSection.cfgsections,
		renderRowActions: () => element('td', {}, element('div'))
	};
	helper.configureOrdering(sortedNodeSection, 'nodes', {
		disabledReason: 'Click the Order header before changing configured order.'
	});
	const sortedActions = sortedNodeSection.renderRowActions('node_a');
	assert.equal(sortedNodeSection.sortable, false, 'display sorting disables native persistent header/drag sorting');
	assert.equal(findElement(sortedActions, (candidate) => candidate.attributes?.class?.includes('drag-handle')).attributes.disabled, true,
		'display sorting disables drag with one compact accessible reason');
	runtime.domById = null;
	delete runtime.uciSections;

	runtime.status = { healthy: true };
	runtime.lifecycleState.active = { generation: 'generation-new', healthy: true };
	await helper.apply({ handleSave: () => { runtime.sequence.push('save'); return Promise.resolve(); } }, null, '1');
	await new Promise((resolve) => setImmediate(resolve));
	assert.deepEqual(runtime.sequence, [ 'save', 'validate', 'commit' ],
		'LuCI backend validates and commits the pending UCI session in one RPC before leaving Apply to procd');
	assert.equal(runtime.indicatorRefreshed, true, 'LuCI refreshes the pending change indicator after commit');
	assert.equal(runtime.modalTitle, 'Applying Steer',
		'Apply shows an explicit transaction progress modal');
	assert.equal(runtime.modalHidden, true, 'Apply closes the progress modal after RPC completion');
	assert.equal(runtime.statusCalls, 2, 'LuCI snapshots the Apply sequence and observes exactly one newer result');
	assert.equal(runtime.validationCalls, 1, 'LuCI validates the candidate once before commit');
	assert.ok(textContent(runtime.lifecycleBar).includes('Running normally'),
		'Apply refreshes the global lifecycle status from the final runtime result');
	assert.equal(runtime.notifications.at(-1).level, 'info');

	runtime.sequence = [];
	runtime.commitCalls = 0;
	runtime.applyResult = { ok: false, activated: false, error: 'start candidate: systemd refused start' };
	const activationFailure = await helper.applyPending();
	assert.equal(activationFailure.saved, true);
	assert.equal(activationFailure.active_unchanged, true);
	const activationNotice = runtime.notifications.at(-1);
	assert.equal(activationNotice.title, 'Configuration saved; application failed');
	assert.ok(textContent(activationNotice.content).includes('previous running configuration') &&
		textContent(activationNotice.content).includes('Apply Saved configuration') &&
		!textContent(activationNotice.content).includes('systemd refused start'),
		'activation failure explains commit-before-apply and the recovery action on every page');

	runtime.sequence = [];
	runtime.commitCalls = 0;
	runtime.applyResult = { ok: true, activated: false };
	runtime.enabled = '0';
	const disabledResult = await helper.applyPending();
	assert.equal(disabledResult.saved, true);
	assert.ok(textContent(runtime.notifications.at(-1).content).includes('runtime resources were cleaned up'),
		'disabled cleanup has a distinct successful result');
	runtime.enabled = '1';
	runtime.applyResult = { ok: true, activated: true };

	runtime.sequence = [];
	runtime.commitCalls = 0;
	runtime.status.generation = 'generation-old';
	runtime.afterApplyGeneration = 'generation-candidate';
	runtime.applyResult = { ok: false, activated: true, error: 'candidate became unhealthy' };
	const changedActiveFailure = await helper.applyPending();
	assert.equal(changedActiveFailure.active_unchanged, false);
	assert.equal(runtime.notifications.at(-1).title, 'Configuration saved; application failed');
	assert.ok(textContent(runtime.notifications.at(-1).content).includes('Check Diagnostics before relying on traffic stability') &&
		!textContent(runtime.notifications.at(-1).content).includes('generation-candidate'),
		'a partial activation never falsely promises that the previous Active generation is still carrying traffic');
	delete runtime.afterApplyGeneration;
	delete runtime.status.generation;
	runtime.applyResult = { ok: true, activated: true };

	runtime.sequence = [];
	await helper.applyPending();
	assert.deepEqual(runtime.sequence, [ 'validate', 'commit' ],
		'Overview Save & Apply commits the existing rpcd candidate without inventing a second form save');
	const applySavedResult = await helper.applySaved();
	assert.equal(applySavedResult.generation, 'generation-saved');
	assert.equal(runtime.applySavedCalls, 1, 'Apply Saved uses its restricted RPC without touching pending form data');
	await helper.discardPending();
	assert.deepEqual(runtime.revertedConfigs, [ 'steer' ]);
	assert.deepEqual(runtime.unloadedConfigs, [ 'steer' ]);
	assert.deepEqual(runtime.loadedConfigs, [ 'steer' ]);

	runtime.commitCalls = 0;
	runtime.applyResult = { ok: true, activated: true, restarted: false };
	await helper.applyPending();
	const unchangedApply = textContent(runtime.notifications.at(-1).content);
	assert.equal(runtime.notifications.at(-1).level, 'info');
	assert.ok(unchangedApply.includes('Steer configuration saved and activated.') && unchangedApply.includes(unchangedLabel),
		'Save & Apply explains that an unchanged runtime kept running without a restart');
	runtime.commitCalls = 0;
	runtime.applyResult = { ok: true, activated: true, restarted: true };
	await helper.applyPending();
	assert.ok(!textContent(runtime.notifications.at(-1).content).includes(unchangedLabel),
		'a restarted runtime does not show the unchanged notice');
	runtime.applySavedResult = { ok: true, activated: true, restarted: false };
	await helper.applySaved();
	assert.ok(textContent(runtime.notifications.at(-1).content).includes(unchangedLabel),
		'Apply Saved shows the shared unchanged-runtime notice');
	const noticesBeforeRestart = runtime.notifications.length;
	runtime.applySavedResult = { ok: true, activated: true, restarted: true };
	await helper.applySaved();
	assert.equal(runtime.notifications.length, noticesBeforeRestart, 'a restarting Apply Saved adds no unchanged notice');
	runtime.setEnabledResult = { ok: true, saved: true, applied: true, activated: true, restarted: false };
	await helper.setGlobalEnabled(true, currentView);
	assert.ok(textContent(runtime.notifications.at(-1).content).includes(unchangedLabel),
		'the global Enable switch shows the shared unchanged-runtime notice');
	delete runtime.setEnabledResult;
	runtime.applyResult = { ok: true, activated: true };

	runtime.sequence = [];
	runtime.commitCalls = 0;
	runtime.candidateValidation = { ok: true, errors: [], warnings: validationIssueFixtures.validation.warnings };
	await helper.applyPending();
	assert.equal(runtime.notifications.at(-1).level, 'warning');
	assert.ok(textContent(runtime.notifications.at(-1).content).includes('connection-stage') &&
		!textContent(runtime.notifications.at(-1).content).includes('DNS_PROJECTION_EMPTY'),
		'non-blocking write warnings remain visible after a successful Apply');

	runtime.sequence = [];
	runtime.candidateValidation = validationIssueFixtures.validation;
	const commitsBeforeInvalidCandidate = runtime.commitCalls;
	const invalidResult = await helper.apply({
		handleSave: () => { runtime.sequence.push('save'); return Promise.resolve(); }
	}, null, '1');
	assert.deepEqual(runtime.sequence, [ 'save', 'validate' ],
		'An invalid candidate is backend-validated without reaching UCI commit');
	assert.equal(runtime.commitCalls, commitsBeforeInvalidCandidate,
		'Unknown Geo selectors remain pending and are never persisted');
	assert.equal(invalidResult.saved, false);
	assert.equal(runtime.notifications.at(-1).level, 'danger');
	const validationText = textContent(runtime.notifications.at(-1).content);
	for (const issue of [ ...runtime.candidateValidation.errors, ...runtime.candidateValidation.warnings ]) {
		assert.ok(!validationText.includes(issue.code) && !validationText.includes(issue.object_id));
		const optionLabel = { node: 'Node', route: 'Route', dns_profile: 'DNS profile' }[issue.option];
		if (optionLabel) assert.ok(validationText.includes(optionLabel));
		else assert.ok(!validationText.includes(issue.option));
	}
	assert.ok(validationText.includes('Go to field'),
		'Every located write issue offers direct object/field navigation');
	for (const secret of validationIssueFixtures.forbidden_message_values) assert.ok(!validationText.includes(secret));

	runtime.sequence = [];
	runtime.candidateValidation = { ok: true, errors: [], warnings: [] };
	runtime.commitStatus = 4;
	const commitsBeforeRPCFailure = runtime.commitCalls;
	const commitFailure = await helper.apply({
		handleSave: () => { runtime.sequence.push('save'); return Promise.resolve(); }
	}, null, '1');
	assert.deepEqual(runtime.sequence, [ 'save', 'validate' ],
		'A nonzero ubus callback status is not confused with its reply object');
	assert.equal(runtime.commitCalls, commitsBeforeRPCFailure,
		'A failed standard UCI commit is never reported as committed');
	assert.equal(commitFailure.saved, false);
	assert.ok(textContent(runtime.notifications.at(-1).content).includes('Operation failed.') &&
		!textContent(runtime.notifications.at(-1).content).includes('commit status 4'));
	runtime.commitStatus = 0;

	await helper.overviewProbe('direct');
	await helper.routeSpeedtest('route_a', true);
	await helper.speedtest('node_a', false);
	assert.deepEqual(runtime.testCalls, [
		{ method: 'overview_probe', args: [ 'direct' ] },
		{ method: 'route_speedtest', args: [ 'route_a', true ] },
		{ method: 'node_speedtest', args: [ 'node_a', false ] }
	], 'LuCI helper forwards every diagnostic argument unchanged');

	console.log('Steer LuCI helper regression tests passed.');
}

main().catch((error) => {
	console.error(error.stack || error);
	process.exit(1);
});
