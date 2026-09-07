// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import XCTest
@testable import SteerApp

final class ProbeDiagnosticsTests: XCTestCase {
    private struct FixtureDocument: Decodable {
        let schemaVersion: Int
        let capability: [String: String]
        let ordinaryUI: OrdinaryUIContract
        let objects: FixtureObjects
        let diagnostics: ProbeDiagnostics
        let probeResults: ProbeLatestResults

        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case capability, objects, diagnostics
            case probeResults = "probe_results"
            case ordinaryUI = "ordinary_ui"
        }
    }

    private struct OrdinaryUIContract: Decodable {
        let latestPerScopeObjectKind: Int
        let requiredFacts: [String]
        let forbiddenFragments: [String]

        enum CodingKeys: String, CodingKey {
            case latestPerScopeObjectKind = "latest_per_scope_object_kind"
            case requiredFacts = "required_facts"
            case forbiddenFragments = "forbidden_fragments"
        }
    }

    private struct FixtureObjects: Decodable {
        let nodes: [FixtureObject]
        let routes: [FixtureObject]
        let subscriptions: [FixtureObject]
    }

    private struct FixtureObject: Decodable {
        let id: String
        let enabled: Bool
    }

    private var repositoryRoot: URL {
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
    }

    private func fixture() throws -> FixtureDocument {
        let url = repositoryRoot.appendingPathComponent("ui/probe-diagnostics-fixtures.json")
        return try JSONDecoder().decode(FixtureDocument.self, from: Data(contentsOf: url))
    }

    func testSharedProbeFixtureContainsOnlyBackendLatestResultDTOs() throws {
        let document = try fixture()
        XCTAssertEqual(document.schemaVersion, 2)
        XCTAssertTrue(document.capability["overview"]?.contains("does not prove a particular outbound") == true)
        XCTAssertEqual(document.ordinaryUI.latestPerScopeObjectKind, 1)
        XCTAssertEqual(Set(document.ordinaryUI.requiredFacts), Set(["tested_at", "ok", "core_metric", "stale"]))
        XCTAssertTrue(document.ordinaryUI.forbiddenFragments.contains("probe.example"))
        XCTAssertEqual(document.objects.nodes.map(\.enabled), [true, false])
        XCTAssertEqual(document.objects.routes.map(\.enabled), [true, false])
        XCTAssertEqual(document.objects.subscriptions.map(\.enabled), [true, false])
        XCTAssertEqual(document.probeResults.latestResults.map(\.scope), ["overview", "nodes", "routes"])
        XCTAssertEqual(document.diagnostics.dnsCapture?.mode, "native_hijack_with_local_shim")
        XCTAssertEqual(document.diagnostics.dnsCapture?.activeGeneration, "generation-a")
        XCTAssertEqual(document.diagnostics.dnsCapture?.configured, true)
        XCTAssertTrue(document.diagnostics.dnsCapture?.detail.contains("port-53 capture artifacts") == true)

        let overview = document.probeResults.latestResults[0]
        XCTAssertTrue(overview.ok)
        XCTAssertFalse(overview.stale)
        XCTAssertEqual(overview.summary, "21 ms")
        XCTAssertEqual(overview.errorSummary, "")
        let download = document.probeResults.latestResults[1]
        XCTAssertEqual(download.summary, "16.0 Mbps")
        let failure = document.probeResults.latestResults[2]
        XCTAssertFalse(failure.ok)
        XCTAssertTrue(failure.stale)
        XCTAssertEqual(failure.errorSummary, "连接超时")

    }
}
