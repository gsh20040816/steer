// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import XCTest
@testable import SteerApp

final class RuntimeApplyRecordTests: XCTestCase {
    private func record(sequence: String, timestamp: String? = nil) throws -> RuntimeApplyRecord {
        var json: [String: Any] = ["sequence": sequence, "result": ["ok": true]]
        if let timestamp { json["timestamp"] = timestamp }
        return try JSONDecoder().decode(RuntimeApplyRecord.self, from: JSONSerialization.data(withJSONObject: json))
    }

    func testLegacyNanosecondsAndMillisecondsResolveToSameDate() throws {
        for sequence in ["1800000000123000000", "1800000000123"] {
            let date = try XCTUnwrap(record(sequence: sequence).appliedAt)
            XCTAssertEqual(date.timeIntervalSince1970, 1_800_000_000.123, accuracy: 0.000001)
        }
    }

    func testExplicitTimestampTakesPriority() throws {
        for timestamp in ["2026-09-16T00:00:00Z", "2026-09-16T08:00:00+08:00", "2026-09-16T00:00:00.123456789Z"] {
            let date = try XCTUnwrap(record(sequence: "1800000000000000000", timestamp: timestamp).appliedAt)
            XCTAssertEqual(date.timeIntervalSince1970, 1_789_516_800, accuracy: 0.124)
        }
    }

    func testMalformedTimestampFallsBackToLegacySequence() throws {
        let date = try XCTUnwrap(record(sequence: "1800000000000000000", timestamp: "invalid").appliedAt)
        XCTAssertEqual(date.timeIntervalSince1970, 1_800_000_000)
    }

    func testUnknownSequencesDoNotProduceDates() throws {
        for sequence in ["", "1", "invalid", "18000000000000", "9999999999999999999", "18000000000000000000", "1.80000000e12"] {
            XCTAssertNil(try record(sequence: sequence).appliedAt, sequence)
        }
    }
}
