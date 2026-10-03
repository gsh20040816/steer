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

    normalizeIntent,

    subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); },

    async init() {
      const [config, ov, runtimeInfo, probeDiagnostics, persistedProbeResults] = await Promise.all([
        S.api.config(), S.api.overview(), S.api.runtime(),
        S.api.diagnostics().catch(() => ({ warnings: ['诊断状态暂时不可用'] })),
        S.api.probeResults().catch(() => ({ latest_results: [], warnings: ['最近测试结果暂时不可用'] }))
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
      const sourceGroup = policy.group_field ? (source[policy.group_field] || '') : '';
      const peers = (Array.isArray(visibleIDs) ? visibleIDs : values.map((item) => item?.[idField]))
        .map((id) => values.find((item) => item?.[idField] === id))
        .filter((item) => movable(item) && (!policy.group_field || (item[policy.group_field] || '') === sourceGroup));
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
      const sourceGroup = policy.group_field ? (source[policy.group_field] || '') : '';
      if (policy.group_field && (target[policy.group_field] || '') !== sourceGroup) return false;
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
      const [refreshedOverview, refreshedRuntime, refreshedDiagnostics, refreshedProbeResults] = await Promise.all([
        S.api.overview(), S.api.runtime(), S.api.diagnostics().catch(() => diagnostics),
        S.api.probeResults().catch(() => probeResults)
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
            const [nextDiagnostics, nextProbeResults] = await Promise.allSettled([S.api.diagnostics(), S.api.probeResults()]);
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
        // allow a later save to silently overwrite new subscription inventory.
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
          S.api.probeResults().catch(() => probeResults)
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
