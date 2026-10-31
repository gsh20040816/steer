// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import XCTest
@testable import SteerApp

final class LocalProxyAddressTests: XCTestCase {
    private struct FixtureDocument: Decodable {
        let schemaVersion: Int
        let cases: [FixtureCase]
    }

    private struct FixtureCase: Decodable {
        let name: String
        let listen: String
        let classification: String
    }

    private func fixtures() throws -> FixtureDocument {
        let repositoryRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let url = repositoryRoot.appendingPathComponent("ui/local-proxy-listen-fixtures.json")
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try decoder.decode(FixtureDocument.self, from: Data(contentsOf: url))
    }

    func testSharedListenAddressFixturesMatchSwiftExposureClassification() throws {
        let document = try fixtures()
        XCTAssertEqual(document.schemaVersion, 1)
        for fixture in document.cases {
            let classification = classifyLocalProxyListen(fixture.listen)
            XCTAssertEqual(classification.rawValue, fixture.classification, fixture.name)
        }
    }
}
