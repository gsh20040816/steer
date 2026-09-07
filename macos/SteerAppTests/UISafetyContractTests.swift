// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import XCTest
@testable import SteerApp

final class UISafetyContractTests: XCTestCase {
    private struct ReferenceDocument: Decodable {
        let schemaVersion: Int
        let intent: JSONValue
        let cases: [ReferenceCase]

        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case intent, cases
        }
    }

    private struct ReferenceCase: Decodable {
        let targetCollection: String
        let targetID: String
        let references: [ExpectedReference]

        enum CodingKeys: String, CodingKey {
            case targetCollection = "target_collection"
            case targetID = "target_id"
            case references
        }
    }

    private struct ExpectedReference: Decodable, Equatable {
        let sourceCollection: String
        let sourceObjectType: String
        let sourceID: String
        let field: String

        enum CodingKeys: String, CodingKey {
            case sourceCollection = "source_collection"
            case sourceObjectType = "source_object_type"
            case sourceID = "source_id"
            case field
        }
    }

    private struct RuleDocument: Decodable {
        let schemaVersion: Int
        let cases: [RuleCase]
        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case cases
        }
    }

    private struct RuleCase: Decodable {
        let name: String
        let rule: JSONValue
        let tokens: [String]
        let dnsContinues: Bool
        enum CodingKeys: String, CodingKey {
            case name, rule, tokens
            case dnsContinues = "dns_continues"
        }
    }

    private struct ValidationDocument: Decodable {
        let schemaVersion: Int
        let validation: ValidationResult
        let forbiddenMessageValues: [String]
        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case validation
            case forbiddenMessageValues = "forbidden_message_values"
        }
    }

    private struct FormInputDocument: Decodable {
        let schemaVersion: Int
        let cases: [FormInputCase]
        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case cases
        }
    }

    private struct FormInputCase: Decodable {
        let format: String
        let value: String
        let valid: Bool
    }

    private struct CreationDocument: Decodable {
        let schemaVersion: Int
        let cases: [CreationCase]
        let ambiguousReferences: [String: [JSONValue]]
        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case cases
            case ambiguousReferences = "ambiguous_references"
        }
    }

    private struct CreationCase: Decodable {
        let collection: String
        let id: String
        let overrides: [String: JSONValue]?
        let expected: JSONValue
    }

    private var repositoryRoot: URL {
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
    }

    private func decode<T: Decodable>(_ type: T.Type, _ path: String) throws -> T {
        let data = try Data(contentsOf: repositoryRoot.appendingPathComponent(path))
        return try JSONDecoder().decode(type, from: data)
    }

    func testSharedReferenceGuardContractMatchesSwift() throws {
        let document = try decode(ReferenceDocument.self, "ui/collection-reference-fixtures.json")
        XCTAssertEqual(document.schemaVersion, 1)
        let root = try XCTUnwrap(document.intent.objectValue)
        for fixture in document.cases {
            let actual = SteerUISpec.inboundReferences(
                root: root, targetCollection: fixture.targetCollection, targetID: fixture.targetID
            ).map {
                ExpectedReference(
                    sourceCollection: $0.sourceCollection,
                    sourceObjectType: $0.sourceObjectType,
                    sourceID: $0.sourceID,
                    field: $0.field
                )
            }
            XCTAssertEqual(actual, fixture.references, "reference drift for \(fixture.targetCollection)/\(fixture.targetID)")
        }
    }

    func testSharedRuleSummaryAndDNSBoundaryMatchSwift() throws {
        let document = try decode(RuleDocument.self, "ui/rule-summary-fixtures.json")
        XCTAssertEqual(document.schemaVersion, 1)
        for fixture in document.cases {
            let rule = try XCTUnwrap(fixture.rule.objectValue)
            XCTAssertEqual(SteerUISpec.ruleSummaryTokens(rule), fixture.tokens, fixture.name)
            XCTAssertEqual(SteerUISpec.ruleDNSContinues(rule), fixture.dnsContinues, fixture.name)
        }
        XCTAssertFalse(SteerUISpec.contract.platformCapabilities["macos"]?.sourceMAC ?? true)
        XCTAssertFalse(SteerUISpec.contract.platformCapabilities["macos"]?.sourceMACReason?.isEmpty ?? true)
    }

    func testSharedValidationIssuesPreserveLocationWithoutSecrets() throws {
        let document = try decode(ValidationDocument.self, "ui/validation-issue-fixtures.json")
        XCTAssertEqual(document.schemaVersion, 1)
        let issues = document.validation.errors + document.validation.warnings
        XCTAssertEqual(Set(issues.compactMap(\.objectType)), Set(["node", "route", "rule", "dns_profile", "local_proxy", "subscription"]))
        XCTAssertTrue(issues.allSatisfy { $0.objectID?.isEmpty == false && $0.option?.isEmpty == false })
        let rendered = issues.map { "\($0.code) \($0.message)" }.joined(separator: "\n")
        for secret in document.forbiddenMessageValues { XCTAssertFalse(rendered.contains(secret)) }
    }

    func testValidationWarningGroupsDecodeBoundedOverviewContract() throws {
        let payload = Data(#"""
        {
          "ok": true,
          "errors": [],
          "warnings": [
            {"code":"INSECURE_TLS","object_type":"node","object_id":"private-node","option":"insecure","message":"raw warning"}
          ],
          "warning_groups": [
            {"code":"INSECURE_TLS","object_type":"node","option":"insecure","count":120,"summary":"TLS certificate verification is disabled","destination":"nodes"}
          ]
        }
        """#.utf8)
        let validation = try JSONDecoder().decode(ValidationResult.self, from: payload)
        XCTAssertEqual(validation.warningGroups.count, 1)
        XCTAssertEqual(validation.warningGroups.first?.count, 120)
        XCTAssertEqual(validation.warningGroups.first?.destination, "nodes")

        let legacy = try JSONDecoder().decode(ValidationResult.self, from: Data(#"{"ok":true,"errors":[],"warnings":[]}"#.utf8))
        XCTAssertTrue(legacy.warningGroups.isEmpty, "an older helper remains a safe empty summary")

    }

    func testSharedHighFrequencyFormFormatsAreAvailableToEveryFrontend() throws {
        let document = try decode(FormInputDocument.self, "ui/form-input-fixtures.json")
        XCTAssertEqual(document.schemaVersion, 1)
        XCTAssertFalse(document.cases.isEmpty)
        XCTAssertTrue(document.cases.allSatisfy { SteerUISpec.contract.inputFormats[$0.format] != nil })
        XCTAssertEqual(SteerUISpec.contract.inputFormats["probe_url"]?.schemes, ["https"])
        XCTAssertTrue(SteerUISpec.contract.inputFormats["probe_url"]?.forbidCredentials ?? false)
        XCTAssertEqual(SteerUISpec.contract.inputFormats["positive_duration"]?.pattern, "^[1-9][0-9]*(ms|s|m|h)$")
    }

    @MainActor
    func testSharedCreationDefaultsAutomaticIDsAndDisambiguatedReferences() throws {
        let document = try decode(CreationDocument.self, "ui/creation-policy-fixtures.json")
        XCTAssertEqual(document.schemaVersion, 1)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        for fixture in document.cases {
            let actual = JSONValue.object(SteerUISpec.creationObject(
                for: fixture.collection, id: fixture.id, overrides: fixture.overrides ?? [:]
            ))
            XCTAssertEqual(try encoder.encode(actual), try encoder.encode(fixture.expected), fixture.collection)
        }
        XCTAssertTrue(SteerUISpec.contract.idPolicy.autoGenerate)
        XCTAssertEqual(SteerUISpec.contract.idPolicy.maxLength, 32)
        var customizedNode: [String: JSONValue] = ["type": .string("socks"), "version": .number(2)]
        SteerUISpec.applyNodeType("shadowtls", to: &customizedNode)
        XCTAssertEqual(customizedNode["version"]?.numberValue, 2, "an explicit value must not be overwritten")
        var incompleteNode: [String: JSONValue] = [:]
        SteerUISpec.applyNodeType("shadowtls", to: &incompleteNode)
        XCTAssertEqual(incompleteNode["version"]?.numberValue, 3, "a newly selected type materializes its visible default")

        let model = AppModel()
        var ambiguousRoot: [String: JSONValue] = [
            "main": .object(["schema_version": .number(9)]),
        ]
        for (collection, values) in document.ambiguousReferences {
            ambiguousRoot[collection] = .array(values)
        }
        model.rawJSON = String(decoding: try encoder.encode(JSONValue.object(ambiguousRoot)), as: UTF8.self)
        let items = model.draftItems(for: "nodes")
        XCTAssertEqual(model.draftReferenceLabel(try XCTUnwrap(items.first { $0.identifier == "node-unique" }), in: "nodes"), "Unique")
        let duplicate = model.draftReferenceLabel(try XCTUnwrap(items.first { $0.identifier == "node-a1b2c3" }), in: "nodes")
        XCTAssertTrue(duplicate.contains("Same · a.example:1080 · 同名项 1"), duplicate)
        for (collection, identifier) in [
            ("routes", "route-a1b2c3"), ("dns_profiles", "dns-a1b2c3"),
            ("local_proxies", "proxy-a1b2c3"),
        ] {
            let item = try XCTUnwrap(model.draftItems(for: collection).first { $0.identifier == identifier })
            let label = model.draftReferenceLabel(item, in: collection)
            XCTAssertTrue(label.contains("Same") && label.contains("同名项 1"), label)
        }
    }

    func testNodeFieldDefaultsAreEffectiveWithoutMutatingExistingDrafts() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        var omitted: [String: JSONValue] = ["type": .string("vless")]
        let omittedBefore = try encoder.encode(JSONValue.object(omitted))
        XCTAssertEqual(
            SteerUISpec.effectiveNodeFieldValue(key: "transport", nodeType: "vless", in: omitted)?.stringValue,
            "tcp"
        )
        XCTAssertEqual(try encoder.encode(JSONValue.object(omitted)), omittedBefore)
        XCTAssertNil(omitted["transport"], "reading an editor default must not materialize it")

        omitted["transport"] = .string("")
        let emptyBefore = try encoder.encode(JSONValue.object(omitted))
        XCTAssertEqual(
            SteerUISpec.effectiveNodeFieldValue(key: "transport", nodeType: "vless", in: omitted)?.stringValue,
            "tcp"
        )
        XCTAssertEqual(try encoder.encode(JSONValue.object(omitted)), emptyBefore)

        for explicit in ["tcp", "raw", "ws"] {
            let object: [String: JSONValue] = ["transport": .string(explicit)]
            XCTAssertEqual(
                SteerUISpec.effectiveNodeFieldValue(key: "transport", nodeType: "vless", in: object)?.stringValue,
                explicit,
                "an explicit transport must override the shared default"
            )
        }
    }

    @MainActor
    func testMacOSNewDraftItemsMaterializeTheSharedCanonicalFixture() throws {
        let document = try decode(CreationDocument.self, "ui/creation-policy-fixtures.json")
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let model = AppModel()
        model.rawJSON = String(decoding: try encoder.encode(JSONValue.object([
            "main": .object(["schema_version": .number(9)]),
            "nodes": .array([.object([
                "id": .string("node-existing"), "enabled": .bool(true), "type": .string("socks"),
                "server": .string("127.0.0.1"), "server_port": .number(1080),
            ])]),
            "routes": .array([.object([
                "id": .string("direct"), "enabled": .bool(true), "kind": .string("direct"),
            ])]),
            "dns_profiles": .array([.object([
                "id": .string("dns-existing"), "enabled": .bool(true), "protocol": .string("udp"),
                "server": .string("1.1.1.1"), "server_port": .number(53),
            ])]),
        ])), as: UTF8.self)

        for fixture in document.cases {
            var actual = try XCTUnwrap(model.newDraftItemObject(for: fixture.collection))
            actual["id"] = .string(fixture.id)
            XCTAssertEqual(
                try encoder.encode(JSONValue.object(actual)),
                try encoder.encode(fixture.expected),
                fixture.collection
            )
        }
    }
}
