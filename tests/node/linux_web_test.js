#!/usr/bin/env node

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '../..');
const uiSpec = JSON.parse(fs.readFileSync(path.join(root, 'ui/steer-ui-spec.json'), 'utf8'));
const localProxyFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/local-proxy-listen-fixtures.json'), 'utf8'));
const subscriptionStatusFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/subscription-status-fixtures.json'), 'utf8'));
const probeDiagnosticsFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/probe-diagnostics-fixtures.json'), 'utf8'));
const stateLifecycleFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/state-lifecycle-fixtures.json'), 'utf8'));
const validationIssueFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/validation-issue-fixtures.json'), 'utf8'));
const collectionReferenceFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-reference-fixtures.json'), 'utf8'));
const ruleSummaryFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/rule-summary-fixtures.json'), 'utf8'));
const formInputFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/form-input-fixtures.json'), 'utf8'));
const creationPolicyFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/creation-policy-fixtures.json'), 'utf8'));
const collectionOrderingFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-ordering-fixtures.json'), 'utf8'));
const collectionDragFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/collection-drag-fixtures.json'), 'utf8'));
const nodeDisplaySortingFixtures = JSON.parse(fs.readFileSync(path.join(root, 'ui/node-display-sorting-fixtures.json'), 'utf8'));

class Element {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.attributes = {};
    this.dataset = {};
    this.style = {};
    this.listeners = {};
    this.className = '';
    this.value = '';
    this.classList = {
      add: (...names) => this.setClasses([...classSet(this), ...names]),
      remove: (...names) => this.setClasses([...classSet(this)].filter((name) => !names.includes(name))),
      toggle: (name, force) => {
        const names = classSet(this);
        const enabled = force == null ? !names.has(name) : !!force;
        if (enabled) names.add(name); else names.delete(name);
        this.setClasses([...names]);
        return enabled;
      },
      contains: (name) => classSet(this).has(name)
    };
  }

  setClasses(names) { this.className = [...new Set(names.filter(Boolean))].join(' '); }
  append(...children) {
    const values = children.flat(Infinity).filter((child) => child != null);
    values.forEach((child) => { if (child instanceof Element) child.parentElement = child.parentNode = this; });
    this.children.push(...values);
    this.syncSelectValue();
  }
  replaceChildren(...children) {
    this.children.forEach((child) => { if (child instanceof Element) child.parentElement = child.parentNode = null; });
    this.children = children.flat(Infinity).filter((child) => child != null);
    this.children.forEach((child) => { if (child instanceof Element) child.parentElement = child.parentNode = this; });
    this.syncSelectValue();
  }
  insertBefore(child, reference) {
    if (!(child instanceof Element)) return child;
    const previousParent = child.parentElement;
    if (previousParent) {
      const previousIndex = previousParent.children.indexOf(child);
      if (previousIndex >= 0) previousParent.children.splice(previousIndex, 1);
    }
    const index = reference == null ? this.children.length : this.children.indexOf(reference);
    this.children.splice(index < 0 ? this.children.length : index, 0, child);
    child.parentElement = child.parentNode = this;
    return child;
  }
  get nextElementSibling() {
    const siblings = this.parentElement?.children || [];
    const index = siblings.indexOf(this);
    return index >= 0 ? (siblings.slice(index + 1).find((item) => item instanceof Element) || null) : null;
  }
  setAttribute(name, value) {
    const normalized = String(value);
    this.attributes[name] = normalized;
    if (['value', 'type', 'placeholder', 'rows', 'id'].includes(name)) this[name] = normalized;
    if (name === 'selected') this.selected = true;
    if (name === 'disabled') this.disabled = true;
    if (name === 'hidden') this.hidden = true;
  }
  getAttribute(name) { return this.attributes[name]; }
  removeAttribute(name) { delete this.attributes[name]; }
  addEventListener(name, listener) { this.listeners[name] = listener; }
  removeEventListener() {}
  remove() {
    const index = this.parentElement?.children.indexOf(this) ?? -1;
    if (index >= 0) this.parentElement.children.splice(index, 1);
    this.parentElement = this.parentNode = null;
    this.removed = true;
  }
  focus() {}
  setSelectionRange(start, end) { this.selectionStart = start; this.selectionEnd = end; }
  contains(candidate) {
    for (let current = candidate; current; current = current.parentElement) if (current === this) return true;
    return false;
  }
  closest(selector) {
    for (let current = this; current; current = current.parentElement) if (matches(current, selector)) return current;
    return null;
  }
  getBoundingClientRect() { return this.rect || { top: 0, left: 0, width: 600, height: 40, bottom: 40, right: 600 }; }
  animate(frames, options) {
    (this.animations ||= []).push({ frames, options });
    return { cancel() {} };
  }
  cloneNode(deep = false) {
    const clone = new Element(this.tag);
    clone.attributes = { ...this.attributes };
    clone.dataset = { ...this.dataset };
    clone.style = { ...this.style };
    clone.className = this.className;
    clone.value = this.value;
    if (deep) clone.append(...this.children.map((child) => child instanceof Element ? child.cloneNode(true) : child));
    return clone;
  }

  syncSelectValue() {
    if (this.tag !== 'select') return;
    const options = this.children.filter((child) => child instanceof Element && child.tag === 'option');
    const selected = options.find((option) => option.selected) || options[0];
    if (selected) this.value = selected.value;
  }

  querySelector(selector) {
    return find(this, (element) => element !== this && matches(element, selector));
  }

  querySelectorAll(selector) { return findAll(this, (element) => element !== this && matches(element, selector)); }
}

function classSet(element) {
  return new Set(String(element.className || '').split(/\s+/).filter(Boolean));
}

function matches(element, selector) {
  if (selector.includes(',')) return selector.split(',').some((part) => matches(element, part.trim()));
  const simple = selector.trim().split(/\s+/).pop();
  if (simple.startsWith('.')) return simple.slice(1).split('.').every((name) => classSet(element).has(name));
  if (simple.startsWith('#')) return element.attributes.id === simple.slice(1);
  if (simple === '[data-collection-id]') return !!element.dataset.collectionId;
  return element.tag === simple;
}

function find(value, predicate) {
  if (value instanceof Element && predicate(value)) return value;
  if (!(value instanceof Element)) return null;
  for (const child of value.children) {
    const match = find(child, predicate);
    if (match) return match;
  }
  return null;
}

function findAll(value, predicate) {
  if (!(value instanceof Element)) return [];
  const matches = predicate(value) ? [value] : [];
  return matches.concat(value.children.flatMap((child) => findAll(child, predicate)));
}

function text(value) {
  if (!(value instanceof Element)) return String(value ?? '');
  return value.children.map(text).join('');
}

function createEnvironment(save, intent = { main: { enabled: true } }, options = {}) {
  const side = new Element('aside');
  const strip = new Element('div');
  const toasts = new Element('div');
  const view = new Element('main');
  const drawerRoot = new Element('div');
  const body = new Element('body');
  side.setAttribute('id', 'side');
  strip.setAttribute('id', 'strip');
  toasts.setAttribute('id', 'toasts');
  view.setAttribute('id', 'view');
  drawerRoot.setAttribute('id', 'drawer-root');
  body.append(side, strip, toasts, view, drawerRoot);
  const documentListeners = new Map();
  const windowListeners = new Map();
  const addListener = (registry, name, listener) => {
    if (!registry.has(name)) registry.set(name, []);
    registry.get(name).push(listener);
  };
  const removeListener = (registry, name, listener) => {
    registry.set(name, (registry.get(name) || []).filter((candidate) => candidate !== listener));
  };
  const dispatch = async (registry, name, event = {}) => {
    const results = (registry.get(name) || []).slice().map((listener) => listener(event));
    await Promise.all(results);
    return event;
  };
  const document = {
    body,
    createElement: (tag) => new Element(tag),
    createTextNode: (value) => String(value),
    querySelector: (selector) => body.querySelector(selector),
    querySelectorAll: (selector) => body.querySelectorAll(selector),
    addEventListener: (name, listener) => addListener(documentListeners, name, listener),
    removeEventListener: (name, listener) => removeListener(documentListeners, name, listener)
  };
  const h = (tag, attributes = {}, ...children) => {
    const element = new Element(tag);
    for (const [name, value] of Object.entries(attributes)) {
      if (value == null || value === false) continue;
      if (name === 'class') element.className = value;
      else if (name === 'dataset') Object.assign(element.dataset, value);
      else if (name.startsWith('on')) element.addEventListener(name.slice(2), value);
      else element.setAttribute(name, value === true ? '' : value);
    }
    element.append(...children);
    return element;
  };
  let touchCount = 0;
  const S = {
    uiSpec,
    h,
    icon: () => new Element('span'),
    asList: (value) => value || [],
    fmtTime: (value) => String(value),
    fmtRevision: (value) => value,
    uid: (prefix) => `${prefix}-test`,
    api: {
      async geodata() { return { names: [], readable: false, count: 0 }; },
      async speedtestNode() { return { scope: 'nodes', object_id: 'node', kind: 'connect', tested_at: '2026-08-26T01:00:00Z', ok: true, stale: false, summary: '21 ms', error_summary: '' }; },
      async probeResults() { return options.probeResults || probeDiagnosticsFixtures.probe_results; },
      ...(options.api || {})
    },
    store: {
      intent,
      diagnostics: options.diagnostics || { warnings: [] },
      probeResults: options.probeResults || { latest_results: [], warnings: [] },
      overview: options.overview || {},
      runtime: options.runtime || {},
      revision: 'revision-1',
      dirty: false,
      pendingApply: options.pendingApply === true,
      save,
      setEnabled: (enabled) => save(enabled),
      applySaved: options.applySaved || (async () => ({ ok: true })),
      installProbeResult(result) {
        const key = (value) => `${value.scope}:${value.object_id || ''}:${value.kind}`;
        this.probeResults = {
          ...this.probeResults,
          latest_results: [result, ...(this.probeResults.latest_results || []).filter((candidate) => key(candidate) !== key(result))]
        };
      },
      async refreshProbeResults() {
        this.probeResults = await S.api.probeResults();
        return this.probeResults;
      },
      touch() { touchCount++; this.dirty = true; }
    }
  };
  const window = {
    S,
    addEventListener: (name, listener) => addListener(windowListeners, name, listener),
    removeEventListener: (name, listener) => removeListener(windowListeners, name, listener)
  };
  const libSource = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/lib.js'), 'utf8');
  new Function('window', 'document', 'Node', libSource)(window, document, Element);
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/ui.js'), 'utf8');
  new Function('window', 'document', 'setTimeout', 'requestAnimationFrame', source)(
    window, document, options.immediateTimers ? (callback) => { callback(); return 0; } : () => 0,
    (callback) => callback()
  );
  return {
    S, window, document, body, side, strip, toasts, view, drawerRoot,
    dispatchDocument: (name, event) => dispatch(documentListeners, name, event),
    dispatchWindow: (name, event) => dispatch(windowListeners, name, event),
    get touchCount() { return touchCount; }
  };
}

function loadView(environment, name) {
  const source = fs.readFileSync(path.join(root, `go/cmd/steer-linux/web/js/views/${name}.js`), 'utf8');
  new Function('window', 'document', 'setTimeout', source)(environment.window, environment.document, () => 0);
}

function attachRealStore(environment, api) {
  environment.S.api = api;
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/store.js'), 'utf8');
  new Function('window', source)(environment.window);
}

function loadApplication(environment, initialHash = '#/advanced') {
  let hash = initialHash;
  const location = {};
  Object.defineProperty(location, 'hash', {
    get: () => hash,
    set: (next) => {
      hash = String(next);
      void environment.dispatchWindow('hashchange', {});
    }
  });
  const history = {
    replaceState(_state, _title, next) { hash = String(next); }
  };
  environment.window.location = location;
  environment.S.auth = { logout() {}, show() {} };
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/app.js'), 'utf8');
  new Function('window', 'document', 'location', 'history', source)(environment.window, environment.document, location, history);
  return { location, history, start: () => environment.dispatchDocument('DOMContentLoaded', {}) };
}

async function flushUI() {
  await Promise.resolve();
  await new Promise((resolve) => setImmediate(resolve));
  await Promise.resolve();
}

function deferred() {
  let resolve, reject;
  const promise = new Promise((onResolve, onReject) => { resolve = onResolve; reject = onReject; });
  return { promise, resolve, reject };
}

function fieldWithLabel(rootElement, label) {
  return findAll(rootElement, (element) => classSet(element).has('field')).find((element) => {
    const labelElement = element.children.find((child) => child instanceof Element && child.tag === 'label');
    return labelElement && text(labelElement) === label;
  });
}

function buttonWithText(rootElement, label) {
  return find(rootElement, (element) => element.tag === 'button' && text(element) === label);
}

function lastButtonWithText(rootElement, label) {
  return findAll(rootElement, (element) => element.tag === 'button' && text(element) === label).at(-1) || null;
}

function createRulesEnvironment(intent) {
  const rootElement = new Element('main');
  const document = {
    querySelector(selector) {
      if (selector === '#view') return rootElement;
      throw new Error(`unexpected selector ${selector}`);
    },
    querySelectorAll() { return []; }
  };
  const h = (tag, attributes = {}, ...children) => {
    const element = new Element(tag);
    for (const [name, value] of Object.entries(attributes)) {
      if (value == null || value === false) continue;
      if (name === 'class') element.className = value;
      else if (name === 'dataset') Object.assign(element.dataset, value);
      else if (name.startsWith('on')) element.addEventListener(name.slice(2), value);
      else element.setAttribute(name, value === true ? '' : value);
    }
    element.append(...children);
    return element;
  };
  let nextID = 1;
  let touchCount = 0;
  const ui = {
    beginRender: (root) => root.replaceChildren(),
    viewHead: (title, subtitle, actions) => h('header', {}, title, subtitle, actions),
    collectionRowAttributes: (_collection, item, _items, _rerender, baseClass) => ({
      class: `${baseClass || ''} entity-row`, dataset: { ruleId: item.id }
    }),
    collectionDragHandle: () => h('button', { class: 'collection-drag-handle' }, '⠿'),
    collectionOrderToolbar: () => h('div', {}, [
      h('button', { disabled: true }, '上移'), h('button', { disabled: true }, '下移')
    ]),
    input: ({ value }) => Object.assign(new Element('input'), { value }),
    toggle: () => new Element('button'),
    selectWithMissing: (options) => options,
    referenceOptions: (_collection, items) => (items || []).map((item) => [item.id, item.name || item.id]),
    creationDraft: (collection, overrides = {}) => ({
      ...(uiSpec.creation_defaults[collection] || {}), id: `rule_${nextID++}`, ...overrides
    }),
    select: (options, value) => Object.assign(new Element('select'), { value }),
    multiChoice: (_options, values) => Object.assign(new Element('div'), { value: values || [], commitPending: () => false }),
    chips: () => Object.assign(new Element('div'), { commitPending: () => false }),
    matchEditor: ({ value }) => ({ el: new Element('textarea'), value: value || [] }),
    field: (label, control, help) => h('label', {}, label, control, help),
    toast: () => {},
    guardCollectionDeletion: () => true,
    takeObjectFocus: () => null,
    focusDrawerOption: () => {},
    drawer(options) {
      const editor = options.renderBody(new Element('div'));
      const rule = editor.submit();
      if (rule !== false) options.onSubmit(rule);
      return {};
    }
  };
  const S = {
    uiSpec,
    h,
    asList: (value) => value == null ? [] : (Array.isArray(value) ? value : [value]),
    uid: () => `rule_${nextID++}`,
    api: { geodata: () => Promise.resolve({ readable: true, names: [], count: 0 }) },
    ui,
    store: {
      intent,
      touch() { touchCount++; }
    }
  };
  const window = { S };
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/views/rules.js'), 'utf8');
  new Function('window', 'document', source)(window, document);

  return {
    S,
    root: rootElement,
    render: () => S.views.rules.render(rootElement),
    async addRule() {
      const button = find(rootElement, (element) => element.tag === 'button' && text(element) === '添加规则');
      assert.ok(button, 'Rules view must expose its add action');
      button.listeners.click();
      await new Promise((resolve) => setImmediate(resolve));
    },
    renderedRuleIDs: () => findAll(rootElement, (element) => element.className.split(' ').includes('rule-row'))
      .map((element) => element.dataset.ruleId),
    get touchCount() { return touchCount; }
  };
}

function loadDNSView() {
  const S = { uiSpec, h: () => ({}), ui: {}, views: {} };
  const window = { S };
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/views/dns.js'), 'utf8');
  new Function('window', source)(window);
  return S.views.dns;
}

function testDNSProtocolSwitchUsesSharedMatrix() {
  const view = loadDNSView();
  const staleDoH = {
    protocol: 'https', server_port: 443, tls_server_name: 'dns.example', path: '/dns-query', insecure: true
  };
  const udpDraft = JSON.parse(JSON.stringify(staleDoH));
  view.applyProtocol(udpDraft, 'udp');
  view.commitProfile(staleDoH, udpDraft);
  assert.deepStrictEqual(staleDoH, { protocol: 'udp', server_port: 53 },
    'DoH to UDP must clear TLS/HTTP fields and move the old default port to the UDP default');

  const customPort = {
    protocol: 'https', server_port: 8443, tls_server_name: 'dns.example', path: '/custom', insecure: true
  };
  view.applyProtocol(customPort, 'udp');
  assert.strictEqual(customPort.server_port, 8443,
    'protocol switching must preserve an explicit non-default port');
  for (const field of ['tls_server_name', 'path', 'insecure']) {
    assert.strictEqual(Object.prototype.hasOwnProperty.call(customPort, field), false,
      `UDP cleanup must remove ${field} using the shared field matrix`);
  }
}

async function testFailedToggleRestoresDraft() {
  const environment = createEnvironment(async () => { throw new Error('network failed'); });
  await environment.S.ui.onToggleEnabled(false);
  assert.strictEqual(environment.S.store.intent.main.enabled, true, 'failed toggle must restore the prior draft value');
  assert.strictEqual(environment.S.store.dirty, false, 'failed toggle must preserve the prior clean state');
  assert.strictEqual(environment.touchCount, 0, 'immediate toggle must not manufacture a dirty draft before saving');
}

async function testGlobalEnableContractUsesTheCompleteDirtyDraft() {
  const intent = draftLifecycleIntent();
  intent.main.log_level = 'debug';
  let requested;
  const environment = createEnvironment(async (enabled) => {
    requested = enabled;
    return { ok: true, res: { applied: true } };
  }, intent);
  environment.S.store.dirty = true;
  environment.S.store.draftValid = false;
  environment.S.store.draftError = 'invalid JSON';
  await environment.S.ui.onToggleEnabled(false);
  assert.strictEqual(requested, false);
  assert.strictEqual(intent.main.enabled, true);
  assert.strictEqual(intent.main.log_level, 'debug');
  assert.strictEqual(environment.S.store.dirty, true);
}

async function testConflictRestoresUntilOverwriteIsChosen() {
  const environment = createEnvironment(async () => ({ ok: true, res: { applied: true } }));
  await environment.S.ui.onToggleEnabled(false);
  assert.strictEqual(environment.S.store.intent.main.enabled, true);
  assert.strictEqual(environment.touchCount, 0);
  assert.ok(!find(environment.body, (element) => element.tag === 'button' && text(element).includes('覆盖保存')));
}

async function testConflictOverwritePreservesEverySaveIntent() {
  async function exercise(apply) {
    const calls = [];
    const environment = createEnvironment(async (requestedApply, force) => {
      calls.push({ apply: requestedApply, force });
      if (calls.length === 1) return { ok: false, conflict: { serverRevision: 'revision-2', external: {} } };
      return { ok: true, res: { applied: requestedApply } };
    });
    await environment.S.ui.onSave(apply);
    lastButtonWithText(environment.body, '覆盖保存（保留本地修改）').listeners.click();
    await flushUI();
    return calls;
  }

  assert.deepStrictEqual(await exercise(false), [{ apply: false, force: undefined }, { apply: false, force: true }],
    'ordinary Save conflict overwrite must remain Save-only');
  assert.deepStrictEqual(await exercise(true), [{ apply: true, force: undefined }, { apply: true, force: true }],
    'Save and Apply conflict overwrite must still Apply');


}

async function testApplyFailureNotificationsTakePrecedenceOverStaleDraft() {
  const failed = {
    ok: true,
    res: { applied: false, apply_result: { error: 'activate failed' } },
    staleDraft: true
  };

  let environment = createEnvironment(async () => failed);
  await environment.S.ui.onSave(true);
  assert.match(text(environment.toasts), /配置已保存但应用失败：activate failed；期间新修改仍未保存/,
    'ordinary Save and Apply must report Apply failure before the stale Draft warning');
  assert.doesNotMatch(text(environment.toasts), /已保存并应用/);

  environment = createEnvironment(async () => failed);
  await environment.S.ui.onToggleEnabled(false);
  assert.match(text(environment.toasts), /已保存为禁用，但应用失败：activate failed；期间新修改仍未保存/,
    'Enable or Disable must not describe a failed Apply as successful when the Draft is also stale');
  assert.doesNotMatch(text(environment.toasts), /已保存并应用禁用/);

  let calls = 0;
  environment = createEnvironment(async () => {
    calls++;
    if (calls === 1) return { ok: false, conflict: { serverRevision: 'revision-2', external: {} } };
    return failed;
  });
  await environment.S.ui.onSave(true);
  lastButtonWithText(environment.body, '覆盖保存（保留本地修改）').listeners.click();
  await flushUI();
  assert.match(text(environment.toasts), /已覆盖保存，但应用失败：activate failed；期间新修改仍未保存/,
    'force overwrite must report Apply failure before the stale Draft warning');
  assert.doesNotMatch(text(environment.toasts), /已覆盖保存并应用/);
}

async function testNewRulesAreStoredBeforeDefault() {
  const defaultRule = { id: 'default', enabled: true, default: true, dns_profile: 'direct_dns', route: 'direct' };
  let environment = createRulesEnvironment({
    rules: [defaultRule],
    dns_profiles: [{ id: 'direct_dns', name: 'Direct DNS' }],
    routes: [{ id: 'direct', name: 'Direct' }]
  });
  environment.render();
  await environment.addRule();
  await environment.addRule();
  assert.deepEqual(environment.S.store.intent.rules.map((rule) => rule.id), ['rule_1', 'rule_2', 'default'],
    'A default-only draft stores consecutive new rules in first-match order before Default');
  assert.deepEqual(environment.renderedRuleIDs(), ['rule_1', 'rule_2'],
    'The rendered ordinary-rule order matches the JSON draft order');
  assert.strictEqual(environment.touchCount, 2, 'Each inserted rule marks the draft dirty once');

  environment = createRulesEnvironment({
    rules: [
      { id: 'existing_a', enabled: true, name: 'Existing A', dns_profile: 'direct_dns', route: 'direct' },
      { id: 'existing_b', enabled: true, name: 'Existing B', dns_profile: 'direct_dns', route: 'direct' },
      { ...defaultRule }
    ],
    dns_profiles: [{ id: 'direct_dns', name: 'Direct DNS' }],
    routes: [{ id: 'direct', name: 'Direct' }]
  });
  environment.render();
  await environment.addRule();
  assert.deepEqual(environment.S.store.intent.rules.map((rule) => rule.id),
    ['existing_a', 'existing_b', 'rule_1', 'default'],
    'A new rule follows existing ordinary rules while remaining before Default');
  assert.deepEqual(environment.renderedRuleIDs(), ['existing_a', 'existing_b', 'rule_1'],
    'Existing and newly inserted rules retain the same storage and display order');
}

function testChipsCommitPendingTokensConsistently() {
  const environment = createEnvironment(async () => ({ ok: true }));
  const updates = [];
  const control = environment.S.ui.chips(['existing'], { onchange: (values) => updates.push(values) });
  const input = find(control, (element) => classSet(element).has('chips__input'));
  const add = find(control, (element) => classSet(element).has('chips__add'));

  input.value = 'blurred';
  input.listeners.blur();
  assert.deepStrictEqual(updates.at(-1), ['existing', 'blurred'], 'blur must commit the pending token');

  const updateCount = updates.length;
  input.value = '  ';
  input.listeners.blur();
  input.value = 'existing';
  add.listeners.click();
  assert.strictEqual(updates.length, updateCount, 'blank and duplicate tokens must not be added');

  let commaPrevented = false;
  input.value = 'literal,comma';
  input.listeners.keydown({ key: ',', preventDefault() { commaPrevented = true; } });
  assert.strictEqual(commaPrevented, false, 'comma must not act as a global chip delimiter');
  input.listeners.blur();
  assert.deepStrictEqual(updates.at(-1), ['existing', 'blurred', 'literal,comma'], 'commas in field content must survive');

  input.value = 'added';
  add.listeners.click();
  assert.deepStrictEqual(updates.at(-1), ['existing', 'blurred', 'literal,comma', 'added'], 'explicit Add must share the commit path');

  let enterPrevented = false;
  input.value = 'entered';
  input.listeners.keydown({ key: 'Enter', preventDefault() { enterPrevented = true; } });
  assert.ok(enterPrevented, 'Enter must submit the pending token without submitting the surrounding form');
  assert.deepStrictEqual(updates.at(-1), ['existing', 'blurred', 'literal,comma', 'added', 'entered'], 'Enter must share the commit path');
}

function representativeIntent(privateKey) {
  return {
    main: { enabled: true },
    subscriptions: [],
    nodes: [{
      id: 'ssh-node', enabled: true, name: 'SSH node', type: 'ssh', server: 'ssh.example', server_port: 22,
      username: 'root', password: 'password-secret', private_key: privateKey,
      host_key_algorithms: ['ssh-ed25519']
    }],
    routes: [{ id: 'direct', enabled: true, kind: 'direct' }],
    dns_profiles: [{ id: 'public', enabled: true, name: 'Public' }],
    local_proxies: [],
    rules: [
      { id: 'rule-one', enabled: true, name: 'Rule one', dns_profile: 'public', route: 'direct' },
      { id: 'default', enabled: true, default: true, name: 'Default', dns_profile: 'public', route: 'direct' }
    ]
  };
}

function openOnlyEditor(environment, viewName) {
  environment.S.views[viewName].render(environment.view);
  const edit = buttonWithText(environment.view, '编辑');
  assert.ok(edit, `${viewName} view must expose its editor`);
  return edit.listeners.click();
}

function testNodeStringListSubmitAndPrivateKeyRoundTrip() {
  const originalKey = '-----BEGIN OPENSSH PRIVATE KEY-----\noriginal\n-----END OPENSSH PRIVATE KEY-----\n';
  const editedKey = '-----BEGIN OPENSSH PRIVATE KEY-----\nline one\n\nline two  \n-----END OPENSSH PRIVATE KEY-----\n';
  const environment = createEnvironment(async () => ({ ok: true }), representativeIntent(originalKey));
  loadView(environment, 'nodes');
  openOnlyEditor(environment, 'nodes');

  let privateKeyField = fieldWithLabel(environment.drawerRoot, '私钥');
  let privateKeyInput = find(privateKeyField, (element) => element.tag === 'textarea');
  assert.ok(privateKeyInput, 'multiline sensitive private_key must render as a textarea');
  assert.ok(privateKeyInput.classList.contains('is-masked'), 'private_key must be masked by default');
  assert.strictEqual(privateKeyInput.value, originalKey, 'the editor must preserve the loaded key including its trailing newline');

  const reveal = find(privateKeyField, (element) => classSet(element).has('input-reveal__button'));
  reveal.listeners.click();
  assert.ok(!privateKeyInput.classList.contains('is-masked'), 'private_key must reveal only after the explicit control is used');
  assert.strictEqual(reveal.getAttribute('aria-pressed'), 'true', 'reveal state must be exposed accessibly');

  privateKeyInput.value = editedKey;
  privateKeyInput.listeners.input({ target: privateKeyInput });

  const algorithmsField = fieldWithLabel(environment.drawerRoot, 'Host key algorithms');
  const pendingAlgorithm = find(algorithmsField, (element) => classSet(element).has('chips__input'));
  pendingAlgorithm.value = 'ssh-rsa';

  const protocolField = fieldWithLabel(environment.drawerRoot, '协议');
  const protocol = find(protocolField, (element) => element.tag === 'select');
  protocol.value = 'trojan';
  protocol.listeners.change({ target: protocol });
  protocol.value = 'ssh';
  protocol.listeners.change({ target: protocol });

  privateKeyField = fieldWithLabel(environment.drawerRoot, '私钥');
  privateKeyInput = find(privateKeyField, (element) => element.tag === 'textarea');
  assert.strictEqual(privateKeyInput.value, editedKey, 'switching SSH authentication fields must not clear private_key');
  const passwordField = fieldWithLabel(environment.drawerRoot, '密码');
  const password = find(passwordField, (element) => element.tag === 'input');
  assert.strictEqual(password.value, 'password-secret', 'switching away from and back to SSH must not clear password');

  const currentAlgorithmsField = fieldWithLabel(environment.drawerRoot, 'Host key algorithms');
  const finalPending = find(currentAlgorithmsField, (element) => classSet(element).has('chips__input'));
  finalPending.value = 'rsa-sha2-512';
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();

  const savedNode = environment.S.store.intent.nodes[0];
  assert.strictEqual(savedNode.private_key, editedKey, 'drawer submit must preserve private_key bytes and newlines');
  assert.strictEqual(savedNode.password, 'password-secret', 'saving private-key authentication must not clear the password secret');
  assert.deepStrictEqual(
    savedNode.host_key_algorithms,
    ['ssh-ed25519', 'ssh-rsa', 'rsa-sha2-512'],
    'node string-list pending tokens must flush on rebuild and drawer submit'
  );

  const reloaded = createEnvironment(async () => ({ ok: true }), JSON.parse(JSON.stringify(environment.S.store.intent)));
  loadView(reloaded, 'nodes');
  openOnlyEditor(reloaded, 'nodes');
  const reloadedKey = find(fieldWithLabel(reloaded.drawerRoot, '私钥'), (element) => element.tag === 'textarea');
  assert.strictEqual(reloadedKey.value, editedKey, 'save and reload must retain every private-key newline');
  assert.ok(reloadedKey.classList.contains('is-masked'), 'a reloaded private key must return to the masked state');
}

function testNodeEditorUsesSharedDefaultWithoutMutatingDraft() {
  const intent = representativeIntent('key');
  intent.nodes = [{
    id: 'reality-node', enabled: true, name: 'Reality node', type: 'vless',
    server: 'proxy.example', server_port: 443,
    uuid: '00000000-0000-4000-8000-000000000001',
    flow: 'xtls-rprx-vision', tls_server_name: 'proxy.example',
    reality_public_key: 'public-key', reality_short_id: '0123456789abcdef'
  }];
  const environment = createEnvironment(async () => ({ ok: true }), intent);
  loadView(environment, 'nodes');
  openOnlyEditor(environment, 'nodes');

  const transportField = fieldWithLabel(environment.drawerRoot, '传输');
  const transport = find(transportField, (element) => element.tag === 'select');
  assert.ok(transport, 'VLESS editor must expose the shared transport picker');
  assert.strictEqual(transport.value, 'tcp', 'omitted transport must display the shared TCP / Raw default');
  assert.ok(!text(transportField).includes('缺失（需修复）'),
    'a field with a shared default must not render the invalid missing choice');
  assert.strictEqual(fieldWithLabel(environment.drawerRoot, '传输路径'), undefined,
    'conditional WebSocket fields must remain hidden when the effective transport is TCP / Raw');
  assert.ok(!Object.hasOwn(intent.nodes[0], 'transport'),
    'opening an existing node must not materialize the editor fallback into the Draft');
}

async function testNodeExportShowsSharedBackendLink() {
  const node = {
    id: 'edge', enabled: true, name: 'Edge', type: 'vless', server: 'proxy.example', server_port: 443,
    uuid: '00000000-0000-4000-8000-000000000001', tls_server_name: 'edge.example'
  };
  const calls = [];
  const environment = createEnvironment(async () => ({ ok: true }), {
    main: { enabled: true }, subscriptions: [], nodes: [node], routes: [], dns_profiles: [], local_proxies: [], rules: []
  }, { api: {
    exportNode: async (value) => {
      calls.push(value);
      return { uri: 'vless://00000000-0000-4000-8000-000000000001@proxy.example:443?security=tls&sni=edge.example#Edge' };
    }
  } });
  loadView(environment, 'nodes');
  environment.S.views.nodes.render(environment.view);
  const button = buttonWithText(environment.view, '导出链接');
  assert.ok(button, 'every shareable Linux node exposes Export link');
  button.listeners.click();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepStrictEqual(calls, [node], 'Linux exports the current Draft node through the shared backend serializer');
  const textarea = find(environment.body, (element) => element.tag === 'textarea' && classSet(element).has('export-link'));
  assert.ok(textarea?.value.startsWith('vless://'));
  assert.ok(Object.hasOwn(textarea.attributes, 'readonly'), 'the credential-bearing link is displayed read-only');
  assert.match(text(environment.body), /包含完整节点凭据/, 'the export dialog warns that the link contains credentials');
}

async function testRuleChoicesAreRestrictedAndSavedOnDrawerSubmit() {
  const intent = representativeIntent('key');
  intent.local_proxies.push({ id: 'proxy-last', enabled: true, name: 'Local proxy', protocol: 'socks', listen: '127.0.0.1', listen_port: 1080 });
  const environment = createEnvironment(async () => ({ ok: true }), intent);
  loadView(environment, 'rules');
  await openOnlyEditor(environment, 'rules');
  const inbound = fieldWithLabel(environment.drawerRoot, '本地代理入口');
  assert.equal(find(inbound, (element) => classSet(element).has('chips__input')), null,
    'inbound no longer accepts arbitrary internal IDs');
  const proxyChoice = buttonWithText(inbound, 'Local proxy');
  proxyChoice.listeners.click();
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.deepStrictEqual(
    environment.S.store.intent.rules[0].inbound,
    ['proxy-last'],
    'rule drawer persists only a selected Local Proxy reference'
  );
}

function testSharedCreationDefaultsAutomaticIDsAndReferenceLabels() {
  assert.strictEqual(creationPolicyFixtures.schema_version, 1);
  const environment = createEnvironment(async () => ({ ok: true }), {
    main: {}, nodes: [], routes: [], dns_profiles: [], local_proxies: [], rules: [], subscriptions: []
  });
  for (const fixture of creationPolicyFixtures.cases) {
    const actual = environment.S.ui.creationDraft(fixture.collection, {
      id: fixture.id, ...(fixture.overrides || {})
    });
    assert.deepStrictEqual(actual, fixture.expected, `${fixture.collection} creation Canonical must match the shared fixture`);
  }
  const generated = environment.S.ui.creationDraft('nodes');
  assert.match(generated.id, new RegExp(uiSpec.id_policy.pattern));
  const options = environment.S.ui.referenceOptions(
    'nodes', creationPolicyFixtures.ambiguous_references.nodes
  );
  assert.strictEqual(options.find(([id]) => id === 'node-unique')[1], 'Unique',
    'an unambiguous selector label stays concise');
  assert.match(options.find(([id]) => id === 'node-a1b2c3')[1], /Same · a\.example:1080 · 同名项 1/,
    'duplicate names include endpoint and a natural ordinal');
  for (const [collection, expected] of [
    ['routes', /Same · 节点 未选择 · 同名项 1/],
    ['dns_profiles', /Same · 1\.1\.1\.1:53 · 同名项 1/],
    ['local_proxies', /Same · 127\.0\.0\.1:1090 · 同名项 1/]
  ]) {
    const label = environment.S.ui.referenceOptions(
      collection, creationPolicyFixtures.ambiguous_references[collection]
    )[0][1];
    assert.match(label, expected, `${collection} duplicate names remain distinguishable`);
  }
}

async function testApplySavedAPIKeepsStructuredFailure() {
  let request = null;
  class TestHeaders {
    constructor() { this.values = {}; }
    set(name, value) { this.values[name.toLowerCase()] = value; }
    has(name) { return Object.hasOwn(this.values, name.toLowerCase()); }
  }
  const sessionStorage = { getItem() { return ''; }, setItem() {}, removeItem() {} };
  const result = {
    ok: false,
    error: 'start candidate Linux generation: systemd refused start',
    candidate_generation: '/run/steer/generations/candidate.failed-new',
    activated: false
  };
  const fetch = async (url, options) => {
    request = { url, options };
    return { ok: false, status: 422, statusText: 'Unprocessable Entity', headers: { get() { return ''; } }, json: async () => result };
  };
  const window = { S: { asList: (value) => value || [] } };
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/api.js'), 'utf8');
  new Function('window', 'sessionStorage', 'Headers', 'fetch', 'document', 'setTimeout', 'location', source)(
    window, sessionStorage, TestHeaders, fetch, { querySelector() { return null; }, body: new Element('body') }, () => 0, {}
  );
  const response = await window.S.api.applySaved();
  assert.strictEqual(request.url, '/api/v1/apply');
  assert.strictEqual(request.options.method, 'POST');
  assert.strictEqual(response.request_ok, false, 'business failure should remain a structured Apply result');
  assert.match(response.error, /refused start/);
}

async function testStoreTracksSavedPendingApply() {
  let overview = { pending_apply: false, status: {} };
  let applyResult = { ok: false, error: 'activate failed', runtime_digest: 'runtime-new' };
  const S = {
    api: {
      async config() {
        return { intent: { main: { enabled: true }, nodes: [], subscriptions: [], routes: [], dns_profiles: [], local_proxies: [], rules: [] }, revision: 'revision-1' };
      },
      async overview() { return overview; },
      async runtime() { return {}; },
      async putConfig(_intent, _revision, apply) {
        assert.strictEqual(apply, false);
        overview = { pending_apply: true, status: {} };
        return { saved: true, applied: false, revision: 'revision-2', request_ok: true };
      },
      async applySaved() { return applyResult; }
    }
  };
  const window = { S };
  const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/store.js'), 'utf8');
  new Function('window', source)(window);
  await S.store.init();
  S.store.touch();
  await S.store.save(false);
  assert.strictEqual(S.store.dirty, false, 'Save should clean only the local draft');
  assert.strictEqual(S.store.pendingApply, true, 'runtime-affecting saved change must remain Applyable while clean');

  const failed = await S.store.applySaved();
  assert.strictEqual(failed.ok, false);
  assert.strictEqual(S.store.pendingApply, true, 'failed Apply must retain Saved pending state');

  applyResult = { ok: true, runtime_digest: 'runtime-new' };
  overview = { pending_apply: false, status: {} };
  const succeeded = await S.store.applySaved();
  assert.strictEqual(succeeded.ok, true);
  assert.strictEqual(S.store.pendingApply, false, 'successful Apply must clear pending state');
}

async function testSharedCollectionOrderingMovesOnlyTheDraft() {
  assert.strictEqual(collectionOrderingFixtures.schema_version, 1);
  assert.deepEqual(new Set(Object.keys(uiSpec.collection_ordering)), new Set(collectionOrderingFixtures.collections));
  for (const fixture of collectionOrderingFixtures.cases) {
    const intent = runtimeTestIntent();
    intent[fixture.collection] = JSON.parse(JSON.stringify(fixture.objects));
    const S = {
      uiSpec,
      api: {
        async config() { return { intent, revision: 'revision-ordering' }; },
        async overview() { return { pending_apply: false, status: {} }; },
        async runtime() { return {}; }
      }
    };
    const window = { S };
    const source = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/store.js'), 'utf8');
    new Function('window', source)(window);
    await S.store.init();

    assert.strictEqual(S.store.moveCollectionItem(
      fixture.collection, fixture.move_id, fixture.offset, fixture.visible_ids
    ), true, fixture.name);
    assert.deepEqual(S.store.intent[fixture.collection].map((item) => item.id), fixture.expected_ids, fixture.name);
    assert.strictEqual(S.store.dirty, true, `${fixture.name} changes only the Draft`);
    assert.deepEqual(
      new Set(S.store.intent[fixture.collection].map((item) => item.id)),
      new Set(fixture.objects.map((item) => item.id)),
      `${fixture.name} preserves stable identities`
    );
  }

  const rules = collectionOrderingFixtures.cases.find((fixture) => fixture.collection === 'rules');
  const intent = runtimeTestIntent();
  intent.rules = JSON.parse(JSON.stringify(rules.objects));
  const S = {
    uiSpec,
    api: {
      async config() { return { intent, revision: 'revision-pinned' }; },
      async overview() { return {}; },
      async runtime() { return {}; }
    }
  };
  new Function('window', fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/store.js'), 'utf8'))({ S });
  await S.store.init();
  assert.strictEqual(S.store.moveCollectionItem('rules', 'default', -1, rules.visible_ids), false);
  assert.strictEqual(S.store.moveCollectionItem('rules', 'rule_a', -1, rules.visible_ids), false);
  assert.strictEqual(S.store.dirty, false, 'pinned and boundary moves do not dirty the Draft');
}

async function testSharedCollectionDragCommitsOnceAndCancellationDoesNotMutate() {
  for (const fixture of collectionDragFixtures.cases) {
    const intent = runtimeTestIntent();
    intent[fixture.collection] = JSON.parse(JSON.stringify(fixture.objects));
    const S = {
      uiSpec,
      api: {
        async config() { return { intent, revision: 'revision-drag' }; },
        async overview() { return {}; },
        async runtime() { return {}; }
      }
    };
    new Function('window', fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/store.js'), 'utf8'))({ S });
    await S.store.init();
    const beforeEpoch = S.store.draftEpoch;
    const moved = fixture.cancel ? false : S.store.moveCollectionItemTo(
      fixture.collection, fixture.source_id, fixture.target_id, fixture.after, fixture.visible_ids
    );
    assert.equal(moved, fixture.expected_mutations === 1, fixture.name);
    assert.deepEqual(S.store.intent[fixture.collection].map((item) => item.id), fixture.expected_ids, fixture.name);
    assert.equal(S.store.draftEpoch - beforeEpoch, fixture.expected_mutations,
      `${fixture.name} must commit at most one Draft mutation`);
  }
}

async function testCollectionDragInteractionCommitsOnDropOnly() {
  const environment = createEnvironment(async () => ({ ok: true }), runtimeTestIntent(), { immediateTimers: true });
  const items = [ { id: 'node_a', name: 'Node A' }, { id: 'node_b', name: 'Node B' } ];
  const moveCalls = [];
  let rerenders = 0;
  environment.S.store.moveCollectionItemTo = (...args) => { moveCalls.push(args); return true; };
  const rerender = () => { rerenders++; };
  const makeRow = (item) => {
    const row = environment.S.h('tr', environment.S.ui.collectionRowAttributes('nodes', item, items, rerender), [
      environment.S.h('td', {}, environment.S.ui.collectionDragHandle('nodes', item, items, rerender))
    ]);
    row.rectTops = [];
    row.getBoundingClientRect = () => {
      const top = environment.view.children.indexOf(row) * 40;
      row.rectTops.push(top);
      return { top, left: 0, width: 600, height: 40, bottom: top + 40, right: 600 };
    };
    return row;
  };
  const rowA = makeRow(items[0]);
  const rowB = makeRow(items[1]);
  environment.view.append(rowA, rowB);
  rowA.classList.add('is-selected');
  rowA.setAttribute('aria-selected', 'true');
  const handleB = rowB.querySelector('.collection-drag-handle');
  const preventDefault = () => {};
  const stopPropagation = () => {};
  environment.document.elementFromPoint = () => rowA;

  handleB.listeners.pointerdown({
    button: 0, pointerType: 'mouse', pointerId: 1, currentTarget: handleB,
    clientX: 10, clientY: 50, preventDefault, stopPropagation
  });
  assert.ok(!classSet(rowA).has('is-selected') && classSet(rowB).has('is-selected') &&
    rowA.getAttribute('aria-selected') === 'false' && rowB.getAttribute('aria-selected') === 'true',
    'starting a drag synchronizes one selected row without waiting for a rerender');
  assert.ok(classSet(rowB).has('is-placeholder'), 'desktop drag keeps a full-row placeholder');
  await environment.dispatchWindow('pointermove', {
    pointerType: 'mouse', pointerId: 1, currentTarget: handleB, clientX: 20, clientY: 5, preventDefault
  });
  assert.ok(classSet(rowA).has('is-drop-before'), 'desktop drag exposes the target insertion edge');
  assert.deepEqual(environment.view.children.slice(-2), [ rowB, rowA ],
    'every Linux collection previews the target order before pointer release');
  const floatingPreview = environment.body.querySelector('.collection-drag-preview');
  const previewTopAfterReorder = floatingPreview.style.top;
  environment.document.elementFromPoint = () => rowB;
  await environment.dispatchWindow('pointermove', {
    pointerType: 'mouse', pointerId: 1, clientX: 24, clientY: 15, preventDefault
  });
  assert.notEqual(floatingPreview.style.top, previewTopAfterReorder,
    'window-level pointer tracking continues after moving the captured handle row in the DOM');
  assert.ok(classSet(rowA).has('is-drop-before'),
    'hovering the source placeholder after live reordering preserves the last valid drop target');
  assert.equal(moveCalls.length, 0, 'live whole-row preview does not mutate the Draft');
  assert.ok(rowA.rectTops.includes(0) && rowA.rectTops.includes(40) &&
    rowB.rectTops.includes(40) && rowB.rectTops.includes(0),
    'FLIP measures every affected row before and after live reordering');
  assert.ok(rowA.animations?.some((animation) => animation.options.duration === 160) &&
    rowB.animations?.some((animation) => animation.options.duration === 160),
    'the dragged row and every displaced peer share the same FLIP animation');
  await environment.dispatchWindow('pointerup', {
    pointerType: 'mouse', pointerId: 1, currentTarget: handleB, preventDefault, stopPropagation
  });
  assert.deepEqual(moveCalls, [ [ 'nodes', 'node_b', 'node_a', false, [ 'node_a', 'node_b' ] ] ]);
  assert.equal(rerenders, 1, 'desktop drop rerenders exactly once after one Draft mutation');

  handleB.listeners.pointerdown({
    button: 0, pointerType: 'mouse', pointerId: 2, currentTarget: handleB,
    clientX: 10, clientY: 50, preventDefault, stopPropagation
  });
  await environment.dispatchDocument('keydown', { key: 'Escape', preventDefault });
  assert.equal(moveCalls.length, 1, 'desktop Escape cancellation does not mutate the Draft');

  environment.document.elementFromPoint = () => rowA;
  handleB.listeners.pointerdown({
    button: 0, pointerType: 'touch', pointerId: 7, currentTarget: handleB,
    clientX: 10, clientY: 50, preventDefault, stopPropagation
  });
  await environment.dispatchWindow('pointermove', {
    pointerType: 'touch', pointerId: 7, currentTarget: handleB, clientX: 20, clientY: 75, preventDefault
  });
  assert.deepEqual(environment.view.children.slice(-2), [ rowA, rowB ], 'touch drag uses the same live row preview');
  await environment.dispatchWindow('pointercancel', { pointerId: 7, currentTarget: handleB });
  assert.equal(moveCalls.length, 1, 'touch cancellation restores the row without a Draft mutation');
  assert.deepEqual(environment.view.children.slice(-2), [ rowB, rowA ], 'touch cancellation restores the original DOM order');

  handleB.listeners.pointerdown({
    button: 0, pointerType: 'touch', pointerId: 8, currentTarget: handleB,
    clientX: 10, clientY: 50, preventDefault, stopPropagation
  });
  await environment.dispatchWindow('pointermove', {
    pointerType: 'touch', pointerId: 8, currentTarget: handleB, clientX: 20, clientY: 45, preventDefault
  });
  await environment.dispatchWindow('pointerup', {
    pointerType: 'touch', pointerId: 8, currentTarget: handleB, preventDefault, stopPropagation
  });
  assert.equal(moveCalls.length, 2, 'touch release commits exactly one shared stable-ID move');
  assert.equal(rerenders, 2);
}

function testNodeDisplaySortingHeadersNeverMutateTheDraft() {
  const intent = runtimeTestIntent();
  intent.nodes = JSON.parse(JSON.stringify(nodeDisplaySortingFixtures.nodes));
  intent.subscriptions = [ { id: 'feed', name: 'Feed', enabled: true, url: 'https://feed.example/sub', update_interval: '24h' } ];
  const originalIDs = intent.nodes.map((node) => node.id);
  const environment = createEnvironment(async () => ({ ok: true }), intent, {
    probeResults: { latest_results: nodeDisplaySortingFixtures.latest_results, warnings: [] }
  });
  loadView(environment, 'nodes');
  const view = environment.S.views.nodes;

  for (const fixture of nodeDisplaySortingFixtures.cases) {
    const group = intent.nodes.filter((node) => (node.source_subscription || '') === fixture.group);
    assert.deepEqual(
      view.sortNodesForDisplay(group, fixture.mode, fixture.direction, environment.S.store.probeResults).map((node) => node.id),
      fixture.expected_ids,
      fixture.name
    );
  }

  view.render(environment.view);
  const button = (label) => find(environment.view, (element) =>
    element.tag === 'button' && classSet(element).has('table-sort') && text(element).includes(label));
  const renderedIDs = () => findAll(environment.view, (element) =>
    element.tag === 'tr' && !!element.dataset.collectionId).map((element) => element.dataset.collectionId);

  button('连接测速').listeners.click();
  assert.deepEqual(renderedIDs(), [ 'fast', 'tie', 'slow', 'stale', 'failed', 'missing', 'invalid' ],
    'first Connection header click defaults to good-to-bad latency');
  assert.ok(text(button('连接测速')).includes('↑') && !/[好坏原始]/.test(text(button('连接测速'))),
    'Linux uses only the active header direction arrow');
  assert.equal(environment.view.querySelector('.collection-drag-handle').disabled, true,
    'manual ordering is disabled while a display-only sort is active');

  button('连接测速').listeners.click();
  assert.deepEqual(renderedIDs(), [ 'slow', 'fast', 'tie', 'stale', 'failed', 'missing', 'invalid' ],
    'second Connection header click reverses ranked metrics only');
  assert.ok(text(button('连接测速')).includes('↓') && !/[好坏原始]/.test(text(button('连接测速'))));

  button('连接测速').listeners.click();
  assert.deepEqual(renderedIDs(), [ 'slow', 'stale', 'fast', 'failed', 'tie', 'missing', 'invalid' ],
    'third Connection header click restores configured order');
  assert.equal(button('连接测速').attributes['aria-pressed'], 'false');

  button('下载测速').listeners.click();
  assert.deepEqual(renderedIDs(), [ 'slow', 'fast', 'tie', 'stale', 'failed', 'missing', 'invalid' ],
    'switching to Download resets its direction to good-to-bad throughput');
  button('下载测速').listeners.click();
  button('下载测速').listeners.click();
  assert.deepEqual(renderedIDs(), [ 'slow', 'stale', 'fast', 'failed', 'tie', 'missing', 'invalid' ]);
  assert.equal(button('顺序'), null, 'configured order is not exposed as a redundant sortable header');
  assert.deepEqual(intent.nodes.map((node) => node.id), originalIDs, 'header sorting never reorders Intent nodes');
  assert.equal(environment.touchCount, 0, 'header sorting never marks the Draft dirty');
}

function runtimeTestIntent(enabled = true) {
  return {
    main: { enabled }, nodes: [], subscriptions: [], routes: [], dns_profiles: [], local_proxies: [], rules: []
  };
}

async function testActualGenerationAndPersistentApplyFixture() {
  const overview = JSON.parse(fs.readFileSync(path.join(root, 'tests/fixtures/linux-web/overview-activate-failed.json'), 'utf8'));
  let applyCalls = 0;
  const environment = createEnvironment(async () => ({ ok: true, res: {} }), runtimeTestIntent(), {
    overview,
    pendingApply: true,
    applySaved: async () => { applyCalls++; return { ok: false, error: 'still failed' }; },
    api: {
      validate: async () => ({ ok: true, errors: [], warnings: [] }),
      probe: async () => ({}),
      logs: async () => ({ output: '' }),
      diagnostics: async () => probeDiagnosticsFixtures.diagnostics,
      probeResults: async () => probeDiagnosticsFixtures.probe_results
    }
  });
  environment.S.ui.renderStatusStrip();
  const stripText = text(environment.strip);
  assert.doesNotMatch(stripText, /candidate\.active-old|candidate\.failed-new/, 'strip must hide internal generation identifiers');
  assert.match(stripText, /运行状态正常运行/);
  assert.match(stripText, /待应用/);

  const applyButton = find(environment.strip, (element) => element.tag === 'button' && text(element) === '应用已保存配置');
  assert.ok(applyButton, 'global strip must expose Apply Saved on every view');
  assert.ok(!Object.hasOwn(applyButton.attributes, 'disabled'), 'clean pending state must keep Apply Saved enabled');
  await applyButton.listeners.click();
  assert.strictEqual(applyCalls, 1);

  const recordText = text(environment.S.ui.applyRecord(overview.status));
  assert.match(recordText, /新配置未启用，当前运行配置保持不变/);
  assert.doesNotMatch(recordText, /candidate\.failed-new|candidate\.active-old/);
  assert.doesNotMatch(recordText, /systemd refused start/, 'Overview Apply summary must not expose the raw backend error chain');
  assert.match(recordText, /已保存配置仍可重试应用/);
  assert.match(recordText, /2026/, 'persistent Apply record must include its timestamp');

  const overviewSource = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/views/overview.js'), 'utf8');
  new Function('window', overviewSource)({ S: environment.S });
  const overviewRoot = new Element('main');
  await environment.S.views.overview.render(overviewRoot);
  assert.match(text(overviewRoot), /当前运行正常/);
  assert.doesNotMatch(text(overviewRoot), /candidate\.active-old|candidate\.failed-new/);

  const diagnosticsSource = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/js/views/diagnostics.js'), 'utf8');
  new Function('window', diagnosticsSource)({ S: environment.S });
  const diagnosticsRoot = new Element('main');
  await environment.S.views.diagnostics.render(diagnosticsRoot);
  assert.match(text(diagnosticsRoot), /最近应用结果/);
  assert.match(text(diagnosticsRoot), /systemd refused start/);

  const disabled = createEnvironment(async () => ({ ok: true, res: {} }), runtimeTestIntent(false), {
    overview: { status: { healthy: false, last_apply: { sequence: '1', result: { ok: true, generation: '/run/steer/generations/candidate.last-apply' } } } }
  });
  disabled.S.ui.renderStatusStrip();
  assert.doesNotMatch(text(disabled.strip), /candidate\.last-apply/, 'disabled status must not invent Active from last Apply');
}

function draftLifecycleIntent() {
  return {
    main: {
      id: 'main', schema_version: 9, enabled: true, log_level: 'warn',
      probe_direct: 'https://direct.example/', probe_proxy: 'https://proxy.example/', speedtest_proxy: 'https://speed.example/'
    },
    bootstrap: { id: 'bootstrap', protocol: 'udp', server: '1.1.1.1', server_port: 53, strategy: 'prefer_ipv4' },
    nodes: [
      { id: 'node', enabled: true, name: 'Node', type: 'socks', server: 'node.example', server_port: 1080 },
      { id: 'feed_stale', enabled: true, name: 'Stale', type: 'socks', server: 'stale.example', server_port: 1080, source_subscription: 'feed', pinned_stale: true }
    ],
    subscriptions: [{ id: 'feed', enabled: true, name: 'Feed', url: 'https://feed.example/sub', update_interval: '6h' }],
    routes: [
      { id: 'direct', enabled: true, kind: 'direct' },
      { id: 'proxy', enabled: true, name: 'Proxy', kind: 'single', node: 'node' },
      { id: 'block', enabled: true, kind: 'block' }
    ],
    dns_profiles: [{ id: 'public', enabled: true, name: 'Public', protocol: 'udp', server: '1.1.1.1', server_port: 53 }],
    local_proxies: [],
    rules: [{ id: 'default', enabled: true, default: true, dns_profile: 'public', route: 'direct' }]
  };
}

async function testSharedProbeDiagnosticsAndDisabledActions() {
  const intent = draftLifecycleIntent();
  intent.main.dns_cache_capacity = 0;
  const disabledNode = probeDiagnosticsFixtures.objects.nodes.find((item) => item.id === 'node_disabled');
  const disabledRoute = probeDiagnosticsFixtures.objects.routes.find((item) => item.id === 'route_disabled');
  intent.nodes.push({ ...disabledNode, name: 'Disabled node', type: 'socks', server: 'disabled.example', server_port: 1080 });
  intent.routes.push({ ...disabledRoute, name: 'Disabled route', node: 'node_disabled' });
  const persistedDiagnostics = JSON.parse(JSON.stringify(probeDiagnosticsFixtures.diagnostics));
  const persistedProbeResults = JSON.parse(JSON.stringify(probeDiagnosticsFixtures.probe_results));
  persistedProbeResults.latest_results[1].object_id = 'node';
  persistedProbeResults.latest_results[2].object_id = 'proxy';
  let nodeCalls = 0, routeCalls = 0;
  const environment = createEnvironment(async () => ({ ok: true }), intent, { diagnostics: persistedDiagnostics, probeResults: persistedProbeResults, api: {
    validate: async () => ({ ok: true, errors: [], warnings: [] }),
    logs: async () => ({ output: 'steer-web probe log' }),
    diagnostics: async () => persistedDiagnostics,
    probeResults: async () => persistedProbeResults,
    probe: async () => persistedProbeResults.latest_results[0],
    speedtestNode: async () => { nodeCalls++; return persistedProbeResults.latest_results[1]; },
    speedtestRoute: async () => { routeCalls++; return persistedProbeResults.latest_results[2]; }
  } });
  environment.S.store.refreshOverview = async () => ({ ok: true });

  loadView(environment, 'general');
  const generalRoot = new Element('main');
  environment.S.views.general.render(generalRoot);
  const capacity = find(generalRoot, (element) => element.tag === 'input' && element.placeholder === '4096');
  assert.ok(capacity, 'Linux General must render the DNS cache capacity field');
  assert.strictEqual(capacity.value, '', 'Linux General must present the default DNS cache capacity as an empty field');
  capacity.listeners.input({ target: { value: '0' } });
  assert.ok(!Object.hasOwn(intent.main, 'dns_cache_capacity'), 'Linux General must normalize an entered zero to the empty default');

  loadView(environment, 'diagnostics');
  const diagnosticsRoot = new Element('main');
  await environment.S.views.diagnostics.render(diagnosticsRoot);
  const renderedDiagnostics = text(diagnosticsRoot);
  for (const expected of ['成功仅表示该地址在测试时可达', '上次', '21 ms', 'steer-web probe log', '最近应用结果', '系统 DNS 接管检查', '系统 DNS 接管已配置']) {
    assert.match(renderedDiagnostics, new RegExp(expected), `Linux Diagnostics must render ${expected}`);
  }
  for (const forbidden of ['probe.example', '尝试次数', 'Connect 7', 'TLS 9', '最近连通性报告']) {
    assert.doesNotMatch(renderedDiagnostics, new RegExp(forbidden), `Linux Diagnostics must hide ${forbidden}`);
  }
  assert.doesNotMatch(renderedDiagnostics, /验证 Direct 路径|验证当前代理路径/);
  assert.strictEqual(findAll(diagnosticsRoot, (element) => element.tag === 'button' && text(element) === '运行测试').length, 3,
    'Diagnostics exclusively owns the three Overview probes');

  loadView(environment, 'overview');
  const overviewRoot = new Element('main');
  await environment.S.views.overview.render(overviewRoot);
  assert.strictEqual(findAll(overviewRoot, (element) => element.tag === 'button' && text(element) === '运行测试').length, 0,
    'Overview must not duplicate Diagnostics probes');
  assert.match(text(overviewRoot), /执行模型、配置生命周期与当前工作副本概览/);
  assert.deepEqual(findAll(overviewRoot, (element) => element.attributes?.['data-overview-region'])
    .map((element) => element.attributes['data-overview-region']),
  ['execution_model', 'configuration_lifecycle', 'object_scale', 'validation_summary', 'last_apply_and_actions']);
  for (const expected of ['节点', '路由', 'DNS Profile', '本地入口', '规则', '订阅', '打开诊断', '系统信息'])
    assert.match(text(overviewRoot), new RegExp(expected), `Linux Overview must render ${expected}`);
  assert.doesNotMatch(text(overviewRoot), /systemd refused start|candidate\.failed-new|candidate\.active-old/,
    'Linux Overview keeps raw errors and internal runtime identities out of ordinary DOM');

  loadView(environment, 'dns');
  const dnsRoot = new Element('main');
  environment.S.views.dns.render(dnsRoot);
  for (const removed of ['Bootstrap 与应用自带加密 DNS', 'infrastructure hostnames', 'Port-53 capture alone']) {
    assert.doesNotMatch(text(dnsRoot), new RegExp(removed), `Linux DNS must not render ${removed}`);
  }

  loadView(environment, 'nodes');
  const nodesRoot = new Element('main');
  environment.S.views.nodes.render(nodesRoot);
  const disabledNodeRow = findAll(nodesRoot, (element) => element.tag === 'tr' && text(element).includes('Disabled node'))[0];
  const disabledNodeTests = findAll(disabledNodeRow, (element) => element.tag === 'button' && ['连接', '下载'].includes(text(element)));
  assert.strictEqual(disabledNodeTests.length, 2);
  assert.ok(disabledNodeTests.every((button) => Object.hasOwn(button.attributes, 'disabled') && button.attributes.title.includes('已停用')));
  await disabledNodeTests[0].listeners.click();
  assert.strictEqual(nodeCalls, 0, 'disabled Linux Node test must not reach the backend');
  const enabledNodeRow = findAll(nodesRoot, (element) => element.tag === 'tr' && text(element).includes('Node'))
    .find((row) => !text(row).includes('Disabled node'));
  assert.match(text(enabledNodeRow), /上次.*成功.*16\.0 Mbps/,
    'Linux Node restores the persisted latest download result beside its action');
  for (const forbidden of probeDiagnosticsFixtures.ordinary_ui.forbidden_fragments)
    assert.ok(!text(enabledNodeRow).includes(forbidden), `Linux Node latest result hides ${forbidden}`);

  loadView(environment, 'routes');
  const latestRoutesRoot = new Element('main');
  environment.S.views.routes.render(latestRoutesRoot);
  const enabledRouteRow = findAll(latestRoutesRoot, (element) => element.tag === 'tr' && text(element).includes('Proxy'))[0];
  assert.match(text(enabledRouteRow), /上次.*已过期.*失败.*连接超时/,
    'Linux Route restores and expires the persisted latest connection result beside its action');
  for (const forbidden of probeDiagnosticsFixtures.ordinary_ui.forbidden_fragments)
    assert.ok(!text(enabledRouteRow).includes(forbidden), `Linux Route latest result hides ${forbidden}`);

  loadView(environment, 'routes');
  const routesRoot = new Element('main');
  environment.S.views.routes.render(routesRoot);
  const disabledRouteRow = findAll(routesRoot, (element) => element.tag === 'tr' && text(element).includes('Disabled route'))[0];
  const disabledRouteTests = findAll(disabledRouteRow, (element) => element.tag === 'button' && ['链测试', '下载'].includes(text(element)));
  assert.strictEqual(disabledRouteTests.length, 2);
  assert.ok(disabledRouteTests.every((button) => Object.hasOwn(button.attributes, 'disabled') && button.attributes.title.includes('已停用')));
  await disabledRouteTests[0].listeners.click();
  assert.strictEqual(routeCalls, 0, 'disabled Linux Route test must not reach the backend');
}

async function testOverviewUsesBoundedBackendWarningGroups() {
  const intent = draftLifecycleIntent();
  const rawWarnings = Array.from({ length: 120 }, (_, index) => ({
    code: 'INSECURE_TLS', object_type: 'node', object_id: `private-node-${index}`,
    option: 'insecure', message: `raw secret warning ${index}`
  }));
  const validation = {
    ok: true, errors: [], warnings: rawWarnings,
    warning_groups: [{
      code: 'INSECURE_TLS', object_type: 'node', option: 'insecure', count: 120,
      summary: 'TLS certificate verification is disabled', destination: 'nodes'
    }]
  };
  const environment = createEnvironment(async () => ({ ok: true }), intent, { api: {
    validate: async () => validation
  } });
  let destination = '';
  environment.S.router = (page) => { destination = page; };
  loadView(environment, 'overview');
  const overviewRoot = new Element('main');
  await environment.S.views.overview.render(overviewRoot);
  const rendered = text(overviewRoot);
  assert.match(rendered, /TLS 证书校验已关闭/);
  assert.match(rendered, /120 个正在使用的节点/);
  assert.doesNotMatch(rendered, /private-node-|raw secret warning/,
    'Overview must not render raw Warning object IDs or messages');
  assert.strictEqual(findAll(overviewRoot, (element) => classSet(element).has('warning-group')).length, 1,
    'a large warning set must render one backend group');
  buttonWithText(overviewRoot, '查看节点').listeners.click();
  assert.strictEqual(destination, 'nodes');
}

async function testExternalRevisionRefreshPreservesDraftAndLifecycleFacts() {
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const server = { intent: draftLifecycleIntent(), revision: 'saved-initial' };
  let overview = {
    saved_revision: server.revision, saved_enabled: true, pending_apply: false,
    status: { healthy: true, generation: 'generation-initial', intent_digest: 'active-initial' }
  };
  let runtime = { steer: 'test-initial' };
  const api = {
    config: async () => ({ intent: clone(server.intent), revision: server.revision }),
    overview: async () => clone(overview),
    runtime: async () => clone(runtime)
  };
  const environment = createEnvironment(async () => ({ ok: true }), clone(server.intent));
  attachRealStore(environment, api);
  await environment.S.store.init();
  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  const localDraft = JSON.stringify(environment.S.store.intent);

  server.intent.main.log_level = 'error';
  server.revision = 'saved-external';
  overview = { ...overview, saved_revision: server.revision };
  const refreshed = await environment.S.store.refreshServerState();
  assert.equal(refreshed.changed, true);
  assert.equal(environment.S.store.externalRevision, 'saved-external');
  assert.equal(JSON.stringify(environment.S.store.intent), localDraft,
    'low-frequency refresh must never overwrite a dirty Draft after an external revision change');
  assert.equal(environment.S.store.dirty, true);
  environment.S.ui.renderStatusStrip();
  assert.match(text(environment.strip), /服务器配置已变化/);
  assert.match(text(environment.strip), /处理配置冲突/);

  const reloaded = await environment.S.store.reload();
  assert.equal(reloaded.ok, true);
  assert.equal(environment.S.store.revision, 'saved-external');
  assert.equal(environment.S.store.intent.main.log_level, 'error');
  assert.equal(environment.S.store.hasExternalChange, false);

  overview = { ...overview, status: { healthy: false, generation: 'generation-cli-apply', intent_digest: 'active-cli' } };
  runtime = { steer: 'test-updated' };
  const runtimeRefresh = await environment.S.store.refreshServerState();
  assert.equal(runtimeRefresh.changed, false);
  assert.equal(environment.S.store.overview.status.generation, 'generation-cli-apply',
    'CLI Apply or service status changes must refresh without replacing the Draft');
  assert.equal(environment.S.store.runtime.steer, 'test-updated',
    'low-frequency refresh must update runtime facts as well as lifecycle status');

  server.intent.main.log_level = 'warn';
  server.revision = 'saved-external-clean';
  overview = { ...overview, saved_revision: server.revision };
  await environment.S.store.refreshServerState();
  let renderedCurrent = 0;
  environment.S.renderCurrent = () => { renderedCurrent++; };
  environment.S.ui.renderStatusStrip();
  const reloadLatest = find(environment.strip, (element) => element.tag === 'button' && text(element) === '重新载入');
  assert.ok(reloadLatest, 'clean external revision exposes one-click reload');
  reloadLatest.listeners.click();
  await flushUI();
  assert.equal(environment.S.store.revision, 'saved-external-clean');
  assert.equal(environment.S.store.intent.main.log_level, 'warn');
  assert.equal(renderedCurrent, 1, 'one-click reload redraws the current object page with one coherent snapshot');

  for (const fixture of stateLifecycleFixtures.cases) {
    const intent = draftLifecycleIntent();
    intent.main.enabled = fixture.draft.enabled;
    const status = {
      healthy: fixture.active.healthy,
      generation: fixture.active.generation || '',
      intent_digest: fixture.active.digest || '',
      last_apply: fixture.active.last_apply_ok == null ? null : {
        sequence: '1', result: { ok: fixture.active.last_apply_ok, generation: fixture.active.generation || '', error: fixture.active.last_apply_ok ? '' : 'activation failed' }
      }
    };
    const rendered = createEnvironment(async () => ({ ok: true }), intent, {
      overview: { saved_enabled: fixture.saved.enabled, pending_apply: fixture.pending_apply, status },
      pendingApply: fixture.pending_apply
    });
    rendered.S.store.dirty = fixture.draft.dirty;
    rendered.S.ui.renderStatusStrip();
    const strip = text(rendered.strip);
    assert.match(strip, /配置开关/);
    assert.match(strip, /运行状态/);
    if (fixture.name === 'pending-disable') {
      assert.match(strip, /配置开关启用/);
      assert.match(strip, /正常运行/,
        'pending disable must keep the service state visible');
    }
    if (fixture.name === 'failed-apply') assert.match(strip, /待应用/);
  }

}

async function testSharedUISafetyContracts() {
	assert.equal(formInputFixtures.schema_version, 1);
	assert.ok(formInputFixtures.cases.length > 0 &&
		formInputFixtures.cases.every((fixture) => uiSpec.input_formats[fixture.format]),
		'Linux consumes the shared high-frequency form format metadata and fixtures');
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const environment = createEnvironment(async () => ({ ok: true }), clone(collectionReferenceFixtures.intent));
  for (const fixture of collectionReferenceFixtures.cases) {
    const actual = environment.S.ui.collectionReferences(
      collectionReferenceFixtures.intent, fixture.target_collection, fixture.target_id
    ).map((reference) => ({
      source_collection: reference.source_collection,
      source_object_type: reference.source_object_type,
      source_id: reference.source_id,
      field: reference.field
    }));
    assert.deepEqual(actual, fixture.references,
      `Linux reference guard drifted for ${fixture.target_collection}/${fixture.target_id}`);
  }
  assert.equal(environment.S.ui.guardCollectionDeletion('nodes', 'node_used', 'Used node'), false);
  assert.match(text(environment.body), /Used route/);
  assert.doesNotMatch(text(environment.body), /Used route \/ node/);
  assert.equal(environment.S.ui.guardCollectionDeletion('nodes', 'node_free', 'Free node'), true);

  const summaryEnvironment = createEnvironment(async () => ({ ok: true }), draftLifecycleIntent());
  loadView(summaryEnvironment, 'rules');
  for (const fixture of ruleSummaryFixtures.cases) {
    assert.deepEqual(summaryEnvironment.S.views.rules.summaryTokens(fixture.rule), fixture.tokens, fixture.name);
    assert.equal(summaryEnvironment.S.views.rules.dnsContinues(fixture.rule), fixture.dns_continues, fixture.name);
  }

  const validation = validationIssueFixtures.validation;
  const renderedIssues = environment.S.ui.issueList([...validation.errors, ...validation.warnings], () => {});
  const issueText = text(renderedIssues);
  for (const issue of [...validation.errors, ...validation.warnings]) {
    assert.ok(!issueText.includes(issue.code) && !issueText.includes(issue.object_id),
      'validation UI must hide internal codes and object IDs');
  }
  for (const expected of ['必填字段尚未填写', '所选节点不存在', '所选路由不存在', '更新间隔必须是大于零的时长']) {
    assert.ok(issueText.includes(expected));
  }
  for (const secret of validationIssueFixtures.forbidden_message_values) assert.ok(!issueText.includes(secret));

  const failedWrite = createEnvironment(async () => {
    const error = new Error('candidate rejected');
    error.details = { validation };
    throw error;
  }, draftLifecycleIntent());
  await failedWrite.S.ui.onSave(true);
  const writePanel = text(failedWrite.body);
  assert.ok(writePanel.includes('所选路由不存在') && writePanel.includes('不影响 DNS 上游选择'),
    'Save/Apply failure must immediately render structured errors and warnings');
}

function createDraftBackend() {
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const state = {
    intent: draftLifecycleIntent(), revision: '"revision-1"', configCalls: 0, puts: [],
    nodeTests: 0, routeTests: 0, subscriptionUpdates: 0, subscriptionCleans: 0, applyCalls: 0
  };
  state.api = {
    async config() { state.configCalls++; return { intent: clone(state.intent), revision: state.revision }; },
    async overview() { return { pending_apply: true, status: { healthy: false } }; },
    async runtime() { return {}; },
    async diagnostics() { return probeDiagnosticsFixtures.diagnostics; },
    async probeResults() { return probeDiagnosticsFixtures.probe_results; },
    async geodata() { return { readable: true, names: [], count: 0 }; },
    async validate() { return { ok: true, errors: [], warnings: [] }; },
    async putConfig(intent, _revision, apply) {
      state.intent = clone(intent);
      state.puts.push({ intent: clone(intent), apply });
      state.revision = `"revision-${state.puts.length + 2}"`;
      return { saved: true, applied: !!apply, revision: state.revision, request_ok: true };
    },
    async applySaved() { state.applyCalls++; return { ok: true }; },
    async speedtestNode() { state.nodeTests++; return { scope: 'nodes', object_id: 'node', kind: 'connect', tested_at: '2026-08-26T01:00:00Z', ok: true, stale: false, summary: '21 ms', error_summary: '' }; },
    async speedtestRoute() { state.routeTests++; return { scope: 'routes', object_id: 'proxy', kind: 'connect', tested_at: '2026-08-26T01:00:00Z', ok: true, stale: false, summary: '21 ms', error_summary: '' }; },
    async subscriptions() {
      return { subscriptions: [{
        id: 'feed', enabled: true, name: 'Feed', url: 'https://feed.example/sub', update_interval: '6h',
        never_fetched: false, last_success: '2026-08-26T01:00:00Z', last_failure: null,
        node_count: 1, current: 0, added: 0, skipped: 0,
        stale: [{ id: 'feed_stale', name: 'Stale', referenced_by: [] }]
      }] };
    },
    async updateSubscription() {
      state.subscriptionUpdates++;
      return { subscriptions: [{
        id: 'feed', enabled: true, node_count: 1, current: 1, added: 1, skipped: 0, stale: []
      }] };
    },
    async cleanNode() { state.subscriptionCleans++; return { subscriptions: [] }; }
  };
  return state;
}

async function testJSONDraftStoreLifecycle() {
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  await environment.S.store.init();

  const changed = draftLifecycleIntent();
  changed.main.log_level = 'debug';
  const validText = JSON.stringify(changed, null, 4);
  let result = environment.S.store.editJSON(validText);
  assert.strictEqual(result.ok, true);
  assert.strictEqual(environment.S.store.dirty, true, 'Advanced input must immediately mark the shared Draft dirty');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'debug', 'valid JSON must update the same parsed Draft');
  assert.strictEqual(environment.S.store.draftText, validText, 'valid Advanced formatting must remain in the shared Draft');

  const lastValidIntent = environment.S.store.intent;
  const invalidText = '{"main":';
  result = environment.S.store.editJSON(invalidText);
  assert.strictEqual(result.ok, false);
  assert.strictEqual(environment.S.store.draftText, invalidText, 'invalid JSON text must be retained verbatim');
  assert.strictEqual(environment.S.store.intent, lastValidIntent, 'invalid JSON must not replace the last parseable Draft object');
  await assert.rejects(() => environment.S.store.save(false), /JSON 配置格式有误/);
  assert.strictEqual(backend.puts.length, 0, 'invalid JSON must not reach the Save API');

  backend.intent.main.log_level = 'error';
  backend.revision = '"revision-server"';
  await environment.S.store.reload();
  assert.strictEqual(environment.S.store.dirty, false);
  assert.strictEqual(environment.S.store.draftValid, true);
  assert.strictEqual(environment.S.store.revision, '"revision-server"');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'error');
  assert.match(environment.S.store.draftText, /"log_level": "error"/, 'reload must synchronize parsed Intent and Advanced text');

  environment.S.store.intent.main.log_level = 'info';
  environment.S.store.touch();
  assert.match(environment.S.store.draftText, /"log_level": "info"/, 'structured edits must synchronize back to Advanced JSON');
}

async function testStoreSnapshotsAndAsyncOrdering() {
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  await environment.S.store.init();

  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  const saveGate = deferred();
  let capturedSnapshot = null;
  let saveRequests = 0;
  backend.api.putConfig = async (snapshot, revision, apply) => {
    saveRequests++;
    capturedSnapshot = snapshot;
    assert.strictEqual(revision, '"revision-1"');
    assert.strictEqual(apply, false);
    await saveGate.promise;
    return { saved: true, applied: false, revision: '"revision-saved"' };
  };

  const saving = environment.S.store.save(false);
  await Promise.resolve();
  assert.strictEqual(environment.S.store.saving, true);
  assert.strictEqual(capturedSnapshot.main.log_level, 'debug');
  const duplicate = await environment.S.store.save(false);
  assert.strictEqual(duplicate.busy, true, 'double Save must not start a second request');
  assert.strictEqual(saveRequests, 1);
  const configCallsBeforeBusyReload = backend.configCalls;
  const busyReload = await environment.S.store.reload();
  assert.strictEqual(busyReload.busy, true, 'Discard/reload must not race an in-flight Save');
  assert.strictEqual(backend.configCalls, configCallsBeforeBusyReload, 'busy reload must not read a stale server revision');

  environment.S.store.intent.main.log_level = 'info';
  environment.S.store.touch();
  assert.strictEqual(capturedSnapshot.main.log_level, 'debug', 'Save request must hold an immutable Draft snapshot');
  saveGate.resolve();
  const saved = await saving;
  assert.strictEqual(saved.ok, true);
  assert.strictEqual(saved.staleDraft, true, 'Save response must detect edits made while it was in flight');
  assert.strictEqual(environment.S.store.dirty, true, 'old Save response must not clean newer Draft edits');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'info');
  assert.match(environment.S.store.draftText, /"log_level": "info"/, 'old Save response must not replace newer Draft text');
  assert.strictEqual(environment.S.store.revision, '"revision-saved"', 'revision may advance only for the saved snapshot');

  const reloadGate = deferred();
  const serverIntent = draftLifecycleIntent();
  serverIntent.main.log_level = 'warn';
  backend.api.config = async () => reloadGate.promise;
  const reloading = environment.S.store.reload();
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'error';
  environment.S.store.touch();
  reloadGate.resolve({ intent: serverIntent, revision: '"revision-reloaded"' });
  const reloadResult = await reloading;
  assert.strictEqual(reloadResult.staleDraft, true, 'reload must detect edits made after it started');
  assert.strictEqual(environment.S.store.dirty, true);
  assert.strictEqual(environment.S.store.intent.main.log_level, 'error', 'stale reload response must preserve newer Draft Intent');
  assert.strictEqual(environment.S.store.revision, '"revision-saved"', 'stale reload response must not replace the Draft base revision');

  const applyGate = deferred();
  let applyRequests = 0;
  backend.api.applySaved = async () => { applyRequests++; await applyGate.promise; return { ok: true }; };
  const applying = environment.S.store.applySaved();
  await Promise.resolve();
  assert.strictEqual(environment.S.store.applying, true);
  assert.strictEqual((await environment.S.store.save(false)).busy, true, 'Save must not overlap Apply Saved');
  assert.strictEqual((await environment.S.store.reload()).busy, true, 'reload must not overlap Apply Saved');
  assert.strictEqual((await environment.S.store.applySaved()).busy, true, 'double Apply Saved must not start twice');
  assert.strictEqual(applyRequests, 1);
  applyGate.resolve();
  assert.strictEqual((await applying).ok, true);
}

async function testSaveAndApplySurviveOverviewRefreshFailure() {
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  await environment.S.store.init();

  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  let overviewGate = deferred();
  backend.api.putConfig = async () => ({ saved: true, applied: false, revision: '"revision-clean"' });
  backend.api.overview = async () => overviewGate.promise;
  let saving = environment.S.store.save(false);
  await flushUI();
  assert.strictEqual(environment.S.store.saving, true, 'Save may still be refreshing overview after PUT succeeds');
  assert.strictEqual(environment.S.store.revision, '"revision-clean"', 'successful PUT must adopt its revision before overview refresh');
  assert.strictEqual(environment.S.store.dirty, false, 'a matching Draft must become clean before overview refresh');
  overviewGate.reject(new Error('overview unavailable'));
  let result = await saving;
  assert.strictEqual(result.ok, true, 'overview refresh failure must not turn a successful Save into failure');
  assert.match(result.overviewError.message, /overview unavailable/);
  assert.strictEqual(environment.S.store.revision, '"revision-clean"');

  environment.S.store.intent.main.log_level = 'info';
  environment.S.store.touch();
  const putGate = deferred();
  overviewGate = deferred();
  backend.api.putConfig = async () => {
    await putGate.promise;
    return { saved: true, applied: false, revision: '"revision-stale"' };
  };
  backend.api.overview = async () => overviewGate.promise;
  saving = environment.S.store.save(false);
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'warn';
  environment.S.store.touch();
  putGate.resolve();
  await flushUI();
  assert.strictEqual(environment.S.store.revision, '"revision-stale"', 'stale Draft Save must still adopt the saved snapshot revision');
  assert.strictEqual(environment.S.store.dirty, true, 'newer Draft edits must stay dirty after the older snapshot is saved');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'warn');
  overviewGate.reject(new Error('overview unavailable again'));
  result = await saving;
  assert.strictEqual(result.ok, true);
  assert.strictEqual(result.staleDraft, true);
  assert.match(result.overviewError.message, /overview unavailable again/);

  backend.api.applySaved = async () => ({ ok: true, generation: 'candidate-new' });
  backend.api.overview = async () => { throw new Error('apply overview unavailable'); };
  result = await environment.S.store.applySaved();
  assert.strictEqual(result.ok, true, 'overview refresh failure must not turn a successful Apply into failure');
  assert.strictEqual(result.generation, 'candidate-new');
  assert.match(result.overviewError.message, /apply overview unavailable/);
}

async function testEnableConflictUsesCurrentDraftObject() {
  for (const dirty of [false, true]) {
    const backend = createDraftBackend();
    const environment = createEnvironment(async () => ({ ok: true }));
    attachRealStore(environment, backend.api);
    await environment.S.store.init();
    const originalRevision = environment.S.store.revision;
    const local = draftLifecycleIntent();
    local.main.log_level = 'debug';
    if (dirty) environment.S.store.editJSON(JSON.stringify(local));
    // A subscription update happened after this browser loaded its draft.
    backend.intent.nodes.push({ id: 'fresh-feed-node', enabled: true });
    const gate = deferred();
    backend.api.setEnabled = async (enabled) => {
      await gate.promise;
      backend.intent.main.enabled = enabled;
      return { saved: true, applied: true, intent: backend.intent, revision: '"new-revision"' };
    };
    const toggling = environment.S.store.setEnabled(false);
    assert.strictEqual((await environment.S.store.setEnabled(true)).busy, true);
    gate.resolve();
    await toggling;
    if (dirty) {
      assert.strictEqual(environment.S.store.intent.main.log_level, 'debug');
      assert.strictEqual(environment.S.store.intent.main.enabled, true);
      assert.strictEqual(environment.S.store.revision, originalRevision);
      assert.strictEqual(environment.S.store.dirty, true);
    } else {
      assert.strictEqual(environment.S.store.intent.main.enabled, false);
      assert.ok(environment.S.store.intent.nodes.some((n) => n.id === 'fresh-feed-node'));
      assert.strictEqual(environment.S.store.revision, '"new-revision"');
      assert.strictEqual(environment.S.store.dirty, false);
    }
    assert.strictEqual(backend.puts.length, 0);
  }
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  await environment.S.store.init();
  const gate = deferred();
  backend.api.setEnabled = async () => {
    await gate.promise;
    return { saved: true, applied: false, intent: backend.intent, revision: '"new-revision"' };
  };
  const toggling = environment.S.store.setEnabled(false);
  environment.S.store.editJSON('{invalid concurrent edit');
  gate.resolve();
  const result = await toggling;
  assert.strictEqual(result.res.applied, false);
  assert.strictEqual(environment.S.store.dirty, true);
  assert.strictEqual(environment.S.store.draftText, '{invalid concurrent edit');
  assert.strictEqual(environment.S.store.revision, '"revision-1"');
}

async function testAdvancedRouterDiscardAndGuardedActions() {
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  for (const name of ['config', 'general', 'nodes', 'routes', 'subscriptions']) loadView(environment, name);
  const application = loadApplication(environment, '#/advanced');
  await application.start();
  await flushUI();

  assert.strictEqual(environment.S.store.listenerCount, 2, 'status strip plus Advanced should own exactly two store listeners');
  for (let index = 0; index < 3; index++) {
    environment.S.router('general');
    await flushUI();
    assert.strictEqual(environment.S.store.listenerCount, 1, 'leaving Advanced must synchronously dispose its store listener');
    environment.S.router('advanced');
    await flushUI();
    assert.strictEqual(environment.S.store.listenerCount, 2, 'returning to Advanced must add only one current listener');
  }

  let editor = find(environment.view, (element) => element.tag === 'textarea' && classSet(element).has('editor-tall'));
  assert.ok(editor, 'Advanced view must render the shared Draft textarea');
  const invalidText = '{"main":';
  editor.value = invalidText;
  editor.listeners.input({ target: editor });
  assert.strictEqual(environment.S.store.dirty, true);
  assert.strictEqual(environment.S.store.draftValid, false);
  assert.match(text(environment.strip), /配置格式有误/);
  assert.ok(buttonWithText(environment.strip, '保存').disabled, 'global Save must be blocked for invalid JSON');
  assert.ok(buttonWithText(environment.view, '保存').disabled, 'Advanced Save must share the same invalid-Draft guard');

  assert.strictEqual(environment.S.router('general'), false, 'invalid JSON must block structured routing');
  assert.strictEqual(application.location.hash, '#/advanced');
  assert.strictEqual(environment.S.store.draftText, invalidText, 'blocked navigation must retain invalid text');
  application.location.hash = '#/routes';
  await flushUI();
  assert.strictEqual(application.location.hash, '#/advanced', 'browser hash navigation must return an invalid Draft to Advanced');
  editor = find(environment.view, (element) => element.tag === 'textarea' && classSet(element).has('editor-tall'));
  assert.strictEqual(editor.value, invalidText, 'hash-route protection must rerender the retained invalid text');
  const unload = { preventDefault() { this.prevented = true; } };
  await environment.dispatchWindow('beforeunload', unload);
  assert.strictEqual(unload.prevented, true, 'browser reload/close must protect the Advanced Draft');
  assert.strictEqual(unload.returnValue, '', 'beforeunload must use the shared dirty state');

  backend.intent.main.log_level = 'error';
  backend.revision = '"revision-2"';
  const callsBeforeDiscard = backend.configCalls;
  buttonWithText(environment.strip, '放弃修改').listeners.click();
  lastButtonWithText(environment.body, '取消').listeners.click();
  assert.strictEqual(backend.configCalls, callsBeforeDiscard, 'Discard Cancel must not reload or mutate state');
  assert.strictEqual(environment.S.store.draftText, invalidText);
  assert.strictEqual(environment.S.store.dirty, true);

  buttonWithText(environment.strip, '放弃修改').listeners.click();
  await lastButtonWithText(environment.body, '放弃修改并重新载入').listeners.click();
  await flushUI();
  assert.strictEqual(environment.S.store.dirty, false);
  assert.strictEqual(environment.S.store.draftValid, true);
  assert.strictEqual(environment.S.store.revision, '"revision-2"');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'error');
  assert.strictEqual(environment.S.store.pendingApply, true, 'Discard must not alter pending Apply semantics');
  assert.strictEqual(buttonWithText(environment.strip, '放弃修改'), null, 'Discard must disappear after the Draft becomes clean');
  assert.doesNotMatch(text(environment.strip), /revision-2/, 'status strip must hide the internal revision identifier');
  editor = find(environment.view, (element) => element.tag === 'textarea' && classSet(element).has('editor-tall'));
  assert.match(editor.value, /"log_level": "error"/, 'Discard must rerender the current Advanced page from Saved');

  const applyGate = deferred();
  let applyRequests = 0;
  backend.api.applySaved = async () => { applyRequests++; await applyGate.promise; return { ok: true }; };
  const applying = environment.S.store.applySaved();
  await Promise.resolve();
  const busyApplyButton = buttonWithText(environment.strip, '应用已保存配置');
  assert.ok(busyApplyButton.disabled, 'Apply Saved button must disable during any control operation');
  await busyApplyButton.listeners.click();
  assert.strictEqual(applyRequests, 1, 'busy guard must prevent a concurrent Apply Saved request');
  applyGate.resolve();
  await applying;

  environment.S.router('nodes');
  await flushUI();
  await buttonWithText(environment.view, '连接').listeners.click();
  environment.S.router('routes');
  await flushUI();
  await buttonWithText(environment.view, '链测试').listeners.click();
  environment.S.router('subscriptions');
  await flushUI();
  const resumedUpdate = buttonWithText(environment.view, '立即更新');
  assert.ok(resumedUpdate, `subscription view failed to render: ${text(environment.view)}`);
  await resumedUpdate.listeners.click();
  await flushUI();
  assert.strictEqual(backend.nodeTests, 1, 'node testing must resume immediately after Discard');
  assert.strictEqual(backend.routeTests, 1, 'route testing must resume immediately after Discard');
  assert.strictEqual(backend.subscriptionUpdates, 1, 'subscription update must resume immediately after Discard');

  environment.S.router('advanced');
  await flushUI();
  editor = find(environment.view, (element) => element.tag === 'textarea' && classSet(element).has('editor-tall'));
  let parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'debug';
  editor.value = JSON.stringify(parsed, null, 3);
  editor.listeners.input({ target: editor });
  await buttonWithText(environment.view, '保存').listeners.click();
  assert.strictEqual(backend.puts.at(-1).intent.main.log_level, 'debug', 'Advanced page Save must use the shared JSON Draft');
  assert.strictEqual(backend.puts.at(-1).apply, false);

  parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'info';
  editor.value = JSON.stringify(parsed, null, 2);
  editor.listeners.input({ target: editor });
  await buttonWithText(environment.strip, '保存').listeners.click();
  assert.strictEqual(backend.puts.at(-1).intent.main.log_level, 'info', 'global Save must use the same Advanced JSON Draft');

  parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'error';
  editor.value = JSON.stringify(parsed, null, 2);
  editor.listeners.input({ target: editor });
  await buttonWithText(environment.view, '保存并应用').listeners.click();
  assert.strictEqual(backend.puts.at(-1).intent.main.log_level, 'error', 'Advanced page Save and Apply must use the shared JSON Draft');
  assert.strictEqual(backend.puts.at(-1).apply, true);

  parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'debug';
  editor.value = JSON.stringify(parsed, null, 2);
  editor.listeners.input({ target: editor });
  await buttonWithText(environment.strip, '保存并应用').listeners.click();
  assert.strictEqual(backend.puts.at(-1).intent.main.log_level, 'debug', 'global Save and Apply must use the same Advanced JSON Draft');
  assert.strictEqual(backend.puts.at(-1).apply, true);

  parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'warn';
  const roundTripText = JSON.stringify(parsed, null, 4);
  editor.value = roundTripText;
  editor.listeners.input({ target: editor });
  assert.strictEqual(environment.S.router('general'), true);
  await flushUI();
  assert.strictEqual(environment.S.store.intent.main.log_level, 'warn');
  assert.strictEqual(environment.S.router('advanced'), true);
  await flushUI();
  editor = find(environment.view, (element) => element.tag === 'textarea' && classSet(element).has('editor-tall'));
  assert.strictEqual(editor.value, roundTripText, 'structured ↔ Advanced navigation must retain the one shared Draft text');

  const reloadGate = deferred();
  backend.api.config = async () => reloadGate.promise;
  buttonWithText(environment.strip, '放弃修改').listeners.click();
  const discardOverlay = findAll(environment.body, (element) => classSet(element).has('dialog-overlay')).at(-1);
  const discarding = lastButtonWithText(environment.body, '放弃修改并重新载入').listeners.click();
  await Promise.resolve();
  parsed = JSON.parse(editor.value);
  parsed.main.log_level = 'info';
  editor.value = JSON.stringify(parsed, null, 2);
  editor.listeners.input({ target: editor });
  reloadGate.resolve({ intent: backend.intent, revision: '"revision-too-old"' });
  await discarding;
  assert.strictEqual(environment.S.store.dirty, true, 'edit during Discard reload must remain dirty');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'info');
  assert.notStrictEqual(discardOverlay.removed, true, 'stale Discard response must not close as if the Draft was discarded');
  assert.match(text(environment.toasts), /重新载入期间工作副本又发生变化/);
}

async function testSubscriptionAsyncWorkPreservesNewDraftAndRoute() {
  const backend = createDraftBackend();
  const environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  for (const name of ['general', 'subscriptions']) loadView(environment, name);
  const application = loadApplication(environment, '#/subscriptions');
  await application.start();
  await flushUI();

  const updateGate = deferred();
  backend.api.updateSubscription = async () => {
    backend.subscriptionUpdates++;
    await updateGate.promise;
    return { subscriptions: [{ id: 'feed', node_count: 1, current: 1, added: 0, skipped: 0, stale: [] }] };
  };
  const configCallsBeforeUpdate = backend.configCalls;
  const updating = buttonWithText(environment.view, '立即更新').listeners.click();
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  environment.S.router('general');
  await flushUI();
  updateGate.resolve();
  await updating;
  await flushUI();
  assert.strictEqual(environment.S.store.dirty, true);
  assert.strictEqual(environment.S.store.intent.main.log_level, 'debug', 'subscription response must preserve edits made while it was in flight');
  assert.strictEqual(backend.configCalls, configCallsBeforeUpdate, 'changed Draft must prevent unconditional subscription reload');
  assert.strictEqual(application.location.hash, '#/general');
  assert.match(text(environment.view), /基础设置/, 'stale subscription render must not overwrite the newer route');
  assert.match(text(environment.toasts), /工作副本已变化/, 'inventory update must explain why the working copy was preserved');

  await environment.S.store.reload();
  environment.S.renderCurrent();
  await flushUI();
  environment.S.router('subscriptions');
  await flushUI();
  const cleanGate = deferred();
  backend.api.cleanNode = async () => {
    backend.subscriptionCleans++;
    await cleanGate.promise;
    return { snapshot: {} };
  };
  buttonWithText(environment.view, '清理失效节点 ×1').listeners.click();
  const configCallsBeforeClean = backend.configCalls;
  const cleaning = lastButtonWithText(environment.body, '移除').listeners.click();
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'info';
  environment.S.store.touch();
  environment.S.router('general');
  await flushUI();
  cleanGate.resolve();
  await cleaning;
  await flushUI();
  assert.strictEqual(environment.S.store.dirty, true);
  assert.strictEqual(environment.S.store.intent.main.log_level, 'info', 'clean response must preserve edits made while it was in flight');
  assert.ok(environment.S.store.intent.nodes.some((node) => node.id === 'feed_stale'), 'preserved Draft must retain its local inventory object');
  assert.strictEqual(backend.configCalls, configCallsBeforeClean, 'changed Draft must prevent unconditional clean reload');
  assert.strictEqual(application.location.hash, '#/general');
  assert.match(text(environment.view), /基础设置/, 'stale clean render must not overwrite the newer route');
}

async function testSubscriptionAsyncWorkReloadsLatestAfterDiscard() {
  let backend = createDraftBackend();
  let environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  loadView(environment, 'subscriptions');
  await environment.S.store.init();
  await environment.S.views.subscriptions.render(environment.view);

  const updateGate = deferred();
  backend.api.updateSubscription = async () => {
    backend.subscriptionUpdates++;
    await updateGate.promise;
    backend.intent.main.log_level = 'warn';
    backend.revision = '"revision-update-latest"';
    return { subscriptions: [{ id: 'feed', node_count: 1, current: 1, added: 0, skipped: 0, stale: [] }] };
  };
  const configCallsBeforeUpdate = backend.configCalls;
  const updating = buttonWithText(environment.view, '立即更新').listeners.click();
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  backend.intent.main.log_level = 'error';
  backend.revision = '"revision-discard-base"';
  await environment.S.store.reload();
  assert.strictEqual(environment.S.store.dirty, false, 'Discard must leave a clean Draft while subscription update is in flight');
  updateGate.resolve();
  await updating;
  await flushUI();
  assert.strictEqual(backend.configCalls, configCallsBeforeUpdate + 2,
    'epoch change with a clean Draft must reload once for Discard and once after subscription update');
  assert.strictEqual(environment.S.store.revision, '"revision-update-latest"');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'warn',
    'subscription completion must load the server state newer than the discarded Draft');

  backend = createDraftBackend();
  environment = createEnvironment(async () => ({ ok: true }));
  attachRealStore(environment, backend.api);
  loadView(environment, 'subscriptions');
  await environment.S.store.init();
  await environment.S.views.subscriptions.render(environment.view);

  const cleanGate = deferred();
  backend.api.cleanNode = async () => {
    backend.subscriptionCleans++;
    await cleanGate.promise;
    backend.intent.nodes = backend.intent.nodes.filter((node) => node.id !== 'feed_stale');
    backend.intent.main.log_level = 'warn';
    backend.revision = '"revision-clean-latest"';
    return { snapshot: {} };
  };
  buttonWithText(environment.view, '清理失效节点 ×1').listeners.click();
  const configCallsBeforeClean = backend.configCalls;
  const cleaning = lastButtonWithText(environment.body, '移除').listeners.click();
  await Promise.resolve();
  environment.S.store.intent.main.log_level = 'debug';
  environment.S.store.touch();
  backend.intent.main.log_level = 'error';
  backend.revision = '"revision-clean-discard-base"';
  await environment.S.store.reload();
  assert.strictEqual(environment.S.store.dirty, false, 'Discard must leave a clean Draft while stale cleanup is in flight');
  cleanGate.resolve();
  await cleaning;
  await flushUI();
  assert.strictEqual(backend.configCalls, configCallsBeforeClean + 2,
    'epoch change with a clean Draft must reload once for Discard and once after stale cleanup');
  assert.strictEqual(environment.S.store.revision, '"revision-clean-latest"');
  assert.strictEqual(environment.S.store.intent.main.log_level, 'warn');
  assert.ok(!environment.S.store.intent.nodes.some((node) => node.id === 'feed_stale'),
    'cleanup completion must load the latest server inventory after the discarded Draft');
}

function localProxyIntent(proxy) {
  const intent = representativeIntent('key');
  intent.local_proxies = proxy ? [proxy] : [];
  return intent;
}

function proxyControl(environment, label, tag) {
  const field = fieldWithLabel(environment.drawerRoot, label);
  assert.ok(field, `local proxy editor must expose ${label}`);
  return find(field, (element) => element.tag === tag);
}

function openProxyEditor(environment) {
  loadView(environment, 'proxies');
  openOnlyEditor(environment, 'proxies');
}

function testLocalProxyAddressFixturesAndMixedLabel() {
  const proxy = {
    id: 'mixed-entry', enabled: true, name: 'Mixed entry', protocol: 'mixed',
    listen: '127.0.0.1', listen_port: 1080
  };
  const environment = createEnvironment(async () => ({ ok: true }), localProxyIntent(proxy));
  for (const fixture of localProxyFixtures.cases) {
    assert.strictEqual(
      environment.S.ui.classifyLocalProxyListen(fixture.listen),
      fixture.classification,
      `Linux classification drifted for ${fixture.name}`
    );
  }
  loadView(environment, 'proxies');
  environment.S.views.proxies.render(environment.view);
  assert.ok(
    find(environment.view, (element) => element.tag === 'span' && text(element) === 'Mixed (SOCKS + HTTP)'),
    'Mixed local proxy rows must use the shared protocol label'
  );
}

function testLocalProxyKeepsPasswordWhileUpdatingUsername() {
  const proxy = {
    id: 'entry', enabled: true, name: 'Entry', protocol: 'mixed', listen: '192.168.50.1', listen_port: 1080,
    username: 'old-user', password: 'saved-secret'
  };
  const environment = createEnvironment(async () => ({ ok: true }), localProxyIntent(proxy));
  openProxyEditor(environment);

  const username = proxyControl(environment, '用户名', 'input');
  const password = proxyControl(environment, '新密码', 'input');
  assert.strictEqual(password.value, '', 'an existing password must never be placed back into the rendered input');
  username.value = 'updated-user';
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(proxy.username, 'updated-user', 'editing username must write back to the draft');
  assert.strictEqual(proxy.password, 'saved-secret', 'the keep action must retain the stored password');
}

function testLocalProxyReplacesAndRemovesAuthentication() {
  let proxy = {
    id: 'entry', enabled: true, name: 'Entry', protocol: 'socks', listen: '127.0.0.1', listen_port: 1080,
    username: 'old-user', password: 'saved-secret'
  };
  let environment = createEnvironment(async () => ({ ok: true }), localProxyIntent(proxy));
  openProxyEditor(environment);
  let action = proxyControl(environment, '认证操作', 'select');
  action.value = 'replace';
  action.listeners.change({ target: action });
  proxyControl(environment, '用户名', 'input').value = 'replacement-user';
  proxyControl(environment, '新密码', 'input').value = 'replacement-secret';
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(proxy.username, 'replacement-user', 'replace must write the username');
  assert.strictEqual(proxy.password, 'replacement-secret', 'replace must write the new password');

  proxy = {
    id: 'entry', enabled: true, name: 'Entry', protocol: 'http', listen: '127.0.0.1', listen_port: 8080,
    username: 'old-user', password: 'saved-secret'
  };
  environment = createEnvironment(async () => ({ ok: true }), localProxyIntent(proxy));
  openProxyEditor(environment);
  action = proxyControl(environment, '认证操作', 'select');
  action.value = 'remove';
  action.listeners.change({ target: action });
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.ok(!Object.hasOwn(proxy, 'username') && !Object.hasOwn(proxy, 'password'),
    'remove authentication must clear the paired fields together');
}

function testLocalProxyBlocksExposedUnauthenticatedDraftAndCreatesCredentials() {
  const intent = localProxyIntent(null);
  const environment = createEnvironment(async () => ({ ok: true }), intent);
  loadView(environment, 'proxies');
  environment.S.views.proxies.render(environment.view);
  buttonWithText(environment.view, '添加端点').listeners.click();

  proxyControl(environment, '名称', 'input').value = 'LAN entry';
  const listen = proxyControl(environment, '监听地址', 'input');
  const action = proxyControl(environment, '认证操作', 'select');
  action.value = 'replace';
  action.listeners.change({ target: action });
  proxyControl(environment, '用户名', 'input').value = 'new-user';
  const newPassword = proxyControl(environment, '新密码', 'input');
  newPassword.value = 'new-secret';
  listen.value = 'router.lan';
  listen.listeners.input({ target: listen });
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(intent.local_proxies.length, 0, 'hostname listeners must not enter the Draft even with authentication');
  assert.ok(text(environment.body.querySelector('#toasts')).includes('不能填写域名'),
    'a rejected hostname must explain that an IP address is required');

  listen.value = '127.0.0.1';
  newPassword.value = '';
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(intent.local_proxies.length, 0, 'incomplete authentication must not enter the Draft');
  assert.ok(text(environment.body.querySelector('#toasts')).includes('同时填写'),
    'incomplete authentication must explain the paired-field requirement');
  newPassword.value = 'new-secret';

  action.value = 'remove';
  action.listeners.change({ target: action });
  listen.value = '0.0.0.0';
  listen.listeners.input({ target: listen });
  const warning = find(environment.drawerRoot, (element) => classSet(element).has('local-proxy-exposure'));
  assert.ok(warning && warning.hidden === false && text(warning).includes('扩大访问范围'),
    'non-loopback input must show a prominent exposure warning');

  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(intent.local_proxies.length, 0, 'an exposed endpoint without authentication must not enter the Draft');
  assert.ok(text(environment.body.querySelector('#toasts')).includes('可能允许其他设备连接'),
    'blocked exposed listeners must explain the risk');

  action.value = 'replace';
  action.listeners.change({ target: action });
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(intent.local_proxies.length, 1, 'paired authentication must allow an exposed endpoint into the Draft');
  assert.strictEqual(intent.local_proxies[0].username, 'new-user', 'new username must be written to the Draft');
  assert.strictEqual(intent.local_proxies[0].password, 'new-secret', 'new password must be written to the Draft');
}

function testLocalProxyBlocksRemovingAuthenticationFromExposedListener() {
  const proxy = {
    id: 'entry', enabled: true, name: 'LAN entry', protocol: 'mixed', listen: '::', listen_port: 1080,
    username: 'user', password: 'secret'
  };
  const environment = createEnvironment(async () => ({ ok: true }), localProxyIntent(proxy));
  openProxyEditor(environment);
  const action = proxyControl(environment, '认证操作', 'select');
  action.value = 'remove';
  action.listeners.change({ target: action });
  buttonWithText(environment.drawerRoot, '保存到工作副本').listeners.click();
  assert.strictEqual(proxy.username, 'user', 'blocked removal must preserve the existing username');
  assert.strictEqual(proxy.password, 'secret', 'blocked removal must preserve the existing password');
  assert.strictEqual(environment.touchCount, 0, 'blocked removal must not mark the Draft dirty');
}

async function testLinuxWebRuntimeFactsResponsiveLogoutAndNodeEndpoints() {
  let logoutCalls = 0;
  const shell = createEnvironment(async () => ({ ok: true }), draftLifecycleIntent());
  shell.S.auth = { logout() { logoutCalls++; } };
  shell.S.ui.renderShell(() => {});
  const logout = buttonWithText(shell.side, '退出登录');
  assert.ok(logout, 'the shell must expose one discoverable Logout action');
  logout.listeners.click();
  assert.strictEqual(logoutCalls, 1, 'desktop and narrow layouts must reuse the auth logout action');

  const css = fs.readFileSync(path.join(root, 'go/cmd/steer-linux/web/style.css'), 'utf8');
  const narrow = css.slice(css.indexOf('@media (max-width: 900px)'));
  assert.match(narrow, /\.side-foot \{ display: flex;/,
    'the narrow layout must keep the footer containing Logout visible');
  assert.match(narrow, /\.side-foot__note \{ display: none;/,
    'the narrow layout may hide the explanatory note without hiding Logout');

  for (const fixture of [
    {
      listen: '127.0.0.1:9080',
      expected: ['控制台仅允许本机访问', 'ssh -L 9080:127.0.0.1:9080 host', 'http://127.0.0.1:9080']
    },
    {
      listen: '[::1]:9443',
      expected: ['控制台仅允许本机访问', 'ssh -L 9443:[::1]:9443 host', 'http://127.0.0.1:9443']
    }
  ]) {
    const environment = createEnvironment(async () => ({ ok: true }), draftLifecycleIntent(), {
      runtime: { web_listen: fixture.listen }
    });
    loadView(environment, 'system');
    await environment.S.views.system.render(environment.view);
    const rendered = text(environment.view);
    for (const expected of fixture.expected) assert.ok(rendered.includes(expected), `${fixture.listen} must render ${expected}`);
  }

  const intent = draftLifecycleIntent();
  intent.subscriptions = [];
  intent.nodes = uiSpec.node_types.map((type, index) => {
    if (type.value === 'tor') return {
      id: 'node-tor', enabled: true, name: 'Node tor', type: 'tor', executable_path: '/usr/bin/tor'
    };
    return {
      id: `node-${type.value}`, enabled: true, name: `Node ${type.value}`, type: type.value,
      server: index === 0 ? '2001:db8::1' : `${type.value}.example`, server_port: 2000 + index
    };
  });
  const nodes = createEnvironment(async () => ({ ok: true }), intent);
  loadView(nodes, 'nodes');
  nodes.S.views.nodes.render(nodes.view);
  const rows = findAll(nodes.view, (element) => element.tag === 'tr');
  for (const [index, node] of intent.nodes.entries()) {
    const row = rows.find((candidate) => text(candidate).includes(node.name));
    assert.ok(row, `${node.type} must render one node row`);
    const rendered = text(row);
    const expected = node.type === 'tor'
      ? '本地 Tor · /usr/bin/tor'
      : (index === 0 ? `[2001:db8::1]:${node.server_port}` : `${node.server}:${node.server_port}`);
    assert.ok(rendered.includes(expected), `${node.type} endpoint summary must render ${expected}`);
    assert.doesNotMatch(rendered, /undefined|:0(?:\D|$)/, `${node.type} must not expose an empty endpoint`);
  }
}

async function testSharedSubscriptionStatusLifecycleAndDisabledUpdate() {
  const statuses = subscriptionStatusFixtures.cases.map((fixture) => fixture.status);
  const intent = draftLifecycleIntent();
  intent.subscriptions = statuses.map((status) => ({
    id: status.id, enabled: status.enabled, name: status.name, url: status.url, update_interval: status.update_interval
  }));
  intent.nodes.push(
    { id: 'failed_blocked', enabled: true, name: 'Blocked stale', type: 'socks', server: 'blocked.example', server_port: 1080, source_subscription: 'failed', pinned_stale: true }
  );
  let updates = 0;
  const environment = createEnvironment(async () => ({ ok: true }), intent, { api: {
    subscriptions: async () => ({ subscriptions: statuses }),
    updateSubscription: async (id) => {
      updates++;
      return { subscriptions: statuses.filter((status) => status.id === id) };
    },
    cleanNode: async () => ({ subscriptions: statuses })
  } });
  environment.S.store.draftEpoch = 0;
  environment.S.store.reload = async () => ({ ok: true });
  environment.S.store.refreshOverview = async () => ({});
  loadView(environment, 'subscriptions');
  await environment.S.views.subscriptions.render(environment.view);

  const rendered = text(environment.view);
  for (const expected of ['未抓取', '成功', '成功 · 有跳过', '最近失败', '已停用', 'subscription server returned HTTP 503']) {
    assert.match(rendered, new RegExp(expected.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
      `Linux subscription UI must render shared state ${expected}`);
  }
  assert.match(rendered, /2（当前 1）/, 'failed-after-success keeps only the referenced stale inventory');
  assert.match(rendered, /1 \/ 1/, 'skipped and referenced stale counts remain persistent');

  const updateButtons = findAll(environment.view, (element) => element.tag === 'button' && text(element) === '立即更新');
  assert.strictEqual(updateButtons.length, statuses.length);
  const disabledIndex = statuses.findIndex((status) => status.id === 'disabled');
  assert.ok(Object.hasOwn(updateButtons[disabledIndex].attributes, 'disabled'),
    'disabled subscription Update is visibly disabled');
  await updateButtons[disabledIndex].listeners.click();
  assert.strictEqual(updates, 0, 'disabled subscription Update never reaches the backend');

  const successIndex = statuses.findIndex((status) => status.id === 'success');
  await updateButtons[successIndex].listeners.click();
  assert.strictEqual(updates, 1);
  const updateToast = text(environment.toasts);
  assert.match(updateToast, /新增 2 · 当前 2 · 已失效 0 · 已跳过 0/,
    'update toast uses the same status contract and all inventory counters');
  assert.match(updateToast, /当前运行配置未改变/);
  assert.match(updateToast, /仍被路由使用的节点已自动保留/);
}

Promise.resolve()
  .then(testDNSProtocolSwitchUsesSharedMatrix)
  .then(testFailedToggleRestoresDraft)
  .then(testGlobalEnableContractUsesTheCompleteDirtyDraft)
  .then(testConflictRestoresUntilOverwriteIsChosen)
  .then(testConflictOverwritePreservesEverySaveIntent)
  .then(testApplyFailureNotificationsTakePrecedenceOverStaleDraft)
  .then(testNewRulesAreStoredBeforeDefault)
  .then(testChipsCommitPendingTokensConsistently)
  .then(testNodeStringListSubmitAndPrivateKeyRoundTrip)
  .then(testNodeEditorUsesSharedDefaultWithoutMutatingDraft)
  .then(testNodeExportShowsSharedBackendLink)
  .then(testRuleChoicesAreRestrictedAndSavedOnDrawerSubmit)
  .then(testSharedCreationDefaultsAutomaticIDsAndReferenceLabels)
  .then(testApplySavedAPIKeepsStructuredFailure)
  .then(testStoreTracksSavedPendingApply)
  .then(testSharedCollectionOrderingMovesOnlyTheDraft)
  .then(testSharedCollectionDragCommitsOnceAndCancellationDoesNotMutate)
  .then(testCollectionDragInteractionCommitsOnDropOnly)
  .then(testNodeDisplaySortingHeadersNeverMutateTheDraft)
  .then(testActualGenerationAndPersistentApplyFixture)
  .then(testSharedProbeDiagnosticsAndDisabledActions)
  .then(testOverviewUsesBoundedBackendWarningGroups)
  .then(testExternalRevisionRefreshPreservesDraftAndLifecycleFacts)
  .then(testSharedUISafetyContracts)
  .then(testJSONDraftStoreLifecycle)
  .then(testStoreSnapshotsAndAsyncOrdering)
  .then(testSaveAndApplySurviveOverviewRefreshFailure)
  .then(testEnableConflictUsesCurrentDraftObject)
  .then(testAdvancedRouterDiscardAndGuardedActions)
  .then(testSubscriptionAsyncWorkPreservesNewDraftAndRoute)
  .then(testSubscriptionAsyncWorkReloadsLatestAfterDiscard)
  .then(testLocalProxyAddressFixturesAndMixedLabel)
  .then(testLocalProxyKeepsPasswordWhileUpdatingUsername)
  .then(testLocalProxyReplacesAndRemovesAuthentication)
  .then(testLocalProxyBlocksExposedUnauthenticatedDraftAndCreatesCredentials)
  .then(testLocalProxyBlocksRemovingAuthenticationFromExposedListener)
  .then(testLinuxWebRuntimeFactsResponsiveLogoutAndNodeEndpoints)
  .then(testSharedSubscriptionStatusLifecycleAndDisabledUpdate)
  .then(() => console.log('Linux web regression tests passed.'))
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
