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
const uiSpec = JSON.parse(fs.readFileSync(path.join(root, 'ui/steer-ui-spec.json'), 'utf8'));
const localProxyFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/local-proxy-listen-fixtures.json'), 'utf8'));
const subscriptionStatusFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/subscription-status-fixtures.json'), 'utf8'));
const probeDiagnosticsFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/probe-diagnostics-fixtures.json'), 'utf8'));
const stateLifecycleFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/state-lifecycle-fixtures.json'), 'utf8'));
const validationIssueFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/validation-issue-fixtures.json'), 'utf8'));
const formInputFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/form-input-fixtures.json'), 'utf8'));
const collectionReferenceFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-reference-fixtures.json'), 'utf8'));
const ruleSummaryFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/rule-summary-fixtures.json'), 'utf8'));
const creationPolicyFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/creation-policy-fixtures.json'), 'utf8'));
const collectionOrderingFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-ordering-fixtures.json'), 'utf8'));
const collectionDragFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-drag-fixtures.json'), 'utf8'));
const nodeDisplaySortingFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/node-display-sorting-fixtures.json'), 'utf8'));

function parseUCIConfig(content) {
	const sections = {};
	let current = null;
	for (const raw of String(content).split(/\r?\n/)) {
		const line = raw.trim();
		let match = line.match(/^config\s+(\w+)\s+'([^']+)'$/);
		if (match) {
			current = { '.name': match[2] };
			(sections[match[1]] ||= []).push(current);
			continue;
		}
		match = line.match(/^(option|list)\s+(\w+)\s+'([^']*)'$/);
		if (!match || !current)
			continue;
		if (match[1] == 'list')
			(current[match[2]] ||= []).push(match[3]);
		else
			current[match[2]] = match[3];
	}
	return sections;
}

function canonicalFixtureSections(intent) {
	const result = {};
	for (const [collection, values] of Object.entries(intent)) {
		const type = { nodes: 'node', routes: 'route', dns_profiles: 'dns_profile', local_proxies: 'local_proxy', subscriptions: 'subscription', rules: 'rule' }[collection];
		if (!type) continue;
		result[type] = values.map((value) => {
			const section = { '.name': value.id };
			for (const [key, item] of Object.entries(value)) {
				if (key == 'id') continue;
				section[key] = typeof(item) == 'boolean' ? (item ? '1' : '0') : item;
			}
			return section;
		});
	}
	return result;
}

function environmentForCreationDefaults(collection, overrides) {
	return Object.fromEntries(Object.entries({
		...(uiSpec.creation_defaults[collection] || {}), ...(overrides || {})
	}).map(([key, value]) => [key, typeof value == 'boolean' ? (value ? '1' : '0') : String(value)]));
}

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

	const values = children == null ? [] : (Array.isArray(children) ? children : [ children ]);
	assert.ok(!values.includes(null), 'LuCI element children must not contain null');
	const node = {
		tag,
		attributes: attributes || {},
		children: values,
		disabled: !!attributes?.disabled,
		hidden: attributes?.hidden != null,
		title: attributes?.title || '',
		value: attributes?.value || '',
		placeholder: attributes?.placeholder || '',
		listeners: {},
		get textContent() { return this._textContent ?? this.children.map((child) => typeof child == 'string' ? child : (child?.textContent || '')).join(' '); },
		set textContent(value) { this._textContent = String(value); },
		replaceChildren: function(...replacement) { this.children = replacement; },
		appendChild: function(child) { this.children.push(child); return child; },
		addEventListener: function(name, listener) {
			(this.listeners[name] ||= []).push(listener);
		},
		setAttribute: function(name, value) { this.attributes[name] = String(value); },
		getAttribute: function(name) { return this.attributes[name]; },
		hasAttribute: function(name) { return Object.hasOwn(this.attributes, name); },
		focus: function() { this.focused = true; },
		matches: function(selector) { return selector == this.tag; },
		querySelector: function(selector) {
			if (selector.startsWith('.')) {
				const className = selector.slice(1);
				return findElements(this, (candidate) => String(candidate.attributes?.class || '').split(/\s+/).includes(className))[0] || null;
			}
			return findElements(this, (candidate) => candidate !== this && candidate.tag == selector)[0] || null;
		},
		querySelectorAll: function(selector) {
			if (selector == 'thead th') return findElements(this, (candidate) => candidate.tag == 'th');
			return findElements(this, (candidate) => candidate !== this && candidate.tag == selector);
		}
	};
	node.classList = {
		contains: (name) => String(node.attributes.class || '').split(/\s+/).includes(name),
		add: (...names) => {
			const values = new Set(String(node.attributes.class || '').split(/\s+/).filter(Boolean));
			names.forEach((name) => values.add(name));
			node.attributes.class = [ ...values ].join(' ');
		},
		remove: (...names) => {
			const removed = new Set(names);
			node.attributes.class = String(node.attributes.class || '').split(/\s+/)
				.filter((name) => name && !removed.has(name)).join(' ');
		},
		toggle: (name, force) => {
			const enabled = force == null ? !node.classList.contains(name) : !!force;
			if (enabled) node.classList.add(name); else node.classList.remove(name);
			return enabled;
		}
	};
	return node;
}

function elementText(value) {
	if (value == null)
		return '';
	if (typeof(value) != 'object')
		return String(value);
	if (value.textContent != null)
		return String(value.textContent);
	return (value.children || []).map(elementText).join(' ');
}

function findElements(root, predicate) {
	if (root == null || typeof root != 'object')
		return [];
	const matches = predicate(root) ? [ root ] : [];
	return matches.concat((root.children || []).flatMap((child) => findElements(child, predicate)));
}

function createEnvironment(sections) {
	const maps = [];
	let pendingChanges = [];
	class DynamicList {}
	DynamicList.prototype.renderWidget = function() {};
	DynamicList.extend = function(definition) {
		class ExtendedDynamicList extends DynamicList {}
		Object.assign(ExtendedDynamicList.prototype, definition);
		return ExtendedDynamicList;
	};
	class TextValue {}
	TextValue.prototype.renderWidget = function(sectionId) {
		const textarea = element('textarea');
		textarea.value = typeof(this.cfgvalue) == 'function' ? this.cfgvalue(sectionId) : '';
		return textarea;
	};
	TextValue.extend = function(definition) {
		class ExtendedTextValue extends TextValue {}
		Object.assign(ExtendedTextValue.prototype, definition);
		return ExtendedTextValue;
	};

	class Option {
		constructor(type, name) {
			this.type = type;
			this.name = name;
			this.values = [];
			this.dependencies = [];
			this.formValues = {};
		}

		value(key, label, description) {
			const value = [ String(key), String(label == null ? key : label) ];
			if (description != null)
				value.push(String(description));
			this.values.push(value);
		}

		depends(...values) { this.dependencies.push(values); }

		cfgvalue(sectionId) { return uci.get('steer', sectionId, this.name); }

		formvalue(sectionId) {
			return Object.hasOwn(this.formValues, sectionId) ? this.formValues[sectionId] : this.cfgvalue(sectionId);
		}

		submit(sectionId, value) {
			/* RichListValue passes only `optional` to LuCI's Dropdown widget. */
			if (this.type == 'RichListValue' && (value == null || value === '') && this.optional !== true)
				return Promise.reject(new TypeError(`${this.name} must not be empty`));

			if (value == null || value === '') {
				if (this.rmempty || this.optional)
					return Promise.resolve(this.remove(sectionId));
				return Promise.reject(new TypeError(`${this.name} must not be empty`));
			}

			return Promise.resolve(this.write(sectionId, value));
		}

		write(sectionId, value) {
			uci.set('steer', sectionId, this.name, value);
		}

		remove(sectionId) {
			uci.unset('steer', sectionId, this.name);
		}
	}

	class Section {
		constructor(type, sectionType) {
			this.type = type;
			this.sectionType = sectionType;
			this.options = [];
			this.tabs = [];
		}

		tab(name) { this.tabs.push(name); }

		option(type, name) {
			const option = new Option(type, name);
			this.options.push(option);
			return option;
		}

		taboption(tab, type, name) {
			return this.option(type, name);
		}

		renderSectionAdd() {
			const input = element('input', { 'class': 'cbi-section-create-name' });
			const button = element('button', { 'class': 'cbi-button-add', disabled: true }, 'Add');
			input.addEventListener('keyup', () => { button.disabled = input.value == ''; });
			return element('div', { 'class': 'cbi-section-create' }, [ input, button ]);
		}
	}

	class Map {
		constructor() {
			this.sections = [];
			maps.push(this);
		}

		section(type, ...arguments_) {
			const sectionType = type == 'NamedSection' ? arguments_[1] : arguments_[0];
			const section = new Section(type, sectionType);
			if (type == 'NamedSection') {
				section.sectionId = arguments_[0];
				section.title = arguments_[2];
			}
			else {
				section.title = arguments_[1];
			}
			this.sections.push(section);
			return section;
		}

		render() {
			return Promise.resolve(element('form'));
		}
	}

	const form = {
		Map,
		GridSection: 'GridSection',
		TableSection: 'TableSection',
		NamedSection: 'NamedSection',
		Flag: 'Flag',
		Value: 'Value',
		DummyValue: 'DummyValue',
		ListValue: 'ListValue',
		RichListValue: 'RichListValue',
		MultiValue: 'MultiValue',
		DynamicList,
		TextValue,
		Button: 'Button'
	};
	const uci = {
		load: () => Promise.resolve(),
		changes: () => Promise.resolve({ steer: pendingChanges }),
		sections: (config, type) => (sections[type] || []).map((section) => ({ ...section })),
		add: (config, type, sectionId) => {
			if (Object.values(sections).flat().some((section) => section['.name'] == sectionId))
				throw new Error(`section ${sectionId} already exists`);
			const section = { '.name': sectionId };
			(sections[type] ||= []).push(section);
			return sectionId;
		},
		remove: (config, sectionId) => {
			for (const values of Object.values(sections)) {
				const index = values.findIndex((section) => section['.name'] == sectionId);
				if (index >= 0) values.splice(index, 1);
			}
		},
		save: () => Promise.resolve(),
		get: (config, sectionId, option) => {
			const section = Object.values(sections).flat()
				.find((candidate) => candidate['.name'] == sectionId);
			return option == null ? section : section?.[option];
		},
		set: (config, sectionId, option, value) => {
			const section = Object.values(sections).flat()
				.find((candidate) => candidate['.name'] == sectionId);
			section[option] = value;
		},
		unset: (config, sectionId, option) => {
			const section = Object.values(sections).flat()
				.find((candidate) => candidate['.name'] == sectionId);
			delete section[option];
		},
		move: (config, sectionId, targetId, after) => {
			for (const values of Object.values(sections)) {
				const sourceIndex = values.findIndex((section) => section['.name'] == sectionId);
				const targetIndex = values.findIndex((section) => section['.name'] == targetId);
				if (sourceIndex < 0 || targetIndex < 0) continue;
				const [ source ] = values.splice(sourceIndex, 1);
				const adjustedTarget = values.findIndex((section) => section['.name'] == targetId);
				values.splice(adjustedTarget + (after ? 1 : 0), 0, source);
				return true;
			}
			return false;
		}
	};
	const view = { extend: (value) => value };
	const speedtestCalls = [];
	const routeSpeedtestCalls = [];
	const overviewProbeCalls = [];
	const importNodeCalls = [];
	const exportNodeCalls = [];
	const cleanSubscriptionCalls = [];
	const updateSubscriptionCalls = [];
	const notifications = [];
	let statusRenderCalls = 0;
	const steer = {
		loadStyle: () => {},
		uiSpecLabel: (label) => String(label),
		issueText: (issue) => `[${issue.code}] ${issue.object_type}/${issue.object_id}/${issue.option}`,
		rpcErrorText: (result) => result?.error_code || result?.error || 'Operation failed.',
		validateInput: (_format, _value) => true,
		creationDefaults: (collection, overrides) => Object.fromEntries(Object.entries({
			...(uiSpec.creation_defaults[collection] || {}), ...(overrides || {})
		}).map(([key, value]) => [key, typeof value == 'boolean' ? (value ? '1' : '0') : String(value)])),
		disambiguateReferences: (references) => {
			const counts = {};
			references.forEach((reference) => { counts[reference.label] = (counts[reference.label] || 0) + 1; });
			return references.map((reference) => ({ ...reference, label: counts[reference.label] > 1
				? `${reference.label}${reference.detail ? ` · ${reference.detail}` : ''} · #${reference.id.slice(-6)}`
				: reference.label }));
		},
		permissions: (methods, includeUCIWrite) => Promise.resolve({
			...Object.fromEntries(methods.map((method) => [ method, environment.permissions[method] !== false ])),
			...(includeUCIWrite ? { uci_write: environment.permissions.uci_write !== false } : {})
		}),
		configureNamedSection: (section, defaults, beforeSectionId) => {
			section.anonymous = false;
			section.autoIDs = true;
			section.sectiontitle = (sectionId) => uci.get('steer', sectionId, 'name') || 'Unnamed';
			section.handleAdd = function() {};
			section.addDefaults = defaults || {};
			section.addBeforeSectionId = beforeSectionId;
			return section;
		},
		configureOrdering: (section, collection, options) => {
			const policy = uiSpec.collection_ordering[collection];
			const disabledReason = options?.disabledReason || '';
			const sectionType = section.sectionType || section.sectiontype;
			section.sortable = !disabledReason;
			section.orderingDisabledReason = disabledReason;
			section.orderingPolicy = policy;
			section.dragFeedback = 'whole_row_placeholder';
			if (typeof(section.cfgsections) != 'function')
				section.cfgsections = () => (sections[sectionType] || [])
					.filter((item) => !section.filter || section.filter(item['.name']))
					.map((item) => item['.name']);
			section.dropItem = (sectionId, targetId, after, cancel = false) => {
				if (cancel) return false;
				const values = sections[sectionType] || [];
				const source = values.find((item) => item['.name'] == sectionId);
				const target = values.find((item) => item['.name'] == targetId);
				const movable = (item) => item &&
					(!policy.movable_kinds?.length || policy.movable_kinds.includes(item.kind)) &&
					(!policy.pinned_last_boolean_field || item[policy.pinned_last_boolean_field] != '1');
				if (disabledReason || !movable(source) || !movable(target) || source == target) return false;
				const group = policy.group_field ? (source[policy.group_field] || '') : '';
				const peers = values.filter((item) => (!section.filter || section.filter(item['.name'])) && movable(item) &&
					(!policy.group_field || (item[policy.group_field] || '') == group));
				if (!peers.includes(source) || !peers.includes(target)) return false;
				const sourceIndex = values.indexOf(source);
				let targetIndex = values.indexOf(target);
				values.splice(sourceIndex, 1);
				if (sourceIndex < targetIndex) targetIndex--;
				const insertIndex = targetIndex + (after ? 1 : 0);
				if (insertIndex == sourceIndex) {
					values.splice(sourceIndex, 0, source);
					return false;
				}
				values.splice(insertIndex, 0, source);
				return true;
			};
			section.moveItem = (sectionId, offset) => {
				const values = sections[sectionType] || [];
				const source = values.find((item) => item['.name'] == sectionId);
				const group = policy.group_field ? (source?.[policy.group_field] || '') : '';
				const peers = values.filter((item) => (!section.filter || section.filter(item['.name'])) &&
					(!policy.movable_kinds?.length || policy.movable_kinds.includes(item.kind)) &&
					(!policy.pinned_last_boolean_field || item[policy.pinned_last_boolean_field] != '1') &&
					(!policy.group_field || (item[policy.group_field] || '') == group));
				const position = peers.indexOf(source);
				const target = peers[position + offset];
				return !!target && section.dropItem(sectionId, target['.name'], offset > 0);
			};
			section.renderRowActions = () => {
				return element('td', {}, [
					element('button', { 'class': 'drag-handle', disabled: !!disabledReason }, '⠿'),
					options?.baseActions === false ? '' : element('button', {}, 'Edit')
				]);
			};
			return section;
		},
		configureRemovalGuard: (section, referencesFor) => {
			section.removalReferences = referencesFor;
			return section;
		},
		collectionReferences: (targetCollection, targetId) => {
			if (targetCollection == 'subscriptions') {
				const owned = new Set((sections.node || []).filter((node) => node.source_subscription == targetId).map((node) => node['.name']));
				return (sections.route || []).filter((route) => owned.has(route.node)).map((route) => ({
					objectType: 'route', id: route['.name'], label: route.name || route['.name'], option: 'node'
				}));
			}
			return uiSpec.collection_references.filter((relation) => relation.target_collection == targetCollection)
				.flatMap((relation) => (sections[relation.source_object_type] || []).filter((source) => {
					const value = source[relation.field];
					return relation.multiple ? (Array.isArray(value) ? value : [ value ]).includes(targetId) : value == targetId;
				}).map((source) => ({
					objectType: relation.source_object_type, id: source['.name'],
					label: source.name || source['.name'], option: relation.field
				})));
		},
		focusSection: () => Promise.resolve(false),
		focusIssue: () => false,
		status: () => Promise.resolve(environment.statusResult),
		runtime: () => Promise.resolve(environment.runtimeResult),
		overviewState: () => Promise.resolve(environment.lifecycleState),
		validate: () => Promise.resolve({ ok: true, errors: [], warnings: [] }),
		geodataCatalog: () => Promise.resolve({}),
		diagnostics: () => Promise.resolve(environment.diagnosticsResult),
		probeResults: () => Promise.resolve(environment.probeResultsResult),
		subscriptions: () => Promise.resolve({ subscriptions: [] }),
		renderStatus: () => { statusRenderCalls++; return element('div'); },
		updateSubscription: (id) => {
			updateSubscriptionCalls.push(id);
			return Promise.resolve(environment.updateSubscriptionResult);
		},
		cleanSubscription: (id, node) => {
			cleanSubscriptionCalls.push({ id, node });
			return Promise.resolve(environment.cleanSubscriptionResult);
		},
		speedtest: (node, download) => {
			speedtestCalls.push({ node, download });
			return Promise.resolve(environment.speedtestResult);
		},
		routeSpeedtest: (route, download) => {
			routeSpeedtestCalls.push({ route, download });
			return Promise.resolve({ scope: 'routes', object_id: route, kind: download ? 'download' : 'connect',
				tested_at: '2026-08-26T05:00:00Z', ok: true, stale: false, summary: download ? '8.0 Mbps' : '55 ms', error_summary: '' });
		},
		overviewProbe: (kind) => {
			overviewProbeCalls.push(kind);
			return Promise.resolve({
				scope: 'overview', kind, ok: true, tested_at: '2026-08-26T05:00:00Z',
				stale: false, summary: '20 ms', error_summary: ''
			});
		},
		importNodes: (document) => {
			importNodeCalls.push(document);
			return Promise.resolve(environment.importNodesResult);
		},
		exportNode: (node) => {
			exportNodeCalls.push(node);
			return Promise.resolve(environment.exportNodeResult);
		},
		applyPending: () => Promise.resolve({ ok: true }),
		applySaved: () => Promise.resolve({ ok: true }),
		discardPending: () => Promise.resolve(),
		apply: () => Promise.resolve()
	};
	const ui = {
		addNotification: (title, body, level) => notifications.push({ title, body, level }),
		showModal: (title, content) => { environment.modal = { title, content }; },
		hideModal: () => { environment.modalHidden = true; }
	};
	const window = { location: {
		pathname: '/cgi-bin/luci/admin/services/steer/nodes', search: '', href: '', reloadCount: 0,
		reload: function() { this.reloadCount++; }
	} };
	const translate = (value) => String(value);

	const environment = {
		form, uci, view, steer, ui, window, maps, translate,
		speedtestCalls, routeSpeedtestCalls, overviewProbeCalls, importNodeCalls, exportNodeCalls, cleanSubscriptionCalls, updateSubscriptionCalls, notifications,
		setPendingChanges: (changes) => { pendingChanges = changes; },
		get statusRenderCalls() { return statusRenderCalls; },
		cleanSubscriptionResult: { ok: true },
		updateSubscriptionResult: { ok: true, subscriptions: [] },
		exportNodeResult: { ok: true, uri: 'socks://user:secret@node.example:1080#Manual' },
		statusResult: {},
		runtimeResult: {},
		diagnosticsResult: JSON.parse(JSON.stringify(probeDiagnosticsFixtures.diagnostics)),
		probeResultsResult: JSON.parse(JSON.stringify(probeDiagnosticsFixtures.probe_results)),
		lifecycleState: {
			ok: true, pending: false,
			desired: { available: true, enabled: true, digest: 'saved-a', counts: {}, validation: { ok: true, errors: [], warnings: [] } },
			saved: { available: true, enabled: true, digest: 'saved-a', counts: {}, validation: { ok: true, errors: [], warnings: [] } },
			active: { healthy: true, generation: 'generation-a', intent_digest: 'active-a' }
		},
		speedtestResult: { scope: 'nodes', object_id: 'node_a', kind: 'connect', tested_at: '2026-08-26T05:00:00Z',
			ok: true, stale: false, summary: '42 ms', error_summary: '' },
			importNodesResult: { nodes: [], skipped: 0 },
			permissions: {},
		modal: null,
		modalHidden: false
	};
	return environment;
}

function loadView(file, dependencies) {
	const source = fs.readFileSync(path.join(root, file), 'utf8');
	const names = Object.keys(dependencies);
	const values = names.map((name) => dependencies[name]);
	return new Function(...names, source)(...values);
}

function loadUcodeRPC(runtime) {
	let source = fs.readFileSync(path.join(root,
		'luci-app-steer/root/usr/share/rpcd/ucode/luci.steer'), 'utf8');
	source = source.replace(/^#![^\n]*\n/, '').replace(/^import \{[^\n]+\} from 'fs';\n/m, '');
	const ubus = {
		defer(object, method, args, callback) {
			const call = { object, method, args };
			runtime.ubusCalls.push(call);
			if (runtime.onUbusCall)
				runtime.onUbusCall(call, runtime);
			switch (method) {
			case 'get': return callback(0, { values: runtime.values });
			case 'changes': return callback(0, { changes: runtime.changes });
			case 'commit': runtime.commitCalls++; return callback(0, { result: 'committed' });
			default: throw new Error(`unexpected ubus method ${method}`);
			}
		}
	};
	runtime.files ||= {};
	runtime.importInputs ||= [];
	runtime.accessCalls ||= [];
	let lastFSError = null;
	const access = (file, mode) => {
		runtime.accessCalls.push({ file, mode: mode || '' });
		if (runtime.programMissing) {
			lastFSError = 'No such file or directory';
			return null;
		}
		if (mode == 'x' && runtime.programNotExecutable) {
			lastFSError = 'Permission denied';
			return null;
		}
		lastFSError = null;
		return true;
	};
	const fs_error = () => lastFSError;
	const popen = (command, mode) => {
		runtime.commands.push(command);
		if (typeof(command) != 'string')
			throw new Error(`unexpected process ${JSON.stringify(command)}`);
		if (command.includes(' _parse-nodes --output ')) {
			assert.equal(mode, 'w');
			if (runtime.processStartFailure) {
				lastFSError = runtime.processStartFailure;
				return null;
			}
			const match = command.match(/ --output '([^']+)'$/);
			assert.ok(match, `node parser output remains a shell-quoted private path: ${command}`);
			return {
				write: (input) => runtime.importInputs.push(input),
				close: () => {
					const status = runtime.importStatus || 0;
					if (status == 0)
						runtime.files[match[1]] = JSON.stringify(runtime.importResult || { nodes: [], skipped: 0 });
					return status;
				}
			};
		}
		const result = command == '/usr/sbin/steer apply'
			? (runtime.applyResult || { ok: true })
			: command.includes(' validate --config ')
			? runtime.validation
			: (command.includes(' _export-node ')
				? (runtime.exportNodeResult || { ok: true, uri: 'socks://user:secret@node.example:1080#Manual' })
				: (command.includes(' _state')
				? (command.includes(' --config ') ? runtime.desiredState : runtime.savedState)
				: (command.includes(' _export-intent') ? runtime.intent : {})));
		return { read: () => JSON.stringify(result), close: () => 0 };
	};
	const open = (file, mode) => {
		if (mode == 'r') {
			if (!(file in runtime.files)) return null;
			return { read: () => runtime.files[file], close: () => {} };
		}
		if (mode == 'w')
			return { write: (document) => runtime.documents.push(document), close: () => {} };
		throw new Error(`unexpected open mode ${mode}`);
	};
	const globals = {
		access,
		fs_error,
		mkdtemp: () => `/tmp/test-candidate-${runtime.documents.length}`,
		open,
		popen,
		rmdir: () => {},
		unlink: (file) => { delete runtime.files[file]; },
		require: (name) => {
			if (name != 'ubus') throw new Error(`unexpected module ${name}`);
			return { connect: () => ubus };
		},
		replace: (value, search, replacement) => String(value).split(search).join(replacement),
		type: (value) => Array.isArray(value) ? 'array' : (value == null ? 'null' : typeof(value)),
		match: (value, expression) => String(value).match(expression),
		json: (value) => JSON.parse(value),
		trim: (value) => String(value).trim(),
		keys: (value) => Object.keys(value),
		sort: (value, compare) => value.sort(compare),
		push: (value, item) => value.push(item),
		substr: (value, start, length) => String(value).substr(start, length),
		join: (separator, value) => value.join(separator),
		length: (value) => value.length,
		split: (value, separator) => String(value).split(separator),
		slice: (value, start) => value.slice(start)
	};
	const names = Object.keys(globals);
	const values = names.map((name) => globals[name]);
	return new Function(...names, source)(...values)['luci.steer'];
}

function callUcodeMethod(method, args) {
	let response;
	const returned = method.call({
		args: Object.assign({ ubus_rpc_session: '0123456789abcdef0123456789abcdef' }, args),
		reply: (value) => { response = value; }
	});
	if (response === undefined) response = returned;
	assert.notEqual(response, undefined, 'ucode RPC must reply exactly once');
	return response;
}

function testCommittedUcodePreviewAndObservedCandidateGuard() {
	const privateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nowner's key\n-----END OPENSSH PRIVATE KEY-----\n";
	const runtime = {
		values: {
			main: { '.type': 'steer', '.name': 'main', '.index': 0, schema_version: '9', enabled: '1' },
			ssh_key: {
				'.type': 'node', '.name': 'ssh_key', '.index': 1, type: 'ssh', uuid: 'node-uuid',
				password: 'node-password', private_key: privateKey, plugin_options: 'token=plugin-secret',
				extra_args: [ '--token', 'argument-secret' ]
			},
			feed: { '.type': 'subscription', '.name': 'feed', '.index': 2, url: 'https://user:secret@example.test/feed' }
		},
		changes: [ [ 'set', 'main', 'enabled', '1' ] ],
		intent: {
			main: { id: 'main' },
			nodes: [ { id: 'ssh_key', uuid: 'node-uuid', password: 'node-password', private_key: privateKey, plugin_options: 'token=plugin-secret', extra_args: [ '--token', 'argument-secret' ] } ],
			subscriptions: [ { id: 'feed', url: 'https://user:secret@example.test/feed' } ],
			future: { nested_token: 'future-token' }
		},
		validation: { ok: true, errors: [], warnings: [] },
		ubusCalls: [], commands: [], documents: [], commitCalls: 0
	};
	const methods = loadUcodeRPC(runtime);
	const pending = callUcodeMethod(methods.intent_preview, { reveal: false });
	assert.equal(pending.source, 'unavailable');
	assert.equal(pending.pending, true);
	assert.equal(pending.available, false);
	assert.equal(pending.intent, null,
		'RPC fails closed instead of returning a committed snapshot or guessed pending Apply input');
	const pendingReveal = callUcodeMethod(methods.intent_preview, { reveal: true });
	assert.equal(pendingReveal.available, false);
	assert.equal(pendingReveal.redacted, true);
	assert.equal(runtime.commands.length, 0, 'pending state never exports or reveals any Canonical intent');

	runtime.changes = [];
	const redacted = callUcodeMethod(methods.intent_preview, { reveal: false });
	assert.equal(redacted.source, 'committed');
	assert.equal(redacted.pending, false);
	assert.equal(redacted.available, true);
	assert.deepEqual(redacted.intent.nodes[0], {
		id: 'ssh_key', uuid: '[REDACTED]', password: '[REDACTED]', private_key: '[REDACTED]', plugin_options: '[REDACTED]', extra_args: '[REDACTED]'
	}, 'default RPC response must redact every authentication-bearing node field before reaching the browser');
	assert.equal(redacted.intent.subscriptions[0].url, '[REDACTED]');
	assert.equal(redacted.intent.future.nested_token, '[REDACTED]');

	const revealed = callUcodeMethod(methods.intent_preview, { reveal: true });
	assert.equal(revealed.intent.nodes[0].private_key, privateKey,
		'plaintext from the committed snapshot requires an explicit reveal=true RPC');
	let committedChecks = 0;
	runtime.onUbusCall = (call) => {
		if (call.method == 'changes' && ++committedChecks == 2)
			runtime.changes = [ [ 'set', 'main', 'enabled', '1' ] ];
	};
	const pendingDuringExport = callUcodeMethod(methods.intent_preview, { reveal: false });
	assert.equal(pendingDuringExport.available, false);
	assert.equal(pendingDuringExport.intent, null,
		'a pending change observed after committed export discards the snapshot before the RPC reply');
	runtime.onUbusCall = null;
	runtime.changes = [];
	const committed = callUcodeMethod(methods.commit_candidate, {});
	assert.equal(committed.committed, true, 'an unchanged candidate passes the pre-commit observed-change guard');
	assert.equal(runtime.commitCalls, 1);
	const candidate = runtime.documents[0];
	assert.ok(candidate.includes("config 'steer' 'main'") && candidate.indexOf("'main'") < candidate.indexOf("'ssh_key'"),
		'candidate serialization preserves rpcd section ordering');
	assert.ok(candidate.includes("option 'private_key' '-----BEGIN OPENSSH PRIVATE KEY-----\nowner'\\''s key\n-----END OPENSSH PRIVATE KEY-----\n'"),
		'candidate serialization preserves physical newlines and quoted apostrophes');
	assert.equal(runtime.commands.some((command) => String(command).includes('/sbin/uci') || String(command).includes("'-P'")), false,
		'best-effort candidate validation must not read the process-global /tmp/.uci delta path');
	assert.ok(runtime.ubusCalls.filter((call) => call.method == 'get').every((call) =>
		call.args.ubus_rpc_session == '0123456789abcdef0123456789abcdef'),
		'every candidate snapshot comes from rpcd uci.get with the exact LuCI session');

	let validationGets = 0;
	runtime.onUbusCall = (call) => {
		if (call.method == 'get' && ++validationGets == 2)
			runtime.values.main.schema_version = '11';
	};
	const racedCommit = callUcodeMethod(methods.commit_candidate, {});
	assert.equal(racedCommit.committed, false,
		'Apply refuses a session candidate change observed after validation');
	assert.equal(runtime.commitCalls, 1, 'a post-validation candidate mismatch never reaches uci.commit');
	runtime.onUbusCall = null;
}

function testOverviewStateSeparatesPendingSavedAndActiveFacts() {
	const active = {
		healthy: true, generation: 'generation-active', intent_digest: 'active-digest',
		last_apply: { sequence: '10', result: { ok: true, generation: 'generation-active' } }
	};
	const desired = {
		available: true, enabled: false, digest: 'desired-digest', counts: { nodes: 2, routes: 3, dns_profiles: 1, local_proxies: 1, rules: 4 },
		validation: { ok: true, errors: [], warnings: [] }
	};
	const saved = {
		available: true, enabled: true, digest: 'saved-digest', counts: { nodes: 1, routes: 3, dns_profiles: 1, local_proxies: 1, rules: 4 },
		validation: { ok: true, errors: [], warnings: [] }
	};
	const runtime = {
		values: { main: { '.type': 'steer', '.name': 'main', '.index': 0, enabled: '0' } },
		changes: [ [ 'set', 'main', 'enabled', '0' ] ],
		desiredState: { saved: desired, active, pending_apply: false }, savedState: { saved, active, pending_apply: false },
		validation: { ok: true, errors: [], warnings: [] }, intent: {},
		ubusCalls: [], commands: [], documents: [], commitCalls: 0
	};
	const methods = loadUcodeRPC(runtime);
	const state = callUcodeMethod(methods.overview_state, {});
	assert.equal(state.ok, true);
	assert.equal(state.pending, true);
	assert.equal(state.pending_apply, false);
	assert.equal(state.desired.enabled, false);
	assert.equal(state.saved.enabled, true);
	assert.equal(state.active.generation, 'generation-active');
	assert.equal(state.active.healthy, true);
	assert.ok(runtime.commands.some((command) => command.includes(' _state --saved-only --config ')));
	assert.ok(runtime.commands.some((command) => command == '/usr/sbin/steer _state'));
}

function testSubscriptionRPCRejectsPendingSession() {
	const runtime = {
		values: {},
		changes: [ [ 'set', 'feed', 'url', 'https://pending.example/sub' ] ],
		validation: { ok: true, errors: [], warnings: [] },
		intent: {}, ubusCalls: [], commands: [], documents: [], commitCalls: 0
	};
	const methods = loadUcodeRPC(runtime);
	for (const [ method, args ] of [
		[ methods.apply_saved, {} ],
		[ methods.subscription_update, { id: 'feed' } ],
		[ methods.subscription_clean, { id: 'feed', node: 'feed_stale' } ],
		[ methods.node_speedtest, { node: 'node_a', download: false } ],
		[ methods.route_speedtest, { route: 'route_a', download: true } ]
	]) {
		const response = callUcodeMethod(method, args);
		assert.equal(response.ok, false);
		assert.equal(response.error_code, 'PENDING_CHANGES');
	}
	assert.equal(runtime.commands.length, 0,
		'server-side pending guard rejects inventory and committed probe operations before starting Steer');
	for (const [ method, args, errorCode ] of [
		[ methods.subscription_update, {}, 'MISSING_SUBSCRIPTION_ID' ],
		[ methods.subscription_clean, { id: 'feed' }, 'MISSING_SUBSCRIPTION_NODE_ID' ],
		[ methods.node_speedtest, {}, 'MISSING_NODE_ID' ],
		[ methods.route_speedtest, {}, 'MISSING_ROUTE_ID' ],
		[ methods.overview_probe, { kind: 'other' }, 'INVALID_PROBE_KIND' ],
		[ methods.node_import, {}, 'MISSING_NODE_DOCUMENT' ]
	]) {
		const response = callUcodeMethod(method, args);
		assert.equal(response.error_code, errorCode, `${errorCode} must be stable and locale-neutral`);
	}
	runtime.changes = [];
	callUcodeMethod(methods.subscription_update, { id: 'feed' });
	assert.equal(runtime.commands.length, 1);
	assert.ok(runtime.commands[0].includes('subscription update --id'));
	const applied = callUcodeMethod(methods.apply_saved, {});
	assert.equal(applied.ok, true);
	assert.equal(runtime.commands[1], '/usr/sbin/steer apply');
}

function testNodeImportUcodeUsesTargetCompatibleStringPopen() {
	const runtime = {
		values: {}, changes: [], validation: { ok: true, errors: [], warnings: [] }, intent: {},
		ubusCalls: [], commands: [], documents: [], commitCalls: 0,
		importResult: { nodes: [ { type: 'vless', server: 'example.com', server_port: 443 } ], skipped: 0 }
	};
	const methods = loadUcodeRPC(runtime);
	const document = 'vless://fixture kept out of the shell command';
	const imported = callUcodeMethod(methods.node_import, { document });
	assert.equal(imported.nodes[0].type, 'vless');
	assert.deepEqual(runtime.importInputs, [ document ], 'the private document is written only to parser stdin');
	assert.equal(runtime.commands[0],
		"/usr/sbin/steer _parse-nodes --output '/tmp/test-candidate-0/result.json'",
		'target ucode receives one fixed string command with a shell-quoted private output path');
	assert.ok(!runtime.commands[0].includes(document), 'node credentials and links never enter the shell command');
	assert.deepEqual(runtime.accessCalls.slice(0, 2), [
		{ file: '/usr/sbin/steer', mode: '' }, { file: '/usr/sbin/steer', mode: 'x' }
	]);

	runtime.importStatus = 1;
	assert.equal(callUcodeMethod(methods.node_import, { document }).error_code, 'IMPORT_PARSE_FAILED');
	runtime.importStatus = 0;
	runtime.programMissing = true;
	assert.equal(callUcodeMethod(methods.node_import, { document }).error_code, 'IMPORT_PROGRAM_MISSING');
	runtime.programMissing = false;
	runtime.programNotExecutable = true;
	assert.equal(callUcodeMethod(methods.node_import, { document }).error_code, 'IMPORT_PROGRAM_NOT_EXECUTABLE');
	runtime.programNotExecutable = false;
	runtime.processStartFailure = 'Invalid argument';
	const startFailure = callUcodeMethod(methods.node_import, { document });
	assert.equal(startFailure.error_code, 'IMPORT_START_FAILED');
	assert.equal(startFailure.error, 'Invalid argument', 'fs.error supplies bounded startup context');
	runtime.processStartFailure = '';
	runtime.importStatus = 127;
	assert.equal(callUcodeMethod(methods.node_import, { document }).error_code, 'IMPORT_PROGRAM_MISSING',
		'a parser disappearing after access preflight remains distinguishable from parse failure');
	runtime.importStatus = 126;
	assert.equal(callUcodeMethod(methods.node_import, { document }).error_code, 'IMPORT_PROGRAM_NOT_EXECUTABLE');
}

function testNodeExportUcodeUsesCurrentCandidateSnapshot() {
	const runtime = {
		values: {
			manual: {
				'.type': 'node', '.name': 'manual', '.index': 0, enabled: '1', name: 'Manual',
				type: 'socks', server: 'node.example', server_port: '1080', username: 'user', password: 'candidate-secret'
			}
		},
		changes: [ [ 'set', 'manual', 'password', 'candidate-secret' ] ],
		validation: { ok: true, errors: [], warnings: [] }, intent: {},
		ubusCalls: [], commands: [], documents: [], commitCalls: 0,
		exportNodeResult: { ok: true, uri: 'socks://user:candidate-secret@node.example:1080#Manual' }
	};
	const methods = loadUcodeRPC(runtime);
	const exported = callUcodeMethod(methods.node_export, { node: 'manual' });
	assert.equal(exported.ok, true);
	assert.equal(exported.uri, runtime.exportNodeResult.uri);
	assert.ok(runtime.documents[0].includes("option 'password' 'candidate-secret'"),
		'Node export snapshots the current LuCI candidate, including unsaved field edits');
	const command = runtime.commands.find((value) => value.includes(' _export-node '));
	assert.ok(command.includes("--id 'manual'") && command.includes("--config '/tmp/test-candidate-0/steer'"));
	assert.ok(!command.includes('candidate-secret'), 'Node credentials never enter the shell command');
	assert.equal(callUcodeMethod(methods.node_export, {}).error_code, 'MISSING_NODE_ID');
}

function allOptions(environment) {
	return environment.maps.flatMap((map) => map.sections.flatMap((section) => section.options));
}

async function renderRules(sections, catalog = {}) {
	const environment = createEnvironment(sections);
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/rules.js',
		{
			dom: {},
			form: environment.form,
			uci: environment.uci,
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			E: element,
			_: environment.translate
		}
	);
	await view.render([ null, catalog ]);
	return environment;
}

async function renderNodes(sections, search = '', subscriptionStatus, page = 'nodes', pendingChanges = [], permissions = {}, probeResultsResult) {
	const environment = createEnvironment(sections);
	if (probeResultsResult) environment.probeResultsResult = probeResultsResult;
	environment.permissions = { ...permissions };
	environment.setPendingChanges(pendingChanges);
	environment.window.location.pathname = `/cgi-bin/luci/admin/services/steer/${page}`;
	environment.window.location.search = search;
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/nodes.js',
		{
			form: environment.form,
			uci: environment.uci,
			ui: environment.ui,
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			window: environment.window,
			E: element,
			_: environment.translate
		}
	);
	environment.nodeView = view;
	environment.rendered = await view.render([ null, subscriptionStatus, { steer: pendingChanges },
		environment.probeResultsResult,
		{
			...Object.fromEntries([ 'subscription_update', 'subscription_clean', 'node_speedtest', 'route_speedtest', 'node_import', 'node_export' ]
				.map((method) => [ method, environment.permissions[method] !== false ])),
			uci_write: environment.permissions.uci_write !== false
		} ]);
	return environment;
}

async function renderDns(sections) {
	const environment = createEnvironment(sections);
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/dns.js',
		{
			form: environment.form,
			uci: environment.uci,
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			E: element,
			_: environment.translate
		}
	);
	environment.rendered = await view.render();
	return environment;
}

async function renderSystem(runtime, status) {
	const environment = createEnvironment({});
	environment.runtimeResult = runtime || {};
	environment.statusResult = status || {};
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/system.js',
		{
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			E: element,
			_: environment.translate
		}
	);
	const loaded = await view.load();
	environment.rendered = view.render(loaded);
	return environment;
}

async function renderAdvanced(initial, revealed) {
	const environment = createEnvironment({});
	environment.previewCalls = [];
	environment.steer.intentPreview = (reveal) => {
		environment.previewCalls.push(reveal === true);
		return Promise.resolve(reveal ? revealed : initial);
	};
	const advanced = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/advanced.js',
		{
			view: environment.view,
			steer: environment.steer,
			E: element,
			_: environment.translate
		}
	);
	const loaded = await advanced.load();
	environment.rendered = advanced.render(loaded);
	environment.advanced = advanced;
	return environment;
}

async function renderLocalProxies(sections) {
	const environment = createEnvironment(sections);
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/local-proxies.js',
		{
			form: environment.form,
			uci: environment.uci,
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			E: element,
			_: environment.translate
		}
	);
	await view.render();
	environment.localProxyView = view;
	return environment;
}

async function renderOverview(sections, page = 'general', lifecycleState, permissions = {}) {
	const environment = createEnvironment(sections);
	environment.permissions = { ...permissions };
	if (lifecycleState) environment.lifecycleState = lifecycleState;
	environment.window.location.pathname = `/cgi-bin/luci/admin/services/steer/${page}`;
	const view = loadView(
		'luci-app-steer/htdocs/luci-static/resources/view/steer/overview.js',
		{
			form: environment.form,
			uci: environment.uci,
			ui: environment.ui,
			view: environment.view,
			steer: environment.steer,
			uiSpec,
			window: environment.window,
			E: element,
			_: environment.translate
		}
	);
	environment.rendered = await view.render([ null, environment.lifecycleState, { ok: true, errors: [], warnings: [] }, environment.diagnosticsResult,
		environment.probeResultsResult, { steer: [] },
		{ overview_probe: environment.permissions.overview_probe !== false } ]);
	return environment;
}

function assertAutomaticIdsAndOptionalNames(environment, message) {
	const addable = environment.maps.flatMap((map) => map.sections)
		.filter((section) => section.addremove);
	assert.ok(addable.length > 0, message + ': expected at least one addable section');
	assert.ok(addable.every((section) => section.anonymous === false && section.autoIDs && typeof section.handleAdd == 'function'),
		message + ': addable entities keep named UCI sections but generate IDs automatically');
	assert.ok(addable.every((section) => section.options.some((option) =>
		option.name == 'name' && option.rmempty === true && option.optional === true && option.modalonly === true)),
		message + ': every Canonical optional name remains modal-only and cannot duplicate the native list title');
}

async function main() {
	const stylesheet = fs.readFileSync(path.join(root,
		'luci-app-steer/htdocs/luci-static/resources/steer/steer.css'), 'utf8');
	assert.ok(stylesheet.includes('@media (min-width: 1153px)') &&
		stylesheet.includes('.cbi-section-table-row[data-title]::before') &&
		stylesheet.includes('.cbi-section-table-titles.named::before') &&
		stylesheet.includes('display: none;') && stylesheet.includes('content: none;'),
		'desktop GridSection duplicate title pseudo-elements are removed from layout at the Argon breakpoint');
	assert.ok(!/cbi-section-table[^}]*::before\s*\{[^}]*visibility\s*:\s*hidden/s.test(stylesheet),
		'GridSection title pseudo-elements must not reserve space through visibility:hidden');
	assert.ok(!/margin(?:-[a-z]+)?\s*:\s*-/i.test(stylesheet),
		'Steer custom layout does not offset LuCI containers with negative margins');
	assert.ok(stylesheet.includes('--steer-space-page:') && stylesheet.includes('--steer-space-card:') &&
		stylesheet.includes('--steer-space-compact:') && stylesheet.includes('--steer-space-cell:'),
		'custom panels, cards, compact controls and table facts share the desktop spacing scale');
	assert.ok(/\.steer-section-heading h3,\s*\.steer-section-heading p\s*\{[^}]*margin:\s*0;[^}]*padding:\s*0;/s.test(stylesheet),
		'import heading and explanatory copy reset Argon padding to one shared content boundary');
	assert.ok(/\.steer-overview-shell\s*\{[^}]*min-width:\s*0;[^}]*overflow:\s*hidden;/s.test(stylesheet) &&
		/\.steer-overview-region\s*\{[^}]*box-sizing:\s*border-box;[^}]*min-width:\s*0;[^}]*padding:\s*var\(--steer-space-card\) var\(--steer-space-page\);/s.test(stylesheet),
		'Overview regions keep one padded content boundary without horizontal overflow');
	assert.ok(stylesheet.includes('grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto minmax(0, 1fr) auto minmax(0, 1fr);') &&
		/@media \(max-width: 1100px\)[\s\S]*\.steer-overview-pipeline \{ grid-template-columns: repeat\(2, minmax\(0, 1fr\)\); \}/.test(stylesheet) &&
		/@media \(max-width: 700px\)[\s\S]*\.steer-overview-pipeline \{ grid-template-columns: 1fr; \}/.test(stylesheet),
		'Overview pipeline has explicit 1920/1280 desktop and 1024/narrow responsive layouts');
	assert.ok(/\.steer-overview-metrics\s*\{[^}]*repeat\(auto-fit, minmax\(8rem, 1fr\)\)/s.test(stylesheet) &&
		/\.steer-overview-actions\s*\{[^}]*flex-wrap:\s*wrap;/s.test(stylesheet),
		'Overview scale and shortcuts reflow without fixed blank columns or squeezed actions');
	assert.ok(stylesheet.includes('--steer-monospace: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;') &&
		stylesheet.includes('.steer-machine-input'),
		'machine-readable import input uses the shared monospace stack');
	assert.equal(formInputFixtures.schema_version, 1);
	assert.equal(creationPolicyFixtures.schema_version, 1);
	assert.equal(collectionOrderingFixtures.schema_version, 1);
	assert.equal(uiSpec.id_policy.auto_generate, true);
	for (const fixture of creationPolicyFixtures.cases) {
		const actual = environmentForCreationDefaults(fixture.collection, fixture.overrides);
		const expected = { ...fixture.expected };
		delete expected.id;
		const expectedUCI = Object.fromEntries(Object.entries(expected).map(([key, value]) => [
			key, typeof value == 'boolean' ? (value ? '1' : '0') : String(value)
		]));
		assert.deepEqual(actual, expectedUCI, `${fixture.collection} UCI creation defaults match Canonical fixture`);
	}
	assert.ok(formInputFixtures.cases.every((fixture) => uiSpec.input_formats[fixture.format]),
		'LuCI loads the shared form input fixtures and generated format metadata');
	for (const fixture of collectionOrderingFixtures.cases) {
		const sections = canonicalFixtureSections({ [fixture.collection]: fixture.objects });
		if (fixture.collection == 'nodes')
			sections.subscription = [ { '.name': 'feed', name: 'Feed' } ];
		let orderingEnvironment;
		switch (fixture.collection) {
		case 'nodes': orderingEnvironment = await renderNodes(sections, '?node_group=_manual'); break;
		case 'routes': orderingEnvironment = await renderNodes(sections, '', undefined, 'routes'); break;
		case 'dns_profiles': orderingEnvironment = await renderDns(sections); break;
		case 'local_proxies': orderingEnvironment = await renderLocalProxies(sections); break;
		case 'rules': orderingEnvironment = await renderRules(sections); break;
		case 'subscriptions': orderingEnvironment = await renderNodes(sections, '', { subscriptions: [] }, 'subscriptions'); break;
		default: throw new Error(`unknown ordering fixture ${fixture.collection}`);
		}
		const sectionType = {
			nodes: 'node', routes: 'route', dns_profiles: 'dns_profile', local_proxies: 'local_proxy',
			rules: 'rule', subscriptions: 'subscription'
		}[fixture.collection];
		const ordered = orderingEnvironment.maps[0].sections.find((section) =>
			section.type == 'GridSection' && section.sectionType == sectionType);
		assert.ok(ordered?.sortable && ordered.orderingPolicy?.stable_id_field == 'id',
			`${fixture.collection} consumes the shared stable-ID ordering policy`);
		assert.ok(elementText(ordered.renderRowActions(fixture.move_id)).includes('⠿') &&
			!elementText(ordered.renderRowActions(fixture.move_id)).includes('Move up'),
			`${fixture.collection} exposes one direct whole-row drag handle without arrow buttons`);
		assert.equal(ordered.moveItem(fixture.move_id, fixture.offset), true, fixture.name);
		assert.deepEqual((sections[sectionType] || []).map((item) => item['.name']), fixture.expected_ids, fixture.name);
	}
	for (const fixture of collectionDragFixtures.cases) {
		const sections = canonicalFixtureSections({ [fixture.collection]: fixture.objects });
		if (fixture.collection == 'nodes')
			sections.subscription = [ { '.name': 'feed', name: 'Feed' } ];
		let dragEnvironment;
		switch (fixture.collection) {
		case 'nodes': dragEnvironment = await renderNodes(sections, '?node_group=_manual'); break;
		case 'routes': dragEnvironment = await renderNodes(sections, '', undefined, 'routes'); break;
		case 'dns_profiles': dragEnvironment = await renderDns(sections); break;
		case 'local_proxies': dragEnvironment = await renderLocalProxies(sections); break;
		case 'rules': dragEnvironment = await renderRules(sections); break;
		case 'subscriptions': dragEnvironment = await renderNodes(sections, '', { subscriptions: [] }, 'subscriptions'); break;
		default: throw new Error(`unknown drag fixture ${fixture.collection}`);
		}
		const sectionType = {
			nodes: 'node', routes: 'route', dns_profiles: 'dns_profile', local_proxies: 'local_proxy',
			rules: 'rule', subscriptions: 'subscription'
		}[fixture.collection];
		const ordered = dragEnvironment.maps[0].sections.find((section) =>
			section.type == 'GridSection' && section.sectionType == sectionType);
		assert.equal(ordered.dragFeedback, 'whole_row_placeholder', `${fixture.collection} uses whole-row drag feedback`);
		assert.equal(ordered.dropItem(
			fixture.source_id, fixture.target_id, fixture.after, fixture.cancel
		), fixture.expected_mutations == 1, fixture.name);
		assert.deepEqual((sections[sectionType] || []).map((item) => item['.name']), fixture.expected_ids, fixture.name);
	}
	assert.deepEqual(uiSpec.node_display_sorting.modes, nodeDisplaySortingFixtures.modes);
	assert.deepEqual(uiSpec.node_display_sorting.direction_modes, nodeDisplaySortingFixtures.direction_modes);
	for (const fixture of nodeDisplaySortingFixtures.cases) {
		const sections = canonicalFixtureSections({ nodes: nodeDisplaySortingFixtures.nodes });
		sections.subscription = [ { '.name': 'feed', name: 'Feed' } ];
		const originalIDs = sections.node.map((item) => item['.name']);
		const query = new URLSearchParams({
			node_group: fixture.group || '_manual', node_sort: fixture.mode,
			node_sort_direction: fixture.direction
		});
		const sortingEnvironment = await renderNodes(
			sections, '?' + query.toString(), undefined, 'nodes', [], {},
			{ latest_results: nodeDisplaySortingFixtures.latest_results, warnings: [] }
		);
		const nodeSection = sortingEnvironment.maps[0].sections.find((section) =>
			section.type == 'GridSection' && section.sectionType == 'node');
		if (fixture.group) {
			const rows = findElements(sortingEnvironment.rendered, (node) =>
				node.tag == 'tr' && node.attributes?.['data-sid']);
			assert.deepEqual(rows.map((row) => row.attributes['data-sid']), fixture.expected_ids, fixture.name);
			assert.equal(nodeSection, undefined, 'subscription groups bypass the heavyweight LuCI GridSection');
		}
		else {
			assert.deepEqual(nodeSection.cfgsections(), fixture.expected_ids, fixture.name);
			assert.equal(nodeSection.sortable, fixture.mode == 'default',
				`${fixture.name} disables configured reordering only while display sorting`);
			assert.equal(!!nodeSection.orderingDisabledReason, fixture.mode != 'default');
		}
		assert.deepEqual(sections.node.map((item) => item['.name']), originalIDs,
			`${fixture.name} never mutates UCI section order`);
	}
	const headerSections = canonicalFixtureSections({ nodes: nodeDisplaySortingFixtures.nodes.slice(0, 2) });
	const headerEnvironment = await renderNodes(
		headerSections, '?node_group=_manual&node_sort=connect&node_sort_direction=best_first',
		undefined, 'nodes', [], {}, { latest_results: nodeDisplaySortingFixtures.latest_results, warnings: [] }
	);
	const orderHeader = element('th', { 'data-sortable-row': '' }, 'Name');
	const connectHeader = element('th', { 'data-widget': 'CBI.ButtonValue' }, 'Connection test');
	const downloadHeader = element('th', { 'data-widget': 'CBI.ButtonValue' }, 'Download test');
	const actionsHeader = element('th', { 'class': 'cbi-section-actions' });
	const sortForm = element('form', {}, element('table', {}, element('thead', {},
		element('tr', {}, [ orderHeader, connectHeader, downloadHeader, actionsHeader ]))));
	headerEnvironment.nodeView.decorateNodeSortHeaders(sortForm);
	assert.equal(elementText(orderHeader), 'Order', 'configured order is a plain non-sortable header');
	assert.equal(findElements(orderHeader, (node) => node.tag == 'button').length, 0,
		'configured order does not expose a redundant sort action');
	assert.ok(elementText(connectHeader).includes('↑') &&
		!/(Best|Worst|Configured|好|坏)/.test(elementText(connectHeader)),
		'active LuCI metric header shows only the normal direction arrow');
	const connectButton = findElements(connectHeader, (node) => node.tag == 'button')[0];
	connectButton.attributes.click({ preventDefault() {}, stopPropagation() {} });
	assert.ok(headerEnvironment.window.location.href.includes('node_sort=connect') &&
		headerEnvironment.window.location.href.includes('node_sort_direction=worst_first'),
		'repeated LuCI header click reverses display direction without a UCI move');
	const thirdClickEnvironment = await renderNodes(
		headerSections, '?node_group=_manual&node_sort=connect&node_sort_direction=worst_first',
		undefined, 'nodes', [], {}, { latest_results: nodeDisplaySortingFixtures.latest_results, warnings: [] }
	);
	const thirdConnectHeader = element('th', { 'data-widget': 'CBI.ButtonValue' }, 'Connection test');
	const thirdForm = element('form', {}, element('table', {}, element('thead', {}, element('tr', {}, [
		element('th', {}, 'Order'), thirdConnectHeader,
		element('th', { 'data-widget': 'CBI.ButtonValue' }, 'Download test')
	]))));
	thirdClickEnvironment.nodeView.decorateNodeSortHeaders(thirdForm);
	findElements(thirdConnectHeader, (node) => node.tag == 'button')[0].attributes.click({ preventDefault() {}, stopPropagation() {} });
	assert.ok(!thirdClickEnvironment.window.location.href.includes('node_sort='),
		'the third click restores configured order instead of requiring a separate Order sorter');
	assert.deepEqual(headerSections.node.map((item) => item['.name']), [ 'slow', 'stale' ]);
	const largeSubscriptionSections = {
		subscription: [ { '.name': 'large_feed', name: 'Large feed' } ],
		node: Array.from({ length: 750 }, (_, index) => ({
			'.name': `feed_node_${index}`, source_subscription: 'large_feed', enabled: '1',
			name: `Feed node ${index}`, type: 'vless', server: `node-${index}.example`, server_port: '443'
		}))
	};
	const largeEnvironment = await renderNodes(largeSubscriptionSections, '?node_group=large_feed');
	assert.equal(largeEnvironment.maps[0].sections.some((section) => section.sectionType == 'node'), false,
		'large subscription groups do not instantiate a LuCI Form section per generated Node');
	assert.equal(findElements(largeEnvironment.rendered, (node) => node.tag == 'tr' && node.attributes?.['data-sid']).length, 750,
		'the lightweight table renders exactly the selected subscription group');
	assert.equal(findElements(largeEnvironment.rendered, (node) => [ 'input', 'select', 'textarea' ].includes(node.tag)).length, 0,
		'the read-only subscription inventory creates no hidden generated edit widgets');
	testCommittedUcodePreviewAndObservedCandidateGuard();
	testOverviewStateSeparatesPendingSavedAndActiveFacts();
	testSubscriptionRPCRejectsPendingSession();
	testNodeImportUcodeUsesTargetCompatibleStringPopen();
	testNodeExportUcodeUsesCurrentCandidateSnapshot();
	const freshSections = parseUCIConfig(fs.readFileSync(path.join(root, 'steer/files/etc/config/steer'), 'utf8'));
	assert.deepEqual(freshSections.route.map((route) => [ route['.name'], route.enabled, route.kind ]), [
		[ 'direct', undefined, 'direct' ],
		[ 'block', '0', 'block' ]
	], 'The packaged fresh UCI keeps Direct before a disabled Reject route');

	for (const fixture of ruleSummaryFixtures.cases) {
		const rule = { '.name': 'fixture_rule', ...fixture.rule };
		if (typeof(rule.default) == 'boolean') rule.default = rule.default ? '1' : '0';
		let fixtureEnvironment = await renderRules({ rule: [ rule ], dns_profile: [], route: [], local_proxy: [] });
		const summaryOption = allOptions(fixtureEnvironment).find((option) => option.name == '_match');
		assert.deepEqual(summaryOption.summaryTokens('fixture_rule'), fixture.tokens, fixture.name);
		assert.equal(summaryOption.dnsContinues('fixture_rule'), fixture.dns_continues, fixture.name);
		const renderedSummary = summaryOption.cfgvalue('fixture_rule');
		if (fixture.name == 'protocol-only' || fixture.name == 'network-only') {
			assert.ok(!renderedSummary.includes('No match condition') && renderedSummary.includes('DNS continues'), fixture.name);
		}
	}

	const referenceSections = canonicalFixtureSections(collectionReferenceFixtures.intent);
	const referenceEnvironments = {
		nodes: await renderNodes(referenceSections, '', { subscriptions: [] }, 'nodes'),
		routes: await renderNodes(referenceSections, '', { subscriptions: [] }, 'routes'),
		dns_profiles: await renderDns(referenceSections),
		local_proxies: await renderLocalProxies(referenceSections),
		subscriptions: await renderNodes(referenceSections, '', { subscriptions: [] }, 'subscriptions')
	};
	for (const fixture of collectionReferenceFixtures.cases) {
		const environment = referenceEnvironments[fixture.target_collection];
		const type = { nodes: 'node', routes: 'route', dns_profiles: 'dns_profile', local_proxies: 'local_proxy', subscriptions: 'subscription' }[fixture.target_collection];
		const section = environment.maps.flatMap((map) => map.sections).find((candidate) => candidate.sectionType == type && candidate.removalReferences);
		const actual = section.removalReferences(fixture.target_id).map((reference) => ({
			source_collection: reference.objectType == 'rule' ? 'rules' : 'routes',
			source_object_type: reference.objectType,
			source_id: reference.id,
			field: reference.option
		}));
		assert.deepEqual(actual, fixture.references,
			`LuCI reference guard drifted for ${fixture.target_collection}/${fixture.target_id}`);
	}

	let environment = await renderRules({
		rule: [ {
			'.name': 'default',
			name: 'Default',
			default: '1',
			dns_profile: 'direct_dns',
			route: 'direct'
		} ],
		dns_profile: [ { '.name': 'direct_dns' } ],
		route: [ { '.name': 'direct' } ],
		local_proxy: [],
	});
	let options = allOptions(environment);
	assertAutomaticIdsAndOptionalNames(environment, 'Rules');
	assert.equal(options.some((option) => option.name == 'inbound'), false,
		'Rules must not create a choice-only MultiValue without candidates');
	const summary = options.find((option) => option.name == '_match');
	assert.ok(summary, 'Ordinary rules retain an explicit match summary');
	const sourceMac = options.find((option) => option.name == 'source_mac_address');
	assert.ok(sourceMac && sourceMac.datatype == 'macaddr' && sourceMac.modalonly,
		'Rules expose a validated source MAC field without asking users for IP aliases');
	const defaultSection = environment.maps[0].sections.find((section) =>
		section.filter?.('default'));
	const ordinaryRuleSection = environment.maps[0].sections.find((section) =>
		section.sectionType == 'rule' && section.filter?.('default') === false);
	assert.equal(ordinaryRuleSection?.addBeforeSectionId, 'default',
		'A first LuCI rule created from a Default-only config is explicitly ordered before Default');
	assert.ok(defaultSection &&
		defaultSection.options.map((option) => option.name).join(',') == 'dns_profile,route',
		'Default is a fixed non-modal row that exposes only DNS profile and route');
	assert.equal(options.some((option) => option.name == 'default'), false,
		'Ordinary rules cannot be converted into or edit the Default rule');

	const geoSections = {
		rule: [ {
			'.name': 'geo_rule',
			domain_match: [ 'domain:example.com', 'geosite:category-example' ],
			ip_match: [ '192.0.2.0/24', 'geoip:example' ],
			dns_profile: 'direct_dns',
			route: 'direct'
		}, {
			'.name': 'default', default: '1', dns_profile: 'direct_dns', route: 'direct'
		} ],
		dns_profile: [ { '.name': 'direct_dns' } ],
		route: [ { '.name': 'direct' } ],
		local_proxy: []
	};
	environment = await renderRules(geoSections, {
		geosite: { ok: true, names: [ 'category-example', 'category-media', 'category-media@test' ] },
		geoip: { ok: true, names: [ 'example', 'private' ] }
	});
	options = allOptions(environment);
	const domainMatch = options.find((option) => option.name == 'domain_match');
	const ipMatch = options.find((option) => option.name == 'ip_match');
	assert.ok(domainMatch && ipMatch && domainMatch.type == ipMatch.type,
		'Domain and destination IP use the same multiline match editor');
	assert.deepEqual(domainMatch.editorSuggestions('domain', 'geosite:media', domainMatch.catalogNames),
		[ 'geosite:category-media', 'geosite:category-media@test' ],
		'Domain editor filters GeoSite names using the current line');
	assert.deepEqual(ipMatch.editorSuggestions('ip', 'geoip:pri', ipMatch.catalogNames),
		[ 'geoip:private' ],
		'Destination editor filters GeoIP names using the current line');
	assert.deepEqual(domainMatch.editorSuggestions('domain', 'do', domainMatch.catalogNames),
		[ 'domain:' ], 'Domain editor completes supported syntax prefixes');
	assert.equal(domainMatch.acceptsSuggestion('Tab'), true,
		'Tab accepts the active completion candidate');
	assert.equal(domainMatch.acceptsSuggestion('Enter'), false,
		'Enter remains available for inserting a new editor line');
	assert.deepEqual(domainMatch.editorLines('domain:example.com\n\n geosite:category-media@test '),
		[ 'domain:example.com', 'geosite:category-media@test' ],
		'Multiline editor stores one normalized expression per non-empty line');
	assert.deepEqual(domainMatch.currentLine('full:a.example\ngeosite:media\nregexp:x', 27),
		{ start: 15, end: 28, value: 'geosite:media' },
		'Completion reads only the line under the cursor');
	assert.equal(domainMatch.validate, undefined,
		'Geo existence is decided by the shared backend validator, not duplicated in the editor');
	const editorInstance = Object.create(domainMatch.type.prototype);
	editorInstance.option = 'domain_match';
	assert.equal(editorInstance.cfgvalue('geo_rule'),
		'domain:example.com\ngeosite:category-example',
		'Editor displays a UCI list as one expression per line');
	editorInstance.write('geo_rule', ' full:www.example.com \n\n geosite:category-media@test ');
	assert.deepEqual(geoSections.rule[0].domain_match,
		[ 'full:www.example.com', 'geosite:category-media@test' ],
		'Editor writes normalized non-empty lines back as one UCI list');
	assert.equal(options.some((option) => [ 'domain', 'domain_suffix', 'domain_keyword', 'geosite', 'geoip', 'ip_cidr' ].includes(option.name)), false,
		'Legacy split fields are absent from the rule editor');
	assert.equal(options.some((option) => option.name == 'rule_set'), false,
		'Users never manage or reference an intermediate rule-set object');

	environment = await renderRules({
		rule: [ {
			'.name': 'stale_inbound',
			inbound: [ 'missing_proxy' ],
			dns_profile: 'direct_dns',
			route: 'direct'
		} ],
		dns_profile: [ { '.name': 'direct_dns' } ],
		route: [ { '.name': 'direct' } ],
		local_proxy: []
	});
	options = allOptions(environment);
	const inbound = options.find((option) => option.name == 'inbound');
	assert.ok(inbound, 'A dangling inbound reference must remain visible for repair');
	assert.deepEqual(inbound.values, [ [ 'missing_proxy', 'Missing: missing_proxy' ] ]);

	environment = await renderNodes(freshSections, '', undefined, 'routes');
	options = allOptions(environment);
	assertAutomaticIdsAndOptionalNames(environment, 'Fresh routes');
	const systemRoutes = environment.maps[0].sections.filter((section) => section.type == 'NamedSection');
	assert.deepEqual(systemRoutes.map((section) => section.sectionId), [ 'direct', 'block' ],
		'Direct and Reject render as fixed system-route sections');
	assert.deepEqual(systemRoutes.map((section) => section.title), [ 'Direct', 'Reject' ],
		'Unnamed system routes use stable localized kind labels');
	assert.ok(systemRoutes.every((section) => section.addremove === false),
		'System routes cannot be removed');
	const directSection = systemRoutes.find((section) => section.sectionId == 'direct');
	assert.equal(directSection.options.find((option) => option.name == '_system_status')?.cfgvalue('direct'),
		'Required · always enabled',
		'Direct system route renders its fixed status instead of an empty UCI value');
	assert.equal(directSection.options.find((option) => option.name == '_system_kind')?.cfgvalue('direct'),
		'Direct',
		'Direct system route renders its fixed kind instead of an empty UCI value');
	assert.equal(systemRoutes.find((section) => section.sectionId == 'block').options.find((option) => option.name == '_system_kind')?.cfgvalue('block'),
		'Reject',
		'Reject system route renders its fixed kind instead of an empty UCI value');
	const rejectSection = systemRoutes.find((section) => section.sectionId == 'block');
	const rejectEnabled = rejectSection.options.find((option) => option.name == 'enabled');
	assert.equal(rejectEnabled?.type, 'Flag',
		'Reject remains explicitly enableable');
	assert.equal(environment.uci.get('steer', 'block', 'enabled'), '0',
		'Fresh Reject starts disabled');
	await rejectEnabled.submit('block', '1');
	assert.equal(environment.uci.get('steer', 'block', 'enabled'), '1',
		'Reject can be enabled without changing its fixed kind');
	assert.ok(systemRoutes.every((section) => {
		const name = section.options.find((option) => option.name == 'name');
		return name?.rmempty === true && name?.optional === true;
	}), 'Fresh Direct and Reject can be saved without optional names');
	await systemRoutes.find((section) => section.sectionId == 'direct').options
		.find((option) => option.name == 'name').submit('direct', '');
	assert.equal(environment.uci.get('steer', 'direct', 'name'), undefined,
		'Saving fresh Direct preserves its absent optional name');
	const emptyRoutes = environment.maps[0].sections.find((section) => section.type == 'GridSection' && section.sectionType == 'route');
	assert.deepEqual(emptyRoutes.addDefaults, { enabled: '1', kind: 'single', node: '' },
		'New route rows are always initialized as enabled single-node routes');
	assert.equal(emptyRoutes.addremove, true,
		'Disabling Single creation must not remove repair/delete controls for existing routes');
	assert.equal(options.some((option) => option.name == 'kind'), false,
		'The route UI cannot create or convert another Direct or Reject route');
	assert.equal(options.some((option) => option.name == 'node'), false,
		'Routes must not create a ListValue without node candidates');
	assert.equal(rejectSection.options.some((option) => option.name == 'detour'), false,
		'Reject cannot be used as or configured with a detour');
	assert.equal(findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Create Reject route').length, 0,
		'Fresh UCI does not offer a duplicate Reject creation action');
	const freshAddRow = emptyRoutes.renderSectionAdd();
	const freshAddButton = freshAddRow.querySelector('.cbi-button-add');
	const freshAddReason = findElements(freshAddRow,
		(node) => node.tag == 'p' && node.children?.[0] == 'Create or enable a proxy node before adding a single-node route.')[0];
	assert.equal(freshAddButton.disabled, true,
		'Add Single remains disabled when no enabled Node exists');
	assert.ok(freshAddReason && freshAddReason.hidden === false,
		'The disabled Add Single control explains how to enable it');

	const legacySections = {
		node: [],
		route: [ { '.name': 'direct', kind: 'direct' } ]
	};
	environment = await renderNodes(legacySections, '', undefined, 'routes');
	const createReject = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Create Reject route')[0];
	assert.ok(createReject, 'An older config without Reject exposes the one-time recovery action');
	await createReject.attributes.click({ preventDefault: () => {} });
	assert.deepEqual(legacySections.route.map((route) => [ route['.name'], route.enabled, route.kind ]), [
		[ 'direct', undefined, 'direct' ],
		[ 'block', '0', 'block' ]
	], 'Reject recovery appends one disabled fixed block route after Direct');
	assert.equal(environment.window.location.reloadCount, 1,
		'Successful Reject recovery reloads the form once');
	environment = await renderNodes(legacySections, '', undefined, 'routes');
	assert.equal(findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Create Reject route').length, 0,
		'Reject recovery disappears after the system route exists');
	const collisionSections = {
		node: [ { '.name': 'node_a', enabled: '1' } ],
		route: [
			{ '.name': 'direct', kind: 'direct' },
			{ '.name': 'block', kind: 'single', node: 'node_a' }
		]
	};
	environment = await renderNodes(collisionSections, '', undefined, 'routes');
	const createCollisionReject = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Create Reject route')[0];
	await createCollisionReject.attributes.click({ preventDefault: () => {} });
	assert.deepEqual(collisionSections.route.map((route) => [ route['.name'], route.kind ]), [
		[ 'direct', 'direct' ], [ 'block', 'single' ], [ 'block_2', 'block' ]
	], 'Reject recovery chooses a stable free ID when block is already occupied');

	const disabledNodeSections = parseUCIConfig(fs.readFileSync(path.join(root, 'steer/files/etc/config/steer'), 'utf8'));
	disabledNodeSections.node = [ { '.name': 'subscription_node', enabled: '0', source_subscription: 'feed' } ];
	environment = await renderNodes(disabledNodeSections, '', undefined, 'routes');
	const guardedRoutes = environment.maps[0].sections.find((section) => section.type == 'GridSection' && section.sectionType == 'route');
	const guardedAddRow = guardedRoutes.renderSectionAdd();
	const guardedInput = guardedAddRow.querySelector('.cbi-section-create-name');
	const guardedButton = guardedAddRow.querySelector('.cbi-button-add');
	assert.equal(guardedButton.disabled, true,
		'A disabled subscription Node is not eligible for Add Single');
	environment.uci.set('steer', 'subscription_node', 'enabled', '1');
	guardedInput.value = 'route_proxy';
	guardedInput.listeners.keyup.forEach((listener) => listener());
	assert.equal(guardedButton.disabled, null,
		'An enabled Node restores Add Single without reloading the page');
	assert.equal(environment.window.location.reloadCount, 0,
		'Dynamic Add Single recovery does not reload the page');

	freshSections.route.push(
		{ '.name': 'single_a', enabled: '1', kind: 'single', node: 'node_a' },
		{ '.name': 'single_b', enabled: '1', kind: 'single', node: 'node_b' }
	);
	environment = await renderRules(freshSections);
	const routePicker = allOptions(environment).find((option) => option.name == 'route');
	assert.deepEqual(routePicker.values, [
		[ 'direct', 'Direct' ], [ 'block', 'Reject' ], [ 'single_a', 'single_a' ], [ 'single_b', 'single_b' ]
	], 'Rules use localized system fallbacks while unnamed Single routes remain distinguishable by stable ID');
	await rejectEnabled.submit('block', '0');
	assert.equal(freshSections.route.find((route) => route['.name'] == 'block').enabled, '0',
		'Reject can be disabled again without deletion or kind mutation');

	environment = await renderNodes({
		node: [],
		route: [ { '.name': 'broken', kind: 'single', node: 'missing_node' } ]
	}, '', undefined, 'routes');
	options = allOptions(environment);
	const missingNode = options.find((option) => option.name == 'node');
	assert.ok(missingNode, 'A dangling route node must remain visible for repair');
	assert.deepEqual(missingNode.values, [ [ 'missing_node', 'Missing: missing_node', 'Group: Missing references' ] ]);

	environment = await renderDns({ dns_profile: [] });
	assertAutomaticIdsAndOptionalNames(environment, 'DNS profiles');
	for (const removed of [ 'Bootstrap and encrypted DNS boundary', 'infrastructure hostnames', 'Port-53 capture alone' ])
		assert.ok(!elementText(environment.rendered).includes(removed), `LuCI DNS must not render ${removed}`);
	let dnsProtocol = allOptions(environment).find((option) => option.name == 'protocol');
	assert.deepEqual(dnsProtocol.values.map((value) => value[0]), [ 'udp', 'tcp', 'tls', 'https', 'quic', 'h3' ],
		'DNS profiles expose exactly the six M1 transports');
	const dnsSection = environment.maps[0].sections.find((section) => section.sectionType == 'dns_profile');
	assert.deepEqual(dnsSection.addDefaults, { enabled: '1', protocol: 'udp', server: '', server_port: '53' },
		'new DNS profiles use the shared default protocol and common port');
	for (const field of [ 'tls_server_name', 'path', 'insecure' ]) {
		const option = allOptions(environment).find((candidate) => candidate.name == field);
		const expected = uiSpec.dns_protocols.filter((protocol) => protocol.fields.includes(field)).map((protocol) => protocol.value);
		assert.deepEqual(option.dependencies.map((dependency) => dependency[1]), expected,
			`${field} visibility must come from the shared DNS field matrix`);
	}
	assert.equal(allOptions(environment).find((option) => option.name == 'tls_server_name').rmempty, false,
		'LuCI derives the encrypted DNS required field from the shared matrix');

	const dnsProfiles = {
		dns_profile: [
			{ '.name': 'default_port', protocol: 'https', server_port: '443', tls_server_name: 'dns.example', path: '/dns-query', insecure: '1' },
			{ '.name': 'custom_port', protocol: 'https', server_port: '8443', tls_server_name: 'dns.example', path: '/custom', insecure: '1' }
		]
	};
	environment = await renderDns(dnsProfiles);
	dnsProtocol = allOptions(environment).find((option) => option.name == 'protocol');
	const tlsServerName = allOptions(environment).find((option) => option.name == 'tls_server_name');
	assert.notEqual(tlsServerName.validate('default_port', ''), true,
		'encrypted DNS dynamically requires a TLS server name before Save');
	environment.steer.validateInput = (format, value) => format == 'dns_http_path' && String(value).startsWith('/') ? true : 'invalid DNS path';
	const dnsPath = allOptions(environment).find((option) => option.name == 'path');
	assert.equal(dnsPath.validate('default_port', 'dns-query'), 'invalid DNS path');
	assert.equal(dnsPath.validate('default_port', '/dns-query'), true,
		'DoH and DoH3 paths use the shared leading-slash format');
	dnsProtocol.write('default_port', 'udp');
	assert.deepEqual(dnsProfiles.dns_profile[0], { '.name': 'default_port', protocol: 'udp', server_port: '53' },
		'DoH to UDP clears inapplicable UCI fields and translates the prior default port');
	dnsProtocol.write('custom_port', 'udp');
	assert.deepEqual(dnsProfiles.dns_profile[1], { '.name': 'custom_port', protocol: 'udp', server_port: '8443' },
		'DNS protocol switching preserves an explicit custom UCI port while clearing stale security fields');

	const revealedIntent = {
		main: { log_level: 'debug' },
		nodes: [ {
			id: 'secret_node', uuid: 'node-uuid', password: 'node-password', obfs_password: 'obfs-password',
			private_key: '-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----',
			plugin_options: 'token=plugin-secret', extra_args: [ '--token', 'argument-secret' ]
		} ],
		local_proxies: [ { id: 'local', password: 'local-password' } ],
		subscriptions: [ { id: 'feed', url: 'https://user:secret@example.test/feed' } ],
		future: { nested: { token: 'future-token' } }
	};
	const redactedIntent = {
		main: { log_level: 'debug' },
		nodes: [ {
			id: 'secret_node', uuid: '[REDACTED]', password: '[REDACTED]', obfs_password: '[REDACTED]',
			private_key: '[REDACTED]', plugin_options: '[REDACTED]', extra_args: '[REDACTED]'
		} ],
		local_proxies: [ { id: 'local', password: '[REDACTED]' } ],
		subscriptions: [ { id: 'feed', url: '[REDACTED]' } ],
		future: { nested: { token: '[REDACTED]' } }
	};
	const pendingPreview = { ok: true, source: 'unavailable', pending: true, available: false, redacted: true, intent: null };
	const committedPreview = { ok: true, source: 'committed', pending: false, available: true, redacted: true, intent: redactedIntent };
	const revealedPreview = { ok: true, source: 'committed', pending: false, available: true, redacted: false, intent: revealedIntent };
	environment = await renderAdvanced(pendingPreview, revealedPreview);
	assert.deepEqual(environment.previewCalls, [ false ], 'Advanced requests a redacted preview by default');
	let previewText = elementText(environment.rendered);
	assert.ok(previewText.includes('Pending candidate preview unavailable') && previewText.includes('No Canonical JSON is shown'),
		'Advanced fails closed and explicitly says that pending Apply input is not being shown');
	for (const secret of [ 'node-uuid', 'node-password', 'obfs-password', 'local-password', 'future-token', 'BEGIN PRIVATE KEY', 'plugin-secret', 'argument-secret', 'user:secret' ]) {
		assert.equal(previewText.includes(secret), false, `pending-unavailable state must not contain ${secret}`);
	}
	assert.equal(previewText.includes('debug'), false, 'pending-unavailable state does not substitute the committed JSON');
	const revealSecrets = findElements(environment.rendered,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal secrets temporarily'))[0];
	assert.equal(revealSecrets.hidden, true, 'Reveal is unavailable while pending Apply input cannot be previewed');

	environment = await renderAdvanced(committedPreview, revealedPreview);
	previewText = elementText(environment.rendered);
	assert.ok(previewText.includes('Committed snapshot') && previewText.includes('debug'),
		'Advanced labels and renders Canonical JSON only when there are no pending changes');
	for (const secret of [ 'node-uuid', 'node-password', 'obfs-password', 'local-password', 'future-token', 'BEGIN PRIVATE KEY', 'plugin-secret', 'argument-secret', 'user:secret' ]) {
		assert.equal(previewText.includes(secret), false, `default committed Preview must deeply redact ${secret}`);
	}
	assert.ok(previewText.includes('[REDACTED]'), 'redacted committed Preview keeps secret locations visible');
	const revealCommittedSecrets = findElements(environment.rendered,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal secrets temporarily'))[0];
	await revealCommittedSecrets.attributes.click();
	assert.deepEqual(environment.previewCalls, [ false, true ], 'secret reveal requires a separate explicit RPC');
	previewText = elementText(environment.rendered);
	assert.ok(previewText.includes('node-uuid') && previewText.includes('node-password') && previewText.includes('BEGIN PRIVATE KEY') && previewText.includes('user:secret'),
		'explicit reveal shows only the committed snapshot temporarily');
	await revealCommittedSecrets.attributes.click();
	previewText = elementText(environment.rendered);
	assert.equal(previewText.includes('node-password'), false, 'Hide immediately scrubs revealed secrets from the preview DOM');

	const freshPage = await renderAdvanced(committedPreview, revealedPreview);
	assert.equal(elementText(freshPage.rendered).includes('node-password'), false,
		'leaving and re-entering Advanced restores default secret redaction');
	const pendingDuringReveal = await renderAdvanced(committedPreview, pendingPreview);
	const transitionReveal = findElements(pendingDuringReveal.rendered,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal secrets temporarily'))[0];
	await transitionReveal.attributes.click();
	assert.ok(elementText(pendingDuringReveal.rendered).includes('Pending candidate preview unavailable'),
		'a pending change observed during Reveal immediately removes the stale committed JSON');
	assert.equal(transitionReveal.hidden, true, 'Reveal becomes unavailable when pending changes appear');
	environment = await renderLocalProxies({ local_proxy: [] });
	assertAutomaticIdsAndOptionalNames(environment, 'Local proxies');
	environment = await renderLocalProxies({ local_proxy: [ {
		'.name': 'entry', enabled: '1', name: 'Entry', protocol: 'mixed', listen: '127.0.0.1', listen_port: '1080'
	} ] });
	options = allOptions(environment);
	const localProtocol = options.find((option) => option.name == 'protocol');
	const mixedLabel = uiSpec.local_proxy_protocols.find((item) => item.value == 'mixed').label;
	assert.equal(localProtocol.values.find((value) => value[0] == 'mixed')[1], mixedLabel,
		'LuCI must render Mixed using the shared protocol label');
	const localListen = options.find((option) => option.name == 'listen');
	const localUsername = options.find((option) => option.name == 'username');
	const localPassword = options.find((option) => option.name == 'password');
	assert.ok(localListen.description?.attributes?.class == 'steer-exposure-warning',
		'LuCI must present a prominent non-loopback exposure warning');
	assert.ok([ localListen, localUsername, localPassword ].every((option) => typeof option.validate == 'function'),
		'LuCI must validate address scope and paired authentication before form submission');
	for (const fixture of localProxyFixtures.cases) {
		assert.equal(environment.localProxyView.classifyLocalProxyListen(fixture.listen), fixture.classification,
			`LuCI classification drifted for ${fixture.name}`);
		localListen.formValues.entry = fixture.listen;
		localUsername.formValues.entry = '';
		localPassword.formValues.entry = '';
		const unauthenticated = localListen.validate('entry', fixture.listen);
		assert.equal(unauthenticated === true, fixture.allow_unauthenticated,
			`LuCI unauthenticated result drifted for ${fixture.name}`);

		localUsername.formValues.entry = 'user';
		localPassword.formValues.entry = 'secret';
		const authenticated = localListen.validate('entry', fixture.listen);
		assert.equal(authenticated === true, fixture.classification != 'invalid',
			`LuCI authenticated result drifted for ${fixture.name}`);
	}
	localListen.formValues.entry = '127.0.0.1';
	localPassword.formValues.entry = '';
	assert.notEqual(localUsername.validate('entry', 'user'), true,
		'LuCI must reject a username without its paired password');
	localUsername.formValues.entry = '';
	assert.notEqual(localPassword.validate('entry', 'secret'), true,
		'LuCI must reject a password without its paired username');
	localListen.formValues.entry = '0.0.0.0';
	localUsername.formValues.entry = '';
	localPassword.formValues.entry = '';
	assert.ok(String(localListen.validate('entry', '0.0.0.0')).includes('reachable from other devices'),
		'LuCI must explain the risk when blocking an exposed unauthenticated listener');
	const groupedFixture = {
		node: [
			{ '.name': 'cfg_manual', name: 'Manual', type: 'hysteria2' },
			{ '.name': 'jdub_0123456789ab', name: 'Subscribed', source_subscription: 'jdub' }
		],
		route: [
			{ '.name': 'route_proxy', kind: 'single', node: 'jdub_0123456789ab', detour: 'route_jp' },
			{ '.name': 'route_jp', kind: 'single', node: 'cfg_manual' },
			{ '.name': 'block', enabled: '1', kind: 'block' }
		],
		subscription: [ { '.name': 'jdub', name: 'Jdub' } ]
	};
	environment = await renderNodes(groupedFixture);
	assertAutomaticIdsAndOptionalNames(environment, 'Nodes');
	const groupedNodes = environment.maps[0].sections.find((section) => section.sectionType == 'node');
	assert.ok(groupedNodes && groupedNodes.addremove === true && groupedNodes.filter('cfg_manual') && !groupedNodes.filter('jdub_0123456789ab'),
		'The default node group shows only manually added nodes');
	assert.equal(groupedNodes.tabs.length, 0,
		'Node editing uses one continuous LuCI form instead of hiding fields behind modal tabs');
	const transport = groupedNodes.options.find((option) => option.name == 'transport');
	assert.equal(transport?.default, 'tcp',
		'LuCI node transport consumes the shared TCP / Raw default for existing omitted values');
	const hysteriaPassword = groupedNodes.options.find((option) => option.name == 'password');
	assert.ok(hysteriaPassword?.password === true && hysteriaPassword.dependencies.some((dependency) =>
		dependency[0] == 'type' && dependency[1] == 'hysteria2'),
		'Hysteria2 exposes its password as a generated secure field');
	assert.notEqual(hysteriaPassword.validate('cfg_manual', ''), true,
		'Hysteria2 password remains required in the native LuCI editor');

	const sshKey = '-----BEGIN OPENSSH PRIVATE KEY-----\nline one\nline two\n-----END OPENSSH PRIVATE KEY-----\n';
	const sshFixture = {
		node: [ { '.name': 'ssh_secret', name: 'SSH', type: 'ssh', server: 'ssh.example', server_port: '22', username: 'root', private_key: sshKey } ],
		route: [],
		subscription: []
	};
	environment = await renderNodes(sshFixture);
	const privateKey = allOptions(environment).find((option) => option.name == 'private_key');
	assert.equal(privateKey.type.prototype.__name__, 'Steer.SensitiveTextValue',
		'SSH multiline private_key uses the secret-preserving editor');
	const privateKeyEditor = Object.create(privateKey.type.prototype);
	privateKeyEditor.option = 'private_key';
	const parsePrivateKey = (formValue) => {
		const configuredValue = privateKeyEditor.cfgvalue('ssh_secret');
		if (formValue == configuredValue)
			return;
		if (formValue == null || formValue == '')
			privateKeyEditor.remove('ssh_secret');
		else
			privateKeyEditor.write('ssh_secret', formValue);
	};
	assert.equal(privateKeyEditor.cfgvalue('ssh_secret'), '__STEER_SECRET_CONFIGURED__',
		'configured private_key uses a non-secret parse sentinel instead of plaintext');
	let privateKeyWidget = privateKeyEditor.renderWidget('ssh_secret');
	let privateKeyTextarea = findElements(privateKeyWidget, (node) => node.tag == 'textarea')[0];
	assert.equal(privateKeyTextarea.value, '', 'SSH private key remains absent from the DOM until explicit reveal');
	parsePrivateKey('');
	parsePrivateKey(null);
	assert.equal(sshFixture.node[0].private_key, sshKey,
		'hidden blank and dependency-inactive parse paths must preserve the configured private_key');
	const revealKey = findElements(privateKeyWidget,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal configured secret'))[0];
	revealKey.attributes.click();
	assert.equal(privateKeyTextarea.value, sshKey, 'private_key plaintext appears only after explicit reveal');
	parsePrivateKey(privateKeyTextarea.value);
	assert.equal(sshFixture.node[0].private_key, sshKey,
		'Reveal followed by an unchanged save must not authorize deletion or rewrite the key');
	privateKeyTextarea.value = '';
	privateKeyTextarea.listeners.input[0]();
	parsePrivateKey('');
	assert.equal(sshFixture.node[0].private_key, sshKey,
		'manually blanking a revealed key still requires the independent Clear action');

	const replacementKey = '-----BEGIN OPENSSH PRIVATE KEY-----\nreplacement\n-----END OPENSSH PRIVATE KEY-----\n';
	privateKeyTextarea.value = replacementKey;
	privateKeyTextarea.listeners.input[0]();
	parsePrivateKey(privateKeyTextarea.value);
	assert.equal(sshFixture.node[0].private_key, replacementKey,
		'an input event plus a non-empty replacement is a provable explicit edit');

	privateKeyWidget = privateKeyEditor.renderWidget('ssh_secret');
	privateKeyTextarea = findElements(privateKeyWidget, (node) => node.tag == 'textarea')[0];
	const revealAfterReplacement = findElements(privateKeyWidget,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal configured secret'))[0];
	const clearThenCancel = findElements(privateKeyWidget,
		(node) => node.tag == 'button' && elementText(node).includes('Clear configured secret'))[0];
	clearThenCancel.attributes.click();
	assert.equal(revealAfterReplacement.disabled, true,
		'Reveal is unavailable while an explicit Clear is pending');
	assert.equal(sshFixture.node[0].private_key, replacementKey, 'Clear is pending until the current modal is saved');
	/* Simulate Cancel/reopen with the same LuCI option instance. */
	privateKeyWidget = privateKeyEditor.renderWidget('ssh_secret');
	privateKeyTextarea = findElements(privateKeyWidget, (node) => node.tag == 'textarea')[0];
	assert.equal(privateKeyTextarea.value, '', 'reopening the same option instance resets reveal and clear authorization');
	parsePrivateKey('');
	assert.equal(sshFixture.node[0].private_key, replacementKey,
		'Cancel/reopen cannot leak a prior Clear authorization into parse/remove');

	const revealAfterReopen = findElements(privateKeyWidget,
		(node) => node.tag == 'button' && elementText(node).includes('Reveal configured secret'))[0];
	const clearAndSave = findElements(privateKeyWidget,
		(node) => node.tag == 'button' && elementText(node).includes('Clear configured secret'))[0];
	clearAndSave.attributes.click();
	assert.equal(revealAfterReopen.disabled, true,
		'Clear prevents Reveal from silently cancelling the pending deletion with stale button text');
	privateKeyTextarea.value = 'replacement typed after Clear';
	privateKeyTextarea.listeners.input[0]();
	assert.equal(clearAndSave.disabled, false, 'typing a replacement cancels pending Clear and re-enables the action');
	assert.equal(revealAfterReopen.disabled, false,
		'typing a replacement leaves the editor in a consistent non-Clear state');
	assert.ok(elementText(clearAndSave).includes('Clear configured secret'),
		'typing a replacement restores the Clear button label to match the actual state');
	privateKeyTextarea.value = '';
	privateKeyTextarea.listeners.input[0]();
	assert.equal(clearAndSave.disabled, false,
		'manually blanking a replacement still requires a fresh explicit Clear action');
	clearAndSave.attributes.click();
	assert.equal(revealAfterReopen.disabled, true, 'the renewed Clear action again locks out Reveal until save or cancel');
	parsePrivateKey('');
	assert.equal(sshFixture.node[0].private_key, undefined,
		'only the current editor\'s explicit Clear action authorizes secret deletion');
	environment = await renderNodes(groupedFixture, '', undefined, 'routes');
	const groupedPicker = environment.maps[0].sections.flatMap((section) => section.options)
		.find((option) => option.name == 'node');
	assert.ok(groupedPicker && groupedPicker.type == 'RichListValue' && groupedPicker.values.some((value) => value[2]?.includes('Jdub')),
		'Node selectors expose the source subscription as a visual group');
	assert.equal(groupedPicker.editable, true,
		'Existing single-node routes render an editable Node selector in GridSection cells');
	assert.deepEqual(groupedPicker.dependencies, [],
		'Node selector is not hidden by a dependency on the unrendered route kind field');
	assert.equal(groupedPicker.textvalue('route_proxy'), 'Subscribed',
		'Route summaries show the node name instead of its internal UCI ID');
	const routeSection = environment.maps[0].sections.find((section) => section.type == 'GridSection' && section.sectionType == 'route');
	const detourPicker = routeSection.options.find((option) => option.name == 'detour');
	assert.equal(detourPicker.editable, true,
		'Existing single-node routes render an editable Detour selector in GridSection cells');
	assert.deepEqual(detourPicker.dependencies, [],
		'Detour selector is not hidden by a dependency on the unrendered route kind field');
	assert.deepEqual(detourPicker.values[0], [ '', 'Direct connection' ],
		'Detour picker can clear an existing detour and dial the node directly');
	assert.ok(detourPicker.values.some((value) => value[0] == 'route_proxy' && value[1] == 'route_proxy') &&
		detourPicker.values.some((value) => value[0] == 'route_jp' && value[1] == 'route_jp'),
		'Unnamed Single detours remain distinguishable by stable route ID');
	assert.equal(routeSection.sectiontitle('route_proxy'), 'Unnamed',
		'Unnamed Single route summaries do not expose the internal UCI section ID');
	assert.equal(detourPicker.values.some((value) => value[0] == 'block'), false,
		'Reject is never offered as a detour');
	await detourPicker.submit('route_proxy', '');
	assert.equal(environment.uci.get('steer', 'route_proxy', 'detour'), undefined,
		'An empty RichListValue is valid and removes the detour UCI option on save');
	await detourPicker.submit('route_proxy', 'route_jp');
	assert.equal(environment.uci.get('steer', 'route_proxy', 'detour'), 'route_jp',
		'A non-empty detour remains writable');
	[ '_route_connect_test', '_route_download_test' ].forEach((name) => {
		const option = routeSection.options.find((candidate) => candidate.name == name);
		assert.ok(option && option.editable && option.default === undefined,
			`Route test action ${name} is visible without a persistent value`);
		assert.deepEqual(option.dependencies, [ [ 'enabled', '1' ] ],
			`Route test action ${name} depends only on the rendered enabled field`);
		assert.equal(option.write('route_proxy', '1'), undefined);
		assert.equal(option.remove('route_proxy'), undefined);
		assert.equal(environment.uci.get('steer', 'route_proxy', name), undefined,
			`Route test action ${name} never enters UCI`);
	});
	const routeTestButton = { disabled: false, textContent: '', title: '', classList: { toggle: () => {} } };
	await routeSection.options.find((option) => option.name == '_route_connect_test')
		.onclick({ currentTarget: routeTestButton }, 'route_proxy');
	assert.deepEqual(environment.routeSpeedtestCalls, [ { route: 'route_proxy', download: false } ],
		'Route chain test passes the route ID to RPC');

	const enabledFixtures = probeDiagnosticsFixtures.objects;
	environment = await renderNodes({
		node: enabledFixtures.nodes.map((item) => ({ '.name': item.id, enabled: item.enabled ? '1' : '0', name: item.id })),
		route: [], subscription: []
	}, '', { subscriptions: [] }, 'nodes', [
		[ 'set', 'main', 'log_level', 'debug' ],
		[ 'set', 'node_enabled', 'server', 'pending.example' ]
	]);
	const pendingNodeSection = environment.maps[0].sections.find((section) => section.sectionType == 'node');
	const pendingConnect = pendingNodeSection.options.find((option) => option.name == '_connect_speedtest');
	const pendingDownload = pendingNodeSection.options.find((option) => option.name == '_download_speedtest');
	assert.ok([ pendingConnect, pendingDownload ].every((option) => option.dependencies.length == 0),
		'Node test actions do not depend on an enabled widget that subscription summaries omit');
	const persistedNodeProbe = elementText(pendingDownload.renderWidget('node_enabled'));
	assert.ok(/Tested at.*Succeeded.*16\.0 Mbps/.test(persistedNodeProbe) && !persistedNodeProbe.includes('Outdated'),
		'LuCI Node restores the persisted latest download result beside its action');
	for (const forbidden of probeDiagnosticsFixtures.ordinary_ui.forbidden_fragments)
		assert.ok(!persistedNodeProbe.includes(forbidden), `LuCI Node latest result hides ${forbidden}`);
	const pendingBatchButtons = findElements(environment.rendered,
		(node) => node.tag == 'button' && String(node.children?.[0] || '').startsWith('Batch'));
	assert.equal(pendingBatchButtons.length, 2);
	assert.ok(pendingBatchButtons.every((button) => button.disabled && button.title.includes('Pending Steer changes')),
		'pending changes visibly disable every batch Node probe');

	const latestRouteEnvironment = await renderNodes({
		node: [ { '.name': 'node_enabled', enabled: '1', name: 'Node' } ],
		route: [ { '.name': 'route_enabled', enabled: '1', name: 'Route', kind: 'single', node: 'node_enabled' } ],
		subscription: []
	}, '', undefined, 'routes', [], {}, probeDiagnosticsFixtures.probe_results);
	const latestRouteSection = latestRouteEnvironment.maps[0].sections.find((section) => section.sectionType == 'route');
	const persistedRouteProbe = elementText(latestRouteSection.options
		.find((option) => option.name == '_route_connect_test').renderWidget('route_enabled'));
	assert.ok(/Tested at.*Outdated.*Failed.*连接超时/.test(persistedRouteProbe),
		'LuCI Route restores and expires the persisted latest connection result beside its action');
	for (const forbidden of probeDiagnosticsFixtures.ordinary_ui.forbidden_fragments)
		assert.ok(!persistedRouteProbe.includes(forbidden), `LuCI Route latest result hides ${forbidden}`);
	const pendingProbeButton = { disabled: false, textContent: '', title: '', classList: { toggle: () => {}, add: () => {} } };
	await pendingConnect.onclick({ currentTarget: pendingProbeButton }, 'node_enabled');
	assert.deepEqual(environment.speedtestCalls, [], 'changed existing and unrelated pending data block a committed Node probe');
	environment.setPendingChanges([ [ 'add', 'node_new', 'node' ] ]);
	await pendingConnect.onclick({ currentTarget: pendingProbeButton }, 'node_new');
	assert.deepEqual(environment.speedtestCalls, [], 'a new pending Node never sends a guaranteed-failing committed probe');
	environment.setPendingChanges([]);
	await pendingConnect.onclick({ currentTarget: pendingProbeButton }, 'node_enabled');
	assert.deepEqual(environment.speedtestCalls, [ { node: 'node_enabled', download: false } ],
		'Node probes resume from committed data after Apply or Discard without a page reload');
	assert.ok(pendingBatchButtons.every((button) => !button.disabled));
	const disabledProbeWidget = pendingConnect.renderWidget('node_disabled');
	const disabledProbeButton = findElements(disabledProbeWidget, (node) => node.tag == 'button')[0];
	assert.ok(disabledProbeButton.disabled && disabledProbeButton.title.includes('Disabled Nodes'),
		'disabled Nodes retain a visible test action with an explicit unavailable reason');
	await pendingConnect.onclick({ currentTarget: disabledProbeButton }, 'node_disabled');
	assert.deepEqual(environment.speedtestCalls, [ { node: 'node_enabled', download: false } ],
		'disabled Node actions never reach the backend even when invoked programmatically');

	environment = await renderNodes({
		node: [ { '.name': 'node_enabled', enabled: '1', name: 'Enabled' } ],
		route: enabledFixtures.routes.map((item) => ({ '.name': item.id, enabled: item.enabled ? '1' : '0', kind: item.kind, node: 'node_enabled' })),
		subscription: []
	}, '', { subscriptions: [] }, 'routes', [ [ 'add', 'new_route', 'route' ] ]);
	const pendingRouteSection = environment.maps[0].sections.find((section) => section.sectionType == 'route');
	const pendingRouteProbe = pendingRouteSection.options.find((option) => option.name == '_route_connect_test');
	await pendingRouteProbe.onclick({ currentTarget: pendingProbeButton }, 'route_enabled');
	assert.deepEqual(environment.routeSpeedtestCalls, [], 'new or edited pending Routes never probe the committed old Route');
	environment = await renderNodes({
		node: [
			{ '.name': 'cfg_manual', name: 'Manual' },
			{ '.name': 'jdub_0123456789ab', name: 'Subscribed', source_subscription: 'jdub' }
		],
		route: [],
		subscription: [ { '.name': 'jdub', name: 'Jdub' } ]
	}, '?node_group=jdub');
	const subscriptionNodes = environment.maps[0].sections.find((section) => section.sectionType == 'node');
	assert.equal(subscriptionNodes, undefined,
		'a subscription group bypasses the heavyweight editable LuCI Node section');
	const subscriptionRows = findElements(environment.rendered,
		(node) => node.tag == 'tr' && node.attributes?.['data-sid']);
	assert.deepEqual(subscriptionRows.map((row) => row.attributes['data-sid']), [ 'jdub_0123456789ab' ],
		'a subscription group renders only that subscription');
	assert.equal(findElements(environment.rendered,
		(node) => [ 'input', 'select', 'textarea' ].includes(node.tag)).length, 0,
		'subscription rows do not create generated edit widgets');
	const subscriptionRowText = elementText(subscriptionRows[0]);
	assert.ok(subscriptionRowText.includes('⠿') && !subscriptionRowText.includes('Edit') &&
		!subscriptionRowText.includes('Move up') && !subscriptionRowText.includes('Move down'),
		'subscription rows expose one direct drag handle without editor or arrow buttons');
	const connectCell = findElements(subscriptionRows[0],
		(node) => node.tag == 'td' && node.attributes?.['data-title'] == 'Connection test')[0];
	const speedtestButton = findElements(connectCell, (node) => node.tag == 'button')[0];
	await speedtestButton.attributes.click({ currentTarget: speedtestButton });
	assert.deepEqual(environment.speedtestCalls, [ { node: 'jdub_0123456789ab', download: false } ],
		'Row speed test passes the section ID instead of the click event to RPC');
	assert.equal(speedtestButton.textContent, '42 ms',
		'Connection result replaces the row test button label');
	assert.equal(speedtestButton.title, 'Succeeded',
		'Connection result keeps raw stages, URL and attempts out of the ordinary node list');
	environment.speedtestResult = {
		ok: false,
		error: 'temporary sing-box: outbound/hysteria2[steer-node-internal] context deadline exceeded',
		results: []
	};
	await speedtestButton.attributes.click({ currentTarget: speedtestButton });
	assert.equal(speedtestButton.textContent, 'Failed');
	assert.equal(speedtestButton.title, 'See diagnostic logs for details.',
		'Failed connection tests do not expose backend process or outbound identifiers in the node list');
	environment = await renderNodes({
		node: [ { '.name': 'jdub_stale', name: 'Stale', source_subscription: 'jdub' } ],
		route: [],
		subscription: [ { '.name': 'jdub', name: 'Jdub' } ]
	}, '', { subscriptions: [ {
		id: 'jdub', name: 'Jdub', enabled: true, never_fetched: false,
		last_success: '2026-08-26T01:00:00Z', last_failure: null,
		node_count: 1, current: 0, added: 0, skipped: 0,
		stale: [ { id: 'jdub_stale', name: 'Stale', referenced_by: [] } ]
	} ] }, 'subscriptions');
	const removeStale = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Remove Stale')[0];
	assert.ok(removeStale, 'Subscription status exposes cleanup only for stale nodes');
	environment.cleanSubscriptionResult = { ok: false, error: 'NODE_STILL_REFERENCED' };
	await removeStale.attributes.click();
	assert.deepEqual(environment.cleanSubscriptionCalls, [ { id: 'jdub', node: 'jdub_stale' } ],
		'Stale cleanup passes subscription and node IDs to RPC');
	assert.equal(environment.window.location.reloadCount, 0,
		'Failed stale cleanup does not reload and hide the backend error');
	assert.equal(environment.notifications[0]?.level, 'danger');
	assert.equal(environment.notifications[0]?.body.children[0], 'NODE_STILL_REFERENCED',
		'Failed stale cleanup shows the backend diagnostic');
	environment.cleanSubscriptionResult = { ok: true };
	await removeStale.attributes.click();
	assert.equal(environment.window.location.reloadCount, 1,
		'Successful stale cleanup reloads the current subscription view');

	const sharedStatuses = subscriptionStatusFixtures.cases.map((fixture) => fixture.status);
	environment = await renderNodes({
		node: [
			{ '.name': 'failed_blocked', name: 'Blocked stale', source_subscription: 'failed', pinned_stale: '1' }
		],
		route: [ { '.name': 'proxy', name: 'Proxy route', node: 'failed_blocked' } ],
		subscription: sharedStatuses.map((status) => ({
			'.name': status.id, enabled: status.enabled ? '1' : '0', name: status.name, url: status.url
		}))
	}, '', { subscriptions: sharedStatuses }, 'subscriptions');
	const sharedText = elementText(environment.rendered);
	for (const expected of [ 'Not fetched', 'subscription server returned HTTP 503', '1 current / 1 unavailable / 1 skipped' ])
		assert.ok(sharedText.includes(expected), `LuCI must render shared subscription status ${expected}`);
	const sharedUpdateButtons = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Update now');
	assert.equal(sharedUpdateButtons.length, sharedStatuses.length);
	const disabledIndex = sharedStatuses.findIndex((status) => status.id == 'disabled');
	assert.equal(sharedUpdateButtons[disabledIndex].disabled, true,
		'disabled subscription Update is visibly disabled');
	await sharedUpdateButtons[disabledIndex].attributes.click();
	assert.deepEqual(environment.updateSubscriptionCalls, [],
		'disabled subscription Update never reaches RPC');

	const successStatus = sharedStatuses.find((status) => status.id == 'success');
	environment = await renderNodes({
		node: [], route: [],
		subscription: [ { '.name': 'success', enabled: '1', name: 'Successful', url: successStatus.url } ]
	}, '', { subscriptions: [ successStatus ] }, 'subscriptions', [ [ 'set', 'success', 'url', 'https://pending.example/sub' ] ]);
	const pendingUpdate = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Update now')[0];
	assert.equal(pendingUpdate.disabled, true);
	assert.ok(pendingUpdate.title.includes('Pending Steer changes'));
	await pendingUpdate.attributes.click();
	assert.deepEqual(environment.updateSubscriptionCalls, [],
		'pending unrelated Steer changes block subscription Update');
	assert.equal(environment.notifications.at(-1)?.level, 'warning');

	environment = await renderNodes({
		node: [], route: [],
		subscription: [ { '.name': 'success', enabled: '1', name: 'Successful', url: successStatus.url } ]
	}, '', { subscriptions: [ successStatus ] }, 'subscriptions');
	const visibleUpdate = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Update now')[0];
	const formNode = findElements(environment.rendered, (node) => node.tag == 'form')[0];
	formNode.listeners.input[0]({});
	assert.equal(visibleUpdate.disabled, true, 'visible unsubmitted input immediately disables Update');
	await visibleUpdate.attributes.click();
	assert.deepEqual(environment.updateSubscriptionCalls, [],
		'visible unsubmitted input blocks Update without reloading the page');
	assert.equal(environment.window.location.reloadCount, 0);

	environment = await renderNodes({
		node: [], route: [],
		subscription: [ { '.name': 'success', enabled: '1', name: 'Successful', url: successStatus.url } ]
	}, '', { subscriptions: [ successStatus ] }, 'subscriptions');
	const updateGate = {};
	updateGate.promise = new Promise((resolve) => { updateGate.resolve = resolve; });
	environment.updateSubscriptionResult = updateGate.promise;
	const inFlightUpdate = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Update now')[0];
	const inFlightForm = findElements(environment.rendered, (node) => node.tag == 'form')[0];
	const updating = inFlightUpdate.attributes.click();
	await Promise.resolve();
	inFlightForm.listeners.change[0]({});
	updateGate.resolve({ ok: true, subscriptions: [ successStatus ] });
	await updating;
	assert.equal(environment.window.location.reloadCount, 0,
		'visible edits made during Update are preserved instead of being lost to reload');
	assert.ok(environment.notifications.some((notification) => elementText(notification.body).includes('were preserved')));
	assert.ok(environment.notifications.some((notification) => {
		const message = elementText(notification.body);
		return message.includes('running configuration was not changed') && message.includes('nodes still used by Routes were kept') && message.includes('Added 2');
	}), 'LuCI subscription Update must report inventory counters, unchanged runtime and retained referenced nodes');
	environment = await renderOverview({
		steer: [ { '.name': 'main', dns_cache_capacity: '0' } ], subscription: []
	}, 'general');
	const cacheCapacity = allOptions(environment).find((option) => option.name == 'dns_cache_capacity');
	assert.equal(cacheCapacity.cfgvalue('main'), '',
		'LuCI General must present the default DNS cache capacity as an empty field');
	const probeOptions = [ 'probe_direct', 'probe_proxy', 'speedtest_proxy' ].map((name) =>
		allOptions(environment).find((option) => option.name == name));
	assert.equal(allOptions(environment).find((option) => option.name == 'enabled'), undefined,
		'General does not duplicate the service Enable control owned by the global status area');
	assert.ok(probeOptions.every((option) => option?.type == 'Value' && option.rmempty === false),
		'Schema 7 probe URLs are required scalar fields');
	const seenProbeFormats = [];
	environment.steer.validateInput = (format, value) => { seenProbeFormats.push([ format, value ]); return value == 'https://valid.example/' ? true : 'invalid probe'; };
	for (const option of probeOptions) {
		assert.equal(option.validate('main', 'http://invalid.example/'), 'invalid probe');
		assert.equal(option.validate('main', 'https://valid.example/'), true);
	}
	assert.ok(seenProbeFormats.every((entry) => entry[0] == 'probe_url'),
		'all three probe fields use the shared strict HTTPS format');
	assert.ok(environment.maps[0].sections.every((section) => section.tabs.length == 0),
		'General renders one LuCI form without a redundant third-level tab menu');
	environment = await renderOverview({}, 'steer');
	assert.equal(environment.maps.length, 0,
		'The Steer root resolves to Overview instead of accidentally rendering General');
	assert.equal(environment.statusRenderCalls, 0,
		'The Steer root uses the lifecycle panel instead of the legacy mixed status headline');
	for (const expected of [ 'Execution model', 'Configuration lifecycle', 'Working copy', 'Saved configuration',
		'Active status', 'Working copy scale', 'Validation and warning summary', 'Recent application and shortcuts' ])
		assert.ok(elementText(environment.rendered).includes(expected), `Overview lifecycle panel must render ${expected}`);
	assert.deepEqual(findElements(environment.rendered, (node) => node.attributes?.['data-overview-region'])
		.map((node) => node.attributes['data-overview-region']),
	[ 'execution_model', 'configuration_lifecycle', 'object_scale', 'validation_summary', 'last_apply_and_actions' ]);
	for (const expected of [ 'Nodes', 'Routes', 'DNS Profiles', 'Local Proxies', 'Rules', 'Subscriptions' ])
		assert.ok(elementText(environment.rendered).includes(expected), `Overview Draft scale must render ${expected}`);
	assert.ok(!elementText(environment.rendered).includes('generation-a'), 'Overview lifecycle panel hides the internal generation identifier');
	assert.equal(findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Run test').length, 0,
		'Overview does not duplicate the probes owned by Diagnostics');
	const groupedWarnings = Array.from({ length: 120 }, (_, index) => ({
		code: 'INSECURE_TLS', object_type: 'node', object_id: `private-node-${index}`,
		option: 'insecure', message: `raw secret warning ${index}`
	}));
	const warningValidation = {
		ok: true, errors: [], warnings: groupedWarnings,
		warning_groups: [{ code: 'INSECURE_TLS', object_type: 'node', option: 'insecure', count: 120,
			summary: 'TLS certificate verification is disabled', destination: 'nodes' }]
	};
	const warningLifecycle = {
		...environment.lifecycleState,
		pending: true,
		desired: { ...environment.lifecycleState.desired, validation: warningValidation },
		saved: { ...environment.lifecycleState.saved, validation: warningValidation }
	};
	const warningEnvironment = await renderOverview({}, 'overview', warningLifecycle);
	const warningText = elementText(warningEnvironment.rendered);
	assert.ok(warningText.includes('TLS certificate verification is disabled') && warningText.includes('120 affected in-use node(s)'),
		'LuCI Overview renders the bounded backend warning group');
	assert.ok(!warningText.includes('private-node-') && !warningText.includes('raw secret warning'),
		'LuCI Overview hides Warning object IDs and raw messages');
	assert.equal(findElements(warningEnvironment.rendered,
		(node) => String(node.attributes?.class || '').split(/\s+/).includes('steer-warning-group')).length, 1,
		'a large Warning set renders one LuCI summary row');
	for (const fixture of stateLifecycleFixtures.cases) {
		const lifecycle = {
			ok: true,
			pending: fixture.draft.dirty,
			pending_apply: fixture.pending_apply,
			desired: { available: true, enabled: fixture.draft.enabled, digest: 'desired', counts: {}, validation: { ok: true, errors: [], warnings: [] } },
			saved: { available: true, enabled: fixture.saved.enabled, digest: fixture.saved.revision, counts: {}, validation: { ok: true, errors: [], warnings: [] } },
			active: {
				healthy: fixture.active.healthy,
				generation: fixture.active.generation || '',
				intent_digest: fixture.active.digest || '',
				last_apply: fixture.active.last_apply_ok == null ? null : { sequence: '1', result: { ok: fixture.active.last_apply_ok, error: fixture.active.last_apply_ok ? '' : 'activation failed' } }
			}
		};
		const lifecycleEnvironment = await renderOverview({}, 'overview', lifecycle);
		const lifecycleText = elementText(lifecycleEnvironment.rendered);
		for (const expected of [ 'Working copy', 'Saved configuration', 'Active status' ])
			assert.ok(lifecycleText.includes(expected), `${fixture.name} must render ${expected}`);
		if (fixture.name == 'pending-disable') {
			assert.ok(!lifecycleText.includes('generation-old') && /Active status\s+Normal/.test(lifecycleText),
				'pending disable keeps the service state visible without exposing the generation identifier');
			assert.ok(lifecycleText.includes('Consistent') && lifecycleText.includes('Unsaved changes'));
		}
		if (fixture.name == 'failed-apply') {
			assert.ok(lifecycleText.includes('The running configuration did not change') && !lifecycleText.includes('generation-old') &&
				!lifecycleText.includes('activation failed'),
				'failed application keeps the recovery state visible without exposing runtime identity');
			assert.ok(lifecycleText.includes('Save, Apply Saved, Save and Apply, and Discard are available in the global status area above.'),
				'Overview points to the single global action owner instead of duplicating lifecycle buttons');
		}
	}
	environment = await renderNodes({ subscription: [], node: [], route: [] }, '', { subscriptions: [] }, 'subscriptions');
	assertAutomaticIdsAndOptionalNames(environment, 'Subscriptions');
	const subscriptionSection = environment.maps[0].sections.find((section) => section.sectionType == 'subscription');
	assert.ok(subscriptionSection && subscriptionSection.addremove && subscriptionSection.anonymous === false,
		'Subscriptions retain stable named UCI sections');
	assert.equal(typeof subscriptionSection.handleAdd, 'function',
		'Subscription creation uses the automatic stable ID path');
	assert.equal(typeof subscriptionSection.handleRemove, 'function',
		'Subscription removal uses the shared reference guard and generated-node cascade');
	assert.equal(subscriptionSection.addDefaults.update_interval, uiSpec.subscription_update_interval_default,
		'LuCI subscription creation uses the shared interval default');
	const subscriptionInterval = subscriptionSection.options.find((option) => option.name == 'update_interval');
	const subscriptionURL = subscriptionSection.options.find((option) => option.name == 'url');
	environment.steer.validateInput = (format, value) => value == 'bad' ? `invalid ${format}` : true;
	assert.equal(subscriptionURL.validate('feed', 'bad'), 'invalid subscription_url');
	assert.equal(subscriptionInterval.validate('feed', 'bad'), 'invalid positive_duration');
	assert.equal(subscriptionInterval.validate('feed', ''), true,
		'an empty interval retains the shared manual-update-only meaning');
	assert.equal(subscriptionInterval.placeholder, uiSpec.subscription_update_interval_default,
		'LuCI update interval field exposes the shared default without changing existing empty values');
	assert.equal(subscriptionInterval.default, undefined,
		'LuCI existing manual-only subscriptions must remain empty');
	environment = await renderOverview({}, 'diagnostics');
	assert.equal(environment.statusRenderCalls, 0,
		'Diagnostics does not duplicate the Overview status panel');
	const overviewTestButtons = findElements(environment.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Run test');
	assert.equal(overviewTestButtons.length, 3,
		'Overview renders direct, proxy and proxy speed-test actions without requiring a healthy status');
	await overviewTestButtons[1].attributes.click({ preventDefault: () => {}, currentTarget: overviewTestButtons[1] });
	assert.deepEqual(environment.overviewProbeCalls, [ 'proxy' ],
		'Overview proxy test remains clickable when no healthy running status was returned');
	const diagnosticText = elementText(environment.rendered);
	for (const expected of [ 'Success only means the target was reachable', 'Tested at', '20 ms', 'Recent logs', 'Recent application result', 'Validation', 'System DNS capture check', 'System DNS capture is configured' ])
		assert.ok(diagnosticText.includes(expected), `LuCI Diagnostics must render ${expected}`);
	assert.ok(!diagnosticText.includes('nodes/node_enabled') && !diagnosticText.includes('routes/route_enabled'));
	for (const forbidden of [ 'probe.example', 'Attempts', 'Connect 7', 'TLS 9', 'Recent connectivity reports' ])
		assert.ok(!diagnosticText.includes(forbidden), `LuCI Diagnostics must not render raw report detail ${forbidden}`);
	assert.ok(!diagnosticText.includes('proves the Direct path') && !diagnosticText.includes('proves the proxy path'));

	const systemEnvironment = await renderSystem({
		steer: 'v1', canonical_schema: 9,
		sing_box: { version: '1.14.0', tags: [ 'with_quic', 'with_utls' ] },
		geodata: { version: '20260827', rule_count: 42 }
	}, {
		generation: 'generation-current',
		last_apply: { sequence: '12', result: { ok: false, generation: 'generation-failed' } }
	});
	const systemText = elementText(systemEnvironment.rendered);
	for (const expected of [ 'v1', '1.14.0', '20260827', '42', '/etc/config/steer' ])
		assert.ok(systemText.includes(expected), `LuCI System must render ${expected}`);
	for (const removed of [ 'generation-current', 'generation-failed', 'with_quic / with_utls', 'DNS capture boundary' ])
		assert.ok(!systemText.includes(removed), `LuCI System must hide ${removed}`);

	const deniedDiagnostics = await renderOverview({}, 'diagnostics', undefined, { overview_probe: false });
	const deniedOverviewButtons = findElements(deniedDiagnostics.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Run test');
	assert.ok(deniedOverviewButtons.every((button) => button.disabled === true));
	await deniedOverviewButtons[0].attributes.click({ preventDefault: () => {}, currentTarget: deniedOverviewButtons[0] });
	assert.deepEqual(deniedDiagnostics.overviewProbeCalls, [], 'read-only sessions never invoke Overview probe RPC');

	const deniedNodes = await renderNodes({
		node: [ { '.name': 'manual', enabled: '1', name: 'Manual', type: 'socks', server: 'node.example', server_port: '1080' } ],
		route: [], subscription: []
	}, '', { subscriptions: [] }, 'nodes', [], { node_speedtest: false, node_import: false, node_export: false });
	const deniedNodeProbe = allOptions(deniedNodes).find((option) => option.name == '_connect_speedtest');
	await deniedNodeProbe.onclick({ currentTarget: { classList: { toggle() {} } } }, 'manual');
	assert.deepEqual(deniedNodes.speedtestCalls, [], 'read-only sessions never invoke Node speed-test RPC');
	const deniedImport = findElements(deniedNodes.rendered,
		(node) => node.tag == 'button' && node.children?.[0] == 'Import nodes')[0];
	assert.equal(deniedImport.disabled, true);
	deniedImport.attributes.click({ preventDefault() {} });
	assert.equal(deniedNodes.modal, null, 'read-only sessions cannot open the Node import workflow');
	const deniedExport = allOptions(deniedNodes).find((option) => option.name == '_export_link');
	const deniedExportWidget = deniedExport.renderWidget('manual', 0, null);
	assert.equal(deniedExportWidget.disabled, true, 'read-only sessions see Node export disabled');

	const deniedRoutes = await renderNodes({
		node: [ { '.name': 'manual', enabled: '1', name: 'Manual' } ],
		route: [ { '.name': 'single', enabled: '1', kind: 'single', node: 'manual' } ], subscription: []
	}, '', { subscriptions: [] }, 'routes', [], { route_speedtest: false });
	const deniedRouteProbe = allOptions(deniedRoutes).find((option) => option.name == '_route_connect_test');
	await deniedRouteProbe.onclick({ currentTarget: { classList: { toggle() {} } } }, 'single');
	assert.deepEqual(deniedRoutes.routeSpeedtestCalls, [], 'read-only sessions never invoke Route speed-test RPC');

	const deniedSubscriptions = await renderNodes({
		node: [ { '.name': 'stale', source_subscription: 'feed' } ], route: [],
		subscription: [ { '.name': 'feed', enabled: '1', name: 'Feed' } ]
	}, '', { subscriptions: [ {
		id: 'feed', enabled: true, name: 'Feed', never_fetched: false, node_count: 1, current: 0, added: 0, skipped: 0,
		stale: [ { id: 'stale', referenced_by: [] } ]
	} ] }, 'subscriptions', [], { subscription_update: false, subscription_clean: false });
	const deniedInventoryButtons = findElements(deniedSubscriptions.rendered,
		(node) => node.tag == 'button' && [ 'Update now', 'Remove Unavailable node' ].includes(node.children?.[0]));
	assert.equal(deniedInventoryButtons.length, 2);
	assert.ok(deniedInventoryButtons.every((button) => button.disabled === true));
	for (const button of deniedInventoryButtons) await button.attributes.click();
	assert.deepEqual(deniedSubscriptions.updateSubscriptionCalls, []);
	assert.deepEqual(deniedSubscriptions.cleanSubscriptionCalls, [],
		'read-only sessions never invoke Subscription update or clean RPC');

	const exportEnvironment = await renderNodes({
		node: [ { '.name': 'manual', enabled: '1', name: 'Manual', type: 'socks', server: 'node.example', server_port: '1080', username: 'user', password: 'secret' } ],
		route: [], subscription: []
	});
	const exportOption = allOptions(exportEnvironment).find((option) => option.name == '_export_link');
	await exportOption.onclick({ preventDefault() {} }, 'manual');
	assert.deepEqual(exportEnvironment.exportNodeCalls, [ 'manual' ]);
	const exportModal = element('div', {}, exportEnvironment.modal.content);
	const exportTextArea = findElements(exportModal, (node) => node.tag == 'textarea')[0];
	assert.ok(exportTextArea.value.startsWith('socks://'));
	assert.equal(exportTextArea.attributes.readonly, true);
	assert.ok(elementText(exportModal).includes('Share links contain the complete Node credentials'),
		'LuCI export warns before revealing the credential-bearing link');

	const importSections = { subscription: [], node: [], route: [] };
	environment = await renderNodes(importSections, '', { subscriptions: [] });
	environment.importNodesResult = {
		nodes: [
			{ enabled: true, name: 'Unsafe TLS', type: 'vless', server: 'unsafe.example', server_port: 443, uuid: 'hidden-uuid', password: 'never-render-this', insecure: true },
			{ enabled: true, name: 'UCI-shaped flag', type: 'socks', server: 'socks.example', server_port: 1080, insecure: '1' }
		],
		skipped: 2
	};
	const importButton = findElements(environment.rendered,
		(node) => node.tag == 'button' && elementText(node) == 'Import nodes')[0];
	assert.ok(importButton, 'Manual Nodes exposes the batch import dialog');
	importButton.attributes.click({ preventDefault: () => {} });
	const importModal = element('div', {}, environment.modal.content);
	const importInput = findElements(importModal, (node) => node.tag == 'textarea')[0];
	assert.ok(String(importInput.attributes.class).split(/\s+/).includes('steer-machine-input') &&
		importInput.attributes.autocomplete == 'off' && importInput.attributes.autocapitalize == 'off' &&
		importInput.attributes.autocorrect == 'off' && importInput.attributes.spellcheck == 'false',
		'Node import uses the semantic machine-input class and disables browser text correction');
	importInput.value = 'two private share links';
	const parseImport = findElements(importModal,
		(node) => node.tag == 'button' && elementText(node) == 'Parse and preview')[0];
	await parseImport.attributes.click({ preventDefault: () => {} });
	assert.deepEqual(environment.importNodeCalls, [ 'two private share links' ]);
	const previewCards = findElements(importModal,
		(node) => node.tag == 'article' && String(node.attributes.class).includes('steer-import-node'));
	assert.equal(previewCards.length, 2, 'The preview covers every valid node in the batch');
	assert.ok(elementText(previewCards[0]).includes('Unsafe TLS') && elementText(previewCards[0]).includes('Certificate verification Disabled'));
	assert.ok(elementText(previewCards[1]).includes('UCI-shaped flag') && elementText(previewCards[1]).includes('Certificate verification Disabled'),
		'JSON true and UCI "1" normalize to the same insecure warning');
	assert.ok(elementText(previewCards[0]).includes('Credentials Present; hidden from preview'));
	assert.ok(elementText(previewCards[1]).includes('Credentials None'),
		'Credential presence is reported only when a real credential field is populated');
	assert.ok(!elementText(importModal).includes('hidden-uuid') && !elementText(importModal).includes('never-render-this'),
		'Credential contents never enter the preview DOM');
	assert.ok(elementText(importModal).includes('2 invalid node(s) were skipped; the complete valid batch is listed below.'));
	const cancelImport = findElements(importModal,
		(node) => node.tag == 'button' && elementText(node) == 'Cancel')[0];
	cancelImport.attributes.click();
	assert.equal(environment.modalHidden, true);
	assert.equal(importSections.node.length, 0,
		'Cancelling the reviewed batch creates no pending UCI section');

	const aclSource = fs.readFileSync(path.join(root,
		'luci-app-steer/root/usr/share/rpcd/acl.d/luci-app-steer.json'), 'utf8');
	const acl = JSON.parse(aclSource)['luci-app-steer'];
	assert.deepEqual(acl.read.uci, [ 'steer' ]);
	assert.equal(acl.read.ubus.service, undefined,
		'unused service.list and firewall UCI grants are removed from read-only sessions');
	for (const method of [ 'status', 'overview_state', 'validate', 'runtime', 'diagnostics', 'probe_results', 'subscriptions' ])
		assert.ok(acl.read.ubus['luci.steer'].includes(method), `read-only sessions retain ${method}`);
	assert.ok(acl.write.ubus['luci.steer'].includes('discard_candidate'),
		'discarding a pending candidate remains a write-authorized Steer operation');
	console.log('LuCI view regression tests passed.');
}

main().catch((error) => {
	console.error(error.stack || error);
	process.exit(1);
});
