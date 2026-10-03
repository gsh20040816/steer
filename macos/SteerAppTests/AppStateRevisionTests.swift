// SPDX-License-Identifier: GPL-3.0-or-later

import XCTest
@testable import SteerApp

final actor RevisionBackend: BackendClient {
    private var snapshot: ConfigurationSnapshot
    private var applyCount = 0
    private var updateContinuation: CheckedContinuation<Void, Never>?
    private var updateStartedContinuation: CheckedContinuation<Void, Never>?

    init(document: String, revision: String) {
        snapshot = ConfigurationSnapshot(document: document, revision: revision)
    }

    func componentStatus() async -> SystemComponentsStatus {
        SystemComponentsStatus(installed: true, embeddedInstallerAvailable: false, updateAvailable: false)
    }

    func installSystemComponents() async throws {}
    func uninstallSystemComponents(removeUserData: Bool) async throws {}

    func validate(document: String) async throws -> ValidationResult {
        ValidationResult(ok: true, errors: [], warnings: [])
    }

    func loadConfiguration() async throws -> ConfigurationSnapshot { snapshot }

    func save(document: String, expectedRevision: String) async throws -> SaveOutcome {
        guard expectedRevision == snapshot.revision else {
            throw BackendClientError.revisionConflict(currentRevision: snapshot.revision)
        }
        let revision = "saved-\(snapshot.revision)"
        snapshot = ConfigurationSnapshot(document: document, revision: revision)
        return SaveOutcome(revision: revision, validation: ValidationResult(ok: true, errors: [], warnings: []))
    }

    func apply(document: String, expectedRevision: String) async throws -> ApplyOutcome {
        guard expectedRevision == snapshot.revision else {
            throw BackendClientError.revisionConflict(currentRevision: snapshot.revision)
        }
        applyCount += 1
        let revision = "applied-\(snapshot.revision)"
        snapshot = ConfigurationSnapshot(document: document, revision: revision)
        return ApplyOutcome(
            status: RuntimeStatus(), applied: true,
            revision: revision, error: "", validation: ValidationResult(ok: true, errors: [], warnings: [])
        )
    }

    func status() async throws -> RuntimeStatus { RuntimeStatus() }
    func logs() async throws -> String { "" }
    func versions() async throws -> RuntimeVersions { RuntimeVersions() }
    func parseNodes(document: String) async throws -> NodeImportResult { NodeImportResult(nodes: [], skipped: 0) }

    func probe(kind: String, nodeID: String?, routeID: String?, download: Bool) async throws -> ProbeLatestResult {
        ProbeLatestResult(
            scope: "overview", objectID: nil, kind: kind, testedAt: "2026-08-26T01:02:03Z",
            ok: true, stale: false, summary: "21 ms", errorSummary: ""
        )
    }

    func subscriptionStatuses() async throws -> [SubscriptionRuntimeStatus] { [] }

    func updateSubscription(id: String) async throws {
        await pauseInventoryOperation()
    }

    func cleanSubscription(id: String, nodeID: String) async throws {
        await pauseInventoryOperation()
    }

    private func pauseInventoryOperation() async {
        updateStartedContinuation?.resume()
        updateStartedContinuation = nil
        await withCheckedContinuation { continuation in
            updateContinuation = continuation
        }
    }

    func geoCatalog(kind: String) async throws -> [String] { [] }

    func replaceSaved(document: String, revision: String) {
        snapshot = ConfigurationSnapshot(document: document, revision: revision)
    }

    func savedSnapshot() -> ConfigurationSnapshot { snapshot }
    func appliedCount() -> Int { applyCount }

    func waitUntilInventoryOperationStarts() async {
        if updateContinuation != nil { return }
        await withCheckedContinuation { continuation in
            updateStartedContinuation = continuation
        }
    }

    func finishInventoryOperation(document: String, revision: String) {
        snapshot = ConfigurationSnapshot(document: document, revision: revision)
        updateContinuation?.resume()
        updateContinuation = nil
    }
}

@MainActor
final class AppStateRevisionTests: XCTestCase {
    func testLoadedConfigurationRevisionMatchesControlSHA256Format() {
        XCTAssertEqual(
            HelperBackendClient.configurationRevision(Data("abc".utf8)),
            "sha256-ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        )
    }

    func testTimerUpdateMakesStaleSaveAndApplyConflictWithoutChangingDraftSavedOrActive() async throws {
        for operation in [DraftConflictOperation.save, .apply] {
            let backend = RevisionBackend(document: "loaded", revision: "revision-1")
            let model = AppModel(backend: backend)
            model.loadDraft()
            try await waitUntil { !model.isBusy && model.savedRevision == "revision-1" }

            await backend.replaceSaved(document: "subscription inventory", revision: "revision-2")
            model.rawJSON = "local draft"
            model.markDirty()
            if operation == .save {
                model.saveDraft()
            } else {
                model.saveAndApplyDraft()
            }
            try await waitUntil { !model.isBusy && model.revisionConflict != nil }

            let saved = await backend.savedSnapshot()
            let appliedCount = await backend.appliedCount()
            XCTAssertEqual(saved.document, "subscription inventory")
            XCTAssertEqual(saved.revision, "revision-2")
            XCTAssertEqual(appliedCount, 0)
            XCTAssertEqual(model.rawJSON, "local draft")
            XCTAssertTrue(model.isDirty)
            XCTAssertEqual(model.revisionConflict?.operation, operation)
            if operation == .save {
                model.keepLocalDraftAfterRevisionConflict()
                XCTAssertNil(model.revisionConflict)
                XCTAssertEqual(model.rawJSON, "local draft")
                XCTAssertEqual(model.savedRevision, "revision-1")
            } else {
                model.reloadSavedAfterRevisionConflict()
                try await waitUntil { !model.isBusy && model.revisionConflict == nil }
                XCTAssertEqual(model.rawJSON, "subscription inventory")
                XCTAssertEqual(model.savedRevision, "revision-2")
                XCTAssertFalse(model.isDirty)
            }
        }
    }

    func testManualSubscriptionUpdatePreservesEditsMadeWhileRunning() async throws {
        let backend = RevisionBackend(document: "loaded", revision: "revision-1")
        let model = AppModel(backend: backend)
        model.loadDraft()
        try await waitUntil { !model.isBusy && model.savedRevision == "revision-1" }

        model.updateSubscription("public")
        await backend.waitUntilInventoryOperationStarts()
        model.rawJSON = "edit made during update"
        model.markDirty()
        await backend.finishInventoryOperation(document: "updated inventory", revision: "revision-2")
        try await waitUntil { !model.subscriptionOperationInProgress("public") }

        XCTAssertEqual(model.rawJSON, "edit made during update")
        XCTAssertEqual(model.savedRevision, "revision-1")
        XCTAssertTrue(model.isDirty)
        XCTAssertEqual(model.revisionConflict?.currentRevision, "revision-2")
        XCTAssertEqual(model.revisionConflict?.operation, .subscriptionInventory)
        XCTAssertTrue(model.message.contains("当前运行配置未改变"))
        XCTAssertTrue(model.message.contains("已自动保留"))
        let appliedBeforeOverwrite = await backend.appliedCount()
        XCTAssertEqual(appliedBeforeOverwrite, 0)

        model.overwriteAfterRevisionConflict()
        try await waitUntil { !model.isBusy && model.revisionConflict == nil }
        let overwritten = await backend.savedSnapshot()
        let appliedCount = await backend.appliedCount()
        XCTAssertEqual(overwritten.document, "edit made during update")
        XCTAssertEqual(model.savedRevision, overwritten.revision)
        XCTAssertFalse(model.isDirty)
        XCTAssertEqual(appliedCount, 0)
    }

    func testManualSubscriptionUpdateReloadsOnlyWhenDraftIsUnchanged() async throws {
        let backend = RevisionBackend(document: "loaded", revision: "revision-1")
        let model = AppModel(backend: backend)
        model.loadDraft()
        try await waitUntil { !model.isBusy && model.savedRevision == "revision-1" }

        model.updateSubscription("public")
        await backend.waitUntilInventoryOperationStarts()
        await backend.finishInventoryOperation(document: "updated inventory", revision: "revision-2")
        try await waitUntil { !model.subscriptionOperationInProgress("public") }

        XCTAssertEqual(model.rawJSON, "updated inventory")
        XCTAssertEqual(model.savedRevision, "revision-2")
        XCTAssertFalse(model.isDirty)
        XCTAssertNil(model.revisionConflict)
        let appliedCount = await backend.appliedCount()
        XCTAssertEqual(appliedCount, 0)
    }

    func testSubscriptionInventoryOperationsPreserveDraftThatWasAlreadyDirty() async throws {
        for operation in ["update", "clean"] {
            let backend = RevisionBackend(document: "loaded", revision: "revision-1")
            let model = AppModel(backend: backend)
            model.loadDraft()
            try await waitUntil { !model.isBusy && model.savedRevision == "revision-1" }

            model.rawJSON = "dirty before operation"
            model.markDirty()
            if operation == "update" {
                model.updateSubscription("public")
            } else {
                model.cleanSubscriptionNode(subscriptionID: "public", nodeID: "stale")
            }
            await backend.waitUntilInventoryOperationStarts()
            await backend.finishInventoryOperation(document: "updated inventory", revision: "revision-2")
            try await waitUntil { !model.subscriptionOperationInProgress("public") }

            XCTAssertEqual(model.rawJSON, "dirty before operation", operation)
            XCTAssertEqual(model.savedRevision, "revision-1", operation)
            XCTAssertTrue(model.isDirty, operation)
            XCTAssertEqual(model.revisionConflict?.currentRevision, "revision-2", operation)
            XCTAssertEqual(model.revisionConflict?.operation, .subscriptionInventory, operation)
            let appliedCount = await backend.appliedCount()
            XCTAssertEqual(appliedCount, 0, operation)
        }
    }

    private func waitUntil(
        timeoutNanoseconds: UInt64 = 2_000_000_000,
        condition: @escaping @MainActor () -> Bool
    ) async throws {
        let deadline = DispatchTime.now().uptimeNanoseconds + timeoutNanoseconds
        while !condition() {
            if DispatchTime.now().uptimeNanoseconds >= deadline {
                XCTFail("timed out waiting for AppModel state")
                return
            }
            try await Task.sleep(nanoseconds: 10_000_000)
        }
    }

}
