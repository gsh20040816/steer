/*
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

'use strict';
'require form';
'require uci';
'require ui';
'require view';
'require steer as steer';
'require steer.ui-spec as uiSpec';

const manualNodeGroup = '_manual';

function nodeGroupID(node) {
	return node.source_subscription || manualNodeGroup;
}

function collectNodeGroups(nodes, subscriptions) {
	const groups = [ { id: manualNodeGroup, label: _('Manual nodes'), count: 0 } ];
	const known = { [manualNodeGroup]: groups[0] };
	subscriptions.forEach((subscription) => {
		const id = subscription['.name'];
		const group = { id, label: subscription.name || id, count: 0 };
		known[id] = group;
		groups.push(group);
	});
	nodes.forEach((node) => {
		const id = nodeGroupID(node);
		if (!known[id]) {
			known[id] = { id, label: _('Missing subscription: %s').format(id), count: 0 };
			groups.push(known[id]);
		}
		known[id].count++;
	});
	return groups;
}

function collectNodeReferences(nodes, routes, groups) {
	const known = {};
	const groupOrder = {};
	groups.forEach((group, index) => { groupOrder[group.id] = index; });
	const references = [];
	nodes.slice().sort((left, right) => {
		const group = (groupOrder[nodeGroupID(left)] ?? groups.length) - (groupOrder[nodeGroupID(right)] ?? groups.length);
		return group || String(left.name || left['.name']).localeCompare(String(right.name || right['.name']));
	}).forEach((node) => {
		known[node['.name']] = true;
		const group = groups.find((candidate) => candidate.id == nodeGroupID(node));
		const endpoint = node.server && node.server_port ? '%s:%s'.format(node.server, node.server_port) : node.type;
		references.push({
			id: node['.name'], label: node.name || node['.name'], group: group?.label || nodeGroupID(node),
			detail: endpoint + (node.source_subscription ? ' · ' + _('Subscription: %s').format(node.source_subscription) : '')
		});
	});
	routes.forEach((route) => {
		const node = route.kind == 'single' ? route.node : null;
		if (node && !known[node]) {
			known[node] = true;
			references.push({ id: node, label: _('Missing: %s').format(node), group: _('Missing references') });
		}
	});
	return steer.disambiguateReferences(references);
}

function addNodeValues(option, references) {
	references.forEach((reference) => option.value(reference.id, reference.label, _('Group: %s').format(reference.group)));
}

function nodeReferenceLabel(references, nodeId) {
	if (!nodeId)
		return null;
	const reference = references.find((candidate) => candidate.id == nodeId);
	return reference ? reference.label : _('Missing node');
}

function collectRouteReferences(routes) {
	const references = routes.filter((route) => route.kind == 'single').map((route) => ({
		id: route['.name'],
		label: routeLabel(route),
		detail: _('Node: %s').format(route.node || _('not selected'))
	}));
	const known = Object.fromEntries(references.map((reference) => [ reference.id, true ]));
	routes.forEach((route) => {
		if (route.detour && !known[route.detour]) {
			known[route.detour] = true;
			references.push({ id: route.detour, label: _('Missing: %s').format(route.detour) });
		}
	});
	return steer.disambiguateReferences(references);
}

function routeKindLabel(kind) {
	switch (kind) {
	case 'direct': return _('Direct');
	case 'block': return _('Reject');
	case 'single': return _('Single node');
	default: return _('Route');
	}
}

function routeLabel(route) {
	if (route?.name)
		return route.name;
	if (route?.kind == 'direct' || route?.kind == 'block')
		return routeKindLabel(route.kind);
	return route?.['.name'] || routeKindLabel(route?.kind);
}

function systemRouteLabel(route) {
	if (!route)
		return _('Route');
	const defaultName = route.kind == 'direct' ? 'direct' : 'block';
	return route.name && route.name != defaultName ? route.name : routeKindLabel(route.kind);
}

function referenceObjectLabel(objectType) {
	return {
		node: _('Node'),
		route: _('Route'),
		dns_profile: _('DNS profile'),
		local_proxy: _('Local proxy'),
		rule: _('Rule'),
		subscription: _('Subscription')
	}[objectType] || objectType;
}

function addSystemRouteSection(map, route) {
	if (!route)
		return;
	const direct = route.kind == 'direct';
	let section = map.section(form.NamedSection, route['.name'], 'route', systemRouteLabel(route));
	section.addremove = false;
	section.anonymous = true;
	section.nodescriptions = true;
	let option;
	if (direct) {
		option = section.option(form.DummyValue, '_system_status', _('Status'));
		const status = _('Required · always enabled');
		option.cfgvalue = function() { return status; };
		option.textvalue = function() { return status; };
	}
	else {
		option = section.option(form.Flag, 'enabled', _('Enabled'));
		option.default = '1';
	}
	option = section.option(form.Value, 'name', _('Name'));
	option.rmempty = true;
	option.optional = true;
	option = section.option(form.DummyValue, '_system_kind', _('Kind'));
	const kind = direct ? _('Direct') : _('Reject');
	option.cfgvalue = function() { return kind; };
	option.textvalue = function() { return kind; };
}

function nextRejectRouteID() {
	let id = 'block';
	let index = 2;
	while (uci.get('steer', id))
		id = 'block_' + index++;
	return id;
}

function renderMissingRejectAction(routes, map) {
	if (routes.some((route) => route.kind == 'block'))
		return null;
	const message = _('This older configuration has no Reject route. Create the fixed disabled system route once, then enable it when needed.');
	const button = E('button', {
		'class': 'cbi-button cbi-button-add',
		'type': 'button',
		'disabled': map.readonly || null,
		'click': function(ev) {
			ev.preventDefault();
			button.disabled = true;
			const sectionId = nextRejectRouteID();
			return Promise.resolve().then(() => {
				uci.add('steer', 'route', sectionId);
				uci.set('steer', sectionId, 'enabled', '0');
				uci.set('steer', sectionId, 'kind', 'block');
				return uci.save();
			}).then(() => window.location.reload()).catch((error) => {
				uci.remove('steer', sectionId);
				button.disabled = false;
				ui.addNotification(_('Create Reject route'), E('p', {}, String(error)), 'danger');
			});
		}
	}, _('Create Reject route'));
	return E('section', { 'class': 'cbi-section steer-reject-recovery' }, [
		E('h3', {}, _('Reject')),
		E('p', {}, message),
		button
	]);
}

function hasEnabledNode() {
	return uci.sections('steer', 'node').some((node) => node.enabled != '0');
}

function configureSingleRouteCreation(section) {
	const unavailable = _('Create or enable a proxy node before adding a single-node route.');
	const handleAdd = section.handleAdd;
	section.handleAdd = function(ev, sectionId) {
		if (!hasEnabledNode()) {
			ui.addNotification(_('Add single-node route'), E('p', {}, unavailable), 'warning');
			return;
		}
		return handleAdd.call(this, ev, sectionId);
	};

	const renderSectionAdd = section.renderSectionAdd;
	section.renderSectionAdd = function(extraClass) {
		const row = renderSectionAdd.call(this, extraClass);
		const button = row.querySelector?.('.cbi-button-add');
		const input = row.querySelector?.('.cbi-section-create-name');
		if (!button || !input)
			return row;
		const reason = E('p', { 'class': 'cbi-section-descr alert-message warning' }, unavailable);
		const refresh = function() {
			const available = hasEnabledNode();
			reason.hidden = available;
			button.title = available ? _('Add single-node route') : unavailable;
			if (section.map?.readonly === true || !available)
				button.disabled = true;
			else if (input.value != '' && !input.classList.contains('cbi-input-invalid'))
				button.disabled = null;
		};
		input.addEventListener('keyup', refresh);
		input.addEventListener('input', refresh);
		input.addEventListener('focus', refresh);
		input.addEventListener('blur', refresh);
		row.appendChild(reason);
		refresh();
		return row;
	};
}

function configureSubscriptionRemoval(section, nodes) {
	section.handleRemove = function(sectionId) {
		const owned = nodes.filter((node) => node.source_subscription == sectionId);
		const config = this.uciconfig || this.map.config;
		owned.forEach((node) => this.map.data.remove(config, node['.name']));
		this.map.data.remove(config, sectionId);
		return this.map.save(null, true);
	};
	steer.configureRemovalGuard(section, (sectionId) => steer.collectionReferences('subscriptions', sectionId),
		_('Subscription cannot be removed'));
}

function protocolLabel(value) {
	const label = uiSpec.node_types.find((item) => item.value == value)?.label;
	return label ? steer.uiSpecLabel(label) : (value || null);
}

function nodeFieldLabel(field) {
	return steer.uiSpecLabel(field.label);
}

function nodeChoiceLabel(item) {
	return steer.uiSpecLabel(item.label);
}

const SensitiveTextValue = form.TextValue.extend({
	__name__: 'Steer.SensitiveTextValue',
	configuredSentinel: '__STEER_SECRET_CONFIGURED__',

	cfgvalue: function(sectionId) {
		return uci.get('steer', sectionId, this.option) ? this.configuredSentinel : '';
	},

	write: function(sectionId, value) {
		const state = this.steerSecretState?.[sectionId];
		const replacement = String(value || '');
		if (state?.clear === true) {
			uci.unset('steer', sectionId, this.option);
			state.original = '';
			state.clear = false;
			state.edited = false;
			return;
		}
		if (state?.edited !== true || replacement == '')
			return;
		uci.set('steer', sectionId, this.option, replacement);
		state.original = replacement;
		state.edited = false;
	},

	remove: function(sectionId) {
		const state = this.steerSecretState?.[sectionId];
		if (state?.clear !== true)
			return;
		uci.unset('steer', sectionId, this.option);
		state.original = '';
		state.clear = false;
		state.edited = false;
	},

	renderWidget: function(sectionId) {
		const widget = form.TextValue.prototype.renderWidget.apply(this, arguments);
		const textarea = widget.matches?.('textarea') ? widget : widget.querySelector('textarea');
		if (!textarea)
			return widget;
		const state = {
			original: String(uci.get('steer', sectionId, this.option) || ''),
			revealed: false,
			edited: false,
			clear: false
		};
		this.steerSecretState ??= {};
		this.steerSecretState[sectionId] = state;
		textarea.value = '';
		textarea.setAttribute('autocomplete', 'off');
		textarea.setAttribute('autocapitalize', 'off');
		textarea.setAttribute('spellcheck', 'false');
		textarea.placeholder = state.original != ''
			? _('Configured secret is hidden. Leave this field blank to keep it unchanged.')
			: textarea.placeholder;
		let reveal, clear;
		textarea.addEventListener('input', () => {
			state.edited = true;
			if (textarea.value != '') {
				state.clear = false;
				if (reveal && !state.revealed)
					reveal.disabled = false;
				if (clear) {
					clear.disabled = false;
					clear.textContent = _('Clear configured secret');
				}
			}
		});
		if (state.original == '')
			return widget;

		reveal = E('button', {
			'type': 'button',
			'class': 'cbi-button cbi-button-action',
			'click': () => {
				state.revealed = true;
				state.edited = false;
				state.clear = false;
				textarea.value = state.original;
				reveal.disabled = true;
				reveal.textContent = _('Revealed until this editor is closed');
			}
		}, _('Reveal configured secret'));
		clear = E('button', {
			'type': 'button',
			'class': 'cbi-button cbi-button-negative',
			'click': () => {
				state.clear = true;
				state.edited = false;
				textarea.value = '';
				reveal.disabled = true;
				clear.disabled = true;
				clear.textContent = _('Secret will be cleared on save');
			}
		}, _('Clear configured secret'));
		return E('div', { 'class': 'steer-secret-editor' }, [ widget, reveal, clear ]);
	}
});

function addGeneratedNodeField(section, field) {
	let widget = form.Value;
	if (field.control == 'boolean')
		widget = form.Flag;
	else if (field.control == 'select' || field.control == 'select-integer')
		widget = form.ListValue;
	else if (field.control == 'string-list')
		widget = form.DynamicList;
	else if (field.multiline && field.sensitive)
		widget = SensitiveTextValue;
	else if (field.multiline)
		widget = form.TextValue;

	const option = section.option(widget, field.key, nodeFieldLabel(field));
	option.modalonly = true;
	if (field.control == 'password')
		option.password = true;
	if (field.control == 'integer')
		option.datatype = 'uinteger';
	if (field.multiline)
		option.rows = 6;
	if (field.placeholder)
		option.placeholder = field.placeholder;
	if (field.default !== undefined)
		option.default = typeof(field.default) == 'boolean' ? (field.default ? '1' : '0') : String(field.default);
	(field.options || []).forEach((item) => option.value(item.value, nodeChoiceLabel(item)));

	if (field.when) {
		field.types.forEach((type) => field.when.values.forEach((value) => {
			option.depends({ type: type, [field.when.field]: value });
		}));
	}
	else {
		field.types.forEach((type) => option.depends('type', type));
	}
	if ((field.required_types || []).length) {
		option.validate = function(sectionId, value) {
			const type = uci.get('steer', sectionId, 'type');
			if (field.required_types.includes(type) && (value == null || value === '' || (Array.isArray(value) && !value.length)))
				return _('%s is required for %s nodes.').format(nodeFieldLabel(field), protocolLabel(type));
			return true;
		};
	}
	return option;
}

function selectedNodeGroup(groups) {
	const requested = new URLSearchParams(window.location.search || '').get('node_group');
	return groups.some((group) => group.id == requested) ? requested : manualNodeGroup;
}

function nextManualNodeID() {
	const prefix = uiSpec.id_policy.collection_prefixes.nodes;
	let index = 1;
	while (uci.get('steer', prefix + '_' + index))
		index++;
	return prefix + '_' + index;
}

function renderNodeGroupNavigation(groups, activeGroup) {
	return E('nav', { 'class': 'steer-node-groups', 'aria-label': _('Node groups') }, groups.map((group) => {
		const query = new URLSearchParams(window.location.search || '');
		query.set('node_group', group.id);
		return E('a', {
			'class': 'steer-node-groups__item' + (group.id == activeGroup ? ' is-active' : ''),
			'href': (window.location.pathname || '') + '?' + query.toString(),
			'aria-current': group.id == activeGroup ? 'page' : null
		}, [ E('span', {}, group.label), E('strong', {}, String(group.count)) ]);
	}));
}

function hasPendingSteerChanges(changes) {
	if (Array.isArray(changes))
		return changes.length > 0;
	if (Array.isArray(changes?.steer))
		return changes.steer.length > 0;
	return changes?.steer != null && Object.keys(changes.steer).length > 0;
}

function subscriptionOperationGate(initialChanges, formNode, permissions) {
	let formDirty = false;
	let pending = hasPendingSteerChanges(initialChanges);
	let version = 0;
	const buttons = [];
	const refresh = function() {
		buttons.forEach((entry) => {
			const reason = permissions?.[entry.method] === true ? (entry.disabledReason || (formDirty
				? _('The visible subscription form has unsaved input.')
				: (pending ? _('Pending Steer changes must be applied or discarded first.') : '')))
				: _('Your session does not have permission to perform this action.');
			entry.button.disabled = reason != '';
			entry.button.title = reason;
		});
	};
	const markDirty = function() {
		formDirty = true;
		version++;
		refresh();
	};
	formNode?.addEventListener?.('input', markDirty);
	formNode?.addEventListener?.('change', markDirty);

	return {
		bind: function(button, disabledReason, method) {
			buttons.push({ button, disabledReason: disabledReason || '', method });
			refresh();
			return button;
		},
		version: function() { return version; },
		blockedReason: function(disabledReason, method) {
			if (permissions?.[method] !== true)
				return Promise.resolve(_('Your session does not have permission to perform this action.'));
			if (disabledReason)
				return Promise.resolve(disabledReason);
			if (formDirty)
				return Promise.resolve(_('The visible subscription form has unsaved input. Save and apply or discard it first.'));
			return uci.changes().then((changes) => {
				pending = hasPendingSteerChanges(changes);
				refresh();
				return pending ? _('Pending Steer changes must be applied or discarded before changing subscription inventory.') : '';
			});
		},
		mayReload: function(startVersion) {
			if (formDirty || version != startVersion)
				return Promise.resolve(false);
			return uci.changes().then((changes) => !hasPendingSteerChanges(changes));
		}
	};
}

function probeOperationGate(initialChanges, allowed) {
	let pending = hasPendingSteerChanges(initialChanges);
	const buttons = [];
	const updateButton = function(entry) {
		const reason = !allowed ? _('Your session does not have permission to test committed Nodes or Routes.')
			: (pending ? _('Pending Steer changes must be applied or discarded before testing committed Nodes or Routes.')
				: entry.disabledReason);
		entry.button.disabled = reason != '';
		entry.button.title = reason || entry.button._steerProbeResultTitle || entry.title;
	};
	const updateButtons = function() { buttons.forEach(updateButton); };
	const refresh = function() {
		return uci.changes().then((changes) => {
			pending = hasPendingSteerChanges(changes);
			updateButtons();
			return allowed && !pending;
		});
	};
	const stateChanged = function() { refresh(); };
	window.addEventListener?.('steer-uci-state-changed', stateChanged);
	window.addEventListener?.('focus', stateChanged);
	return {
		bind: function(button, title, disabledReason) {
			if (!button) return button;
			const entry = { button, title: title || button.title || '', disabledReason: disabledReason || '' };
			buttons.push(entry);
			updateButton(entry);
			return button;
		},
		bindForm: function(formNode) {
			const selector = 'output[for$="._connect_speedtest"] button, output[for$="._download_speedtest"] button, output[for$="._route_connect_test"] button, output[for$="._route_download_test"] button';
			Array.from(formNode?.querySelectorAll?.(selector) || []).forEach((button) =>
				this.bind(button, button.title, button._steerProbeDisabledReason));
		},
		allow: function(disabledReason) {
			if (!allowed) {
				ui.addNotification(_('Probe is not permitted'), E('p', {}, _('Your session does not have permission to test committed Nodes or Routes.')), 'warning');
				return Promise.resolve(false);
			}
			return refresh().then((allowed) => {
				if (!allowed)
					ui.addNotification(_('Probe is locked'), E('p', {}, _('Apply or discard pending Steer changes before testing the committed configuration.')), 'warning');
				else if (disabledReason)
					ui.addNotification(_('Probe is unavailable'), E('p', {}, disabledReason), 'warning');
				return allowed && !disabledReason;
			});
		},
		refresh
	};
}

function renderSubscriptionStatus(result, gate) {
	const subscriptions = result?.subscriptions || [];
	return E('section', { 'class': 'steer-subscription-status' }, [
		E('h3', {}, _('Subscription status')),
		subscriptions.length ? E('div', { 'class': 'table' }, [
			E('div', { 'class': 'tr table-titles' }, [ E('div', { 'class': 'th' }, _('Name')), E('div', { 'class': 'th' }, _('Last update')), E('div', { 'class': 'th' }, _('Nodes')), E('div', { 'class': 'th' }, _('Action')) ]),
			...subscriptions.map((subscription) => {
				const stale = subscription.stale || [];
				const last = subscription.last_success ? new Date(subscription.last_success).toLocaleString() : _('Not fetched');
				const failure = subscription.last_failure;
				const disabledUpdate = subscription.enabled ? '' : _('Disabled subscriptions cannot be updated. Enable and save this subscription first.');
				const updateButton = E('button', {
					'class': 'btn cbi-button-action',
					'click': function() {
						return gate.blockedReason(disabledUpdate, 'subscription_update').then((reason) => {
							if (reason) {
								ui.addNotification(_('Subscription inventory is locked'), E('p', {}, reason), 'warning');
								return;
							}
							const startVersion = gate.version();
							ui.showModal(_('Updating subscription'), [ E('p', { 'class': 'spinning' }, _('Downloading and validating every node.')) ]);
							return steer.updateSubscription(subscription.id).then((update) => {
							ui.hideModal();
							if (!update?.ok) {
								ui.addNotification(_('Subscription update failed'), E('p', {}, steer.rpcErrorText(update)), 'danger');
								return gate.mayReload(startVersion).then((reload) => {
									if (reload && update?.error_code != 'PENDING_CHANGES') window.location.reload();
									return update;
								});
							}
							const status = (update.subscriptions || []).find((item) => item.id == subscription.id) || {};
							const message = _('Subscription nodes updated. The running configuration was not changed. Removed unreferenced nodes were deleted automatically; nodes still used by Routes were kept and should be unreferenced soon. Added %d, current %d, unavailable %d, skipped %d.').format(
								status.added || 0, status.current || 0, (status.stale || []).length, status.skipped || 0);
							ui.addNotification(null, E('p', {}, message), (status.stale || []).length ? 'warning' : 'info');
							return gate.mayReload(startVersion).then((reload) => {
								if (reload) window.location.reload();
								else ui.addNotification(null, E('p', {}, _('Visible edits appeared while the update was running; they were preserved and the page was not reloaded.')), 'warning');
								return update;
							});
						});
						});
					}
				}, _('Update now'));
				gate.bind(updateButton, disabledUpdate, 'subscription_update');
				const actions = [ updateButton ];
				stale.forEach((node) => {
					const references = node.referenced_by || [];
					const cleanButton = E('button', {
					'class': 'btn cbi-button-negative',
					'click': function() {
						if (references.length) {
							ui.addNotification(_('Subscription node removal failed'), E('ul', {}, references.map((reference) => E('li', {}, [
								E('span', {}, '%s %s'.format(referenceObjectLabel(reference.object_type), reference.name || reference.id)),
								E('button', { 'class': 'btn cbi-button-action', 'click': () => steer.focusIssue({
									object_type: reference.object_type, object_id: reference.id, option: 'node'
								}) }, _('Go to reference'))
							]))), 'danger');
							return;
						}
						return gate.blockedReason('', 'subscription_clean').then((reason) => {
							if (reason) {
								ui.addNotification(_('Subscription inventory is locked'), E('p', {}, reason), 'warning');
								return;
							}
							const startVersion = gate.version();
							return steer.cleanSubscription(subscription.id, node.id).then((clean) => {
							if (!clean?.ok) {
								ui.addNotification(_('Subscription node removal failed'), E('p', {}, steer.rpcErrorText(clean)), 'danger');
								return clean;
							}
							ui.addNotification(null, E('p', {}, _('Unavailable node removed. The running configuration was not changed.')), 'info');
							return gate.mayReload(startVersion).then((reload) => {
								if (reload) window.location.reload();
								else ui.addNotification(null, E('p', {}, _('Visible edits appeared while cleanup was running; they were preserved and the page was not reloaded.')), 'warning');
								return clean;
							});
						});
						});
					}
				}, references.length ? _('In use: %s').format(node.name || _('Unavailable node')) : _('Remove %s').format(node.name || _('Unavailable node')));
					gate.bind(cleanButton, '', 'subscription_clean');
					actions.push(' ', cleanButton);
				});
				const failureDetail = failure
					? _('Last failure: %s · %s').format(failure.at ? new Date(failure.at).toLocaleString() : '—', failure.summary)
					: '';
				return E('div', { 'class': 'tr' }, [
					E('div', { 'class': 'td' }, subscription.name || subscription.id),
					E('div', { 'class': 'td', 'title': failureDetail }, failureDetail ? [ last, E('br'), failureDetail ] : last),
					E('div', { 'class': 'td' }, _('%d current / %d unavailable / %d skipped').format(subscription.current || 0, stale.length, subscription.skipped || 0)),
					E('div', { 'class': 'td' }, actions)
				]);
			})
		]) : E('p', {}, _('No subscriptions configured.'))
	]);
}

function enabledFlag(value) {
	return value === true || value === 1 || value === '1' || value === 'true';
}

function hasCredentials(node) {
	return [ 'uuid', 'username', 'password', 'private_key', 'obfs_password' ].some((field) => {
		const value = node?.[field];
		return Array.isArray(value) ? value.length > 0 : value != null && String(value) != '';
	});
}

function previewFacts(node) {
	const values = [
		[ _('Protocol'), node.type ],
		[ _('Server'), node.server ],
		[ _('Port'), node.server_port ]
	];
	if (hasCredentials(node))
		values.push([ _('Credentials'), _('Present; hidden from preview') ]);
	else
		values.push([ _('Credentials'), _('None') ]);
	if (node.server_ports?.length)
		values.push([ _('Port hopping ranges'), node.server_ports.join(', ') ]);
	if (node.tls_server_name)
		values.push([ _('TLS server name'), node.tls_server_name ]);
	if (node.flow)
		values.push([ _('Flow'), node.flow ]);
	if (node.packet_encoding)
		values.push([ _('UDP packet encoding'), node.packet_encoding ]);
	if (node.utls_fingerprint)
		values.push([ _('uTLS fingerprint'), node.utls_fingerprint ]);
	if (node.reality_public_key)
		values.push([ _('REALITY parameters'), _('Present; hidden from preview') ]);
	if (node.obfs_type)
		values.push([ _('Obfuscation'), node.obfs_type ]);
	if (node.hop_interval)
		values.push([ _('Port hopping interval'), node.hop_interval ]);
	if (node.up_mbps)
		values.push([ _('Upload Mbps'), node.up_mbps ]);
	if (node.down_mbps)
		values.push([ _('Download Mbps'), node.down_mbps ]);
	values.push([ _('Certificate verification'), enabledFlag(node.insecure) ? _('Disabled') : _('Enabled') ]);
	return values;
}

function renderImportPreview(node, index, nameInput) {
	return E('article', { 'class': 'steer-test-card steer-import-node' }, [
		E('h5', {}, _('%d. %s').format(index + 1, node.name || node.id || _('Unnamed node'))),
		nameInput ? E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, _('Name')),
			E('div', { 'class': 'cbi-value-field' }, nameInput)
		]) : '',
		E('dl', { 'class': 'steer-status__facts' }, previewFacts(node).map((fact) =>
			E('div', {}, [ E('dt', {}, fact[0]), E('dd', {}, String(fact[1])) ])))
	]);
}

function renderImportButton(allowed) {
	return E('section', { 'class': 'cbi-section' }, [
		E('div', { 'class': 'steer-section-heading' }, [
			E('div', {}, [
				E('h3', {}, _('Import nodes')),
				E('p', {}, _('Paste one node link per line, or paste a Base64 subscription document. You can preview the parsed nodes before adding them.'))
			]),
			E('button', {
				'class': 'cbi-button cbi-button-add',
				'disabled': allowed ? null : true,
				'title': allowed ? '' : _('Your session does not have permission to import Nodes.'),
				click: function(ev) {
					ev.preventDefault();
					if (!allowed) {
						ui.addNotification(_('Node import is not permitted'), E('p', {}, _('Your session does not have permission to import Nodes.')), 'warning');
						return;
					}
					showImportDialog();
				}
			}, _('Import nodes'))
		])
	]);
}

function nodeExportDisabledReason(sectionId, allowed) {
	if (!allowed)
		return _('Your session does not have permission to export Nodes.');
	const type = uci.get('steer', sectionId, 'type');
	if (type == 'tor')
		return _('Tor Nodes do not have a share-link format.');
	if (type == 'ssh' && !uci.get('steer', sectionId, 'password'))
		return _('SSH Nodes that only use a private key cannot be exported as share links.');
	return '';
}

function showNodeExportDialog(sectionId) {
	const content = E('div', {}, E('p', { 'class': 'spinning' }, _('Generating the Node share link…')));
	ui.showModal(_('Export Node link'), [
		E('p', { 'class': 'alert-message warning' }, _('Share links contain the complete Node credentials. Store and share them only through trusted channels.')),
		content,
		E('div', { 'class': 'right' }, E('button', { 'class': 'cbi-button', 'click': ui.hideModal }, _('Close')))
	]);
	return steer.exportNode(sectionId).then((result) => {
		if (result?.ok === false || !result?.uri)
			throw new Error(steer.rpcErrorText(result));
		const input = E('textarea', {
			'class': 'cbi-input-textarea steer-machine-input', 'rows': 5, 'readonly': true,
			'autocomplete': 'off', 'autocapitalize': 'off', 'spellcheck': 'false'
		});
		input.value = result.uri;
		const copy = E('button', {
			'class': 'cbi-button cbi-button-positive',
			'click': function() {
				if (window.navigator?.clipboard?.writeText) {
					return window.navigator.clipboard.writeText(input.value).then(() => {
						ui.addNotification(_('Node link copied'), E('p', {}, _('The Node share link was copied to the clipboard.')), 'info');
					}).catch(() => {
						input.focus();
						input.select();
						ui.addNotification(_('Copy Node link'), E('p', {}, _('Automatic copy failed. Copy the selected link manually.')), 'warning');
					});
				}
				input.focus();
				input.select();
				ui.addNotification(_('Copy Node link'), E('p', {}, _('Automatic copy is unavailable. Copy the selected link manually.')), 'warning');
			}
		}, _('Copy link'));
		content.replaceChildren(E('div', {}, [
			E('p', {}, uci.get('steer', sectionId, 'name') || _('Unnamed')),
			input,
			E('div', { 'class': 'right' }, copy)
		]));
	}).catch((error) => {
		content.replaceChildren(E('p', { 'class': 'alert-message danger' }, String(error)));
	});
}

function decorateNodeExportOption(option, allowed) {
	const renderWidget = option.renderWidget;
	option.renderWidget = function(sectionId, optionIndex, cfgvalue) {
		const widget = typeof(renderWidget) == 'function'
			? renderWidget.call(this, sectionId, optionIndex, cfgvalue)
			: E('button', { 'class': 'cbi-button cbi-button-action' }, this.inputtitle || _('Export link'));
		const button = widget?.matches?.('button') ? widget : widget?.querySelector?.('button');
		const reason = nodeExportDisabledReason(sectionId, allowed);
		if (button && reason) {
			button.disabled = true;
			button.title = reason;
		}
		return widget;
	};
}

function setSpeedtestButton(button, state, label, detail) {
	if (!button)
		return;
	button.disabled = state == 'testing';
	button.classList.toggle('spinning', state == 'testing');
	button.classList.toggle('cbi-button-positive', state == 'success');
	button.classList.toggle('cbi-button-negative', state == 'error');
	button.textContent = label;
	button.title = detail || '';
	button._steerProbeResultTitle = state == 'testing' ? '' : (detail || '');
}

function latestProbePresentation(result) {
	if (!result)
		return { text: _('Not tested'), ok: null, stale: false };
	const tested = new Date(result.tested_at);
	const testedAt = Number.isNaN(tested.getTime()) ? '—' : tested.toLocaleString();
	return {
		text: [ _('Tested at'), testedAt, result.stale ? _('Outdated') : '',
			result.ok ? _('Succeeded') : _('Failed'), result.ok ? result.summary : result.error_summary ]
			.filter(Boolean).join(' · '),
		ok: result.ok,
		stale: result.stale === true
	};
}

function findLatestProbe(probeResults, scope, objectId, kind) {
	return (probeResults?.latest_results || []).find((result) =>
		result.scope == scope && result.object_id == objectId && result.kind == kind);
}

function nodeProbeMetric(result, mode) {
	if (!result || result.scope != 'nodes' || result.kind != mode || result.ok !== true || result.stale === true)
		return null;
	const value = result.metric_value;
	return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
}

function nodeDisplaySortState() {
	const contract = uiSpec.node_display_sorting || {};
	const query = new URLSearchParams(window.location.search || '');
	const requestedMode = query.get('node_sort') || 'default';
	const requestedDirection = query.get('node_sort_direction') || contract.default_direction || 'best_first';
	return {
		mode: (contract.modes || []).includes(requestedMode) ? requestedMode : 'default',
		direction: (contract.direction_modes || []).includes(requestedDirection)
			? requestedDirection : (contract.default_direction || 'best_first')
	};
}

function sortNodeSectionIDs(sectionIds, mode, direction, probeResults) {
	const ids = (sectionIds || []).slice();
	if (mode == 'default') return ids;
	const latest = probeResults?.latest_results || [];
	const decorated = ids.map((sectionId, index) => {
		const result = latest.find((candidate) =>
			candidate.scope == 'nodes' && candidate.object_id == sectionId && candidate.kind == mode);
		const metric = nodeProbeMetric(result, mode);
		return { sectionId: sectionId, index: index, metric: metric, ranked: metric != null };
	});
	const contract = uiSpec.node_display_sorting || {};
	const goodDirection = mode == 'connect' ? contract.connect_direction : contract.download_direction;
	const metricDirection = direction == 'worst_first'
		? (goodDirection == 'ascending' ? 'descending' : 'ascending')
		: goodDirection;
	decorated.sort((left, right) => {
		if (left.ranked != right.ranked) return left.ranked ? -1 : 1;
		if (!left.ranked) return left.index - right.index;
		const compared = metricDirection == 'ascending'
			? left.metric - right.metric : right.metric - left.metric;
		return compared || left.index - right.index;
	});
	return decorated.map((entry) => entry.sectionId);
}

function nextNodeDisplaySort(mode) {
	const current = nodeDisplaySortState();
	const contract = uiSpec.node_display_sorting || {};
	if (mode == 'default') return { mode: 'default', direction: contract.default_direction || 'best_first' };
	if (current.mode == mode && current.direction == 'worst_first')
		return { mode: 'default', direction: contract.default_direction || 'best_first' };
	return {
		mode: mode,
		direction: current.mode == mode
			? (current.direction == 'best_first' ? 'worst_first' : 'best_first')
			: (contract.default_direction || 'best_first')
	};
}

function navigateNodeDisplaySort(mode) {
	const next = nextNodeDisplaySort(mode);
	const query = new URLSearchParams(window.location.search || '');
	if (next.mode == 'default') {
		query.delete('node_sort');
		query.delete('node_sort_direction');
	}
	else {
		query.set('node_sort', next.mode);
		query.set('node_sort_direction', next.direction);
	}
	window.location.href = (window.location.pathname || '') + (query.toString() ? '?' + query.toString() : '');
}

function decorateNodeSortHeaders(formNode) {
	const state = nodeDisplaySortState();
	const table = formNode?.querySelector?.('#cbi-steer-node table') || formNode?.querySelector?.('table');
	if (!table) return;
	const headers = Array.from(table.querySelectorAll('thead th'));
	const byWidgetSuffix = (suffix) => headers.find((header) =>
		String(header.getAttribute('data-widget') || '').endsWith(suffix));
	const byLabel = (label, suffix) => {
		const normalized = String(label || '').replace(/\s+/g, '');
		return headers.find((header) => String(header.textContent || '').replace(/\s+/g, '').includes(normalized))
			|| byWidgetSuffix(suffix);
	};
	const orderHeader = headers.find((header) => !header.hasAttribute('data-widget') && !header.classList.contains('cbi-section-actions'));
	const install = (header, label, mode) => {
		if (!header) return;
		const active = state.mode == mode;
		const metricMode = mode != 'default';
		const direction = state.direction == 'best_first' ? '↑' : '↓';
		header.replaceChildren(E('button', {
			'class': 'steer-node-sort-header' + (active ? ' is-active' : ''),
			'type': 'button', 'aria-pressed': active ? 'true' : 'false',
			'title': metricMode ? label : _('Order'),
			'aria-label': active ? '%s %s'.format(label, direction) : label,
			'click': (ev) => { ev.preventDefault(); ev.stopPropagation(); navigateNodeDisplaySort(mode); }
		}, [ E('span', {}, label), active ? E('span', { 'class': 'steer-node-sort-arrow', 'aria-hidden': 'true' }, metricMode ? direction : '↑') : '' ]));
	};
	if (orderHeader)
		orderHeader.replaceChildren(_('Order'));
	install(byLabel(_('Connection test'), '_connect_speedtest'), _('Connection test'), 'connect');
	install(byLabel(_('Download test'), '_download_speedtest'), _('Download test'), 'download');
}

function lightweightNodeEndpoint(node) {
	if (!node?.server)
		return node?.type == 'tor' ? _('Local') : '—';
	return node.server_port ? '%s:%s'.format(node.server, node.server_port) : node.server;
}

function renderLightweightNodeProbe(node, download, probeResults, gate) {
	const sectionId = node['.name'];
	const kind = download ? 'download' : 'connect';
	const option = download ? '_download_speedtest' : '_connect_speedtest';
	const output = E('small', { 'class': 'steer-probe-latest' });
	renderLatestProbe(output, findLatestProbe(probeResults, 'nodes', sectionId, kind));
	const button = E('button', {
		'class': 'cbi-button cbi-button-action',
		'type': 'button',
		'click': function(ev) { return runSpeedtest(sectionId, download, ev.currentTarget, gate); }
	}, _('Test'));
	button._steerProbeOutput = output;
	button._steerProbeScope = 'nodes';
	button._steerProbeObjectID = sectionId;
	button._steerProbeKind = kind;
	button._steerProbeDisabledReason = nodeProbeDisabledReason(sectionId);
	gate.bind(button, _('Test'), button._steerProbeDisabledReason);
	return E('output', { 'for': 'cbid.steer.%s.%s'.format(sectionId, option) },
		E('div', { 'class': 'steer-probe-action' }, [ button, output ]));
}

function renderLightweightNodeTable(nodes, activeGroup, displaySort, probeResults, gate, exportAllowed) {
	const configuredIds = nodes.map((node) => node['.name']);
	const nodeById = Object.fromEntries(nodes.map((node) => [ node['.name'], node ]));
	const displayedIds = sortNodeSectionIDs(configuredIds, displaySort.mode, displaySort.direction, probeResults);
	const orderingDisabledReason = displaySort.mode == 'default' ? '' : _('Order');
	const orderingSection = {
		sectiontype: 'node',
		uciconfig: 'steer',
		map: { config: 'steer', data: uci, readonly: false },
		cfgsections: function() {
			return uci.sections('steer', 'node')
				.filter((node) => nodeGroupID(node) == activeGroup.id)
				.map((node) => node['.name']);
		},
		renderRowActions: function() { return E('td', {}, E('div')); }
	};
	steer.configureOrdering(orderingSection, 'nodes', { baseActions: false, disabledReason: orderingDisabledReason });

	const body = E('tbody', {}, displayedIds.map((sectionId) => {
		const node = nodeById[sectionId];
		const exportReason = nodeExportDisabledReason(sectionId, exportAllowed);
		const exportButton = E('button', {
			'class': 'cbi-button cbi-button-action', 'type': 'button',
			'disabled': exportReason ? true : null, 'title': exportReason,
			'click': function(ev) { ev.preventDefault(); return showNodeExportDialog(sectionId); }
		}, _('Export link'));
		return E('tr', {
			'id': 'cbi-steer-' + sectionId,
			'class': 'tr cbi-section-table-row steer-lightweight-node-row',
			'data-sid': sectionId,
			'dragenter': function(ev) { return orderingSection.handleDragEnter(ev); },
			'dragover': function(ev) { return orderingSection.handleDragOver(ev); },
			'dragleave': function(ev) { return orderingSection.handleDragLeave(ev); },
			'drop': function(ev) { return orderingSection.handleDrop(ev); }
		}, [
			orderingSection.renderRowActions(sectionId),
			E('td', { 'data-title': _('Enabled') }, node.enabled == '0' ? _('Disabled') : _('Enabled')),
			E('td', { 'data-title': _('Name') }, E('strong', {}, node.name || _('Unnamed'))),
			E('td', { 'data-title': _('Protocol') }, protocolLabel(node.type) || '—'),
			E('td', { 'data-title': _('Server'), 'class': 'steer-node-endpoint' }, lightweightNodeEndpoint(node)),
			E('td', { 'data-title': _('Connection test') }, renderLightweightNodeProbe(node, false, probeResults, gate)),
			E('td', { 'data-title': _('Download test') }, renderLightweightNodeProbe(node, true, probeResults, gate)),
			E('td', { 'data-title': _('Share link') }, exportButton)
		]);
	}));
	const table = E('table', { 'class': 'table cbi-section-table steer-lightweight-node-table' }, [
		E('thead', {}, E('tr', { 'class': 'tr cbi-section-table-titles' }, [
			E('th', {}, _('Order')),
			E('th', {}, _('Enabled')),
			E('th', {}, _('Name')),
			E('th', {}, _('Protocol')),
			E('th', {}, _('Server')),
			E('th', { 'data-widget': '_connect_speedtest' }, _('Connection test')),
			E('th', { 'data-widget': '_download_speedtest' }, _('Download test')),
			E('th', {}, _('Share link'))
		])),
		body
	]);
	const section = E('section', { 'class': 'cbi-section steer-lightweight-node-section' }, [
		E('h3', {}, _('Proxy nodes — %s (%d)').format(activeGroup.label, nodes.length)),
		E('div', { 'class': 'table-responsive' }, table)
	]);
	decorateNodeSortHeaders(section);
	return section;
}

function renderLatestProbe(output, result) {
	if (!output) return;
	const latest = latestProbePresentation(result);
	output.className = 'steer-probe-latest' + (latest.stale ? ' is-stale' : (latest.ok === false ? ' is-error' : ''));
	output.replaceChildren(latest.text);
}

function decorateProbeOption(option, scope, kind, probeResults, disabledReasonFor) {
	const renderWidget = option.renderWidget;
	option.renderWidget = function(sectionId, optionIndex, cfgvalue) {
		const widget = typeof(renderWidget) == 'function'
			? renderWidget.call(this, sectionId, optionIndex, cfgvalue)
			: E('button', { 'class': 'cbi-button cbi-button-action' }, this.inputtitle || _('Test'));
		const button = widget?.matches?.('button') ? widget : widget?.querySelector?.('button');
		const output = E('small', { 'class': 'steer-probe-latest' });
		const result = findLatestProbe(probeResults, scope, sectionId, kind);
		renderLatestProbe(output, result);
		if (button) {
			const disabledReason = disabledReasonFor?.(sectionId) || '';
			button._steerProbeOutput = output;
			button._steerProbeScope = scope;
			button._steerProbeObjectID = sectionId;
			button._steerProbeKind = kind;
			button._steerProbeDisabledReason = disabledReason;
			if (disabledReason) {
				button.disabled = true;
				button.title = disabledReason;
			}
		}
		return E('div', { 'class': 'steer-probe-action' }, [ widget, output ]);
	};
}

function refreshLatestProbe(button) {
	return steer.probeResults().then((results) => {
		const latest = findLatestProbe(
			results, button?._steerProbeScope, button?._steerProbeObjectID, button?._steerProbeKind);
		renderLatestProbe(button?._steerProbeOutput, latest);
		return latest;
	});
}

function nodeProbeDisabledReason(sectionId) {
	return uci.get('steer', sectionId, 'enabled') == '0'
		? _('Disabled Nodes cannot be tested. Enable and apply this Node first.') : '';
}

function runSpeedtest(sectionId, download, button, gate, refreshAfter = true) {
	return gate.allow(nodeProbeDisabledReason(sectionId)).then((allowed) => {
	if (!allowed) return false;
	setSpeedtestButton(button, 'testing', _('Testing…'));
	return steer.speedtest(sectionId, download).then((result) => {
		setSpeedtestButton(button, result.ok ? 'success' : 'error', result.ok ? (result.summary || _('Succeeded')) : _('Failed'),
			result.ok ? _('Succeeded') : (result.error_summary || _('See diagnostic logs for details.')));
		renderLatestProbe(button?._steerProbeOutput, result);
		return result.ok;
	}).catch((error) => {
		setSpeedtestButton(button, 'error', _('Failed'), _('See diagnostic logs for details.'));
		return refreshLatestProbe(button).then((latest) => latest?.ok === true).catch(() => false);
	});
	}).then((result) => refreshAfter ? gate.refresh().then(() => {
		if (nodeDisplaySortState().mode == (download ? 'download' : 'connect')) window.location.reload();
		return result;
	}) : result);
}

function runRouteSpeedtest(sectionId, download, button, gate) {
	return gate.allow().then((allowed) => {
	if (!allowed) return false;
	setSpeedtestButton(button, 'testing', _('Testing…'));
	return steer.routeSpeedtest(sectionId, download).then((result) => {
		setSpeedtestButton(button, result.ok ? 'success' : 'error', result.ok ? (result.summary || _('Succeeded')) : _('Failed'),
			result.ok ? _('Succeeded') : (result.error_summary || _('See diagnostic logs for details.')));
		renderLatestProbe(button?._steerProbeOutput, result);
		return result.ok;
	}).catch((error) => {
		setSpeedtestButton(button, 'error', _('Failed'), _('See diagnostic logs for details.'));
		return refreshLatestProbe(button).then((latest) => latest?.ok === true).catch(() => false);
	});
	}).then((result) => gate.refresh().then(() => result));
}

function findSpeedtestButton(sectionId, download) {
	const option = download ? '_download_speedtest' : '_connect_speedtest';
	const id = 'cbid.steer.%s.%s'.format(sectionId, option);
	return document.querySelector('output[for="%s"] button'.format(id));
}

function runBatchSpeedtest(sectionIds, download, button, gate) {
	return gate.allow().then((allowed) => {
	if (!allowed) return false;
	const title = download ? _('Batch download test') : _('Batch connection test');
	const buttons = Array.from(document.querySelectorAll('.steer-speedtest-batch button'));
	let cursor = 0;
	let completed = 0;
	let succeeded = 0;
	buttons.forEach((candidate) => { candidate.disabled = true; });
	button.textContent = '%s · 0/%d'.format(title, sectionIds.length);

	const worker = async function() {
		while (cursor < sectionIds.length) {
			const sectionId = sectionIds[cursor++];
			if (await runSpeedtest(sectionId, download, findSpeedtestButton(sectionId, download), gate, false))
				succeeded++;
			completed++;
			button.textContent = '%s · %d/%d'.format(title, completed, sectionIds.length);
		}
	};

	const workers = [];
	for (let i = 0; i < Math.min(4, sectionIds.length); i++)
		workers.push(worker());
	return Promise.all(workers).then(() => {
		button.textContent = '%s · %d/%d'.format(title, succeeded, sectionIds.length);
		button.title = _('%d/%d succeeded; click to test again.').format(succeeded, sectionIds.length);
		buttons.forEach((candidate) => { candidate.disabled = false; });
	});
	}).then((result) => gate.refresh().then(() => {
		if (nodeDisplaySortState().mode == (download ? 'download' : 'connect')) window.location.reload();
		return result;
	}));
}

function renderBatchSpeedtests(sectionIds, gate) {
	if (!sectionIds.length)
		return null;
	const connect = E('button', { 'class': 'btn cbi-button-action', 'click': function(ev) { return runBatchSpeedtest(sectionIds, false, ev.currentTarget, gate); } }, _('Batch connection test'));
	const download = E('button', { 'class': 'btn cbi-button-action', 'click': function(ev) { return runBatchSpeedtest(sectionIds, true, ev.currentTarget, gate); } }, _('Batch download test'));
	gate.bind(connect, _('Batch connection test'));
	gate.bind(download, _('Batch download test'));
	return E('div', { 'class': 'steer-speedtest-batch' }, [ connect, download ]);
}

function showImportDialog() {
	const input = E('textarea', {
		'class': 'cbi-input-textarea steer-machine-input',
		'rows': 5,
		'autocomplete': 'off',
		'autocapitalize': 'off',
		'autocorrect': 'off',
		'spellcheck': 'false',
			'placeholder': 'vless://…  vmess://…  ss://…  hysteria2://…  tuic://…  trojan://…'
	});
	const preview = E('div', { 'class': 'steer-import-preview' });

	const review = function(ev) {
		ev.preventDefault();
		preview.replaceChildren(E('p', { 'class': 'spinning' }, _('Parsing and validating nodes…')));
		return steer.importNodes(input.value).then((parsed) => {
			const nodes = parsed?.nodes || [];
			if (!nodes.length)
				throw new Error(parsed?.ok === false ? steer.rpcErrorText(parsed) : _('No valid node was returned.'));
			input.value = '';
			const nameInput = nodes.length == 1 ? E('input', {
				'class': 'cbi-input-text', 'value': nodes[0].name, 'autocomplete': 'off'
			}) : null;
			const skippedReasons = (parsed?.skipped_reasons || []).slice(0, 3);
			const save = function(saveEvent) {
				saveEvent.preventDefault();
				if (nameInput)
					nodes[0].name = nameInput.value.trim() || nodes[0].name;
				nodes.forEach((node) => {
					const id = nextManualNodeID();
					uci.add('steer', 'node', id);
					Object.keys(node).filter((option) => option != 'id' && !option.startsWith('source_') && option != 'pinned_stale').forEach((option) => {
						let value = node[option];
						if (typeof(value) == 'boolean')
							value = value ? '1' : '0';
						else if (typeof(value) == 'number')
							value = String(value);
						uci.set('steer', id, option, value);
					});
				});
				saveEvent.currentTarget.disabled = true;
				return uci.save().then(() => {
					ui.hideModal();
					window.location.href = L.url('admin/services/steer/nodes');
				}).catch((error) => {
					saveEvent.currentTarget.disabled = false;
					ui.addNotification(_('Node import failed'), E('p', {}, String(error)), 'danger');
				});
			};
			preview.replaceChildren(E('div', {}, [
				E('h4', {}, _('%d node(s) ready to import').format(nodes.length)),
				E('p', {}, _('Review every node below. Credentials are reported only as present or absent and their contents stay hidden.')),
				parsed.skipped ? E('div', { 'class': 'alert-message warning' }, [
					E('p', {}, _('%d invalid node(s) were skipped; the complete valid batch is listed below.').format(parsed.skipped)),
					skippedReasons.length ? E('ul', {}, skippedReasons.map((reason) => E('li', {}, reason.detail))) : ''
				]) : '',
				E('div', { 'class': 'steer-test-grid' }, nodes.map((node, index) => renderImportPreview(node, index, nameInput))),
				E('div', { 'class': 'right' }, E('button', {
					'class': 'cbi-button cbi-button-positive', click: save
				}, _('Import into pending configuration')))
			]));
		}).catch((error) => {
			preview.replaceChildren(E('div', { 'class': 'alert-message danger' }, String(error)));
		});
	};

	ui.showModal(_('Import nodes'), [
		E('p', {}, _('Paste one node link per line, or paste a Base64 subscription document. You can preview the parsed nodes before adding them.')),
		input,
		preview,
		E('div', { 'class': 'right' }, [
			E('button', { 'class': 'cbi-button', 'click': ui.hideModal }, _('Cancel')),
			' ',
			E('button', { 'class': 'cbi-button cbi-button-action', 'click': review }, _('Parse and preview'))
		])
	]);
	input.focus();
}

const nodesView = view.extend({
	load: function() {
		return Promise.all([
			uci.load('steer'), steer.subscriptions(), uci.changes(), steer.probeResults(),
			steer.permissions([ 'subscription_update', 'subscription_clean', 'node_speedtest', 'route_speedtest', 'node_import', 'node_export' ], true)
		]);
	},

	render: function(data) {
		let m, s, o;
		const nodes = uci.sections('steer', 'node');
		const routes = uci.sections('steer', 'route');
		const subscriptions = uci.sections('steer', 'subscription');
		const nodeGroups = collectNodeGroups(nodes, subscriptions);
		const activeNodeGroup = selectedNodeGroup(nodeGroups);
		const activeGroup = nodeGroups.find((group) => group.id == activeNodeGroup);
		const nodeReferences = collectNodeReferences(nodes, routes, nodeGroups);
		const routeReferences = collectRouteReferences(routes);
		const enabledNodeIds = nodes.filter((node) => nodeGroupID(node) == activeNodeGroup && node.enabled != '0')
			.map((node) => node['.name']);
		const summaryOnly = activeNodeGroup != manualNodeGroup;
		const page = (window.location.pathname || '').split('/').pop();
		const probeResults = data?.[3] || { latest_results: [] };
		const displaySort = nodeDisplaySortState();
		const permissions = data?.[4] || {};
		steer.loadStyle(this);

		m = new form.Map('steer', page == 'routes' ? _('Routes') : (page == 'subscriptions' ? _('Node subscriptions') : _('Proxy nodes')));

		if (page == 'subscriptions') {
			s = m.section(form.GridSection, 'subscription', _('Node subscriptions'));
			steer.configureNamedSection(s, steer.creationDefaults('subscriptions'));
			steer.configureOrdering(s, 'subscriptions');
			configureSubscriptionRemoval(s, nodes);
			s.addremove = true;
			s.nodescriptions = true;
			s.addbtntitle = _('Add subscription');
			o = s.option(form.Flag, 'enabled', _('Enabled')); o.default = '1'; o.editable = true;
			o = s.option(form.Value, 'name', _('Name')); o.rmempty = true; o.optional = true; o.modalonly = true;
			o = s.option(form.Value, 'url', _('Subscription URL')); o.rmempty = false; o.editable = true;
			o.validate = function(_sectionId, value) { return steer.validateInput('subscription_url', value); };
			o = s.option(form.Value, 'update_interval', _('Update interval')); o.placeholder = uiSpec.subscription_update_interval_default; o.modalonly = true;
			o.validate = function(_sectionId, value) { return steer.validateInput('positive_duration', value); };
			const subscriptionSection = s;
			return m.render().then((formNode) => steer.focusSection(subscriptionSection, 'subscription').then(() => {
				const gate = subscriptionOperationGate(data?.[2], formNode, permissions);
				return E([], [ renderSubscriptionStatus(data?.[1], gate), formNode ]);
			}));
		}
		const probeGate = probeOperationGate(data?.[2], page == 'routes'
			? permissions.route_speedtest === true : permissions.node_speedtest === true);

		if (page == 'routes') {
			addSystemRouteSection(m, routes.find((route) => route.kind == 'direct'));
			addSystemRouteSection(m, routes.find((route) => route.kind == 'block'));
			const rejectRecovery = renderMissingRejectAction(routes, m);

			s = m.section(form.GridSection, 'route', _('Single-node routes'));
			steer.configureNamedSection(s, steer.creationDefaults('routes', { node: enabledNodeIds[0] || '' }));
			steer.configureOrdering(s, 'routes');
			steer.configureRemovalGuard(s, (sectionId) => steer.collectionReferences('routes', sectionId),
				_('Route is still referenced'));
			configureSingleRouteCreation(s);
			s.addremove = true;
			s.nodescriptions = true;
			s.addbtntitle = _('Add single-node route');
			s.filter = function(sectionId) {
				return uci.get('steer', sectionId, 'kind') == 'single';
			};
			o = s.option(form.Flag, 'enabled', _('Enabled'));
			o.default = '1';
			o.editable = true;

			o = s.option(form.Value, 'name', _('Name'));
			o.rmempty = true;
			o.optional = true;
			o.modalonly = true;

			o = s.option(form.DummyValue, '_single_kind', _('Kind'));
			o.textvalue = function() { return _('Single node'); };

			if (nodeReferences.length) {
				o = s.option(form.RichListValue, 'node', _('Node'));
				/* This GridSection already filters to kind=single; no kind widget is rendered. */
				o.editable = true;
				o.rmempty = false;
				addNodeValues(o, nodeReferences);
				o.textvalue = function(sectionId) {
					return nodeReferenceLabel(nodeReferences, uci.get('steer', sectionId, 'node'));
				};
			}

			if (routeReferences.length) {
				o = s.option(form.RichListValue, 'detour', _('Detour route'));
				/* This GridSection already filters to kind=single; no kind widget is rendered. */
				o.editable = true;
				o.optional = true;
				o.rmempty = true;
				o.value('', _('Direct connection'));
				routeReferences.forEach((reference) => o.value(reference.id, reference.label));
				o.textvalue = function(sectionId) {
					const detour = uci.get('steer', sectionId, 'detour');
					if (!detour)
						return _('Direct connection');
					return routeReferences.find((reference) => reference.id == detour)?.label || _('Missing route');
				};
				o.description = _('The selected single-node route dials through the detour proxy first.');
			}

			o = s.option(form.Button, '_route_connect_test', _('Chain connection test'));
			/* This GridSection already filters to kind=single; no kind widget is rendered. */
			o.depends('enabled', '1');
			o.editable = true;
			o.inputtitle = _('Test');
			o.inputstyle = 'action';
			o.write = function() {};
			o.remove = function() {};
			o.onclick = function(ev, sectionId) { return runRouteSpeedtest(sectionId, false, ev.currentTarget, probeGate); };
			decorateProbeOption(o, 'routes', 'connect', probeResults);

			o = s.option(form.Button, '_route_download_test', _('Chain download test'));
			o.depends('enabled', '1');
			o.editable = true;
			o.inputtitle = _('Test');
			o.inputstyle = 'action';
			o.write = function() {};
			o.remove = function() {};
			o.onclick = function(ev, sectionId) { return runRouteSpeedtest(sectionId, true, ev.currentTarget, probeGate); };
			decorateProbeOption(o, 'routes', 'download', probeResults);
			const routeSection = s;
			return m.render().then((formNode) => steer.focusSection(routeSection, 'route').then(() => {
				probeGate.bindForm(formNode);
				return rejectRecovery ? E([], [ rejectRecovery, formNode ]) : formNode;
			}));
		}

		if (summaryOnly) {
			const visibleNodes = nodes.filter((node) => nodeGroupID(node) == activeNodeGroup);
			const contents = [ renderNodeGroupNavigation(nodeGroups, activeNodeGroup) ];
			const batchSpeedtests = renderBatchSpeedtests(enabledNodeIds, probeGate);
			if (batchSpeedtests)
				contents.push(batchSpeedtests);
			contents.push(renderLightweightNodeTable(visibleNodes, activeGroup, displaySort, probeResults, probeGate,
				permissions.node_export === true));
			return Promise.resolve(E([], contents));
		}

		s = m.section(form.GridSection, 'node', _('Proxy nodes — %s (%d)').format(activeGroup.label, activeGroup.count));
		steer.configureNamedSection(s, steer.creationDefaults('nodes'));
		steer.configureRemovalGuard(s, (sectionId) => steer.collectionReferences('nodes', sectionId),
			_('Node is still referenced'));
		s.addremove = activeNodeGroup == manualNodeGroup;
		/* Subscription nodes are generated data; avoid building editable widgets for every row. */
		s.readonly = summaryOnly;
		if (summaryOnly)
			s.renderRowActions = function() { return E([]); };
		const orderingDisabledReason = displaySort.mode == 'default' ? '' : _('Order');
		steer.configureOrdering(s, 'nodes', { baseActions: !summaryOnly, disabledReason: orderingDisabledReason });
		s.nodescriptions = true;
		s.addbtntitle = _('Add proxy node');
		s.filter = function(sectionId) {
			return nodeGroupID(uci.get('steer', sectionId)) == activeNodeGroup;
		};
		const configuredSections = s.cfgsections;
		s.cfgsections = function() {
			return sortNodeSectionIDs(configuredSections.call(this), displaySort.mode, displaySort.direction, probeResults);
		};
		/* Header sorting is display-only; never call the native UCI reordering sorter. */
		s.handleSort = function() {};
		o = s.option(form.Flag, 'enabled', _('Enabled'));
		o.default = '1';
		o.editable = !summaryOnly;

		o = s.option(form.Button, '_connect_speedtest', _('Connection test'));
		o.editable = true;
		o.inputtitle = _('Test');
		o.inputstyle = 'action';
		o.write = function() {};
		o.remove = function() {};
		o.onclick = function(ev, sectionId) { return runSpeedtest(sectionId, false, ev.currentTarget, probeGate); };
		decorateProbeOption(o, 'nodes', 'connect', probeResults, nodeProbeDisabledReason);

		o = s.option(form.Button, '_download_speedtest', _('Download test'));
		o.editable = true;
		o.inputtitle = _('Test');
		o.inputstyle = 'action';
		o.write = function() {};
		o.remove = function() {};
		o.onclick = function(ev, sectionId) { return runSpeedtest(sectionId, true, ev.currentTarget, probeGate); };
		decorateProbeOption(o, 'nodes', 'download', probeResults, nodeProbeDisabledReason);

		o = s.option(form.Button, '_export_link', _('Share link'));
		o.editable = true;
		o.inputtitle = _('Export link');
		o.inputstyle = 'action';
		o.write = function() {};
		o.remove = function() {};
		o.onclick = function(ev, sectionId) {
			const reason = nodeExportDisabledReason(sectionId, permissions.node_export === true);
			if (reason) {
				ui.addNotification(_('Node export is unavailable'), E('p', {}, reason), 'warning');
				return;
			}
			return showNodeExportDialog(sectionId);
		};
		decorateNodeExportOption(o, permissions.node_export === true);

		o = s.option(form.Value, 'name', _('Name'));
		o.rmempty = true;
		o.optional = true;
		o.modalonly = true;

		o = s.option(form.ListValue, 'type', _('Protocol'));
		uiSpec.node_types.forEach((item) => o.value(item.value, protocolLabel(item.value)));
		o.rmempty = false;
		o.editable = !summaryOnly;
		o.textvalue = function(sectionId) {
			return protocolLabel(uci.get('steer', sectionId, 'type'));
		};

		o = s.option(form.Value, 'server', _('Server'));
		o.rmempty = false;
		o.editable = !summaryOnly;
		uiSpec.node_types.filter((item) => item.value != 'tor').forEach((item) => o.depends('type', item.value));

		o = s.option(form.Value, 'server_port', _('Port'));
		o.datatype = 'port';
		o.rmempty = false;
		o.editable = !summaryOnly;
		uiSpec.node_types.filter((item) => item.value != 'tor').forEach((item) => o.depends('type', item.value));

		uiSpec.node_fields
			.filter((field) => ![ 'enabled', 'name', 'server', 'server_port' ].includes(field.key))
			.forEach((field) => addGeneratedNodeField(s, field));

		const nodeSection = s;
		return m.render().then((formNode) => steer.focusSection(nodeSection, 'node').then(() => {
			const contents = [ renderNodeGroupNavigation(nodeGroups, activeNodeGroup) ];
			decorateNodeSortHeaders(formNode);
			probeGate.bindForm(formNode);
			const batchSpeedtests = renderBatchSpeedtests(enabledNodeIds, probeGate);
			if (batchSpeedtests)
				contents.push(batchSpeedtests);
			if (activeNodeGroup == manualNodeGroup)
				contents.push(renderImportButton(permissions.node_import === true && permissions.uci_write === true));
			contents.push(formNode);
			return E([], contents);
		}));
	},

		handleSaveApply: function(ev, mode) {
		return steer.apply(this, ev, mode);
	}
});

nodesView.nodeProbeMetric = nodeProbeMetric;
nodesView.sortNodeSectionIDs = sortNodeSectionIDs;
nodesView.nextNodeDisplaySort = nextNodeDisplaySort;
nodesView.decorateNodeSortHeaders = decorateNodeSortHeaders;

return nodesView;
