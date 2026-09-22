// SPDX-License-Identifier: GPL-3.0-or-later
import XCTest
@testable import SteerApp

@MainActor
final class WireGuardTests: XCTestCase {
    func testImportIsAtomicAndCreatesReferencedRuleBeforeDefault() throws {
        let model = AppModel()
        model.rawJSON = """
        {"routes":[{"id":"direct","kind":"direct","enabled":true}],"rules":[{"id":"default","enabled":true,"default":true,"dns_profile":"dns","route":"direct"}]}
        """
        let imported = WireGuardImportResult(tunnel:.object(["enabled":.bool(true),"address":.array([.string("10.77.0.2/32")]),"private_key":.string("test-only"),"peers":.array([])]),dns:nil,warnings:nil)
        XCTAssertTrue(model.importWireGuard(imported,name:"wg_direct",createRule:true))
        let root = try JSONDecoder().decode(JSONValue.self,from:Data(model.rawJSON.utf8)).objectValue!
        let tunnel = root["wireguard_tunnels"]!.arrayValue![0].objectValue!
        let route = root["routes"]!.arrayValue![1].objectValue!
        let rules = root["rules"]!.arrayValue!
        XCTAssertEqual(route["tunnel"]?.stringValue,tunnel["id"]?.stringValue)
        XCTAssertEqual(rules[0].objectValue?["route"]?.stringValue,route["id"]?.stringValue)
        XCTAssertEqual(rules[0].objectValue?["allowed_ips"]?.boolValue,true)
        XCTAssertNil(rules[0].objectValue?["ip_match"])
        XCTAssertEqual(rules[1].objectValue?["id"]?.stringValue,"default")
        XCTAssertTrue(model.isDirty)
        XCTAssertNotNil(model.deletionBlockReason(for:"wireguard_tunnels",at:0))
    }
    func testImportWithoutDefaultDoesNotPartiallyWrite() throws {
        let model=AppModel();model.rawJSON="{}"
        let imported=WireGuardImportResult(tunnel:.object([:]),dns:nil,warnings:nil)
        XCTAssertFalse(model.importWireGuard(imported,name:"wg",createRule:true))
        XCTAssertEqual(model.rawJSON,"{}")
    }
    func testAllowedIPsHasConnectionOnlySummaryAndIsRemovedFromDefault() {
        let rule:[String:JSONValue] = ["allowed_ips":.bool(true)]
        XCTAssertEqual(SteerUISpec.ruleSummaryTokens(rule),["allowed_ips:1"])
        XCTAssertTrue(SteerUISpec.ruleDNSContinues(rule))
        let updated = RuleDraftPolicy.replacement(for:["default":.bool(true)],proposed:rule)
        XCTAssertNil(updated["allowed_ips"])
    }
}
