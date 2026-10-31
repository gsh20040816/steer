// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import XCTest
@testable import SteerApp

final class SubscriptionStatusTests: XCTestCase {
    private struct FixtureDocument: Decodable {
        let schemaVersion: Int
        let subscriptionNodes: SubscriptionNodeInventory
        let cases: [FixtureCase]

        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case subscriptionNodes = "subscription_nodes"
            case cases
        }
    }

    private struct FixtureCase: Decodable {
        let name: String
        let status: SubscriptionRuntimeStatus
    }

    private func fixtures() throws -> FixtureDocument {
        let repositoryRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let url = repositoryRoot.appendingPathComponent("ui/subscription-status-fixtures.json")
        let decoder = JSONDecoder()
        return try decoder.decode(FixtureDocument.self, from: Data(contentsOf: url))
    }

    func testSharedSubscriptionStatusFixtureCoversEveryLifecycleState() throws {
        let document = try fixtures()
        XCTAssertEqual(document.schemaVersion, 1)
        XCTAssertEqual(Set(document.cases.map(\.name)), Set([
            "never-fetched", "success", "success-with-skipped",
            "failed-after-success-with-referenced-stale", "disabled",
        ]))

        let byName = Dictionary(uniqueKeysWithValues: document.cases.map { ($0.name, $0.status) })
        XCTAssertEqual(byName["never-fetched"]?.stateLabel, "未抓取")
        XCTAssertEqual(byName["success"]?.stateLabel, "成功")
        XCTAssertEqual(byName["success-with-skipped"]?.stateLabel, "成功 · 跳过 2")
        XCTAssertEqual(byName["disabled"]?.stateLabel, "已停用")

        let failed = try XCTUnwrap(byName["failed-after-success-with-referenced-stale"])
        XCTAssertEqual(failed.stateLabel, "最近失败")
        XCTAssertEqual(failed.lastSuccess, "2026-08-26T02:00:00Z")
        XCTAssertEqual(failed.lastFailure?.summary, "subscription server returned HTTP 503")
        XCTAssertEqual(failed.inventorySummary, "新增 0 · 当前 1 · 已失效 1 · 已跳过 1")
        XCTAssertEqual(failed.stale[0].referencedBy.first?.id, "proxy")

        // The stored nodes of the same subscriptions, as the read interface lists them.
        XCTAssertEqual(document.subscriptionNodes.subscriptions.map(\.id), document.cases.map(\.status.id))
        for status in document.cases.map(\.status) {
            let nodes = document.subscriptionNodes.subscriptions.first { $0.id == status.id }?.nodes ?? []
            XCTAssertEqual(nodes.count, status.nodeCount, status.id)
            XCTAssertEqual(nodes.filter(\.pinnedStale).map(\.id), status.stale.map(\.id), status.id)
            XCTAssertTrue(nodes.allSatisfy { $0.manualCopy?["id"] == nil && $0.manualCopy?["source_subscription"] == nil })
        }
    }
}
