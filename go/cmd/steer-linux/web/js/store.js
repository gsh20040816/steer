/* SPDX-License-Identifier: GPL-3.0-or-later */
/* 工作副本 store：Intent 文档 + 修订号 + dirty 状态 + Save/Apply 状态机。 */
'use strict';
(function () {
  const S = window.S;

  let intent = null;
  let revision = '';
  let dirty = false;
  let draftText = '';
  let draftError = '';
  let mutationEpoch = 0;
  let stateEpoch = 0;
  let saving = false;
  let reloading = false;
  let applying = false;
  let overview = null;
  let runtime = null;
  let diagnostics = { warnings: [] };
  let probeResults = { latest_results: [], warnings: [] };
  let externalRevision = '';
  let lastRefreshedAt = '';
  /* 订阅节点来自订阅节点存储，不进草稿：[{ id, nodes }]，按已保存配置里的订阅分组。 */
  let subscriptionNodeLists = [];
  let manualCopies = new Map();
  /* 最近一次读取失败的错误（含结构化错误码）；读取成功后清空。 */
  let subscriptionNodesError = null;
  let subscriptionNodesSequence = 0;

  const listeners = new Set();
  const emit = () => { for (const fn of listeners) fn(); };
  const LIST_FIELDS = ['nodes', 'subscriptions', 'routes', 'dns_profiles', 'local_proxies', 'rules'];
  const normalizeIntent = (value) => {
    if (!value || typeof value !== 'object') return value;
    LIST_FIELDS.forEach((field) => { if (!Array.isArray(value[field])) value[field] = []; });
    return value;
  };
  const serializeIntent = () => JSON.stringify(intent, null, 2);
  const normalizeDiagnostics = (value) => ({
    ...(value && typeof value === 'object' ? value : {}),
    warnings: Array.isArray(value?.warnings) ? value.warnings : []
  });
  const normalizeProbeResults = (value) => ({
    latest_results: Array.isArray(value?.latest_results) ? value.latest_results : [],
    warnings: Array.isArray(value?.warnings) ? value.warnings : []
  });
  const probeResultKey = (result) => result?.scope === 'overview'
    ? `overview:${result.kind}`
    : `${result?.scope || ''}:${result?.object_id || ''}:${result?.kind || ''}`;
  const installIntent = (value) => {
    intent = normalizeIntent(value);
    draftText = serializeIntent();
    draftError = '';
    mutationEpoch++;
  };
  const snapshotIntent = () => JSON.parse(JSON.stringify(intent));
  const manualCopyKey = (subscriptionId, nodeId) => `${subscriptionId}\n${nodeId}`;
  /* manual_copy 单独保存：节点对象原样交给导出等接口，不能夹带额外字段。 */
  const installSubscriptionNodes = (value) => {
    const lists = [];
    const copies = new Map();
    for (const entry of Array.isArray(value?.subscriptions) ? value.subscriptions : []) {
      if (!entry || typeof entry.id !== 'string') continue;
      const nodes = (Array.isArray(entry.nodes) ? entry.nodes : [])
        .filter((listed) => listed && typeof listed === 'object')
        .map((listed) => {
          const { manual_copy: manualCopy, ...node } = listed;
          if (manualCopy && typeof manualCopy === 'object') copies.set(manualCopyKey(entry.id, node.id), manualCopy);
          return node;
        });
      lists.push({ id: entry.id, nodes });
    }
    subscriptionNodeLists = lists;
    manualCopies = copies;
    subscriptionNodesError = null;
  };
  /* 只采用最近一次发起的读取，乱序返回的旧列表不能覆盖新列表；失败时保留上一份列表。 */
  const loadSubscriptionNodes = async () => {
    const sequence = ++subscriptionNodesSequence;
    try {
      const value = await S.api.subscriptionNodes();
      if (sequence === subscriptionNodesSequence) installSubscriptionNodes(value);
      return { ok: sequence === subscriptionNodesSequence };
    } catch (error) {
      if (sequence === subscriptionNodesSequence) subscriptionNodesError = error || new Error('');
      throw error;
    }
  };
  /* 订阅节点操作失败后同样只刷新订阅节点列表和状态，不动草稿；刷新失败不覆盖操作本身的错误。 */
  const refreshAfterNodeError = async () => {
    await Promise.allSettled([loadSubscriptionNodes(), store.refreshOverview()]);
    emit();
  };
  const parseDraft = (text) => {
    const parsed = JSON.parse(text);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('Canonical JSON 顶层必须是对象');
    }
    if (!parsed.main || typeof parsed.main !== 'object' || Array.isArray(parsed.main) ||
        !parsed.bootstrap || typeof parsed.bootstrap !== 'object' || Array.isArray(parsed.bootstrap)) {
      throw new Error('Canonical JSON 必须包含 main 与 bootstrap 对象');
    }
    return normalizeIntent(parsed);
  };

  const store = {
    get intent() { return intent; },
    get revision() { return revision; },
    get dirty() { return dirty; },
    get draftText() { return draftText; },
    get draftValid() { return draftError === ''; },
    get draftError() { return draftError; },
    get draftEpoch() { return mutationEpoch; },
    get saving() { return saving; },
    get reloading() { return reloading; },
    get applying() { return applying; },
    get listenerCount() { return listeners.size; },
    get pendingApply() { return overview?.pending_apply === true; },
    get overview() { return overview; },
    get runtime() { return runtime; },
    get diagnostics() { return diagnostics; },
    get probeResults() { return probeResults; },
    get externalRevision() { return externalRevision; },
    get hasExternalChange() { return externalRevision !== '' && externalRevision !== revision; },
    get lastRefreshedAt() { return lastRefreshedAt; },
    /* 草稿里每个订阅一组存储节点，按草稿里的订阅顺序；还没有存储节点的订阅（例如未保存的新订阅）是空分组。
       草稿里已删除的订阅不出现，和保存后后台删除它的节点存储一致。 */
    get subscriptionNodes() {
      const lists = new Map(subscriptionNodeLists.map((entry) => [entry.id, entry]));
      const seen = new Set();
      return (Array.isArray(intent?.subscriptions) ? intent.subscriptions : []).map((subscription) => {
        if (!subscription?.id || seen.has(subscription.id)) return null;
        seen.add(subscription.id);
        return lists.get(subscription.id) || { id: subscription.id, nodes: [] };
      }).filter(Boolean);
    },
    /* 手动节点（草稿）+ 订阅节点：路由选择、引用标签和删除保护都用这份列表。 */
    get allNodes() {
      return [...(Array.isArray(intent?.nodes) ? intent.nodes : []), ...store.subscriptionNodes.flatMap((entry) => entry.nodes)];
    },
    get subscriptionNodesError() { return subscriptionNodesError; },

    normalizeIntent,

    /* 「复制到我的节点」直接使用后台按共享规则生成的 manual_copy（不含 ID）。 */
    manualCopy(node) {
      const copy = node && manualCopies.get(manualCopyKey(node.source_subscription, node.id));
      return copy ? JSON.parse(JSON.stringify(copy)) : null;
    },

    async refreshSubscriptionNodes() {
      try {
        return await loadSubscriptionNodes();
      } finally {
        emit();
      }
    },

    /* 启用/停用只写订阅节点存储，不改草稿和配置的 revision；成功时后台返回完整列表。 */
    async setSubscriptionNodeEnabled(subscriptionId, nodeId, enabled) {
      const sequence = ++subscriptionNodesSequence;
      let value;
      try {
        value = await S.api.setSubscriptionNodeEnabled(subscriptionId, nodeId, enabled);
      } catch (error) {
        await refreshAfterNodeError();
        throw error;
      }
      if (sequence === subscriptionNodesSequence) {
        installSubscriptionNodes(value);
        emit();
      }
      await store.refreshOverview().catch(() => null);
      return value;
    },

    /* 清理只删除没有引用的已失效节点，写订阅节点存储；配置和它的 revision 不变。 */
    async cleanSubscriptionNode(subscriptionId, nodeId) {
      let result;
      try {
        result = await S.api.cleanNode(subscriptionId, nodeId);
      } catch (error) {
        await refreshAfterNodeError();
        throw error;
      }
      const [refreshed] = await Promise.allSettled([store.refreshSubscriptionNodes(), store.refreshOverview()]);
      return { result, refreshError: refreshed.status === 'rejected' ? refreshed.reason : null };
    },

    subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); },

    async init() {
      const [config, ov, runtimeInfo, probeDiagnostics, persistedProbeResults] = await Promise.all([
        S.api.config(), S.api.overview(), S.api.runtime(),
        S.api.diagnostics().catch(() => ({ warnings: ['诊断状态暂时不可用'] })),
        S.api.probeResults().catch(() => ({ latest_results: [], warnings: ['最近测试结果暂时不可用'] })),
        loadSubscriptionNodes().catch(() => null)
      ]);
      installIntent(config.intent);
      revision = config.revision;
      overview = ov;
      runtime = runtimeInfo;
      diagnostics = normalizeDiagnostics(probeDiagnostics);
      probeResults = normalizeProbeResults(persistedProbeResults);
      externalRevision = ov.saved_revision && ov.saved_revision !== revision ? ov.saved_revision : '';
      lastRefreshedAt = new Date().toISOString();
      emit();
    },

    touch() {
      draftText = serializeIntent();
      draftError = '';
      mutationEpoch++;
      dirty = true;
      emit();
    },
    moveCollectionItem(collection, itemID, offset, visibleIDs) {
      const policy = S.uiSpec.collection_ordering?.[collection];
      const values = intent?.[collection];
      if (!policy || !Array.isArray(values) || ![-1, 1].includes(offset)) return false;
      const idField = policy.stable_id_field || 'id';
      const source = values.find((item) => item?.[idField] === itemID);
      const movable = (item) => item &&
        (!policy.movable_kinds?.length || policy.movable_kinds.includes(item.kind)) &&
        (!policy.pinned_last_boolean_field || item[policy.pinned_last_boolean_field] !== true);
      if (!movable(source)) return false;
      const peers = (Array.isArray(visibleIDs) ? visibleIDs : values.map((item) => item?.[idField]))
        .map((id) => values.find((item) => item?.[idField] === id))
        .filter(movable);
      const position = peers.findIndex((item) => item[idField] === itemID);
      const target = peers[position + offset];
      if (position < 0 || !target) return false;
      return store.moveCollectionItemTo(collection, itemID, target[idField], offset > 0, visibleIDs);
    },
    moveCollectionItemTo(collection, itemID, targetID, after, visibleIDs) {
      const policy = S.uiSpec.collection_ordering?.[collection];
      const values = intent?.[collection];
      if (!policy || !Array.isArray(values) || !itemID || !targetID || itemID === targetID) return false;
      const idField = policy.stable_id_field || 'id';
      const source = values.find((item) => item?.[idField] === itemID);
      const target = values.find((item) => item?.[idField] === targetID);
      const movable = (item) => item &&
        (!policy.movable_kinds?.length || policy.movable_kinds.includes(item.kind)) &&
        (!policy.pinned_last_boolean_field || item[policy.pinned_last_boolean_field] !== true);
      if (!movable(source) || !movable(target)) return false;
      const allowedIDs = new Set(Array.isArray(visibleIDs) ? visibleIDs : values.map((item) => item?.[idField]));
      if (!allowedIDs.has(itemID) || !allowedIDs.has(targetID)) return false;
      const sourceIndex = values.indexOf(source);
      let targetIndex = values.indexOf(target);
      values.splice(sourceIndex, 1);
      if (sourceIndex < targetIndex) targetIndex--;
      const insertIndex = targetIndex + (after ? 1 : 0);
      if (insertIndex === sourceIndex) {
        values.splice(sourceIndex, 0, source);
        return false;
      }
      values.splice(insertIndex, 0, source);
      store.touch();
      return true;
    },
    editJSON(text) {
      draftText = String(text ?? '');
      try {
        intent = parseDraft(draftText);
        draftError = '';
      } catch (error) {
        draftError = error.message;
      }
      mutationEpoch++;
      dirty = true;
      emit();
      return { ok: draftError === '', error: draftError };
    },
    async refreshOverview() {
      const expectedState = stateEpoch;
      const refreshed = await S.api.overview();
      if (expectedState !== stateEpoch) return { ok: false, superseded: true };
      overview = refreshed;
      externalRevision = refreshed.saved_revision && refreshed.saved_revision !== revision ? refreshed.saved_revision : '';
      lastRefreshedAt = new Date().toISOString();
      emit();
      return { ok: true };
    },

    async refreshServerState() {
      if (saving || reloading || applying) return { ok: false, busy: true };
      const expectedState = stateEpoch;
      /* 定时的订阅更新只改订阅节点存储、不改配置的 revision，所以节点列表一起刷新。 */
      const [refreshedOverview, refreshedRuntime, refreshedDiagnostics, refreshedProbeResults] = await Promise.all([
        S.api.overview(), S.api.runtime(), S.api.diagnostics().catch(() => diagnostics),
        S.api.probeResults().catch(() => probeResults),
        loadSubscriptionNodes().catch(() => null)
      ]);
      if (expectedState !== stateEpoch) return { ok: false, superseded: true };
      overview = refreshedOverview;
      runtime = refreshedRuntime;
      diagnostics = normalizeDiagnostics(refreshedDiagnostics);
      probeResults = normalizeProbeResults(refreshedProbeResults);
      externalRevision = refreshedOverview.saved_revision && refreshedOverview.saved_revision !== revision ? refreshedOverview.saved_revision : '';
      lastRefreshedAt = new Date().toISOString();
      emit();
      return { ok: true, changed: externalRevision !== '', revision: refreshedOverview.saved_revision || '' };
    },

    /* 保存（可选 Apply）。修订冲突以 { ok:false, conflict } 返回，由 UI 弹冲突对话框。 */
    async save(apply, force = false) {
      if (draftError) {
        const error = new Error(`当前 JSON 配置格式有误：${draftError}`);
        error.code = 'INVALID_DRAFT';
        throw error;
      }
      if (saving || reloading || applying) return { ok: false, busy: true };
      const savedSnapshot = snapshotIntent();
      const savedRevision = force ? null : revision;
      const startedMutation = mutationEpoch;
      const expectedState = ++stateEpoch;
      saving = true;
      emit();
      try {
        let res;
        try {
          res = await S.api.putConfig(savedSnapshot, savedRevision, apply);
        } catch (err) {
          if (expectedState !== stateEpoch) return { ok: false, superseded: true };
          const staleDraft = mutationEpoch !== startedMutation;
          if (err.code === 'CONFLICT') { emit(); return { ok: false, conflict: err, staleDraft }; }
          err.staleDraft = staleDraft;
          throw err;
        }

        /* PUT 已成功时先提交本地修订状态；overview 只是后续的状态刷新。 */
        revision = res.revision;
        externalRevision = '';
        const staleDraft = mutationEpoch !== startedMutation;
        if (!staleDraft) {
          dirty = false;
          draftText = serializeIntent();
        }
        emit();

        let overviewError = null;
        try {
          const refreshedOverview = await S.api.overview();
          if (expectedState === stateEpoch) {
            overview = refreshedOverview;
            /* 保存成功后，后台删掉了被删除订阅的节点存储；新订阅也从这里开始出现。 */
            const [nextDiagnostics, nextProbeResults] = await Promise.allSettled([
              S.api.diagnostics(), S.api.probeResults(), loadSubscriptionNodes()
            ]);
            if (nextDiagnostics.status === 'fulfilled') diagnostics = normalizeDiagnostics(nextDiagnostics.value);
            if (nextProbeResults.status === 'fulfilled') probeResults = normalizeProbeResults(nextProbeResults.value);
            emit();
          }
        } catch (error) {
          overviewError = error;
        }
        return { ok: true, res, staleDraft, overviewError };
      } finally {
        saving = false;
        emit();
      }
    },

    async setEnabled(enabled) {
      if (saving || reloading || applying) return { ok: false, busy: true };
      const startedMutation = mutationEpoch;
      const wasDirty = dirty;
      ++stateEpoch;
      applying = true;
      emit();
      try {
        const res = await S.api.setEnabled(enabled);
        overview = { ...overview, saved_enabled: enabled, saved_revision: res.revision };
        if (!wasDirty && startedMutation === mutationEpoch) {
          installIntent(res.intent);
          revision = res.revision;
          externalRevision = '';
          dirty = false;
        }
        // Preserve the original revision for dirty drafts: advancing it would
        // allow a later save to silently overwrite changes saved elsewhere.
        let overviewError = null;
        try { await store.refreshOverview(); } catch (error) { overviewError = error; }
        return { ok: true, res, staleDraft: dirty, overviewError };
      } finally {
        applying = false;
        emit();
      }
    },

    async applySaved() {
      if (saving || reloading || applying) return { ok: false, busy: true };
      ++stateEpoch;
      applying = true;
      emit();
      try {
        const result = await S.api.applySaved();
        let overviewError = null;
        try {
          await store.refreshOverview();
        } catch (error) {
          overviewError = error;
        }
        try { await store.refreshProbeResults(); } catch (_) { /* keep latest safe cache */ }
        return { ...result, overviewError };
      } finally {
        applying = false;
        emit();
      }
    },

    /* 以服务器为准：丢弃本地修改。 */
    async reload() {
      if (saving || reloading || applying) return { ok: false, busy: true };
      const operation = ++stateEpoch;
      const startedMutation = mutationEpoch;
      reloading = true;
      emit();
      try {
        const [config, ov, probeDiagnostics, persistedProbeResults] = await Promise.all([
          S.api.config(), S.api.overview(), S.api.diagnostics().catch(() => diagnostics),
          S.api.probeResults().catch(() => probeResults),
          loadSubscriptionNodes().catch(() => null)
        ]);
        if (operation !== stateEpoch) return { ok: false, superseded: true };
        if (mutationEpoch !== startedMutation) return { ok: false, staleDraft: true };
        installIntent(config.intent);
        revision = config.revision;
        externalRevision = '';
        overview = ov;
        diagnostics = normalizeDiagnostics(probeDiagnostics);
        probeResults = normalizeProbeResults(persistedProbeResults);
        lastRefreshedAt = new Date().toISOString();
        dirty = false;
        emit();
        return { ok: true };
      } finally {
        if (operation === stateEpoch) {
          reloading = false;
          emit();
        }
      }
    },

    async refreshDiagnostics() {
      diagnostics = normalizeDiagnostics(await S.api.diagnostics());
      emit();
      return diagnostics;
    },

    async refreshProbeResults() {
      probeResults = normalizeProbeResults(await S.api.probeResults());
      emit();
      return probeResults;
    },

    installProbeResult(result) {
      if (!result || !result.scope || !result.kind) return;
      const key = probeResultKey(result);
      probeResults = normalizeProbeResults({
        ...probeResults,
        latest_results: [result, ...probeResults.latest_results.filter((candidate) => probeResultKey(candidate) !== key)]
      });
      emit();
    }
  };

  S.store = store;
})();
