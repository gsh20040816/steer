// SPDX-License-Identifier: GPL-3.0-or-later

import XCTest
@testable import SteerApp

private actor DraftEditorBackend: BackendClient {
    let importResult: NodeImportResult
    let exportResult: String
    private var exportedNodes: [JSONValue] = []

    init(
        importResult: NodeImportResult = NodeImportResult(nodes: [], skipped: 0),
        exportResult: String = "socks://user:secret@proxy.example:1080#Edge"
    ) {
        self.importResult = importResult
        self.exportResult = exportResult
    }

    func componentStatus() async -> SystemComponentsStatus {
        SystemComponentsStatus(installed: true, embeddedInstallerAvailable: false, updateAvailable: false)
    }
    func installSystemComponents() async throws {}
    func uninstallSystemComponents(removeUserData: Bool) async throws {}
    func validate(document: String) async throws -> ValidationResult {
        ValidationResult(ok: true, errors: [], warnings: [])
    }
    func loadConfiguration() async throws -> ConfigurationSnapshot {
        ConfigurationSnapshot(document: "{}", revision: "test")
    }
    func save(document: String, expectedRevision: String) async throws -> SaveOutcome {
        SaveOutcome(revision: "test", validation: ValidationResult(ok: true, errors: [], warnings: []))
    }
    func apply(document: String, expectedRevision: String) async throws -> ApplyOutcome {
        ApplyOutcome(
            status: RuntimeStatus(), applied: true, revision: "test", error: "",
            validation: ValidationResult(ok: true, errors: [], warnings: [])
        )
    }
    func status() async throws -> RuntimeStatus { RuntimeStatus() }
    func logs() async throws -> String { "" }
    func versions() async throws -> RuntimeVersions { RuntimeVersions() }
    func parseNodes(document: String) async throws -> NodeImportResult { importResult }
    func exportNode(node: JSONValue) async throws -> String {
        exportedNodes.append(node)
        return exportResult
    }
    func lastExportedNode() -> JSONValue? { exportedNodes.last }
    func probe(kind: String, nodeID: String?, routeID: String?, download: Bool) async throws -> ProbeLatestResult {
        ProbeLatestResult(
            scope: "overview", objectID: nil, kind: kind, testedAt: "2026-08-26T00:00:00Z",
            ok: true, stale: false, summary: "21 ms", errorSummary: ""
        )
    }
    func subscriptionStatuses() async throws -> [SubscriptionRuntimeStatus] { [] }
    func updateSubscription(id: String) async throws {}
    func cleanSubscription(id: String, nodeID: String) async throws {}
    func geoCatalog(kind: String) async throws -> [String] { [] }
}

final class GeoCatalogCompletionTests: XCTestCase {
    func testSearchScansTheCompleteCatalogBeforeLimitingResults() {
        var catalog = (0..<1_000).map { String(format: "category-%04d", $0) }
        catalog.append("category-late@ads")

        XCTAssertEqual(geoCatalogMatches(catalog, query: "late ads"), ["category-late@ads"])
        XCTAssertEqual(geoCatalogMatches(catalog, query: "CATEGORY-0999"), ["category-0999"])
        XCTAssertEqual(geoCatalogMatches(catalog, query: "", limit: 25).count, 25)
    }

    func testAttributeSelectorPresentationAndInsertionValueStayExact() {
        XCTAssertEqual(
            geoCatalogPresentation("category-example@cn"),
            GeoCatalogPresentation(category: "category-example", attribute: "cn")
        )
        XCTAssertEqual(
            DraftStringListCodec.appendingUnique("geosite:category-example@cn", to: "geosite:cn"),
            "geosite:cn\ngeosite:category-example@cn"
        )
        XCTAssertEqual(
            DraftStringListCodec.appendingUnique("geoip:private", to: ""),
            "geoip:private"
        )
    }
}

@MainActor
final class DraftCacheAndRouteGraphTests: XCTestCase {
    func testDraftRevisionIsDecodedOnceAcrossCountsCollectionsAndLookups() throws {
        let model = AppModel()
        model.rawJSON = try largeDraft(nodeCount: 1_200, ruleCount: 120)

        XCTAssertEqual(model.itemCount(for: "nodes"), 1_200)
        XCTAssertEqual(model.draftItems(for: "nodes").count, 1_200)
        XCTAssertEqual(model.draftItems(for: "rules").count, 120)
        XCTAssertNotNil(model.draftItemObject(for: "rules", at: 119))
        XCTAssertNil(model.draftSyntaxError)
        XCTAssertEqual(model.draftDecodeCount, 1)

        for _ in 0..<20 {
            _ = model.itemCount(for: "nodes")
            _ = model.draftItems(for: "rules")
            _ = model.draftValue(in: "main", key: "schema_version")
        }
        XCTAssertEqual(model.draftDecodeCount, 1)

        model.rawJSON += "\n"
        XCTAssertEqual(model.itemCount(for: "nodes"), 1_200)
        XCTAssertEqual(model.draftDecodeCount, 2)

        model.rawJSON = "{ invalid"
        XCTAssertNotNil(model.draftSyntaxError)
        XCTAssertEqual(model.draftItems(for: "nodes").count, 0)
        XCTAssertEqual(model.draftDecodeCount, 3)
    }

    func testRouteGraphRejectsTwoNodeAndDeepCyclesButAllowsAValidChain() {
        let model = routeModel(routes: [
            route("a", node: "node", detour: "b"),
            route("b", node: "node", detour: "c"),
            route("c", node: "node"),
            route("d", node: "node"),
        ])

        XCTAssertNotNil(model.routeDetourProblem(routeID: "b", detourID: "a"), "A→B→A must fail")
        XCTAssertNotNil(model.routeDetourProblem(routeID: "c", detourID: "a"), "A→B→C→A must fail")
        XCTAssertNil(model.routeDetourProblem(routeID: "d", detourID: "a"), "D→A→B→C is valid")
        XCTAssertTrue(model.routeDetourCandidates(editingRouteID: "c").allSatisfy { $0.identifier != "a" })
        XCTAssertTrue(model.routeDetourCandidates(editingRouteID: "d").contains { $0.identifier == "a" })
    }

    func testRouteReferencesRejectSelfMissingDisabledAndBrokenChains() {
        let model = routeModel(routes: [
            route("good", node: "node"),
            route("disabled-route", node: "node", enabled: false),
            route("disabled-node-route", node: "disabled-node"),
            route("missing-node-route", node: "missing-node"),
        ])

        XCTAssertEqual(model.nodeReferenceProblem("disabled-node"), "节点已停用")
        XCTAssertEqual(model.nodeReferenceProblem("missing-node"), "节点不存在")
        XCTAssertNotNil(model.routeDetourProblem(routeID: "good", detourID: "good"))
        XCTAssertEqual(model.routeDetourProblem(routeID: "good", detourID: "missing"), "前置路由不存在")
        XCTAssertEqual(model.routeDetourProblem(routeID: "good", detourID: "disabled-route"), "前置路由已停用")
        XCTAssertNotNil(model.routeDetourProblem(routeID: "good", detourID: "disabled-node-route"))
        XCTAssertNotNil(model.routeDetourProblem(routeID: "good", detourID: "missing-node-route"))
    }

    private func largeDraft(nodeCount: Int, ruleCount: Int) throws -> String {
        let nodes: [[String: Any]] = (0..<nodeCount).map { index in
            [
                "id": "node-\(index)", "enabled": true, "name": "Node \(index)",
                "type": "socks", "server": "127.0.0.1", "server_port": 1_080,
            ]
        }
        let rules: [[String: Any]] = (0..<ruleCount).map { index in
            [
                "id": "rule-\(index)", "enabled": true, "default": false,
                "dns_profile": "dns", "route": "direct", "domain_match": ["domain:\(index).example"],
            ]
        }
        let root: [String: Any] = [
            "main": ["schema_version": 9, "enabled": false],
            "nodes": nodes,
            "routes": [["id": "direct", "enabled": true, "kind": "direct"]],
            "dns_profiles": [[
                "id": "dns", "enabled": true, "protocol": "udp", "server": "1.1.1.1", "server_port": 53,
            ]],
            "rules": rules,
            "subscriptions": [],
            "local_proxies": [],
        ]
        return String(decoding: try JSONSerialization.data(withJSONObject: root), as: UTF8.self)
    }

    private func routeModel(routes: [[String: Any]]) -> AppModel {
        let model = AppModel()
        let root: [String: Any] = [
            "main": ["schema_version": 9, "enabled": false],
            "nodes": [
                ["id": "node", "enabled": true, "type": "socks", "server": "127.0.0.1", "server_port": 1_080],
                ["id": "disabled-node", "enabled": false, "type": "socks", "server": "127.0.0.1", "server_port": 1_081],
            ],
            "routes": routes,
        ]
        model.rawJSON = String(decoding: try! JSONSerialization.data(withJSONObject: root), as: UTF8.self)
        return model
    }

    private func route(_ id: String, node: String, detour: String = "", enabled: Bool = true) -> [String: Any] {
        var value: [String: Any] = ["id": id, "enabled": enabled, "kind": "single", "node": node]
        if !detour.isEmpty { value["detour"] = detour }
        return value
    }
}

@MainActor
final class NodeImportPreviewTests: XCTestCase {
    func testZeroSkipTUICResultDecodesWhenHelperOmitsSkippedReasons() throws {
        let response = #"""
        {
          "nodes": [
            {
              "id": "",
              "enabled": true,
              "name": "singbox_tuic",
              "type": "tuic",
              "server": "laxeb.shgao.top",
              "server_port": 39823,
              "uuid": "e410ea86-9009-4654-8c87-f2dcd442b75e",
              "password": "e410ea86-9009-4654-8c87-f2dcd442b75e",
              "congestion_control": "bbr",
              "udp_relay_mode": "quic",
              "tls_server_name": "laxeb.shgao.top",
              "alpn": ["h3"]
            }
          ],
          "skipped": 0
        }
        """#

        let result = try JSONDecoder().decode(NodeImportResult.self, from: Data(response.utf8))

        XCTAssertEqual(result.nodes.count, 1)
        XCTAssertEqual(result.skipped, 0)
        XCTAssertTrue(result.skippedReasons.isEmpty)
        XCTAssertEqual(result.nodes[0].objectValue?["type"]?.stringValue, "tuic")
        XCTAssertEqual(result.nodes[0].objectValue?["server_port"]?.numberValue, 39_823)
    }

    func testParsePreviewsSafeFieldsWithoutMutatingDraftThenImportsOnlySelection() async throws {
        let first: JSONValue = .object([
            "name": .string("First"), "type": .string("trojan"),
            "server": .string("edge.example"), "server_port": .number(443),
            "tls_server_name": .string("edge.example"), "insecure": .bool(false),
            "password": .string("credential-must-not-render"),
            "private_key": .string("private-key-must-not-render"),
        ])
        let second: JSONValue = .object([
            "name": .string("Second"), "type": .string("socks"),
            "server": .string("127.0.0.1"), "server_port": .number(1_080),
            "password": .string("second-secret"),
        ])
        let backend = DraftEditorBackend(importResult: NodeImportResult(nodes: [first, second], skipped: 2))
        let model = AppModel(backend: backend)
        model.rawJSON = #"{"main":{"schema_version":9},"nodes":[]}"#
        model.isDirty = false
        let original = model.rawJSON

        guard var preview = await model.previewNodeImport("encoded subscription") else {
            return XCTFail("expected preview")
        }

        XCTAssertEqual(model.rawJSON, original)
        XCTAssertFalse(model.isDirty)
        XCTAssertEqual(model.itemCount(for: "nodes"), 0)
        XCTAssertEqual(preview.items.count, 2)
        XCTAssertEqual(preview.items[0].protocolName, "trojan")
        XCTAssertEqual(preview.items[0].server, "edge.example")
        XCTAssertEqual(preview.items[0].port, 443)
        XCTAssertEqual(preview.items[0].tlsVerification, "验证证书")
        XCTAssertEqual(preview.items[1].tlsVerification, "不适用")
        XCTAssertEqual(preview.skippedSummary, "已跳过 2 个无法识别或字段不完整的条目")
        let renderedSafeFields = preview.items.flatMap {
            [$0.name, $0.protocolName, $0.server, $0.port.formatted(), $0.tlsVerification]
        }.joined(separator: " ")
        XCTAssertFalse(renderedSafeFields.contains("credential-must-not-render"))
        XCTAssertFalse(renderedSafeFields.contains("private-key-must-not-render"))
        XCTAssertFalse(renderedSafeFields.contains("second-secret"))

        preview.items[0].name = "Renamed"
        preview.items[1].selected = false
        XCTAssertTrue(model.confirmNodeImport(preview))
        XCTAssertTrue(model.isDirty)
        XCTAssertEqual(model.itemCount(for: "nodes"), 1)
        let imported = try XCTUnwrap(model.draftItemObject(for: "nodes", at: 0))
        XCTAssertEqual(imported["name"]?.stringValue, "Renamed")
        XCTAssertEqual(imported["password"]?.stringValue, "credential-must-not-render")
        XCTAssertNil(imported["source_subscription"])
    }

    func testCancelAfterPreviewLeavesNodeCountAndDirtyStateUntouched() async {
        let backend = DraftEditorBackend(importResult: NodeImportResult(nodes: [
            .object([
                "name": .string("Preview only"), "type": .string("http"),
                "server": .string("proxy.example"), "server_port": .number(80),
            ]),
        ], skipped: 0))
        let model = AppModel(backend: backend)
        model.rawJSON = #"{"main":{"schema_version":9},"nodes":[]}"#
        model.isDirty = false

        let preview = await model.previewNodeImport("http://proxy.example")
        XCTAssertNotNil(preview)
        XCTAssertEqual(model.itemCount(for: "nodes"), 0)
        XCTAssertFalse(model.isDirty)
    }

    func testNodeExportUsesTheCurrentUnsavedDraftNode() async throws {
        let backend = DraftEditorBackend()
        let model = AppModel(backend: backend)
        model.rawJSON = #"{"main":{"schema_version":9},"nodes":[{"id":"edge","enabled":true,"name":"Unsaved name","type":"socks","server":"proxy.example","server_port":1080,"username":"user","password":"unsaved-secret"}]}"#
        model.isDirty = true
        let item = try XCTUnwrap(model.draftItems(for: "nodes").first)

        let link = await model.exportNode(item)

        XCTAssertEqual(link, "socks://user:secret@proxy.example:1080#Edge")
        let exported = await backend.lastExportedNode()?.objectValue
        XCTAssertEqual(exported?["name"]?.stringValue, "Unsaved name")
        XCTAssertEqual(exported?["password"]?.stringValue, "unsaved-secret")
        XCTAssertTrue(model.isDirty, "export must not save or mutate the working copy")
    }
}
