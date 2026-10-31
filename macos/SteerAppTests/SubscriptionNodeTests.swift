// SPDX-License-Identifier: GPL-3.0-or-later

import XCTest
@testable import SteerApp

private let savedDocument = #"""
{"main":{"schema_version":10,"enabled":true},
 "nodes":[{"id":"manual-1","enabled":true,"name":"Home","type":"socks","server":"home.example","server_port":1080}],
 "routes":[{"id":"direct","enabled":true,"kind":"direct"},
           {"id":"route-jp","enabled":true,"name":"Japan","kind":"single","node":"jdub-a1"}],
 "dns_profiles":[],"rules":[],"local_proxies":[],
 "subscriptions":[{"id":"jdub","enabled":true,"name":"JDUB","url":"https://sub.example/jdub"},
                  {"id":"spare","enabled":true,"name":"Spare","url":"https://sub.example/spare"}]}
"""#

private func storedNode(
    _ id: String, name: String, enabled: Bool = true, stale: Bool = false
) -> [String: JSONValue] {
    var node: [String: JSONValue] = [
        "id": .string(id), "enabled": .bool(enabled), "name": .string(name),
        "type": .string("trojan"), "server": .string("\(id).example"), "server_port": .number(443),
        "password": .string("secret-\(id)"), "tls_server_name": .string("\(id).example"),
        "source_subscription": .string("jdub"), "source_fingerprint": .string("fp-\(id)"),
    ]
    if stale { node["pinned_stale"] = .bool(true) }
    return node
}

/// Mirrors the control service: the configuration document never holds
/// subscription nodes, and every listed node carries the shared manual copy.
private actor SubscriptionNodeBackend: BackendClient {
    private var snapshot = ConfigurationSnapshot(document: savedDocument, revision: "revision-1")
    private var groups: [(id: String, nodes: [[String: JSONValue]])] = [
        (id: "jdub", nodes: [
            storedNode("jdub-a1", name: "Tokyo"),
            storedNode("jdub-old", name: "Old", stale: true),
            storedNode("jdub-off", name: "Off", enabled: false),
        ]),
        (id: "spare", nodes: []),
    ]
    private var referencedNodeIDs: Set<String> = ["jdub-a1"]
    private var nextApplyResult: RuntimeApplyResult?
    private var documents: [String] = []
    private var nodeReads = 0
    private var nodeSets: [String] = []
    private var cleans: [String] = []

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
        documents.append(document)
        snapshot = ConfigurationSnapshot(document: document, revision: "saved-\(documents.count)")
        return SaveOutcome(revision: snapshot.revision, validation: ValidationResult(ok: true, errors: [], warnings: []))
    }
    func apply(document: String, expectedRevision: String) async throws -> ApplyOutcome {
        documents.append(document)
        snapshot = ConfigurationSnapshot(document: document, revision: "applied-\(documents.count)")
        return ApplyOutcome(
            status: RuntimeStatus(), applied: true, revision: snapshot.revision, error: "",
            validation: ValidationResult(ok: true, errors: [], warnings: []), applyResult: nextApplyResult
        )
    }
    func status() async throws -> RuntimeStatus { RuntimeStatus() }
    func logs() async throws -> String { "" }
    func versions() async throws -> RuntimeVersions { RuntimeVersions() }
    func parseNodes(document: String) async throws -> NodeImportResult { NodeImportResult(nodes: [], skipped: 0) }
    func probe(kind: String, nodeID: String?, routeID: String?, download: Bool) async throws -> ProbeLatestResult {
        throw BackendClientError.helperUnavailable
    }
    func subscriptionStatuses() async throws -> [SubscriptionRuntimeStatus] { [] }
    func updateSubscription(id: String) async throws {
        guard let index = groups.firstIndex(where: { $0.id == id }) else { return }
        groups[index].nodes.append(storedNode("jdub-new", name: "New"))
    }
    func cleanSubscription(id: String, nodeID: String) async throws {
        cleans.append("\(id)/\(nodeID)")
        guard let index = groups.firstIndex(where: { $0.id == id }) else { return }
        groups[index].nodes.removeAll { $0["id"]?.stringValue == nodeID }
    }
    func geoCatalog(kind: String) async throws -> [String] { [] }

    func subscriptionNodes() async throws -> SubscriptionNodeInventory {
        nodeReads += 1
        return try inventory()
    }

    func setSubscriptionNodeEnabled(
        subscriptionID: String, nodeID: String, enabled: Bool
    ) async throws -> SubscriptionNodeInventory {
        nodeSets.append("\(subscriptionID)/\(nodeID)/\(enabled)")
        if !enabled, referencedNodeIDs.contains(nodeID) {
            throw BackendClientError.subscriptionOperationFailed(
                code: "NODE_STILL_REFERENCED",
                detail: "NODE_STILL_REFERENCED: route \"route-jp\" still references subscription node \"\(nodeID)\"",
                validation: ValidationResult(ok: false, errors: [ValidationIssue(
                    code: "DISABLED_NODE", objectType: "route", objectID: "route-jp", option: "node",
                    message: "route references a disabled node"
                )], warnings: [])
            )
        }
        guard let group = groups.firstIndex(where: { $0.id == subscriptionID }),
              let node = groups[group].nodes.firstIndex(where: { $0["id"]?.stringValue == nodeID }) else {
            throw BackendClientError.subscriptionOperationFailed(
                code: "SUBSCRIPTION_NODE_NOT_FOUND", detail: "missing", validation: nil
            )
        }
        groups[group].nodes[node]["enabled"] = .bool(enabled)
        return try inventory()
    }

    private func inventory() throws -> SubscriptionNodeInventory {
        let payload = JSONValue.object(["subscriptions": .array(groups.map { group in
            .object(["id": .string(group.id), "nodes": .array(group.nodes.map { node in
                var copy = node
                for key in ["id", "source_subscription", "source_fingerprint", "pinned_stale"] {
                    copy.removeValue(forKey: key)
                }
                copy["enabled"] = .bool(true)
                var listed = node
                listed["manual_copy"] = .object(copy)
                return .object(listed)
            })])
        })])
        return try JSONDecoder().decode(SubscriptionNodeInventory.self, from: JSONEncoder().encode(payload))
    }

    func returnApplyResult(_ result: RuntimeApplyResult?) { nextApplyResult = result }
    func sentDocuments() -> [String] { documents }
    func nodeReadCount() -> Int { nodeReads }
    func nodeSetCalls() -> [String] { nodeSets }
    func cleanCalls() -> [String] { cleans }
}

@MainActor
final class SubscriptionNodeTests: XCTestCase {
    func testSubscriptionNodePayloadDecodesNodesCredentialsAndManualCopy() throws {
        let payload = Data(#"""
        {"subscriptions":[
          {"id":"jdub","nodes":[
            {"id":"jdub-a1","enabled":true,"name":"Tokyo","type":"trojan","server":"tokyo.example","server_port":443,
             "password":"subscription-secret","source_subscription":"jdub","source_fingerprint":"fp-a1",
             "manual_copy":{"enabled":true,"name":"Tokyo","type":"trojan","server":"tokyo.example","server_port":443,"password":"subscription-secret"}},
            {"id":"jdub-old","enabled":false,"type":"socks","server":"old.example","server_port":1080,
             "source_subscription":"jdub","source_fingerprint":"fp-old","pinned_stale":true,
             "manual_copy":{"enabled":true,"type":"socks","server":"old.example","server_port":1080}}
          ]},
          {"id":"empty","nodes":[]}
        ]}
        """#.utf8)
        // The helper decodes the control payload as JSON and re-encodes it.
        let value = try JSONDecoder().decode(JSONValue.self, from: payload)
        let inventory = try JSONDecoder().decode(SubscriptionNodeInventory.self, from: JSONEncoder().encode(value))

        XCTAssertEqual(inventory.subscriptions.map(\.id), ["jdub", "empty"])
        XCTAssertTrue(inventory.subscriptions[1].nodes.isEmpty)
        let current = try XCTUnwrap(inventory.node(subscriptionID: "jdub", nodeID: "jdub-a1"))
        XCTAssertTrue(current.enabled)
        XCTAssertFalse(current.pinnedStale)
        XCTAssertEqual(current.object["password"]?.stringValue, "subscription-secret")
        XCTAssertEqual(current.object["source_subscription"]?.stringValue, "jdub")
        XCTAssertNil(current.object["manual_copy"], "the manual copy is not part of the node")
        let copy = try XCTUnwrap(current.manualCopy)
        XCTAssertNil(copy["id"])
        XCTAssertNil(copy["source_subscription"])
        XCTAssertEqual(copy["password"]?.stringValue, "subscription-secret")
        let stale = try XCTUnwrap(inventory.node(subscriptionID: "jdub", nodeID: "jdub-old"))
        XCTAssertTrue(stale.pinnedStale)
        XCTAssertFalse(stale.enabled)
        XCTAssertNil(inventory.node(subscriptionID: "empty", nodeID: "jdub-a1"))
    }

    func testSharedSubscriptionNodeWordingAndErrorCodes() {
        let contract = SteerUISpec.contract.subscriptionNodes
        XCTAssertTrue(contract.readOnly)
        XCTAssertEqual(contract.manualCopyField, "manual_copy")
        XCTAssertEqual(SteerUISpec.subscriptionNodeActionLabel("enable"), "启用")
        XCTAssertEqual(SteerUISpec.subscriptionNodeActionLabel("disable"), "停用")
        XCTAssertEqual(SteerUISpec.subscriptionNodeActionLabel("clean"), "清理")
        XCTAssertEqual(SteerUISpec.subscriptionNodeActionLabel("copy_to_manual"), "复制到我的节点")
        XCTAssertFalse(contract.errorCodes.isEmpty)
        for code in contract.errorCodes {
            XCTAssertNotNil(SubscriptionNodeErrorText.message(code: code), code)
        }
        let error = BackendClientError.subscriptionOperationFailed(
            code: "NODE_STILL_REFERENCED", detail: "NODE_STILL_REFERENCED: raw detail", validation: nil
        )
        XCTAssertFalse(error.localizedDescription.contains("raw detail"), "known codes show localized text")
    }

    func testSubscriptionNodesAreListedReadOnlyBesideManualNodes() async throws {
        let (model, backend) = try await loadedModel()

        XCTAssertEqual(model.nodeItems.map(\.identifier), ["manual-1", "jdub-a1", "jdub-old", "jdub-off"])
        XCTAssertEqual(model.nodeCount, 4)
        XCTAssertEqual(model.itemCount(for: "nodes"), 1, "the draft holds only manual nodes")
        XCTAssertEqual(model.draftItems(for: "nodes").map(\.identifier), ["manual-1"])
        let subscriptionItems = model.subscriptionNodeItems
        XCTAssertTrue(subscriptionItems.allSatisfy { $0.subscriptionOwned && $0.sourceSubscription == "jdub" })
        XCTAssertEqual(subscriptionItems.map(\.index), [0, 1, 2], "positions follow the subscription")
        XCTAssertEqual(subscriptionItems.first { $0.identifier == "jdub-old" }?.pinnedStale, true)
        XCTAssertEqual(subscriptionItems.first { $0.identifier == "jdub-off" }?.enabled, false)
        let tokyo = try XCTUnwrap(subscriptionItems.first)
        XCTAssertEqual(tokyo.title, "Tokyo")
        XCTAssertEqual(model.nodeObject(for: tokyo)?["password"]?.stringValue, "secret-jdub-a1")
        XCTAssertNil(model.nodeObject(for: tokyo)?["manual_copy"])
        XCTAssertEqual(
            model.draftItems(for: "routes").first { $0.identifier == "route-jp" }?.detail, "Tokyo",
            "route details name subscription nodes"
        )
        let reads = await backend.nodeReadCount()
        XCTAssertEqual(reads, 1)
    }

    func testRoutePickerAndNodeLookupsIncludeSubscriptionNodes() async throws {
        let (model, _) = try await loadedModel()

        let groups = model.routeNodeCandidateGroups
        XCTAssertEqual(groups.map(\.id), ["_manual", "jdub"])
        XCTAssertEqual(groups.map(\.title), ["手动节点", "JDUB"])
        XCTAssertEqual(groups[1].items.map(\.identifier), ["jdub-a1", "jdub-old"], "disabled nodes are not selectable")
        XCTAssertNil(model.nodeReferenceProblem("jdub-a1"))
        XCTAssertEqual(model.nodeReferenceProblem("jdub-off"), "节点已停用")
        XCTAssertEqual(model.nodeReferenceProblem("missing"), "节点不存在")
        XCTAssertEqual(model.nodeReferences("jdub-a1").map(\.sourceID), ["route-jp"])
        XCTAssertTrue(model.nodeReferences("jdub-old").isEmpty)
        XCTAssertEqual(model.newDraftItemObject(for: "routes")?["node"]?.stringValue, "manual-1")
    }

    func testDeletingSubscriptionIsGuardedByItsNodesAndNeverEditsDraftNodes() async throws {
        let (model, _) = try await loadedModel()
        let nodesBefore = try nodeIDs(in: model.rawJSON)

        XCTAssertNotNil(model.deletionBlockReason(for: "subscriptions", at: 0))
        XCTAssertEqual(model.deletionReferences(for: "subscriptions", at: 0).map(\.sourceID), ["route-jp"])
        model.removeDraftItem(from: "subscriptions", at: 0)
        XCTAssertEqual(model.draftItems(for: "subscriptions").map(\.identifier), ["jdub", "spare"])

        XCTAssertNil(model.deletionBlockReason(for: "subscriptions", at: 1))
        model.removeDraftItem(from: "subscriptions", at: 1)
        XCTAssertEqual(model.draftItems(for: "subscriptions").map(\.identifier), ["jdub"])
        XCTAssertEqual(try nodeIDs(in: model.rawJSON), nodesBefore)

        let route = try XCTUnwrap(model.draftItems(for: "routes").first { $0.identifier == "route-jp" })
        var object = try XCTUnwrap(model.draftItemObject(for: "routes", at: route.index))
        object["node"] = .string("manual-1")
        XCTAssertTrue(model.replaceDraftItem(in: "routes", at: route.index, object: object))
        XCTAssertNil(model.deletionBlockReason(for: "subscriptions", at: 0))
        model.removeDraftItem(from: "subscriptions", at: 0)
        XCTAssertTrue(model.draftItems(for: "subscriptions").isEmpty)
        XCTAssertEqual(try nodeIDs(in: model.rawJSON), nodesBefore, "only the subscription leaves the draft")
        XCTAssertTrue(model.subscriptionNodeItems.isEmpty, "nodes of a removed subscription are no longer selectable")
        XCTAssertEqual(model.nodeReferenceProblem("jdub-a1"), "节点不存在")
    }

    func testCopyToMyNodesAddsTheSharedManualCopyWithANewID() async throws {
        let (model, _) = try await loadedModel()
        let routesBefore = model.draftItems(for: "routes").map(\.detail)
        let source = try XCTUnwrap(model.subscriptionNodeItems.first { $0.identifier == "jdub-old" })

        let copied = try XCTUnwrap(model.copySubscriptionNodeToManual(source))

        XCTAssertFalse(copied.subscriptionOwned)
        XCTAssertTrue(model.isDirty)
        XCTAssertNotEqual(copied.identifier, source.identifier)
        XCTAssertTrue(copied.identifier.hasPrefix("node-"))
        XCTAssertNotNil(copied.identifier.range(of: SteerUISpec.contract.idPolicy.pattern, options: .regularExpression))
        let object = try XCTUnwrap(model.draftItemObject(for: "nodes", at: copied.index))
        XCTAssertEqual(object["name"]?.stringValue, "Old")
        XCTAssertEqual(object["password"]?.stringValue, "secret-jdub-old")
        XCTAssertEqual(object["enabled"]?.boolValue, true)
        for key in ["source_subscription", "source_fingerprint", "pinned_stale", "manual_copy"] {
            XCTAssertNil(object[key], key)
        }
        XCTAssertEqual(model.draftItems(for: "nodes").map(\.identifier), ["manual-1", copied.identifier])
        XCTAssertEqual(model.subscriptionNodeItems.map(\.identifier), ["jdub-a1", "jdub-old", "jdub-off"])
        XCTAssertEqual(model.draftItems(for: "routes").map(\.detail), routesBefore, "routes are not redirected")

        let second = try XCTUnwrap(model.copySubscriptionNodeToManual(source))
        XCTAssertNotEqual(second.identifier, copied.identifier)
        XCTAssertNil(model.copySubscriptionNodeToManual(copied), "manual nodes are not copied")
    }

    func testDraftsSentToSaveAndApplyNeverContainSubscriptionNodes() async throws {
        let (model, backend) = try await loadedModel()
        let source = try XCTUnwrap(model.subscriptionNodeItems.first)
        let copied = try XCTUnwrap(model.copySubscriptionNodeToManual(source))

        model.saveDraft()
        try await waitUntil { !model.isBusy && !model.isDirty }
        model.setDraftValue(in: "main", key: "log_level", value: .string("info"))
        model.saveAndApplyDraft()
        try await waitUntil { !model.isBusy && !model.isDirty }

        let documents = await backend.sentDocuments()
        XCTAssertEqual(documents.count, 2)
        for document in documents {
            XCTAssertEqual(try nodeIDs(in: document), ["manual-1", copied.identifier])
            for key in ["source_subscription", "source_fingerprint", "pinned_stale", "manual_copy"] {
                XCTAssertFalse(document.contains(key), key)
            }
        }
        let reads = await backend.nodeReadCount()
        XCTAssertEqual(reads, 3, "nodes are reloaded after each save")
        XCTAssertEqual(model.subscriptionNodeItems.count, 3)
    }

    func testEnableAndDisableWriteTheNodeStoreAndReportReferencedNodes() async throws {
        let (model, backend) = try await loadedModel()
        let draftBefore = model.rawJSON

        let stale = try XCTUnwrap(model.subscriptionNodeItems.first { $0.identifier == "jdub-old" })
        model.setSubscriptionNodeEnabled(stale, enabled: false)
        try await waitUntil { !model.subscriptionNodeOperationInProgress(stale) }
        XCTAssertEqual(model.subscriptionNodeItems.first { $0.identifier == "jdub-old" }?.enabled, false)
        XCTAssertEqual(model.message, "已停用订阅节点 Old")

        let off = try XCTUnwrap(model.subscriptionNodeItems.first { $0.identifier == "jdub-off" })
        model.setSubscriptionNodeEnabled(off, enabled: true)
        try await waitUntil { !model.subscriptionNodeOperationInProgress(off) }
        XCTAssertEqual(model.subscriptionNodeItems.first { $0.identifier == "jdub-off" }?.enabled, true)
        XCTAssertEqual(model.message, "已启用订阅节点 Off")
        XCTAssertTrue(model.subscriptionNodeError.isEmpty)

        let used = try XCTUnwrap(model.subscriptionNodeItems.first { $0.identifier == "jdub-a1" })
        model.setSubscriptionNodeEnabled(used, enabled: false)
        try await waitUntil { !model.subscriptionNodeOperationInProgress(used) }
        XCTAssertEqual(model.subscriptionNodeItems.first { $0.identifier == "jdub-a1" }?.enabled, true)
        XCTAssertTrue(model.subscriptionNodeError.hasPrefix("停用订阅节点失败："), model.subscriptionNodeError)
        XCTAssertEqual(
            model.subscriptionNodeError, "停用订阅节点失败：仍有路由在使用这个节点（Japan），请先修改这些路由"
        )
        XCTAssertFalse(model.subscriptionNodeError.contains("route-jp"), "internal IDs stay hidden")

        let calls = await backend.nodeSetCalls()
        XCTAssertEqual(calls, ["jdub/jdub-old/false", "jdub/jdub-off/true", "jdub/jdub-a1/false"])
        XCTAssertEqual(model.rawJSON, draftBefore, "enable/disable never edits the draft")
        XCTAssertFalse(model.isDirty)
        XCTAssertNil(model.revisionConflict)
    }

    func testCleanAndUpdateRefreshSubscriptionNodesWithoutTouchingAnEditedDraft() async throws {
        let (model, backend) = try await loadedModel()
        model.setDraftValue(in: "main", key: "log_level", value: .string("debug"))
        let edited = model.rawJSON

        model.cleanSubscriptionNode(subscriptionID: "jdub", nodeID: "jdub-old")
        try await waitUntil { !model.subscriptionOperationInProgress("jdub") }
        XCTAssertEqual(model.subscriptionNodeItems.map(\.identifier), ["jdub-a1", "jdub-off"])

        model.updateSubscription("jdub")
        try await waitUntil { !model.subscriptionOperationInProgress("jdub") }
        XCTAssertEqual(model.subscriptionNodeItems.map(\.identifier), ["jdub-a1", "jdub-off", "jdub-new"])

        XCTAssertEqual(model.rawJSON, edited)
        XCTAssertTrue(model.isDirty)
        XCTAssertNil(model.revisionConflict, "the store changed, the Saved configuration did not")
        let cleans = await backend.cleanCalls()
        XCTAssertEqual(cleans, ["jdub/jdub-old"])
    }

    func testTimerUpdatesOfTheNodeStoreAreShownAfterARefresh() async throws {
        let (model, backend) = try await loadedModel()
        model.setDraftValue(in: "main", key: "log_level", value: .string("debug"))
        let edited = model.rawJSON

        try await backend.updateSubscription(id: "jdub") // the background timer
        model.refreshStatus()
        try await waitUntil { !model.isBusy }
        XCTAssertEqual(model.subscriptionNodeItems.last?.identifier, "jdub-new")

        try await backend.cleanSubscription(id: "jdub", nodeID: "jdub-new")
        model.refreshSubscriptionNodes()
        try await waitUntil { model.subscriptionNodeItems.last?.identifier == "jdub-off" }
        XCTAssertEqual(model.rawJSON, edited)
        XCTAssertTrue(model.isDirty)
    }

    func testUnchangedRuntimeNoticeFollowsTheSharedApplyResultCondition() throws {
        func result(_ json: String) throws -> RuntimeApplyResult {
            try JSONDecoder().decode(RuntimeApplyResult.self, from: Data(json.utf8))
        }
        XCTAssertEqual(SteerUISpec.unchangedRuntimeNotice, "运行配置没有变化，未重启")
        XCTAssertTrue(SteerUISpec.showsUnchangedRuntimeNotice(
            try result(#"{"ok":true,"activated":true,"restarted":false}"#)
        ))
        for json in [
            #"{"ok":true,"activated":true,"restarted":true}"#,
            #"{"ok":true,"activated":true}"#,
            #"{"ok":false,"activated":true,"restarted":false}"#,
            #"{"ok":true,"activated":false,"restarted":false}"#,
        ] {
            XCTAssertFalse(SteerUISpec.showsUnchangedRuntimeNotice(try result(json)), json)
        }
        XCTAssertFalse(SteerUISpec.showsUnchangedRuntimeNotice(nil))

        let record = try JSONDecoder().decode(RuntimeApplyRecord.self, from: Data(#"""
        {"sequence":"1","result":{"ok":true,"activated":true,"restarted":false}}
        """#.utf8))
        XCTAssertTrue(SteerUISpec.showsUnchangedRuntimeNotice(record.result))
    }

    func testApplyMessagesShowTheUnchangedRuntimeNoticeOnlyWithoutARestart() async throws {
        let (model, backend) = try await loadedModel()
        let unchanged = RuntimeApplyResult(ok: true, generation: "g2", activated: true, restarted: false, error: nil)
        let restarted = RuntimeApplyResult(ok: true, generation: "g3", activated: true, restarted: true, error: nil)

        await backend.returnApplyResult(unchanged)
        model.setDraftValue(in: "main", key: "log_level", value: .string("info"))
        model.saveAndApplyDraft()
        try await waitUntil { !model.isBusy && !model.isDirty }
        XCTAssertTrue(model.message.hasSuffix("；运行配置没有变化，未重启"), model.message)

        model.applySaved()
        try await waitUntil { !model.isBusy }
        XCTAssertTrue(model.message.hasSuffix("；运行配置没有变化，未重启"), model.message)

        await backend.returnApplyResult(restarted)
        model.applySaved()
        try await waitUntil { !model.isBusy }
        XCTAssertFalse(model.message.contains("运行配置没有变化"), model.message)
    }

    private func loadedModel() async throws -> (AppModel, SubscriptionNodeBackend) {
        let backend = SubscriptionNodeBackend()
        let model = AppModel(backend: backend)
        model.loadInitialState()
        try await waitUntil { model.hasInitializedDraft && !model.isBusy }
        XCTAssertFalse(model.isDirty)
        return (model, backend)
    }

    private func nodeIDs(in document: String) throws -> [String] {
        let root = try JSONDecoder().decode(JSONValue.self, from: Data(document.utf8)).objectValue
        return (root?["nodes"]?.arrayValue ?? []).compactMap { $0.objectValue?["id"]?.stringValue }
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
