// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import CryptoKit
import SwiftUI

enum AppPage: String, CaseIterable, Identifiable {
    case overview = "Overview"
    case general = "General"
    case configuration = "Configuration"
    case nodes = "Nodes"
    case routes = "Routes"
    case dns = "DNS"
    case rules = "Rules"
    case subscriptions = "Subscriptions"
    case proxies = "Local Proxies"
    case diagnostics = "Diagnostics"
    case settings = "Settings"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .overview: return "circle.grid.2x2"
        case .general: return "switch.2"
        case .configuration: return "doc.text"
        case .nodes: return "point.3.connected.trianglepath.dotted"
        case .routes: return "arrow.triangle.branch"
        case .dns: return "network"
        case .rules: return "list.number"
        case .subscriptions: return "arrow.down.circle"
        case .proxies: return "rectangle.connected.to.line.below"
        case .diagnostics: return "stethoscope"
        case .settings: return "gearshape"
        }
    }
}

extension JSONValue {
    var objectValue: [String: JSONValue]? {
        guard case let .object(value) = self else { return nil }
        return value
    }

    var stringValue: String? {
        guard case let .string(value) = self else { return nil }
        return value
    }

    var boolValue: Bool? {
        guard case let .bool(value) = self else { return nil }
        return value
    }

    var numberValue: Double? {
        guard case let .number(value) = self else { return nil }
        return value
    }

    var arrayValue: [JSONValue]? {
        guard case let .array(value) = self else { return nil }
        return value
    }
}

struct RuntimeStatus: Decodable, Sendable {
    var healthy = false
    var generationID = ""
    var intentDigest = ""
    var runtimeDigest = ""
    var error = ""
    var lastApply: RuntimeApplyRecord? = nil

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        healthy = try container.decodeIfPresent(Bool.self, forKey: .healthy) ?? false
        generationID = try container.decodeIfPresent(String.self, forKey: .generationID) ?? ""
        intentDigest = try container.decodeIfPresent(String.self, forKey: .intentDigest) ?? ""
        runtimeDigest = try container.decodeIfPresent(String.self, forKey: .runtimeDigest) ?? ""
        error = try container.decodeIfPresent(String.self, forKey: .error) ?? ""
        lastApply = try container.decodeIfPresent(RuntimeApplyRecord.self, forKey: .lastApply)
    }

    enum CodingKeys: String, CodingKey {
        case healthy
        case generationID = "generation_id"
        case intentDigest = "intent_digest"
        case runtimeDigest = "runtime_digest"
        case error
        case lastApply = "last_apply"
    }
}

struct RuntimeApplyRecord: Decodable, Sendable {
    let sequence: String
    let timestamp: String?
    let result: RuntimeApplyResult
}

struct RuntimeApplyResult: Decodable, Sendable {
    let ok: Bool
    let generation: String?
    let activated: Bool?
    let error: String?
}

struct OverviewCounts: Decodable, Sendable {
    var nodes = 0
    var subscriptions = 0
    var routes = 0
    var dnsProfiles = 0
    var localProxies = 0
    var rules = 0

    enum CodingKeys: String, CodingKey {
        case nodes, subscriptions, routes, rules
        case dnsProfiles = "dns_profiles"
        case localProxies = "local_proxies"
    }
}

struct OverviewIntentState: Decodable, Sendable {
    var available = false
    var enabled = false
    var counts = OverviewCounts()
    var validation = ValidationResult(ok: false, errors: [], warnings: [])
}

struct OverviewLifecycleState: Decodable, Sendable {
    var saved = OverviewIntentState()
    var active = RuntimeStatus()
    var pendingApply = false

    enum CodingKeys: String, CodingKey {
        case saved, active
        case pendingApply = "pending_apply"
    }
}

struct ValidationIssue: Codable, Identifiable, Sendable, Equatable {
    var id: String { "\(code):\(objectID ?? ""):\(option ?? "")" }
    let code: String
    let objectType: String?
    let objectID: String?
    let option: String?
    let message: String

    enum CodingKeys: String, CodingKey {
        case code
        case objectType = "object_type"
        case objectID = "object_id"
        case option
        case message
    }
}

struct ValidationWarningGroup: Codable, Identifiable, Sendable, Equatable {
    var id: String { "\(code):\(objectType):\(option)" }
    let code: String
    let objectType: String
    let option: String
    let count: Int
    let summary: String
    let destination: String?

    enum CodingKeys: String, CodingKey {
        case code
        case objectType = "object_type"
        case option, count, summary, destination
    }
}

struct ValidationResult: Codable, Sendable, Equatable {
    let ok: Bool
    let errors: [ValidationIssue]
    let warnings: [ValidationIssue]
    let warningGroups: [ValidationWarningGroup]

    init(ok: Bool, errors: [ValidationIssue], warnings: [ValidationIssue], warningGroups: [ValidationWarningGroup] = []) {
        self.ok = ok
        self.errors = errors
        self.warnings = warnings
        self.warningGroups = warningGroups
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        ok = try container.decode(Bool.self, forKey: .ok)
        errors = try container.decodeIfPresent([ValidationIssue].self, forKey: .errors) ?? []
        warnings = try container.decodeIfPresent([ValidationIssue].self, forKey: .warnings) ?? []
        warningGroups = try container.decodeIfPresent([ValidationWarningGroup].self, forKey: .warningGroups) ?? []
    }

    enum CodingKeys: String, CodingKey {
        case ok, errors, warnings
        case warningGroups = "warning_groups"
    }
}

struct RuntimeVersions {
    var helper = "—"
    var singBox = "—"
    var singBoxTags: [String] = []
    var geoVersion = "—"
    var geoRuleCount: Int? = nil
}

struct ApplyOutcome: Sendable {
    let status: RuntimeStatus
    let saved: Bool
    let applied: Bool
    let revision: String
    let error: String
    let validation: ValidationResult?
}

struct SaveOutcome: Sendable {
    let revision: String
    let validation: ValidationResult
}

struct ConfigurationSnapshot: Sendable {
    let document: String
    let revision: String
}

enum DraftConflictOperation: Sendable, Equatable {
    case save
    case apply
    case subscriptionInventory
}

struct DraftRevisionConflict: Sendable {
    let currentRevision: String
    let operation: DraftConflictOperation
}

enum DraftGuardAction: Sendable, Equatable {
    case reload
    case installSystemComponents
    case terminate

    var title: String {
        switch self {
        case .reload: return "重新载入前处理未保存修改"
        case .installSystemComponents: return "安装或修复前处理未保存修改"
        case .terminate: return "退出前处理未保存修改"
        }
    }

    var explanation: String {
        switch self {
        case .reload:
            return "选择保存会先保存当前工作副本再重新载入；选择丢弃会载入已保存配置。"
        case .installSystemComponents:
            return "选择保存会在继续前保存当前工作副本；选择丢弃会在安装完成后重新载入已保存配置。"
        case .terminate:
            return "选择保存会在保存成功后退出；选择丢弃会直接退出。取消不会改变任何配置。"
        }
    }
}

enum DraftGuardDecision: Sendable, Equatable {
    case save
    case discard
    case cancel
}

struct ProbeLatestResult: Decodable, Sendable, Identifiable {
    let scope: String
    let objectID: String?
    let kind: String
    let testedAt: String
    let ok: Bool
    let stale: Bool
    let summary: String
    let metricValue: Double?
    let errorSummary: String

    var id: String { scope == "overview" ? "overview:\(kind)" : "\(scope):\(objectID ?? ""):\(kind)" }

    enum CodingKeys: String, CodingKey {
        case scope, kind, ok, stale, summary
        case objectID = "object_id"
        case testedAt = "tested_at"
        case errorSummary = "error_summary"
        case metricValue = "metric_value"
    }

    init(
        scope: String, objectID: String?, kind: String, testedAt: String,
        ok: Bool, stale: Bool, summary: String, errorSummary: String, metricValue: Double? = nil
    ) {
        self.scope = scope
        self.objectID = objectID
        self.kind = kind
        self.testedAt = testedAt
        self.ok = ok
        self.stale = stale
        self.summary = summary
        self.metricValue = metricValue
        self.errorSummary = errorSummary
    }

    var localizedTestedAt: String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = formatter.date(from: testedAt) ?? ISO8601DateFormatter().date(from: testedAt)
        guard let date else { return "时间未知" }
        return DateFormatter.localizedString(from: date, dateStyle: .none, timeStyle: .short)
    }

}

enum NodeDisplaySorting {
    static func metric(_ result: ProbeLatestResult?, mode: String) -> Double? {
        guard let result, result.scope == "nodes", result.kind == mode,
              result.ok, !result.stale else { return nil }
        guard let value = result.metricValue, value.isFinite, value >= 0 else { return nil }
        return value
    }

    static func sortedIDs(
        _ originalIDs: [String], mode: String, direction: String,
        latestResults: [ProbeLatestResult]
    ) -> [String] {
        guard mode != "default" else { return originalIDs }
        var results: [String: ProbeLatestResult] = [:]
        for result in latestResults where result.scope == "nodes" && result.kind == mode {
            if let objectID = result.objectID { results[objectID] = result }
        }
        let contract = SteerUISpec.contract.nodeDisplaySorting
        let goodDirection = mode == "connect" ? contract.connectDirection : contract.downloadDirection
        let metricDirection = direction == "worst_first"
            ? (goodDirection == "ascending" ? "descending" : "ascending")
            : goodDirection
        return originalIDs.enumerated().map { index, id in
            (id: id, index: index, metric: metric(results[id], mode: mode))
        }.sorted { left, right in
            switch (left.metric, right.metric) {
            case (.some, .none): return true
            case (.none, .some): return false
            case (.none, .none): return left.index < right.index
            case let (.some(leftMetric), .some(rightMetric)):
                if leftMetric == rightMetric { return left.index < right.index }
                return metricDirection == "ascending" ? leftMetric < rightMetric : leftMetric > rightMetric
            }
        }.map(\.id)
    }
}

struct ProbeLatestPresentation: Identifiable, Sendable {
    let id: String
    let text: String
    let ok: Bool
    let stale: Bool
}

struct ProbeDiagnostics: Decodable, Sendable {
    let dnsCapture: DNSCaptureDiagnostic?
    let warnings: [String]

    enum CodingKeys: String, CodingKey {
        case warnings
        case dnsCapture = "dns_capture"
    }

    static let empty = ProbeDiagnostics(dnsCapture: nil, warnings: [])
}

struct ProbeLatestResults: Decodable, Sendable {
    let latestResults: [ProbeLatestResult]
    let warnings: [String]

    enum CodingKeys: String, CodingKey {
        case latestResults = "latest_results"
        case warnings
    }

    static let empty = ProbeLatestResults(latestResults: [], warnings: [])
}

struct DNSCaptureDiagnostic: Decodable, Sendable {
    let mode: String
    let activeGeneration: String?
    let configured: Bool
    let detail: String

    enum CodingKeys: String, CodingKey {
        case mode, configured, detail
        case activeGeneration = "active_generation"
    }
}

struct SubscriptionFailure: Decodable, Sendable {
    let at: String?
    let summary: String
}

struct SubscriptionReference: Decodable, Sendable, Identifiable {
    let objectType: String
    let id: String
    let name: String?

    enum CodingKeys: String, CodingKey {
        case id, name
        case objectType = "object_type"
    }
}

struct SubscriptionStaleNode: Decodable, Sendable, Identifiable {
    let id: String
    let name: String?
    let referencedBy: [SubscriptionReference]

    enum CodingKeys: String, CodingKey {
        case id, name
        case referencedBy = "referenced_by"
    }
}

struct SubscriptionRuntimeStatus: Decodable, Identifiable, Sendable {
    let id: String
    let name: String?
    let url: String
    let enabled: Bool
    let updateInterval: String?
    let neverFetched: Bool
    let lastSuccess: String?
    let lastFailure: SubscriptionFailure?
    let nodeCount: Int
    let current: Int
    let added: Int
    let skipped: Int
    let stale: [SubscriptionStaleNode]

    enum CodingKeys: String, CodingKey {
        case id, name, url, enabled, current, added, skipped, stale
        case updateInterval = "update_interval"
        case neverFetched = "never_fetched"
        case lastSuccess = "last_success"
        case lastFailure = "last_failure"
        case nodeCount = "node_count"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decodeIfPresent(String.self, forKey: .name)
        url = try container.decode(String.self, forKey: .url)
        enabled = try container.decode(Bool.self, forKey: .enabled)
        updateInterval = try container.decodeIfPresent(String.self, forKey: .updateInterval)
        neverFetched = try container.decodeIfPresent(Bool.self, forKey: .neverFetched) ?? true
        lastSuccess = try container.decodeIfPresent(String.self, forKey: .lastSuccess)
        lastFailure = try container.decodeIfPresent(SubscriptionFailure.self, forKey: .lastFailure)
        nodeCount = try container.decodeIfPresent(Int.self, forKey: .nodeCount) ?? 0
        current = try container.decodeIfPresent(Int.self, forKey: .current) ?? 0
        added = try container.decodeIfPresent(Int.self, forKey: .added) ?? 0
        skipped = try container.decodeIfPresent(Int.self, forKey: .skipped) ?? 0
        stale = try container.decodeIfPresent([SubscriptionStaleNode].self, forKey: .stale) ?? []
    }

    var stateLabel: String {
        if !enabled { return "已停用" }
        if lastFailureIsLatest {
            return neverFetched ? "抓取失败" : "最近失败"
        }
        if neverFetched { return "未抓取" }
        if skipped > 0 { return "成功 · 跳过 \(skipped)" }
        return "成功"
    }

    var inventorySummary: String {
        "新增 \(added) · 当前 \(current) · 已失效 \(stale.count) · 已跳过 \(skipped)"
    }

    private var lastFailureIsLatest: Bool {
        guard let failure = lastFailure else { return false }
        guard let success = lastSuccess, let failureAt = failure.at else { return true }
        let formatter = ISO8601DateFormatter()
        guard let successDate = formatter.date(from: success),
              let failureDate = formatter.date(from: failureAt) else { return true }
        return failureDate > successDate
    }
}

private struct SubscriptionStatusResponse: Decodable {
    let ok: Bool
    let subscriptions: [SubscriptionRuntimeStatus]
}

private struct GeoCatalogResponse: Decodable {
    let kind: String
    let names: [String]
}

private struct GeoManifestSummary: Decodable {
    struct Upstream: Decodable { let version: String }
    let upstream: Upstream
    let rules: [JSONValue]
}

struct SystemComponentFact: Identifiable, Sendable, Equatable {
    enum State: String, Sendable {
        case ready
        case missing
        case outdated
        case inactive
        case invalid
    }

    let id: String
    let label: String
    let path: String
    let state: State
    let detail: String
    let requiredForInstallation: Bool

    init(
        id: String,
        label: String,
        path: String,
        state: State,
        detail: String,
        requiredForInstallation: Bool = true
    ) {
        self.id = id
        self.label = label
        self.path = path
        self.state = state
        self.detail = detail
        self.requiredForInstallation = requiredForInstallation
    }

    var ready: Bool { state == .ready }
}

struct SystemComponentsStatus: Sendable {
    let installed: Bool
    let embeddedInstallerAvailable: Bool
    let embeddedUninstallerAvailable: Bool
    let updateAvailable: Bool
    let hasInstalledArtifacts: Bool
    let facts: [SystemComponentFact]

    init(
        installed: Bool,
        embeddedInstallerAvailable: Bool,
        updateAvailable: Bool,
        embeddedUninstallerAvailable: Bool = false,
        hasInstalledArtifacts: Bool? = nil,
        facts: [SystemComponentFact] = []
    ) {
        self.installed = installed
        self.embeddedInstallerAvailable = embeddedInstallerAvailable
        self.embeddedUninstallerAvailable = embeddedUninstallerAvailable
        self.updateAvailable = updateAvailable
        self.hasInstalledArtifacts = hasInstalledArtifacts ?? installed
        self.facts = facts
    }

    init(
        facts: [SystemComponentFact],
        embeddedInstallerAvailable: Bool,
        embeddedUninstallerAvailable: Bool,
        hasInstalledArtifacts: Bool
    ) {
        self.facts = facts
        let installationFacts = facts.filter(\.requiredForInstallation)
        self.installed = !installationFacts.isEmpty && installationFacts.allSatisfy(\.ready)
        self.embeddedInstallerAvailable = embeddedInstallerAvailable
        self.embeddedUninstallerAvailable = embeddedUninstallerAvailable
        self.updateAvailable = facts.contains { $0.state == .outdated }
        self.hasInstalledArtifacts = hasInstalledArtifacts
    }

    var issues: [SystemComponentFact] { facts.filter { $0.requiredForInstallation && !$0.ready } }
}

private struct ControlResponse: Decodable {
    let schemaVersion: Int
    let ok: Bool
    let status: RuntimeStatus?
    let saved: Bool?
    let applied: Bool?
    let revision: String?
    let payload: JSONValue?
    let validation: ValidationResult?
    let errorCode: String?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case ok, status, saved, applied, revision, payload, validation, error
        case errorCode = "error_code"
    }
}

indirect enum JSONValue: Codable, Sendable {
    case object([String: JSONValue])
    case array([JSONValue])
    case string(String)
    case number(Double)
    case bool(Bool)
    case null

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null; return }
        if let value = try? container.decode([String: JSONValue].self) { self = .object(value); return }
        if let value = try? container.decode([JSONValue].self) { self = .array(value); return }
        if let value = try? container.decode(String.self) { self = .string(value); return }
        if let value = try? container.decode(Bool.self) { self = .bool(value); return }
        self = .number(try container.decode(Double.self))
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .object(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

enum RuleDraftPolicy {
    static let matchKeys = [
        "inbound", "domain_match", "ip_match", "source_ip_cidr", "source_mac_address",
        "network", "protocol", "port",
    ]

    static func isDefault(_ object: [String: JSONValue]) -> Bool {
        object["default"]?.boolValue == true
    }

    static func replacement(
        for existing: [String: JSONValue],
        proposed: [String: JSONValue]
    ) -> [String: JSONValue] {
        guard isDefault(existing) else {
            var ordinary = proposed
            ordinary["default"] = .bool(false)
            return ordinary
        }

        var pinned = existing
        for key in ["name", "dns_profile", "route"] {
            if let value = proposed[key] {
                pinned[key] = value
            } else {
                pinned.removeValue(forKey: key)
            }
        }
        pinned["enabled"] = .bool(true)
        pinned["default"] = .bool(true)
        for key in matchKeys { pinned.removeValue(forKey: key) }
        return pinned
    }
}

struct NodeImportResult: Decodable, Sendable {
    let nodes: [JSONValue]
    let skipped: Int
    let skippedReasons: [NodeImportSkippedReason]

    enum CodingKeys: String, CodingKey {
        case nodes, skipped
        case skippedReasons = "skipped_reasons"
    }

    init(nodes: [JSONValue], skipped: Int, skippedReasons: [NodeImportSkippedReason] = []) {
        self.nodes = nodes
        self.skipped = skipped
        self.skippedReasons = skippedReasons
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try container.decode([JSONValue].self, forKey: .nodes)
        skipped = try container.decode(Int.self, forKey: .skipped)
        skippedReasons = try container.decodeIfPresent(
            [NodeImportSkippedReason].self,
            forKey: .skippedReasons
        ) ?? []
    }
}

private struct NodeExportResult: Decodable {
    let uri: String
}

struct NodeImportSkippedReason: Decodable, Sendable {
    let scheme: String?
    let code: String
    let parameter: String?
    let detail: String

    enum CodingKeys: String, CodingKey {
        case scheme, code, parameter, detail
    }
}

struct NodeImportPreviewItem: Identifiable {
    let id = UUID()
    var selected = true
    var name: String
    let protocolName: String
    let server: String
    let port: Int
    let tlsVerification: String
    fileprivate let sourceObject: [String: JSONValue]

    init?(value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        sourceObject = object
        name = object["name"]?.stringValue ?? ""
        protocolName = object["type"]?.stringValue ?? "unknown"
        server = object["server"]?.stringValue ?? ""
        port = Int(object["server_port"]?.numberValue ?? 0)

        let tlsProtocols = Set([
            "trojan", "hysteria", "hysteria2", "tuic", "shadowtls", "anytls", "naive", "naive+https",
        ])
        let security = object["security"]?.stringValue ?? ""
        let usesTLS = tlsProtocols.contains(protocolName)
            || ["tls", "reality"].contains(security)
            || object["tls_server_name"]?.stringValue?.isEmpty == false
            || object["reality_public_key"]?.stringValue?.isEmpty == false
            || object["alpn"]?.arrayValue?.isEmpty == false
        if usesTLS {
            tlsVerification = object["insecure"]?.boolValue == true ? "跳过证书验证" : "验证证书"
        } else {
            tlsVerification = "不适用"
        }
    }

    fileprivate func importedObject() -> [String: JSONValue] {
        var object = sourceObject
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedName.isEmpty {
            object.removeValue(forKey: "name")
        } else {
            object["name"] = .string(trimmedName)
        }
        let prefix = SteerUISpec.contract.idPolicy.collectionPrefixes["nodes"] ?? "node"
        object["id"] = .string("\(prefix)-\(UUID().uuidString.lowercased().prefix(8))")
        object.removeValue(forKey: "source_subscription")
        object.removeValue(forKey: "source_fingerprint")
        object.removeValue(forKey: "pinned_stale")
        return object
    }
}

struct NodeImportPreview {
    var items: [NodeImportPreviewItem]
    let skipped: Int
    let skippedReasons: [NodeImportSkippedReason]

    init(items: [NodeImportPreviewItem], skipped: Int, skippedReasons: [NodeImportSkippedReason] = []) {
        self.items = items
        self.skipped = skipped
        self.skippedReasons = skippedReasons
    }

    var skippedSummary: String? {
        skipped == 0 ? nil : "已跳过 \(skipped) 个无法识别或字段不完整的条目"
    }
}

enum BackendClientError: LocalizedError {
    case helperUnavailable
    case invalidResponse
    case validationFailed(ValidationResult)
    case revisionConflict(currentRevision: String)
    case processFailed(String)

    var errorDescription: String? {
        switch self {
        case .helperUnavailable:
            return "未安装 Steer 系统组件；请在“系统”页完成首次安装。"
        case .invalidResponse:
            return "Steer 服务返回了无法识别的信息。"
        case .validationFailed:
            return "配置校验失败，未保存也未应用。"
        case .revisionConflict:
            return "服务器配置已在其他操作中更新；请先处理配置冲突。"
        case let .processFailed(message):
            return message
        }
    }
}

protocol BackendClient: Sendable {
    func componentStatus() async -> SystemComponentsStatus
    func installSystemComponents() async throws
    func uninstallSystemComponents(removeUserData: Bool) async throws
    func validate(document: String) async throws -> ValidationResult
    func loadConfiguration() async throws -> ConfigurationSnapshot
    func save(document: String, expectedRevision: String) async throws -> SaveOutcome
    func apply(document: String, expectedRevision: String) async throws -> ApplyOutcome
    func setEnabled(_ enabled: Bool) async throws -> (ApplyOutcome, ConfigurationSnapshot)
    func status() async throws -> RuntimeStatus
    func overviewState() async throws -> OverviewLifecycleState
    func logs() async throws -> String
    func versions() async throws -> RuntimeVersions
    func parseNodes(document: String) async throws -> NodeImportResult
    func exportNode(node: JSONValue) async throws -> String
    func probe(kind: String, nodeID: String?, routeID: String?, download: Bool) async throws -> ProbeLatestResult
    func diagnostics() async throws -> ProbeDiagnostics
    func probeResults() async throws -> ProbeLatestResults
    func subscriptionStatuses() async throws -> [SubscriptionRuntimeStatus]
    func updateSubscription(id: String) async throws
    func cleanSubscription(id: String, nodeID: String) async throws
    func geoCatalog(kind: String) async throws -> [String]
}

extension BackendClient {
    func setEnabled(_ enabled: Bool) async throws -> (ApplyOutcome, ConfigurationSnapshot) {
        throw BackendClientError.helperUnavailable
    }
    func diagnostics() async throws -> ProbeDiagnostics { .empty }
    func probeResults() async throws -> ProbeLatestResults { .empty }
    func overviewState() async throws -> OverviewLifecycleState {
        let active = try await status()
        return OverviewLifecycleState(active: active)
    }
    func exportNode(node: JSONValue) async throws -> String {
        throw BackendClientError.helperUnavailable
    }
}

struct HelperBackendClient: BackendClient {
    private static let installedHelperPath = "/usr/local/libexec/steer/steer-macos"
    private static let configurationPath = "/Library/Application Support/Steer/config/config.json"
    private static let runtimePlistPath = "/Library/LaunchDaemons/com.steer.steer.plist"
    private static let controlPlistPath = "/Library/LaunchDaemons/com.steer.steer.control.plist"
    private static let subscriptionPlistPath = "/Library/LaunchDaemons/com.steer.steer.subscription.plist"
    private static let installedSingBoxPath = "/usr/local/libexec/steer/sing-box"
    private static let geoManifestPath = "/Library/Application Support/Steer/geodata-seed/manifest.json"
    private static let controlSocketPath = "/var/run/steer/control.sock"

    private let validationHelperURL: URL

    init(helperURL: URL? = nil) {
        validationHelperURL = helperURL
            ?? Self.embeddedInstallerResource("steer-macos")
            ?? URL(fileURLWithPath: Self.installedHelperPath)
    }

    func componentStatus() async -> SystemComponentsStatus {
        let fileManager = FileManager.default
        let savedEnabled = Self.savedConfigurationEnabled()
        let embeddedHelper = Self.embeddedInstallerResource("steer-macos")
        let installerAvailable = embeddedInstallerURL.map {
            fileManager.isExecutableFile(atPath: $0.path)
        } ?? false
        let uninstallerAvailable = embeddedUninstallerURL.map {
            fileManager.isExecutableFile(atPath: $0.path)
        } ?? false

        var facts = [
            Self.fileFact(id: "helper", label: "steer-macos helper", path: Self.installedHelperPath, executable: true),
            Self.fileFact(id: "sing_box", label: "sing-box", path: Self.installedSingBoxPath, executable: true),
            Self.fileFact(id: "runtime_plist", label: "Runtime LaunchDaemon plist", path: Self.runtimePlistPath),
            Self.fileFact(id: "control_plist", label: "Control LaunchDaemon plist", path: Self.controlPlistPath),
            Self.fileFact(id: "subscription_plist", label: "Subscription LaunchDaemon plist", path: Self.subscriptionPlistPath),
            Self.fileFact(id: "config", label: "Canonical configuration", path: Self.configurationPath, readable: true),
            Self.fileFact(id: "geo_seed", label: "Geo seed manifest", path: Self.geoManifestPath),
        ]

        if let embeddedHelper {
            await Self.markVersionMismatch(
                in: &facts, id: "helper", installed: URL(fileURLWithPath: Self.installedHelperPath), embedded: embeddedHelper
            )
        }
        if let embeddedSingBox = Self.embeddedInstallerResource("sing-box") {
            await Self.markVersionMismatch(
                in: &facts, id: "sing_box", installed: URL(fileURLWithPath: Self.installedSingBoxPath), embedded: embeddedSingBox
            )
        }
        for (id, installedPath, resourceName) in [
            ("runtime_plist", Self.runtimePlistPath, "com.steer.steer.plist"),
            ("control_plist", Self.controlPlistPath, "com.steer.steer.control.plist"),
            ("subscription_plist", Self.subscriptionPlistPath, "com.steer.steer.subscription.plist"),
        ] {
            Self.markPayloadMismatch(in: &facts, id: id, installedPath: installedPath, embedded: Self.embeddedInstallerResource(resourceName))
        }
        Self.markPayloadMismatch(
            in: &facts, id: "geo_seed", installedPath: Self.geoManifestPath,
            embedded: Bundle.main.resourceURL?.appendingPathComponent("geodata-seed/manifest.json")
        )

        facts.append(await Self.serviceFact(
            id: "runtime_service",
            label: "Runtime LaunchDaemon",
            launchdLabel: "com.steer.steer",
            requiredForInstallation: false,
            inactiveDetail: savedEnabled == false ? "按停用配置未加载" : "未加载"
        ))
        facts.append(await Self.serviceFact(id: "control_service", label: "Control LaunchDaemon", launchdLabel: "com.steer.steer.control"))
        facts.append(await Self.serviceFact(id: "subscription_service", label: "Subscription LaunchDaemon", launchdLabel: "com.steer.steer.subscription"))
        facts.append(Self.socketFact())

        let programPaths = [
            Self.installedHelperPath, Self.installedSingBoxPath, Self.runtimePlistPath,
            Self.controlPlistPath, Self.subscriptionPlistPath, Self.geoManifestPath, Self.controlSocketPath,
        ]
        let hasArtifacts = programPaths.contains { fileManager.fileExists(atPath: $0) }
        return SystemComponentsStatus(
            facts: facts,
            embeddedInstallerAvailable: installerAvailable,
            embeddedUninstallerAvailable: uninstallerAvailable,
            hasInstalledArtifacts: hasArtifacts
        )
    }

    func installSystemComponents() async throws {
        guard let installer = embeddedInstallerURL,
              FileManager.default.isExecutableFile(atPath: installer.path) else {
            throw BackendClientError.processFailed("当前 App 不包含正式的系统组件 payload；源码开发请运行 macos/scripts/install-launchdaemon.sh。")
        }
        _ = try await Self.executePrivileged(Self.command([installer.path]))
    }

    func uninstallSystemComponents(removeUserData: Bool) async throws {
        guard let uninstaller = embeddedUninstallerURL,
              FileManager.default.isExecutableFile(atPath: uninstaller.path) else {
            throw BackendClientError.processFailed("当前 App 不包含受控系统组件卸载器。")
        }
        var arguments = [uninstaller.path]
        if removeUserData { arguments.append("--remove-user-data") }
        _ = try await Self.executePrivileged(Self.command(arguments))
    }

    func validate(document: String) async throws -> ValidationResult {
        try requireExecutable(validationHelperURL)
        return try await withTemporaryDocument(document) { url in
            let result = try await Self.execute(validationHelperURL, ["validate", "--config", url.path])
            guard let validation = try? JSONDecoder().decode(ValidationResult.self, from: result.stdout) else {
                throw result.error
            }
            return validation
        }
    }

    func loadConfiguration() async throws -> ConfigurationSnapshot {
        let url = URL(fileURLWithPath: Self.configurationPath)
        let output: Data
        do {
            output = try await Task.detached { try Data(contentsOf: url) }.value
        } catch {
            throw BackendClientError.processFailed(
                "无法读取系统配置。请重新运行 macos/scripts/install-launchdaemon.sh 一次以更新只读权限。"
            )
        }
        guard let document = String(data: output, encoding: .utf8) else {
            throw BackendClientError.invalidResponse
        }
        return ConfigurationSnapshot(document: document, revision: Self.configurationRevision(output))
    }

    func save(document: String, expectedRevision: String) async throws -> SaveOutcome {
        let validation = try await validate(document: document)
        guard validation.ok else { throw BackendClientError.validationFailed(validation) }
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        return try await withTemporaryDocument(document) { url in
            let result = try await Self.execute(helper, [
                "control", "--operation", "save", "--input", url.path,
                "--expected-revision", expectedRevision,
            ])
            let response = try Self.decodeControlResponse(result)
            if let validation = response.validation, !validation.ok {
                throw BackendClientError.validationFailed(validation)
            }
            if response.errorCode == "REVISION_CONFLICT" {
                guard let revision = response.revision, !revision.isEmpty else {
                    throw BackendClientError.invalidResponse
                }
                throw BackendClientError.revisionConflict(currentRevision: revision)
            }
            guard response.ok, response.saved == true else {
                throw BackendClientError.processFailed(response.error ?? "Steer control service 拒绝保存配置。")
            }
            guard let revision = response.revision, !revision.isEmpty else {
                throw BackendClientError.invalidResponse
            }
            return SaveOutcome(revision: revision, validation: response.validation ?? validation)
        }
    }

    func apply(document: String, expectedRevision: String) async throws -> ApplyOutcome {
        let validation = try await validate(document: document)
        guard validation.ok else { throw BackendClientError.validationFailed(validation) }
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        return try await withTemporaryDocument(document) { url in
            let result = try await Self.execute(helper, [
                "control", "--operation", "apply", "--input", url.path,
                "--expected-revision", expectedRevision,
            ])
            let response = try Self.decodeControlResponse(result)
            if let validation = response.validation, !validation.ok {
                throw BackendClientError.validationFailed(validation)
            }
            if response.errorCode == "REVISION_CONFLICT" {
                guard let revision = response.revision, !revision.isEmpty else {
                    throw BackendClientError.invalidResponse
                }
                throw BackendClientError.revisionConflict(currentRevision: revision)
            }
            if response.saved != true {
                throw BackendClientError.processFailed(response.error ?? "配置未保存。")
            }
            guard let revision = response.revision, !revision.isEmpty else {
                throw BackendClientError.invalidResponse
            }
            let status: RuntimeStatus
            if let responseStatus = response.status {
                status = responseStatus
            } else {
                status = try await self.status()
            }
            return ApplyOutcome(
                status: status, saved: true, applied: response.applied == true,
                revision: revision, error: response.error ?? "",
                validation: response.validation ?? validation
            )
        }
    }

    func setEnabled(_ enabled: Bool) async throws -> (ApplyOutcome, ConfigurationSnapshot) {
        let response = try await controlOperation(["set-enabled", "--enabled", enabled ? "true" : "false"])
        guard response.saved == true, let revision = response.revision,
              let payload = response.payload, let status = response.status else {
            throw BackendClientError.processFailed(response.error ?? "启用状态未保存")
        }
        let document = String(decoding: try JSONEncoder.pretty.encode(payload), as: UTF8.self)
        return (ApplyOutcome(status: status, saved: true, applied: response.applied == true,
                             revision: revision, error: response.error ?? "", validation: response.validation),
                ConfigurationSnapshot(document: document, revision: revision))
    }

    private func controlOperation(_ arguments: [String]) async throws -> ControlResponse {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["control", "--operation"] + arguments)
        return try Self.decodeControlResponse(result)
    }

    func status() async throws -> RuntimeStatus {
        let response = try await controlOperation(["status"])
        guard response.ok, let status = response.status else {
            throw BackendClientError.processFailed(response.error ?? "状态获取失败")
        }
        return status
    }

    func overviewState() async throws -> OverviewLifecycleState {
        let response = try await controlOperation(["state"])
        guard response.ok, let payload = response.payload else {
            throw BackendClientError.processFailed(response.error ?? "状态获取失败")
        }
        return try JSONDecoder().decode(OverviewLifecycleState.self, from: JSONEncoder().encode(payload))
    }

    func logs() async throws -> String {
        let sources = [
            ("control.error.log", "/Library/Logs/Steer/control.error.log"),
            ("control.log", "/Library/Logs/Steer/control.log"),
            ("sing-box.error.log", "/Library/Logs/Steer/sing-box.error.log"),
            ("sing-box.log", "/Library/Logs/Steer/sing-box.log"),
            ("subscription.error.log", "/Library/Logs/Steer/subscription.error.log"),
            ("subscription.log", "/Library/Logs/Steer/subscription.log"),
        ]
        var sections: [String] = []
        for (name, path) in sources {
            let result = try await Self.execute(
                URL(fileURLWithPath: "/usr/bin/tail"),
                ["-n", "120", path]
            )
            guard result.status == 0 else { continue }
            let body = String(decoding: result.stdout, as: UTF8.self)
                .replacingOccurrences(
                    of: "\u{001B}\\[[0-9;]*m",
                    with: "",
                    options: .regularExpression
                )
                .trimmingCharacters(in: .whitespacesAndNewlines)
            if !body.isEmpty { sections.append("== \(name) ==\n\(body)") }
        }
        return sections.joined(separator: "\n\n")
    }

    func versions() async throws -> RuntimeVersions {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        let singBox = URL(fileURLWithPath: "/usr/local/libexec/steer/sing-box")
        try requireExecutable(helper)
        try requireExecutable(singBox)
        let helperResult = try await Self.execute(helper, ["version"])
        let singBoxResult = try await Self.execute(singBox, ["version", "--name"])
        let singBoxBuild = try await Self.execute(singBox, ["version"])
        let buildText = String(decoding: singBoxBuild.stdout, as: UTF8.self)
        let buildLines = buildText.split(separator: "\n")
        let tagsLine = buildLines.first {
            String($0).trimmingCharacters(in: .whitespaces).lowercased().hasPrefix("tags:")
        }
        let tagsText = tagsLine.flatMap {
            $0.split(separator: ":", maxSplits: 1).last.map(String.init)
        } ?? ""
        let tags = tagsText.split { $0 == "," || $0.isWhitespace }.map(String.init)
        let manifest = try? JSONDecoder().decode(
            GeoManifestSummary.self,
            from: Data(contentsOf: URL(fileURLWithPath: Self.geoManifestPath))
        )
        return RuntimeVersions(
            helper: String(decoding: helperResult.stdout, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines),
            singBox: String(decoding: singBoxResult.stdout, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines),
            singBoxTags: tags,
            geoVersion: manifest?.upstream.version ?? "—",
            geoRuleCount: manifest?.rules.count
        )
    }

    func parseNodes(document: String) async throws -> NodeImportResult {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        return try await withTemporaryDocument(document) { url in
            let result = try await Self.execute(helper, ["parse-nodes", "--input", url.path])
            guard let parsed = try? JSONDecoder().decode(NodeImportResult.self, from: result.stdout) else {
                throw result.status == 0 ? BackendClientError.invalidResponse : result.error
            }
            return parsed
        }
    }

    func exportNode(node: JSONValue) async throws -> String {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let data = try JSONEncoder.pretty.encode(node)
        guard let document = String(data: data, encoding: .utf8) else {
            throw BackendClientError.invalidResponse
        }
        return try await withTemporaryDocument(document) { url in
            let result = try await Self.execute(helper, ["export-node", "--input", url.path])
            guard result.status == 0,
                  let exported = try? JSONDecoder().decode(NodeExportResult.self, from: result.stdout),
                  !exported.uri.isEmpty else {
                throw result.status == 0 ? BackendClientError.invalidResponse : result.error
            }
            return exported.uri
        }
    }

    func probe(kind: String, nodeID: String?, routeID: String?, download: Bool) async throws -> ProbeLatestResult {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        var arguments = ["probe", "--kind", kind]
        if let nodeID { arguments += ["--node", nodeID] }
        if let routeID { arguments += ["--route", routeID] }
        if download { arguments.append("--download") }
        let result = try await Self.execute(helper, arguments)
        guard let report = try? JSONDecoder().decode(ProbeLatestResult.self, from: result.stdout) else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        return report
    }

    func diagnostics() async throws -> ProbeDiagnostics {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["_diagnostics"])
        guard let diagnostics = try? JSONDecoder().decode(ProbeDiagnostics.self, from: result.stdout) else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        return diagnostics
    }

    func probeResults() async throws -> ProbeLatestResults {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["_probe-results"])
        guard let results = try? JSONDecoder().decode(ProbeLatestResults.self, from: result.stdout) else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        return results
    }

    func subscriptionStatuses() async throws -> [SubscriptionRuntimeStatus] {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["subscription", "status"])
        guard let response = try? JSONDecoder().decode(SubscriptionStatusResponse.self, from: result.stdout), response.ok else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        return response.subscriptions
    }

    func updateSubscription(id: String) async throws {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["control", "--operation", "subscription-update", "--id", id])
        let response = try Self.decodeControlResponse(result)
        guard response.ok else { throw BackendClientError.processFailed(response.error ?? "订阅更新失败。") }
    }

    func cleanSubscription(id: String, nodeID: String) async throws {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["control", "--operation", "subscription-clean", "--id", id, "--node", nodeID])
        let response = try Self.decodeControlResponse(result)
        guard response.ok else { throw BackendClientError.processFailed(response.error ?? "失效节点清理失败。") }
    }

    func geoCatalog(kind: String) async throws -> [String] {
        let helper = URL(fileURLWithPath: Self.installedHelperPath)
        try requireExecutable(helper)
        let result = try await Self.execute(helper, ["geo-catalog", "--kind", kind])
        guard let response = try? JSONDecoder().decode(GeoCatalogResponse.self, from: result.stdout) else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        return response.names
    }

    private func requireExecutable(_ url: URL) throws {
        guard FileManager.default.isExecutableFile(atPath: url.path) else {
            throw BackendClientError.helperUnavailable
        }
    }

    private var embeddedInstallerURL: URL? {
        Self.embeddedInstallerResource("install-embedded-payload.sh")
    }

    private var embeddedUninstallerURL: URL? {
        Self.embeddedInstallerResource("uninstall-embedded-payload.sh")
    }

    private static func fileFact(
        id: String,
        label: String,
        path: String,
        executable: Bool = false,
        readable: Bool = false
    ) -> SystemComponentFact {
        let url = URL(fileURLWithPath: path)
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: path) else {
            return SystemComponentFact(id: id, label: label, path: path, state: .missing, detail: "缺失")
        }
        guard attributes[.type] as? FileAttributeType == .typeRegular else {
            return SystemComponentFact(id: id, label: label, path: path, state: .invalid, detail: "不是 regular file")
        }
        if executable && !FileManager.default.isExecutableFile(atPath: url.path) {
            return SystemComponentFact(id: id, label: label, path: path, state: .invalid, detail: "不可执行")
        }
        if readable && !FileManager.default.isReadableFile(atPath: url.path) {
            return SystemComponentFact(id: id, label: label, path: path, state: .invalid, detail: "当前管理员不可读")
        }
        return SystemComponentFact(id: id, label: label, path: path, state: .ready, detail: "就绪")
    }

    private static func replaceFact(
        in facts: inout [SystemComponentFact],
        id: String,
        state: SystemComponentFact.State,
        detail: String
    ) {
        guard let index = facts.firstIndex(where: { $0.id == id }) else { return }
        let current = facts[index]
        facts[index] = SystemComponentFact(
            id: current.id,
            label: current.label,
            path: current.path,
            state: state,
            detail: detail,
            requiredForInstallation: current.requiredForInstallation
        )
    }

    private static func markVersionMismatch(
        in facts: inout [SystemComponentFact],
        id: String,
        installed: URL,
        embedded: URL
    ) async {
        guard facts.first(where: { $0.id == id })?.ready == true else { return }
        guard let installedVersion = try? await execute(installed, ["version"]),
              let embeddedVersion = try? await execute(embedded, ["version"]),
              installedVersion.status == 0, embeddedVersion.status == 0 else {
            replaceFact(in: &facts, id: id, state: .invalid, detail: "无法读取版本")
            return
        }
        if installedVersion.stdout != embeddedVersion.stdout {
            replaceFact(in: &facts, id: id, state: .outdated, detail: "与 App payload 版本不一致")
        }
    }

    private static func markPayloadMismatch(
        in facts: inout [SystemComponentFact],
        id: String,
        installedPath: String,
        embedded: URL?
    ) {
        guard facts.first(where: { $0.id == id })?.ready == true, let embedded,
              let installedData = try? Data(contentsOf: URL(fileURLWithPath: installedPath)),
              let embeddedData = try? Data(contentsOf: embedded) else { return }
        if installedData != embeddedData {
            replaceFact(in: &facts, id: id, state: .outdated, detail: "与 App payload 不一致")
        }
    }

    private static func serviceFact(
        id: String,
        label: String,
        launchdLabel: String,
        requiredForInstallation: Bool = true,
        inactiveDetail: String = "未加载"
    ) async -> SystemComponentFact {
        let result = try? await execute(URL(fileURLWithPath: "/bin/launchctl"), ["print", "system/\(launchdLabel)"])
        return SystemComponentFact(
            id: id,
            label: label,
            path: "system/\(launchdLabel)",
            state: result?.status == 0 ? .ready : .inactive,
            detail: result?.status == 0 ? "已加载" : inactiveDetail,
            requiredForInstallation: requiredForInstallation
        )
    }

    private struct SavedConfigurationState: Decodable {
        struct Main: Decodable { let enabled: Bool? }
        let main: Main?
    }

    private static func savedConfigurationEnabled() -> Bool? {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: configurationPath)),
              let configuration = try? JSONDecoder().decode(SavedConfigurationState.self, from: data) else {
            return nil
        }
        return configuration.main?.enabled
    }

    private static func socketFact() -> SystemComponentFact {
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: controlSocketPath) else {
            return SystemComponentFact(
                id: "control_socket", label: "Control socket", path: controlSocketPath, state: .missing, detail: "缺失"
            )
        }
        let ready = attributes[.type] as? FileAttributeType == .typeSocket
        return SystemComponentFact(
            id: "control_socket", label: "Control socket", path: controlSocketPath,
            state: ready ? .ready : .invalid, detail: ready ? "就绪" : "不是 Unix socket"
        )
    }

    private static func embeddedInstallerResource(_ name: String) -> URL? {
        Bundle.main.resourceURL?
            .appendingPathComponent("Installer", isDirectory: true)
            .appendingPathComponent(name, isDirectory: false)
    }

    private static func decodeControlResponse(_ result: ProcessResult) throws -> ControlResponse {
        guard let response = try? JSONDecoder().decode(ControlResponse.self, from: result.stdout) else {
            throw result.status == 0 ? BackendClientError.invalidResponse : result.error
        }
        guard response.schemaVersion == 1 else {
            throw BackendClientError.invalidResponse
        }
        return response
    }

    static func configurationRevision(_ content: Data) -> String {
        "sha256-" + SHA256.hash(data: content).map { String(format: "%02x", $0) }.joined()
    }

    private func withTemporaryDocument<T>(
        _ document: String,
        operation: (URL) async throws -> T
    ) async throws -> T {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("steer-draft-\(UUID().uuidString).json")
        try Data(document.utf8).write(to: url, options: .atomic)
        defer { try? FileManager.default.removeItem(at: url) }
        return try await operation(url)
    }

    private struct ProcessResult: Sendable {
        let stdout: Data
        let stderr: String
        let status: Int32

        var error: BackendClientError {
            let message = stderr.trimmingCharacters(in: .whitespacesAndNewlines)
            return .processFailed(message.isEmpty ? "steer-macos 执行失败（退出码 \(status)）。" : message)
        }
    }

    private static func execute(_ executable: URL, _ arguments: [String]) async throws -> ProcessResult {
        try await Task.detached {
            let process = Process()
            let stdout = Pipe()
            let stderr = Pipe()
            process.executableURL = executable
            process.arguments = arguments
            process.standardOutput = stdout
            process.standardError = stderr
            try process.run()
            let output = stdout.fileHandleForReading.readDataToEndOfFile()
            let errorOutput = stderr.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            return ProcessResult(
                stdout: output,
                stderr: String(decoding: errorOutput, as: UTF8.self),
                status: process.terminationStatus
            )
        }.value
    }

    private static func executePrivileged(_ shellCommand: String) async throws -> Data {
        let result = try await execute(URL(fileURLWithPath: "/usr/bin/osascript"), [
            "-e", "on run argv",
            "-e", "do shell script (item 1 of argv) with administrator privileges",
            "-e", "end run",
            shellCommand,
        ])
        guard result.status == 0 else { throw result.error }
        return result.stdout
    }

    private static func command(_ arguments: [String]) -> String {
        arguments.map(shellQuote).joined(separator: " ")
    }

    private static func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}

@MainActor
final class AppModel: ObservableObject {
    @Published var selectedPage: AppPage = .overview
    @Published var rawJSON = "{\n  \"main\": {}\n}"
    @Published var runtime = RuntimeStatus()
    @Published private(set) var overviewLifecycle = OverviewLifecycleState()
    @Published var validation: ValidationResult?
    @Published private(set) var validationFocus: ValidationIssue?
    @Published var isDirty = false
    @Published var isBusy = false
    @Published var message = ""
    @Published var diagnosticsLog = ""
    @Published var versions = RuntimeVersions()
    @Published var systemComponentsInstalled = false
    @Published var embeddedInstallerAvailable = false
    @Published var embeddedUninstallerAvailable = false
    @Published var systemComponentsUpdateAvailable = false
    @Published var systemComponentsHaveArtifacts = false
    @Published var systemComponentFacts: [SystemComponentFact] = []
    @Published var subscriptionRuntime: [SubscriptionRuntimeStatus] = []
    @Published private(set) var latestProbeResults: [String: ProbeLatestResult] = [:]
    @Published private(set) var diagnosticsWarnings: [String] = []
    @Published private(set) var diagnosticsDNSCapture: DNSCaptureDiagnostic?
    @Published var geositeNames: [String] = []
    @Published var geoipNames: [String] = []
    @Published private(set) var savedRevision = ""
    @Published private(set) var revisionConflict: DraftRevisionConflict?
    @Published private(set) var pendingDraftAction: DraftGuardAction?
    @Published private(set) var hasInitializedDraft = false
    @Published private(set) var activeProbeKeys: Set<String> = []
    @Published private(set) var isBatchNodeProbeRunning = false
    @Published private(set) var activeSubscriptionOperationIDs: Set<String> = []

    private let backend: BackendClient
    private var draftMutationSequence: UInt64 = 0
    private var cachedDraftDocument: String?
    private var cachedDraftValue: JSONValue?
    private var cachedDraftError: String?
    private var cachedDraftItems: [String: [DraftItem]] = [:]
    private(set) var draftDecodeCount = 0
    private var initialStateLoadInProgress = false
    private var terminationReply: ((Bool) -> Void)?

    init(backend: BackendClient? = nil) {
        self.backend = backend ?? HelperBackendClient()
    }

    var draftEnabled: Bool {
        parseDraft()?.objectValue?["main"]?.objectValue?["enabled"]?.boolValue ?? false
    }

    var canSaveDraft: Bool {
        !isBusy && pendingDraftAction == nil && isDirty
            && savedRevision.isEmpty == false && draftSyntaxError == nil
    }

    var systemComponentsNeedRepair: Bool {
        !systemComponentsInstalled && systemComponentsHaveArtifacts
    }

    var canSaveForPendingDraftAction: Bool {
        !isBusy && isDirty && draftSyntaxError == nil && pendingDraftAction != nil
            && (savedRevision.isEmpty == false
                || (pendingDraftAction == .installSystemComponents && !systemComponentsInstalled))
    }

    var canApplySaved: Bool {
        !isBusy && savedRevision.isEmpty == false && pendingDraftAction == nil
    }

    var canSaveAndApplyDraft: Bool {
        !isBusy && pendingDraftAction == nil
            && savedRevision.isEmpty == false && draftSyntaxError == nil
    }

    var canEditDraft: Bool { !isBusy && pendingDraftAction == nil }

    var savedEnabled: Bool { overviewLifecycle.saved.available ? overviewLifecycle.saved.enabled : draftEnabled }

    var canToggleEnabled: Bool { canApplySaved }

    var runtimeStatusText: String {
        if !runtime.error.isEmpty { return "服务状态异常" }
        return runtime.healthy ? "服务运行正常" : (runtime.generationID.isEmpty ? "服务未运行" : "服务运行异常")
    }

    var enableToggleHelp: String {
        if isBusy { return "保存或应用操作正在进行" }
        if pendingDraftAction != nil { return "请先完成当前工作副本确认" }
        if savedRevision.isEmpty { return "尚未加载已保存配置" }
        return "使用最新已保存配置切换运行状态，保留未保存修改"
    }

    var hasActiveGeneration: Bool {
        runtime.healthy && !runtime.generationID.isEmpty && !runtime.intentDigest.isEmpty
    }

    var draftGuardTitle: String {
        pendingDraftAction?.title ?? "处理未保存修改"
    }

    var draftGuardExplanation: String {
        pendingDraftAction?.explanation ?? ""
    }

    var draftSyntaxError: String? {
        refreshDraftCache()
        return cachedDraftError
    }

    var draftSchemaVersion: Int {
        Int(parseDraft()?.objectValue?["main"]?.objectValue?["schema_version"]?.numberValue ?? 0)
    }

    var draftLogLevel: String {
        parseDraft()?.objectValue?["main"]?.objectValue?["log_level"]?.stringValue ?? "—"
    }

    var draftDNSCacheCapacityLabel: String {
        guard let value = parseDraft()?.objectValue?["main"]?.objectValue?["dns_cache_capacity"]?.numberValue,
              value != 0 else { return "默认" }
        return Int(value).formatted()
    }

    func itemCount(for key: String) -> Int {
        parseDraft()?.objectValue?[key]?.arrayValue?.count ?? 0
    }

    func draftValue(in section: String, key: String) -> JSONValue? {
        parseDraft()?.objectValue?[section]?.objectValue?[key]
    }

    func setDraftValue(in section: String, key: String, value: JSONValue?) {
        guard canEditDraft, var root = parseDraft()?.objectValue else { return }
        var object = root[section]?.objectValue ?? [:]
        if let value {
            object[key] = value
        } else {
            object.removeValue(forKey: key)
        }
        root[section] = .object(object)
        writeDraft(.object(root), context: "\(section).\(key)")
    }

    func enabledItemCount(for key: String) -> Int {
        draftItems(for: key).filter(\.enabled).count
    }

    func markDirty() {
        draftMutationSequence &+= 1
        isDirty = true
        validation = nil
        validationFocus = nil
        message = "工作副本有未保存修改"
    }

    func updateRawDraft(_ document: String) {
        guard canEditDraft else { return }
        rawJSON = document
        markDirty()
    }

    func loadInitialState() {
        guard !hasInitializedDraft, !initialStateLoadInProgress,
              !isBusy, pendingDraftAction == nil else { return }
        initialStateLoadInProgress = true
        isBusy = true
        message = "正在连接 Steer 服务…"
        let documentBeforeLoad = rawJSON
        let sequenceBeforeLoad = draftMutationSequence
        let draftWasDirtyBeforeLoad = isDirty
        Task {
            defer {
                self.initialStateLoadInProgress = false
                self.isBusy = false
            }
            do {
                let components = await self.backend.componentStatus()
                self.updateComponentStatus(components)
                guard components.installed else {
                    self.hasInitializedDraft = true
                    self.message = components.embeddedInstallerAvailable
                        ? "尚未安装系统组件；请在“系统”页完成一次管理员授权安装"
                        : "尚未安装系统组件；当前为源码开发构建"
                    return
                }

                let snapshot = try await self.backend.loadConfiguration()
                let preservedConcurrentDraft = draftWasDirtyBeforeLoad || !self.draftMatches(
                    document: documentBeforeLoad, sequence: sequenceBeforeLoad
                )
                if preservedConcurrentDraft {
                    self.savedRevision = snapshot.revision
                    self.revisionConflict = nil
                    self.isDirty = true
                } else {
                    self.replaceDraft(with: snapshot)
                }
                self.hasInitializedDraft = true
                let lifecycle = try await self.backend.overviewState()
                self.installOverviewLifecycle(lifecycle)
                if !preservedConcurrentDraft {
                    self.validation = lifecycle.saved.validation
                } else {
                    let document = self.rawJSON
                    let sequence = self.draftMutationSequence
                    if let validation = try? await self.backend.validate(document: document),
                       self.draftMatches(document: document, sequence: sequence) {
                        self.validation = validation
                    }
                }
                if let diagnostics = try? await self.backend.diagnostics() {
                    self.installDiagnostics(diagnostics)
                }
                if let probeResults = try? await self.backend.probeResults() {
                    self.installProbeResults(probeResults)
                }
                self.versions = try await self.backend.versions()
                self.subscriptionRuntime = try await self.backend.subscriptionStatuses()
                self.geositeNames = try await self.backend.geoCatalog(kind: "geosite")
                self.geoipNames = try await self.backend.geoCatalog(kind: "geoip")
                if preservedConcurrentDraft {
                    self.message = "已连接服务；载入期间产生的本地修改已保留，尚未保存"
                } else {
                    self.message = self.runtime.healthy
                        ? "Steer 运行正常"
                        : (self.runtime.generationID.isEmpty ? "已连接服务，Steer 当前未运行" : "Steer 运行异常")
                }
            } catch {
                self.runtime.error = "状态获取失败：\(error.localizedDescription)"
                self.message = "连接 Steer 服务失败：\(error.localizedDescription)"
            }
        }
    }

    func installSystemComponents() {
        requestGuardedDraftAction(.installSystemComponents)
    }

    func uninstallSystemComponents(removeUserData: Bool) {
        guard !isBusy else { return }
        perform(message: removeUserData
                ? "正在卸载 Steer 系统组件并删除用户数据；macOS 将请求管理员授权…"
                : "正在卸载 Steer 系统组件；macOS 将请求管理员授权…") {
            try await self.backend.uninstallSystemComponents(removeUserData: removeUserData)
            let components = await self.backend.componentStatus()
            self.updateComponentStatus(components)
            guard !components.installed && !components.hasInstalledArtifacts else {
                throw BackendClientError.processFailed("卸载器已结束，但仍检测到 Steer 程序组件。")
            }
            self.runtime = RuntimeStatus()
            self.overviewLifecycle = OverviewLifecycleState()
            self.versions = RuntimeVersions()
            self.subscriptionRuntime = []
            self.geositeNames = []
            self.geoipNames = []
            if removeUserData {
                self.savedRevision = ""
                self.isDirty = true
                self.message = "系统组件和用户数据已删除；当前工作副本未保存"
            } else {
                self.message = "系统组件已卸载；已保留 /Library/Application Support/Steer/config、state 与 /Library/Logs/Steer"
            }
        }
    }

    func refreshStatus() {
        perform(message: "正在读取运行状态…") {
            async let lifecycle = self.backend.overviewState()
            let components = await self.backend.componentStatus()
            self.updateComponentStatus(components)
            do {
                self.installOverviewLifecycle(try await lifecycle)
            } catch {
                self.runtime.error = "状态获取失败：\(error.localizedDescription)"
                throw error
            }
            self.message = self.runtimeStatusText
        }
    }

    func refreshLogs() {
        perform(message: "正在读取最近日志…") {
            self.diagnosticsLog = try await self.backend.logs()
            self.message = self.diagnosticsLog.isEmpty ? "当前没有日志输出" : "已读取最近日志"
        }
    }

    func refreshDiagnostics() {
        perform(message: "正在刷新诊断数据…") {
            async let lifecycle = self.backend.overviewState()
            async let diagnostics = self.backend.diagnostics()
            async let probeResults = self.backend.probeResults()
            async let logs = self.backend.logs()
            self.installOverviewLifecycle(try await lifecycle)
            self.installDiagnostics(try await diagnostics)
            self.installProbeResults(try await probeResults)
            self.diagnosticsLog = try await logs
            self.message = "诊断数据已刷新"
        }
    }

    func previewNodeImport(_ document: String) async -> NodeImportPreview? {
        guard !isBusy, pendingDraftAction == nil else { return nil }
        isBusy = true
        message = "正在解析节点分享链接…"
        defer { isBusy = false }
        do {
            let result = try await backend.parseNodes(document: document)
            let preview = NodeImportPreview(
                items: result.nodes.compactMap(NodeImportPreviewItem.init(value:)),
                skipped: result.skipped,
                skippedReasons: result.skippedReasons
            )
            message = result.skipped == 0
                ? "已解析 \(preview.items.count) 个节点；确认前不会修改工作副本"
                : "已解析 \(preview.items.count) 个节点；\(preview.skippedSummary ?? "")"
            return preview
        } catch {
            message = "导入节点失败：\(error.localizedDescription)"
            return nil
        }
    }

    func exportNode(_ item: DraftItem) async -> String? {
        guard !isBusy, pendingDraftAction == nil,
              let object = draftItemObject(for: "nodes", at: item.index) else { return nil }
        isBusy = true
        message = "正在导出节点分享链接…"
        defer { isBusy = false }
        do {
            let link = try await backend.exportNode(node: .object(object))
            message = "已生成 \(item.title) 的分享链接"
            return link
        } catch {
            message = "导出节点失败：\(error.localizedDescription)"
            return nil
        }
    }

    @discardableResult
    func confirmNodeImport(_ preview: NodeImportPreview) -> Bool {
        guard !isBusy, pendingDraftAction == nil else { return false }
        let selected = preview.items.filter(\.selected)
        guard !selected.isEmpty else {
            message = "请至少选择一个节点"
            return false
        }
        var imported = false
        mutateCollection("nodes") { values in
            values.append(contentsOf: selected.map { .object($0.importedObject()) })
            imported = true
        }
        guard imported else { return false }
        message = preview.skipped == 0
            ? "已导入 \(selected.count) 个节点到工作副本"
            : "已导入 \(selected.count) 个节点；\(preview.skippedSummary ?? "")"
        return true
    }

    func validate() {
        let document = rawJSON
        let sequence = draftMutationSequence
        perform(message: "正在校验…") {
            let result = try await self.backend.validate(document: document)
            guard self.draftMatches(document: document, sequence: sequence) else {
                self.message = "校验完成，但工作副本已变化；旧结果已丢弃"
                return
            }
            self.validation = result
            self.message = result.ok ? "校验通过" : "校验发现问题"
        }
    }

    func focusValidationIssue(_ issue: ValidationIssue) {
        let page: AppPage?
        switch issue.objectType {
        case "steer", "bootstrap": page = .general
        case "node": page = .nodes
        case "route": page = .routes
        case "dns_profile": page = .dns
        case "local_proxy": page = .proxies
        case "rule": page = .rules
        case "subscription": page = .subscriptions
        default: page = nil
        }
        guard let page else { return }
        validationFocus = issue
        selectedPage = page
    }

    func takeValidationFocus(objectType: String) -> ValidationIssue? {
        guard validationFocus?.objectType == objectType else { return nil }
        let issue = validationFocus
        validationFocus = nil
        return issue
    }

    func saveAndApplyDraft() {
        guard !isBusy, pendingDraftAction == nil, savedRevision.isEmpty == false else { return }
        let document = rawJSON
        let expectedRevision = savedRevision
        let draftSequence = draftMutationSequence
        perform(message: "正在保存并应用配置…") {
            do {
                try await self.applyCurrentDraft(
                    document: document,
                    expectedRevision: expectedRevision,
                    draftSequence: draftSequence
                )
            } catch {
                if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                if self.recordRevisionConflict(error, operation: .apply) { return }
                throw error
            }
        }
    }

    // Compatibility for callers that still use the old name. New UI surfaces use
    // the explicit Save and Apply wording so its persistence side effect is clear.
    func apply() {
        saveAndApplyDraft()
    }

    func applySaved() {
        guard !isBusy, pendingDraftAction == nil else { return }
        let draftWasDirty = isDirty
        let draftDocumentBeforeApply = rawJSON
        let draftSequenceBeforeApply = draftMutationSequence
        perform(message: "正在应用已保存配置…") {
            let snapshot = try await self.backend.loadConfiguration()
            let outcome: ApplyOutcome
            do {
                outcome = try await self.backend.apply(
                    document: snapshot.document,
                    expectedRevision: snapshot.revision
                )
            } catch let error as BackendClientError {
                if case .revisionConflict = error {
                    self.message = "服务器配置在应用前再次变化；运行配置未改变，请重试"
                    return
                }
                if case let .validationFailed(result) = error {
                    if !draftWasDirty,
                       self.draftMatches(document: snapshot.document, sequence: draftSequenceBeforeApply) {
                        self.validation = result
                    }
                    self.message = "已保存配置校验失败：\(result.errors.count) 个错误，运行配置未改变"
                    return
                }
                throw error
            }

            self.runtime = outcome.status
            await self.refreshOverviewLifecycleIfAvailable()
            await self.refreshProbeResultsIfAvailable()
            self.updateComponentStatus(await self.backend.componentStatus())
            guard outcome.saved else {
                throw BackendClientError.processFailed(
                    outcome.error.isEmpty ? "已保存配置未应用" : outcome.error
                )
            }
            if !draftWasDirty,
               self.draftMatches(
                   document: draftDocumentBeforeApply,
                   sequence: draftSequenceBeforeApply
               ) {
                self.replaceDraft(with: ConfigurationSnapshot(
                    document: snapshot.document,
                    revision: outcome.revision
                ))
                self.validation = outcome.validation
            }
            if outcome.applied {
                self.message = self.runtime.healthy
                    ? "已保存配置已应用，Steer 运行正常"
                    : "已保存配置已应用，Steer 已停用"
            } else {
                self.message = "已保存配置应用失败：\(outcome.error.isEmpty ? "运行配置未改变" : outcome.error)"
            }
        }
    }

    func setEnabledAndApply(_ enabled: Bool) {
        guard canToggleEnabled else { return }
        let document = rawJSON
        let sequence = draftMutationSequence
        let wasDirty = isDirty
        perform(message: enabled ? "正在启用 Steer…" : "正在停用 Steer…") {
            let (outcome, snapshot) = try await self.backend.setEnabled(enabled)
            self.runtime = outcome.status
            self.overviewLifecycle.saved.available = true
            self.overviewLifecycle.saved.enabled = enabled
            if !wasDirty, self.draftMatches(document: document, sequence: sequence) {
                self.replaceDraft(with: snapshot)
            }
            // Dirty drafts keep their original revision so a later save cannot
            // overwrite subscription changes that were not present in the draft.
            await self.refreshOverviewLifecycleIfAvailable()
            await self.refreshProbeResultsIfAvailable()
            self.message = outcome.applied
                ? (enabled ? "Steer 已启用" : "Steer 已停用并清理运行资源")
                : "启用状态已保存，但应用失败：\(outcome.error)"
            if self.isDirty { self.message += "；未保存修改已保留" }
        }
    }

    func saveDraft() {
        guard !isBusy, pendingDraftAction == nil, isDirty, savedRevision.isEmpty == false else { return }
        let document = rawJSON
        let expectedRevision = savedRevision
        let draftSequence = draftMutationSequence
        perform(message: "正在保存配置…") {
            do {
                try await self.saveCurrentDraft(
                    document: document,
                    expectedRevision: expectedRevision,
                    draftSequence: draftSequence
                )
            } catch {
                if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                if self.recordRevisionConflict(error, operation: .save) { return }
                throw error
            }
        }
    }

    func loadDraft() {
        requestGuardedDraftAction(.reload)
    }

    func beginTerminationGuard(reply: @escaping (Bool) -> Void) -> Bool {
        guard isDirty else { return false }
        guard !isBusy, pendingDraftAction == nil else {
            message = "当前操作尚未完成，已取消退出"
            return false
        }
        pendingDraftAction = .terminate
        terminationReply = reply
        return true
    }

    func resolveDraftGuard(_ decision: DraftGuardDecision) {
        guard let action = pendingDraftAction else { return }
        pendingDraftAction = nil

        switch decision {
        case .cancel:
            message = "已取消，配置未发生变化"
            finishTerminationIfNeeded(action: action, allow: false)
        case .discard:
            switch action {
            case .reload:
                reloadDraftNow(discardedLocalChanges: true)
            case .installSystemComponents:
                installSystemComponentsNow(decision: .discard)
            case .terminate:
                finishTerminationIfNeeded(action: action, allow: true)
            }
        case .save:
            switch action {
            case .reload:
                saveThenReloadDraft()
            case .installSystemComponents:
                installSystemComponentsNow(decision: .save)
            case .terminate:
                saveBeforeTermination()
            }
        }
    }

    private func requestGuardedDraftAction(_ action: DraftGuardAction) {
        guard !isBusy, pendingDraftAction == nil else { return }
        guard isDirty else {
            switch action {
            case .reload:
                reloadDraftNow(discardedLocalChanges: false)
            case .installSystemComponents:
                installSystemComponentsNow(decision: nil)
            case .terminate:
                break
            }
            return
        }
        pendingDraftAction = action
        message = "当前工作副本有未保存修改；请选择保存、丢弃或取消"
    }

    private func reloadDraftNow(discardedLocalChanges: Bool) {
        let documentBeforeReload = rawJSON
        let sequenceBeforeReload = draftMutationSequence
        perform(message: "正在读取已保存配置…") {
            let snapshot = try await self.backend.loadConfiguration()
            guard self.draftMatches(
                document: documentBeforeReload,
                sequence: sequenceBeforeReload
            ) else {
                self.isDirty = true
                self.message = "重新载入期间产生的新修改已保留；当前工作副本未被替换"
                return
            }
            self.replaceDraft(with: snapshot)
            self.message = discardedLocalChanges
                ? "已丢弃本地修改并重新载入已保存配置"
                : "已读取已保存配置"
        }
    }

    private func saveThenReloadDraft() {
        let document = rawJSON
        let expectedRevision = savedRevision
        let draftSequence = draftMutationSequence
        perform(message: "正在保存并重新载入配置…") {
            do {
                let draftStayedAtSavedVersion = try await self.saveCurrentDraft(
                    document: document,
                    expectedRevision: expectedRevision,
                    draftSequence: draftSequence
                )
                guard draftStayedAtSavedVersion else {
                    self.message = "操作开始时的工作副本已保存；期间产生的新修改已保留，未继续重新载入"
                    return
                }
            } catch {
                if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                if self.recordRevisionConflict(error, operation: .save) { return }
                throw error
            }
            let snapshot = try await self.backend.loadConfiguration()
            guard self.draftMatches(document: document, sequence: draftSequence) else {
                self.isDirty = true
                self.message = "工作副本已保存；重新载入期间产生的新修改已保留"
                return
            }
            self.replaceDraft(with: snapshot)
            self.message = "工作副本已保存并重新载入"
        }
    }

    private func installSystemComponentsNow(decision: DraftGuardDecision?) {
        let document = rawJSON
        let expectedRevision = savedRevision
        let wasInstalled = systemComponentsInstalled
        let draftSequence = draftMutationSequence
        let repairing = systemComponentsNeedRepair || systemComponentsUpdateAvailable
        perform(message: repairing
                ? "正在修复 Steer 系统组件；macOS 将请求一次管理员授权…"
                : "正在安装 Steer 系统组件；macOS 将请求一次管理员授权…") {
            if decision == .save, wasInstalled {
                do {
                    let draftStayedAtSavedVersion = try await self.saveCurrentDraft(
                        document: document,
                        expectedRevision: expectedRevision,
                        draftSequence: draftSequence
                    )
                    guard draftStayedAtSavedVersion else {
                        self.message = "操作开始时的工作副本已保存；期间产生的新修改已保留，未继续安装"
                        return
                    }
                } catch {
                    if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                    if self.recordRevisionConflict(error, operation: .save) { return }
                    throw error
                }
            }

            try await self.backend.installSystemComponents()
            let components = await self.backend.componentStatus()
            self.updateComponentStatus(components)
            guard components.installed else {
                throw BackendClientError.processFailed("安装器已结束，但系统组件验收未通过。")
            }

            let installedSnapshot = try await self.backend.loadConfiguration()
            if decision == .save, !wasInstalled {
                do {
                    try await self.saveCurrentDraft(
                        document: document,
                        expectedRevision: installedSnapshot.revision,
                        draftSequence: draftSequence
                    )
                } catch {
                    if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                    if self.recordRevisionConflict(error, operation: .save) { return }
                    throw error
                }
            } else if decision != .save {
                if self.draftMatches(document: document, sequence: draftSequence) {
                    self.replaceDraft(with: installedSnapshot)
                } else {
                    if !wasInstalled {
                        self.savedRevision = installedSnapshot.revision
                        self.revisionConflict = nil
                    }
                    self.isDirty = true
                }
            }

            self.hasInitializedDraft = true
            self.installOverviewLifecycle(try await self.backend.overviewState())
            self.versions = try await self.backend.versions()
            self.subscriptionRuntime = try await self.backend.subscriptionStatuses()
            self.geositeNames = try await self.backend.geoCatalog(kind: "geosite")
            self.geoipNames = try await self.backend.geoCatalog(kind: "geoip")
            if self.isDirty {
                self.message = "系统组件\(repairing ? "修复" : "安装")完成；操作期间产生的新修改已保留，尚未保存"
                return
            }
            switch decision {
            case .save:
                self.message = "系统组件\(repairing ? "修复" : "安装")完成；当前工作副本已保存，运行配置未改变"
            case .discard:
                self.message = "系统组件\(repairing ? "修复" : "安装")完成；本地修改已丢弃并重新载入已保存配置"
            case .cancel:
                break
            case nil:
                self.message = "系统组件\(repairing ? "修复" : "安装")完成；已载入已保存配置"
            }
        }
    }

    private func saveBeforeTermination() {
        guard !isBusy else {
            finishTerminationIfNeeded(action: .terminate, allow: false)
            return
        }
        let document = rawJSON
        let expectedRevision = savedRevision
        let draftSequence = draftMutationSequence
        isBusy = true
        message = "正在保存工作副本；保存成功后退出…"
        Task {
            var allowTermination = false
            defer {
                self.isBusy = false
                self.finishTerminationIfNeeded(action: .terminate, allow: allowTermination)
            }
            do {
                allowTermination = try await self.saveCurrentDraft(
                    document: document,
                    expectedRevision: expectedRevision,
                    draftSequence: draftSequence
                )
                if !allowTermination {
                    self.message = "退出前的工作副本已保存；操作期间产生的新修改已保留，退出已取消"
                }
            } catch {
                if self.recordValidationFailure(error, document: document, sequence: draftSequence) {
                    return
                }
                if !self.recordRevisionConflict(error, operation: .save) {
                    self.message = "退出前保存失败：\(error.localizedDescription)"
                }
            }
        }
    }

    private func finishTerminationIfNeeded(action: DraftGuardAction, allow: Bool) {
        guard action == .terminate else { return }
        let reply = terminationReply
        terminationReply = nil
        reply?(allow)
    }

    var revisionConflictExplanation: String {
        guard let revisionConflict else { return "" }
        switch revisionConflict.operation {
        case .subscriptionInventory:
            return "订阅节点已更新，当前运行配置未改变；无引用的消失节点已自动删除，仍被路由使用的节点已自动保留并应尽快解除引用。更新期间本地工作副本也发生了修改；重新载入会丢弃本地修改，覆盖保存会保留本地修改。"
        case .apply:
            return "服务器配置已在加载后发生变化。重新载入会丢弃本地修改；覆盖保存会保存并应用当前本地工作副本。"
        case .save:
            return "服务器配置已在加载后发生变化。重新载入会丢弃本地修改；覆盖保存只保存当前本地工作副本。"
        }
    }

    func keepLocalDraftAfterRevisionConflict() {
        guard revisionConflict != nil else { return }
        revisionConflict = nil
        isDirty = true
        message = "已保留本地工作副本；再次保存时仍会检查服务器配置是否变化"
    }

    func reloadSavedAfterRevisionConflict() {
        let documentBeforeReload = rawJSON
        let sequenceBeforeReload = draftMutationSequence
        revisionConflict = nil
        perform(message: "正在重新载入服务器配置…") {
            let snapshot = try await self.backend.loadConfiguration()
            guard self.draftMatches(
                document: documentBeforeReload,
                sequence: sequenceBeforeReload
            ) else {
                self.isDirty = true
                self.message = "重新载入期间产生的新修改已保留"
                return
            }
            self.replaceDraft(with: snapshot)
            self.subscriptionRuntime = try await self.backend.subscriptionStatuses()
            self.message = "已重新载入服务器配置；本地修改已丢弃"
        }
    }

    func overwriteAfterRevisionConflict() {
        guard let conflict = revisionConflict else { return }
        let document = rawJSON
        let draftSequence = draftMutationSequence
        revisionConflict = nil
        switch conflict.operation {
        case .apply:
            perform(message: "正在覆盖保存并应用…") {
                do {
                    try await self.applyCurrentDraft(
                        document: document,
                        expectedRevision: conflict.currentRevision,
                        draftSequence: draftSequence
                    )
                } catch {
                    if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                    if self.recordRevisionConflict(error, operation: .apply) { return }
                    throw error
                }
            }
        case .save, .subscriptionInventory:
            perform(message: "正在覆盖服务器配置…") {
                do {
                    let draftStayedAtSavedVersion = try await self.saveCurrentDraft(
                        document: document,
                        expectedRevision: conflict.currentRevision,
                        draftSequence: draftSequence
                    )
                    if conflict.operation == .subscriptionInventory, draftStayedAtSavedVersion {
                        self.message = "已覆盖服务器配置；当前运行配置未改变"
                    }
                } catch {
                    if self.recordValidationFailure(error, document: document, sequence: draftSequence) { return }
                    if self.recordRevisionConflict(error, operation: conflict.operation) { return }
                    throw error
                }
            }
        }
    }

    func runProbe(kind: String, nodeID: String? = nil, routeID: String? = nil, download: Bool = false) {
        guard pendingDraftAction == nil else { return }
        if (nodeID != nil || routeID != nil), isDirty {
            message = "请先保存或丢弃工作副本中的修改，再测试节点或路由"
            return
        }
        if let nodeID, draftItems(for: "nodes").first(where: { $0.identifier == nodeID })?.enabled == false {
            message = "已停用节点不能测试；请先启用并保存"
            return
        }
        if let routeID, draftItems(for: "routes").first(where: { $0.identifier == routeID })?.enabled == false {
            message = "已停用路由不能测试；请先启用并保存"
            return
        }
        let key = probeKey(kind: kind, nodeID: nodeID, routeID: routeID, download: download)
        let isOverview = nodeID == nil && routeID == nil
        guard activeProbeKeys.insert(key).inserted else { return }
        message = isOverview
            ? "正在使用设备当前网络环境访问测试目标…"
            : (download ? "正在运行下载测速…" : "正在运行连接测试…")
        Task {
            defer { activeProbeKeys.remove(key) }
            do {
                let result = try await backend.probe(kind: kind, nodeID: nodeID, routeID: routeID, download: download)
                latestProbeResults[key] = result
                if isOverview {
                    message = result.ok
                        ? "连通性测试完成：\(result.summary)"
                        : "连通性测试失败：\(result.errorSummary)"
                } else {
                    message = result.ok
                        ? "测试完成：\(result.summary)"
                        : "\(download ? "下载测速" : "连接测试")失败：\(result.errorSummary)"
                }
            } catch {
                if let results = try? await backend.probeResults() {
                    installProbeResults(results)
                }
                message = isOverview
                    ? "连通性测试失败；请检查已保存的测试地址和当前网络，详细原因可查看诊断日志"
                    : (download ? "下载测速失败；详细原因请查看诊断日志" : "连接测试失败；详细原因请查看诊断日志")
            }
        }
    }

    func runAllNodeProbes(download: Bool, nodeIDs: [String]) {
        let enabledIDs = Set(draftItems(for: "nodes").filter(\.enabled).map(\.identifier))
        let eligibleNodeIDs = nodeIDs.filter { enabledIDs.contains($0) }
        guard pendingDraftAction == nil,
              !isDirty, !isBatchNodeProbeRunning, !eligibleNodeIDs.isEmpty else { return }
        isBatchNodeProbeRunning = true
        message = download ? "正在批量下载测速…" : "正在批量连接测试…"
        Task {
            defer { isBatchNodeProbeRunning = false }
            var succeeded = 0
            for nodeID in eligibleNodeIDs {
                let key = probeKey(kind: "speedtest", nodeID: nodeID, routeID: nil, download: download)
                guard activeProbeKeys.insert(key).inserted else { continue }
                do {
                    let result = try await backend.probe(kind: "speedtest", nodeID: nodeID, routeID: nil, download: download)
                    latestProbeResults[key] = result
                    if result.ok { succeeded += 1 }
                } catch {
                    if let results = try? await backend.probeResults() {
                        installProbeResults(results)
                    }
                }
                activeProbeKeys.remove(key)
            }
            message = "批量测试完成：成功 \(succeeded)/\(eligibleNodeIDs.count)"
        }
    }

    func probeInProgress(scope: String, objectID: String, download: Bool) -> Bool {
        activeProbeKeys.contains("\(scope):\(objectID):\(download ? "download" : "connect")")
    }

    func overviewProbeInProgress(_ kind: String) -> Bool {
        activeProbeKeys.contains("overview:\(kind)")
    }

    func overviewProbeSummary(_ kind: String) -> String {
        let key = "overview:\(kind)"
        guard let result = latestProbeResults[key] else { return "未测试" }
        let summary = result.ok ? result.summary : result.errorSummary
        return summary + (result.stale ? " · 已过期" : "")
    }

    func overviewProbeDetail(_ kind: String) -> String? {
        guard let result = latestProbeResults["overview:\(kind)"] else { return nil }
        return "上次 \(result.localizedTestedAt)\(result.stale ? " · 已过期" : "")"
    }

    func overviewProbeIsStale(_ kind: String) -> Bool {
        latestProbeResults["overview:\(kind)"]?.stale == true
    }

    func latestProbePresentation(scope: String, objectID: String, download: Bool) -> ProbeLatestPresentation? {
        let kind = download ? "download" : "connect"
        let key = "\(scope):\(objectID):\(kind)"
        guard let result = latestProbeResults[key] else { return nil }
        let outcome = result.ok ? "成功" : "失败"
        let metric = result.ok ? result.summary : result.errorSummary
        let label = download ? "下载" : "连接"
        return ProbeLatestPresentation(
            id: kind,
            text: "\(label) · 上次 \(result.localizedTestedAt) · \(result.stale ? "已过期 · " : "")\(outcome) · \(metric)",
            ok: result.ok,
            stale: result.stale
        )
    }

    func nodeItemsSortedForDisplay(_ items: [DraftItem], mode: String, direction: String) -> [DraftItem] {
        guard mode != "default" else { return items }
        let orderedIDs = NodeDisplaySorting.sortedIDs(
            items.map(\.identifier), mode: mode, direction: direction,
            latestResults: Array(latestProbeResults.values)
        )
        let byID = Dictionary(uniqueKeysWithValues: items.map { ($0.identifier, $0) })
        return orderedIDs.compactMap { byID[$0] }
    }

    private func installDiagnostics(_ diagnostics: ProbeDiagnostics) {
        diagnosticsWarnings = diagnostics.warnings
        diagnosticsDNSCapture = diagnostics.dnsCapture
    }

    private func installProbeResults(_ results: ProbeLatestResults) {
        diagnosticsWarnings = Array(Set(diagnosticsWarnings + results.warnings)).sorted()
        latestProbeResults = Dictionary(uniqueKeysWithValues: results.latestResults.map { ($0.id, $0) })
    }

    private func probeKey(kind: String, nodeID: String?, routeID: String?, download: Bool) -> String {
        nodeID.map { "nodes:\($0):\(download ? "download" : "connect")" }
            ?? routeID.map { "routes:\($0):\(download ? "download" : "connect")" }
            ?? "overview:\(kind)"
    }

    func updateSubscription(_ id: String) {
        let operationID = "update:\(id)"
        guard pendingDraftAction == nil else { return }
        guard subscriptionStatus(id)?.enabled != false else {
            message = "已停用的订阅不能更新；请先启用并保存配置"
            return
        }
        guard activeSubscriptionOperationIDs.insert(operationID).inserted else { return }
        let startingWasDirty = isDirty
        let startingDraftSequence = draftMutationSequence
        message = "正在更新订阅…"
        Task {
            defer { activeSubscriptionOperationIDs.remove(operationID) }
            do {
                try await backend.updateSubscription(id: id)
                let snapshot = try await backend.loadConfiguration()
                subscriptionRuntime = try await backend.subscriptionStatuses()
                let updateSummary = subscriptionStatus(id)?.inventorySummary ?? "节点库存已更新"
                if !startingWasDirty && draftMutationSequence == startingDraftSequence {
                    replaceDraft(with: snapshot)
                    message = "订阅节点已更新，当前运行配置未改变；无引用的消失节点已自动删除，仍被路由使用的节点已自动保留并应尽快解除引用。\(updateSummary)"
                } else {
                    presentSubscriptionInventoryConflict(snapshot)
                }
            } catch {
                if let refreshed = try? await backend.subscriptionStatuses() {
                    subscriptionRuntime = refreshed
                }
                let summary = subscriptionStatus(id)?.lastFailure?.summary ?? error.localizedDescription
                message = "订阅更新失败：\(summary)"
            }
        }
    }

    func cleanSubscriptionNode(subscriptionID: String, nodeID: String) {
        let operationID = "clean:\(subscriptionID):\(nodeID)"
        guard pendingDraftAction == nil else { return }
        guard activeSubscriptionOperationIDs.insert(operationID).inserted else { return }
        let startingWasDirty = isDirty
        let startingDraftSequence = draftMutationSequence
        message = "正在清理已失效节点…"
        Task {
            defer { activeSubscriptionOperationIDs.remove(operationID) }
            do {
                try await backend.cleanSubscription(id: subscriptionID, nodeID: nodeID)
                let snapshot = try await backend.loadConfiguration()
                subscriptionRuntime = try await backend.subscriptionStatuses()
                if !startingWasDirty && draftMutationSequence == startingDraftSequence {
                    replaceDraft(with: snapshot)
                    message = "已清理失效节点；当前运行配置未改变"
                } else {
                    presentSubscriptionInventoryConflict(snapshot)
                }
            } catch {
                if let refreshed = try? await backend.subscriptionStatuses() {
                    subscriptionRuntime = refreshed
                }
                message = "失效节点清理失败：\(error.localizedDescription)"
            }
        }
    }

    func subscriptionOperationInProgress(_ id: String) -> Bool {
        activeSubscriptionOperationIDs.contains { $0.hasPrefix("update:\(id)") || $0.hasPrefix("clean:\(id):") }
    }

    func subscriptionStatus(_ id: String) -> SubscriptionRuntimeStatus? {
        subscriptionRuntime.first { $0.id == id }
    }

    func draftItems(for key: String) -> [DraftItem] {
        guard let root = parseDraft()?.objectValue else { return [] }
        if let cached = cachedDraftItems[key] { return cached }
        guard
              case let .array(values)? = root[key] else { return [] }
        let items = values.enumerated().map { index, value in
            let object = value.objectValue ?? [:]
            let identifier = object["id"]?.stringValue ?? "item-\(index + 1)"
            let name = object["name"]?.stringValue ?? ""
            let enabled = object["enabled"]?.boolValue ?? true
            let kind = object["type"]?.stringValue
                ?? object["kind"]?.stringValue
                ?? object["protocol"]?.stringValue
                ?? (object["default"]?.boolValue == true ? "default" : "")
            let detail = draftItemDetail(key: key, object: object)
            let sourceSubscription = object["source_subscription"]?.stringValue
            let pinnedStale = object["pinned_stale"]?.boolValue ?? false
            return DraftItem(
                id: "\(key):\(identifier)", index: index, identifier: identifier,
                title: name.isEmpty ? draftItemFallbackTitle(key: key, kind: kind) : name, kind: kind,
                detail: detail, enabled: enabled,
                subscriptionOwned: sourceSubscription?.isEmpty == false,
                sourceSubscription: sourceSubscription?.isEmpty == false ? sourceSubscription : nil,
                pinnedStale: pinnedStale
            )
        }
        cachedDraftItems[key] = items
        return items
    }

    func draftItemObject(for key: String, at index: Int) -> [String: JSONValue]? {
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key], values.indices.contains(index) else { return nil }
        return values[index].objectValue
    }

    func draftReferenceLabel(_ item: DraftItem, in collection: String) -> String {
        let items = draftItems(for: collection)
        let sameTitle = items.filter { $0.title == item.title }
        guard sameTitle.count > 1 else { return item.title }
        var qualifiers: [String] = []
        if !item.detail.isEmpty { qualifiers.append(item.detail) }
        if collection == "nodes", let source = item.sourceSubscription {
            qualifiers.append("订阅 \(referencedTitle(key: "subscriptions", identifier: source))")
        }
        let ordinal = (sameTitle.firstIndex(where: { $0.id == item.id }) ?? 0) + 1
        qualifiers.append("同名项 \(ordinal)")
        return ([item.title] + qualifiers).joined(separator: " · ")
    }

    func nodeReferenceProblem(_ identifier: String) -> String? {
        guard !identifier.isEmpty else { return "未选择节点" }
        guard let node = draftItems(for: "nodes").first(where: { $0.identifier == identifier }) else {
            return "节点不存在"
        }
        return node.enabled ? nil : "节点已停用"
    }

    func routeDetourProblem(routeID: String, detourID: String) -> String? {
        guard !detourID.isEmpty else { return nil }
        guard let detour = draftItems(for: "routes").first(where: { $0.identifier == detourID }) else {
            return "前置路由不存在"
        }
        guard detour.kind == "single" else { return "前置路由必须是单节点路由" }
        guard detour.enabled else { return "前置路由已停用" }
        if let problem = routeChainProblem(startingAt: detourID) { return "前置链无效：\(problem)" }
        return routeDetourWouldCycle(routeID: routeID, detourID: detourID) ? "前置链存在循环引用" : nil
    }

    func routeDetourCandidates(editingRouteID: String) -> [DraftItem] {
        draftItems(for: "routes").filter { route in
            route.kind == "single" && route.enabled && route.identifier != editingRouteID
                && routeDetourProblem(routeID: editingRouteID, detourID: route.identifier) == nil
        }
    }

    func routeDetourWouldCycle(routeID: String, detourID: String) -> Bool {
        guard !routeID.isEmpty, !detourID.isEmpty else { return false }
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root["routes"] else { return false }
        var graph: [String: String] = [:]
        for value in values {
            guard let object = value.objectValue,
                  let identifier = object["id"]?.stringValue else { continue }
            graph[identifier] = object["detour"]?.stringValue ?? ""
        }
        graph[routeID] = detourID
        var visited = Set<String>()
        var current = routeID
        while let next = graph[current], !next.isEmpty {
            guard visited.insert(current).inserted else { return true }
            current = next
        }
        return !visited.insert(current).inserted
    }

    private func routeChainProblem(startingAt identifier: String) -> String? {
        guard let root = parseDraft()?.objectValue,
              case let .array(routeValues)? = root["routes"] else { return "路由不存在" }
        var routes: [String: [String: JSONValue]] = [:]
        for value in routeValues {
            guard let object = value.objectValue, let id = object["id"]?.stringValue else { continue }
            routes[id] = object
        }
        let nodes = Set(draftItems(for: "nodes").filter(\.enabled).map(\.identifier))
        var visited = Set<String>()
        var current = identifier
        while !current.isEmpty {
            guard visited.insert(current).inserted else { return "路由链存在循环引用" }
            guard let route = routes[current] else { return "路由不存在" }
            guard route["kind"]?.stringValue == "single" else { return "前置路由不是单节点路由" }
            guard route["enabled"]?.boolValue ?? true else { return "前置路由已停用" }
            let node = route["node"]?.stringValue ?? ""
            guard nodes.contains(node) else { return "路由节点缺失或已停用" }
            current = route["detour"]?.stringValue ?? ""
        }
        return nil
    }

    func newDraftItemObject(for key: String) -> [String: JSONValue]? {
        defaultItem(for: key).objectValue
    }

    @discardableResult
    func replaceDraftItem(in key: String, at index: Int, object: [String: JSONValue]) -> Bool {
        var replaced = false
        mutateCollection(key) { values in
            guard values.indices.contains(index), let existing = values[index].objectValue else { return }
            if key == "rules" {
                let replacement = RuleDraftPolicy.replacement(for: existing, proposed: object)
                if RuleDraftPolicy.isDefault(existing) {
                    values.remove(at: index)
                    values.append(.object(replacement))
                } else {
                    values[index] = .object(replacement)
                }
            } else {
                values[index] = .object(object)
            }
            replaced = true
        }
        return replaced
    }

    func setDraftItemEnabled(in key: String, at index: Int, enabled: Bool) {
        mutateCollection(key) { values in
            guard values.indices.contains(index), var object = values[index].objectValue else { return }
            if key == "rules", RuleDraftPolicy.isDefault(object) {
                object["enabled"] = .bool(true)
                values[index] = .object(object)
                return
            }
            object["enabled"] = .bool(enabled)
            values[index] = .object(object)
        }
    }

    func draftItemEnabled(in key: String, identifiedBy identifier: String) -> Bool? {
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key],
              let object = values.compactMap(\.objectValue).first(where: {
                  $0["id"]?.stringValue == identifier
              }) else { return nil }
        return object["enabled"]?.boolValue ?? true
    }

    func setDraftItemEnabled(in key: String, identifiedBy identifier: String, enabled: Bool) {
        mutateCollection(key) { values in
            guard let index = values.firstIndex(where: {
                $0.objectValue?["id"]?.stringValue == identifier
            }), var object = values[index].objectValue else { return }
            if key == "rules", RuleDraftPolicy.isDefault(object) {
                object["enabled"] = .bool(true)
            } else {
                object["enabled"] = .bool(enabled)
            }
            values[index] = .object(object)
        }
    }

    func moveDraftItem(in key: String, at index: Int, offset: Int) {
        mutateCollection(key) { values in
            let destination = index + offset
            guard values.indices.contains(index), values.indices.contains(destination) else { return }
            if key == "rules" {
                guard values[index].objectValue.map(RuleDraftPolicy.isDefault) != true,
                      values[destination].objectValue.map(RuleDraftPolicy.isDefault) != true else { return }
            }
            values.swapAt(index, destination)
        }
    }

    @discardableResult
    func moveDraftItem(in key: String, identifiedBy identifier: String, offset: Int, visibleIDs: [String]) -> Bool {
        guard offset == -1 || offset == 1,
              let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key],
              let policy = SteerUISpec.orderingPolicy(for: key) else { return false }
        let idField = policy.stableIDField
        let objects = values.compactMap(\.objectValue)
        guard let source = objects.first(where: { $0[idField]?.stringValue == identifier }),
              SteerUISpec.isMovable(collection: key, object: source) else { return false }
        let sourceGroup = SteerUISpec.orderingGroup(collection: key, object: source)
        let peers = visibleIDs.compactMap { visibleID in
            objects.first(where: { $0[idField]?.stringValue == visibleID })
        }.filter {
            SteerUISpec.isMovable(collection: key, object: $0) &&
                SteerUISpec.orderingGroup(collection: key, object: $0) == sourceGroup
        }
        guard let position = peers.firstIndex(where: { $0[idField]?.stringValue == identifier }),
              peers.indices.contains(position + offset),
              let targetID = peers[position + offset][idField]?.stringValue else { return false }
        mutateCollection(key) { collection in
            guard let sourceIndex = collection.firstIndex(where: { $0.objectValue?[idField]?.stringValue == identifier }),
                  var targetIndex = collection.firstIndex(where: { $0.objectValue?[idField]?.stringValue == targetID }) else { return }
            let moved = collection.remove(at: sourceIndex)
            if sourceIndex < targetIndex { targetIndex -= 1 }
            collection.insert(moved, at: offset < 0 ? targetIndex : targetIndex + 1)
        }
        return true
    }

    @discardableResult
    func moveDraftItem(in key: String, identifiedBy identifier: String, before targetIdentifier: String?) -> Bool {
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key],
              let policy = SteerUISpec.orderingPolicy(for: key) else { return false }
        let idField = policy.stableIDField
        let objects = values.compactMap(\.objectValue)
        guard let source = objects.first(where: { $0[idField]?.stringValue == identifier }),
              SteerUISpec.isMovable(collection: key, object: source) else { return false }
        let sourceGroup = SteerUISpec.orderingGroup(collection: key, object: source)
        let target = targetIdentifier.flatMap { targetID in
            objects.first(where: { $0[idField]?.stringValue == targetID })
        }
        if let target {
            guard SteerUISpec.isMovable(collection: key, object: target),
                  SteerUISpec.orderingGroup(collection: key, object: target) == sourceGroup,
                  target[idField]?.stringValue != identifier else { return false }
        }
        mutateCollection(key) { collection in
            guard let sourceIndex = collection.firstIndex(where: { $0.objectValue?[idField]?.stringValue == identifier }) else { return }
            let moved = collection.remove(at: sourceIndex)
            if let targetIdentifier,
               let targetIndex = collection.firstIndex(where: { $0.objectValue?[idField]?.stringValue == targetIdentifier }) {
                collection.insert(moved, at: targetIndex)
                return
            }
            let lastPeer = collection.lastIndex(where: { value in
                guard let object = value.objectValue else { return false }
                return SteerUISpec.isMovable(collection: key, object: object) &&
                    SteerUISpec.orderingGroup(collection: key, object: object) == sourceGroup
            })
            collection.insert(moved, at: lastPeer.map { $0 + 1 } ?? collection.endIndex)
        }
        return true
    }

    @discardableResult
    func appendDraftItem(to key: String, object: [String: JSONValue]) -> Bool {
        if key == "rules", RuleDraftPolicy.isDefault(object) { return false }
        var appended = false
        mutateCollection(key) { values in
            if key == "rules",
               let defaultIndex = values.firstIndex(where: {
                   $0.objectValue.map(RuleDraftPolicy.isDefault) == true
               }) {
                values.insert(.object(object), at: defaultIndex)
            } else {
                values.append(.object(object))
            }
            appended = true
        }
        return appended
    }

    func removeDraftItem(from key: String, at index: Int) {
        if let reason = deletionBlockReason(for: key, at: index) {
            message = reason
            return
        }
        if key == "rules", let object = draftItemObject(for: key, at: index), RuleDraftPolicy.isDefault(object) {
            message = "Default 规则必须保留"
            return
        }
        if key == "subscriptions",
           var root = parseDraft()?.objectValue,
           case var .array(subscriptions)? = root[key], subscriptions.indices.contains(index),
           let identifier = subscriptions[index].objectValue?["id"]?.stringValue {
            subscriptions.remove(at: index)
            root[key] = .array(subscriptions)
            if case let .array(nodes)? = root["nodes"] {
                root["nodes"] = .array(nodes.filter {
                    $0.objectValue?["source_subscription"]?.stringValue != identifier
                })
            }
            writeDraft(.object(root), context: key)
            return
        }
        mutateCollection(key) { values in
            guard values.indices.contains(index) else { return }
            values.remove(at: index)
        }
    }

    func deletionBlockReason(for key: String, at index: Int) -> String? {
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key], values.indices.contains(index),
              let object = values[index].objectValue,
              let identifier = object["id"]?.stringValue else { return "项目已不存在" }

        switch key {
        case "routes":
            let kind = object["kind"]?.stringValue ?? ""
            if kind == "direct" || kind == "block" { return "Direct 和 Reject 是系统路由，不能删除" }
        case "rules":
            if object["default"]?.boolValue == true { return "Default 规则必须保留" }
        default:
            break
        }
        let references = SteerUISpec.inboundReferences(root: root, targetCollection: key, targetID: identifier)
        if !references.isEmpty {
            return "仍被其他配置使用，请先修改相关引用"
        }
        return nil
    }

    func deletionReferences(for key: String, at index: Int) -> [UIObjectReference] {
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key], values.indices.contains(index),
              let identifier = values[index].objectValue?["id"]?.stringValue else { return [] }
        return SteerUISpec.inboundReferences(root: root, targetCollection: key, targetID: identifier)
    }

    func moveDraftItem(in key: String, from source: IndexSet, to destination: Int) {
        mutateCollection(key) { values in
            guard key == "rules" else {
                values.move(fromOffsets: source, toOffset: destination)
                return
            }
            let defaultIndex = values.firstIndex {
                $0.objectValue.map(RuleDraftPolicy.isDefault) == true
            }
            guard !source.contains(where: { index in
                values.indices.contains(index) && values[index].objectValue.map(RuleDraftPolicy.isDefault) == true
            }) else { return }
            values.move(fromOffsets: source, toOffset: min(destination, defaultIndex ?? values.endIndex))
        }
    }

    private func replaceDraft(with snapshot: ConfigurationSnapshot) {
        rawJSON = snapshot.document
        savedRevision = snapshot.revision
        revisionConflict = nil
        isDirty = false
        validation = nil
        validationFocus = nil
    }

    private func updateComponentStatus(_ components: SystemComponentsStatus) {
        systemComponentsInstalled = components.installed
        embeddedInstallerAvailable = components.embeddedInstallerAvailable
        embeddedUninstallerAvailable = components.embeddedUninstallerAvailable
        systemComponentsUpdateAvailable = components.updateAvailable
        systemComponentsHaveArtifacts = components.hasInstalledArtifacts
        systemComponentFacts = components.facts
    }

    private func installOverviewLifecycle(_ lifecycle: OverviewLifecycleState) {
        overviewLifecycle = lifecycle
        runtime = lifecycle.active
    }

    private func refreshOverviewLifecycleIfAvailable() async {
        do {
            installOverviewLifecycle(try await backend.overviewState())
        } catch {
            runtime.error = "状态刷新失败：\(error.localizedDescription)"
        }
    }

    private func draftMatches(document: String, sequence: UInt64) -> Bool {
        draftMutationSequence == sequence && rawJSON == document
    }

    @discardableResult
    private func adoptSavedRevision(
        _ revision: String,
        document: String,
        draftSequence: UInt64
    ) -> Bool {
        let draftStayedAtSavedVersion = draftMatches(document: document, sequence: draftSequence)
        savedRevision = revision
        revisionConflict = nil
        isDirty = !draftStayedAtSavedVersion
        return draftStayedAtSavedVersion
    }

    @discardableResult
    private func saveCurrentDraft(
        document: String,
        expectedRevision: String,
        draftSequence: UInt64
    ) async throws -> Bool {
        let outcome = try await backend.save(document: document, expectedRevision: expectedRevision)
        let draftStayedAtSavedVersion = adoptSavedRevision(
            outcome.revision,
            document: document,
            draftSequence: draftSequence
        )
        await refreshOverviewLifecycleIfAvailable()
        await refreshProbeResultsIfAvailable()
        if draftStayedAtSavedVersion { validation = outcome.validation }
        message = draftStayedAtSavedVersion
            ? "配置已保存；运行配置未改变"
            : "操作开始时的工作副本已保存；期间产生的新修改仍未保存"
        return draftStayedAtSavedVersion
    }

    @discardableResult
    private func applyCurrentDraft(
        document: String,
        expectedRevision: String,
        draftSequence: UInt64
    ) async throws -> Bool {
        let outcome = try await backend.apply(document: document, expectedRevision: expectedRevision)
        runtime = outcome.status
        await refreshOverviewLifecycleIfAvailable()
        await refreshProbeResultsIfAvailable()
        updateComponentStatus(await backend.componentStatus())
        guard outcome.saved else {
            throw BackendClientError.processFailed(
                outcome.error.isEmpty ? "配置未保存，运行配置未改变" : outcome.error
            )
        }
        let draftStayedAtSavedVersion = adoptSavedRevision(
            outcome.revision,
            document: document,
            draftSequence: draftSequence
        )
        if draftStayedAtSavedVersion, let writeValidation = outcome.validation {
            validation = writeValidation
        }
        if !outcome.applied {
            message = draftStayedAtSavedVersion
                ? "配置已保存，但应用失败：\(outcome.error.isEmpty ? "运行配置未改变" : outcome.error)"
                : "操作开始时的工作副本已保存但应用失败；期间产生的新修改仍未保存"
        } else {
            message = draftStayedAtSavedVersion
                ? (runtime.healthy ? "配置已应用，Steer 运行正常" : "配置已保存，Steer 已停用")
                : "操作开始时的工作副本已应用；期间产生的新修改仍未保存"
        }
        return draftStayedAtSavedVersion
    }

    private func refreshProbeResultsIfAvailable() async {
        if let results = try? await backend.probeResults() {
            installProbeResults(results)
        }
    }

    @discardableResult
    private func recordRevisionConflict(_ error: Error, operation: DraftConflictOperation) -> Bool {
        guard let backendError = error as? BackendClientError,
              case let .revisionConflict(currentRevision) = backendError else { return false }
        revisionConflict = DraftRevisionConflict(currentRevision: currentRevision, operation: operation)
        isDirty = true
        message = "服务器配置已变化；本地工作副本、已保存配置和运行配置均未被此次操作修改"
        return true
    }

    @discardableResult
    private func recordValidationFailure(_ error: Error, document: String, sequence: UInt64) -> Bool {
        guard let backendError = error as? BackendClientError,
              case let .validationFailed(result) = backendError else { return false }
        if draftMatches(document: document, sequence: sequence) {
            validation = result
            validationFocus = nil
            message = "配置校验失败：\(result.errors.count) 个错误，\(result.warnings.count) 个警告；未保存也未应用"
        } else {
            message = "保存时校验失败，但工作副本已变化；旧问题结果已丢弃"
        }
        return true
    }

    private func presentSubscriptionInventoryConflict(_ snapshot: ConfigurationSnapshot) {
        revisionConflict = DraftRevisionConflict(
            currentRevision: snapshot.revision,
            operation: .subscriptionInventory
        )
        isDirty = true
        message = "订阅节点已更新，当前运行配置未改变；无引用的消失节点已自动删除，仍被路由使用的节点已自动保留并应尽快解除引用，更新期间的本地修改也已保留。"
    }

    private func perform(message pendingMessage: String, operation: @escaping () async throws -> Void) {
        guard !isBusy, pendingDraftAction == nil else { return }
        isBusy = true
        message = pendingMessage
        Task {
            defer { isBusy = false }
            do {
                try await operation()
            } catch {
                message = error.localizedDescription
            }
        }
    }

    private func parseDraft() -> JSONValue? {
        refreshDraftCache()
        return cachedDraftValue
    }

    private func refreshDraftCache() {
        guard cachedDraftDocument != rawJSON else { return }
        cachedDraftDocument = rawJSON
        cachedDraftItems.removeAll(keepingCapacity: true)
        draftDecodeCount += 1
        guard let data = rawJSON.data(using: .utf8) else {
            cachedDraftValue = nil
            cachedDraftError = "配置无法编码为 UTF-8"
            return
        }
        do {
            cachedDraftValue = try JSONDecoder().decode(JSONValue.self, from: data)
            cachedDraftError = nil
        } catch {
            cachedDraftValue = nil
            cachedDraftError = error.localizedDescription
        }
    }

    private func mutateCollection(_ key: String, _ mutate: (inout [JSONValue]) -> Void) {
        guard pendingDraftAction == nil else { return }
        guard var root = parseDraft()?.objectValue else {
            message = "当前 JSON 配置格式有误，无法修改此项目"
            return
        }
        var values: [JSONValue]
        if case let .array(existing)? = root[key] { values = existing } else { values = [] }
        mutate(&values)
        root[key] = .array(values)
        writeDraft(.object(root), context: key)
    }

    private func writeDraft(_ value: JSONValue, context: String) {
        guard pendingDraftAction == nil else { return }
        do {
            let data = try JSONEncoder.pretty.encode(value)
            rawJSON = String(decoding: data, as: UTF8.self)
            markDirty()
        } catch {
            message = "写回 \(context) 失败：\(error.localizedDescription)"
        }
    }

    private func defaultItem(for key: String) -> JSONValue {
        let prefix = SteerUISpec.contract.idPolicy.collectionPrefixes[key] ?? "item"
        let id = "\(prefix)-\(UUID().uuidString.lowercased().prefix(8))"
        let firstNode = draftItems(for: "nodes").first(where: \.enabled)?.identifier ?? ""
        let routes = draftItems(for: "routes")
        let firstRoute = routes.first(where: { $0.enabled && $0.kind == "direct" })?.identifier
            ?? routes.first(where: \.enabled)?.identifier ?? ""
        let firstDNS = draftItems(for: "dns_profiles").first(where: \.enabled)?.identifier ?? ""
        var overrides: [String: JSONValue] = [:]
        switch key {
        case "routes":
            overrides["node"] = .string(firstNode)
        case "rules":
            overrides["dns_profile"] = .string(firstDNS)
            overrides["route"] = .string(firstRoute)
        default: break
        }
        return .object(SteerUISpec.creationObject(for: key, id: id, overrides: overrides))
    }

    private func draftItemDetail(key: String, object: [String: JSONValue]) -> String {
        if let server = object["server"]?.stringValue {
            let port = object["server_port"]?.numberValue.map { String(Int($0)) } ?? ""
            return port.isEmpty ? server : "\(server):\(port)"
        }
        if let listen = object["listen"]?.stringValue {
            let port = object["listen_port"]?.numberValue.map { String(Int($0)) } ?? ""
            return port.isEmpty ? listen : "\(listen):\(port)"
        }
        if let url = object["url"]?.stringValue { return url }
        if key == "routes" {
            let kind = object["kind"]?.stringValue ?? ""
            if kind == "direct" { return "系统直连" }
            if kind == "block" { return "拒绝连接" }
            let node = referencedTitle(key: "nodes", identifier: object["node"]?.stringValue)
            let detour = referencedTitle(key: "routes", identifier: object["detour"]?.stringValue)
            if !detour.isEmpty { return "\(detour) → \(node.isEmpty ? "未选择节点" : node)" }
            return node.isEmpty ? "未选择节点" : node
        }
        if key == "rules" {
            let labels = [
                "inbound": "本地入口", "domain_match": "域名", "ip_match": "目标 IP",
                "source_ip_cidr": "源 IP", "source_mac_address": "源 MAC",
                "network": "网络", "protocol": "协议", "port": "端口",
            ]
            var summary = SteerUISpec.ruleSummaryTokens(object).map { token -> String in
                if token == "default" { return "Default" }
                let parts = token.split(separator: ":", maxSplits: 1).map(String.init)
                return "\(labels[parts[0]] ?? parts[0]) \(parts.count > 1 ? parts[1] : "")"
            }
            if summary.isEmpty { summary = ["无匹配条件"] }
            if SteerUISpec.ruleDNSContinues(object) { summary.append("DNS 继续后续规则") }
            let decision = [
                object["dns_profile"]?.stringValue.map { "DNS \(referencedTitle(key: "dns_profiles", identifier: $0))" },
                object["route"]?.stringValue.map { "路由 \(referencedTitle(key: "routes", identifier: $0))" },
            ].compactMap { $0 }.joined(separator: " · ")
            if !decision.isEmpty { summary.append(decision) }
            return summary.joined(separator: " · ")
        }
        let route = object["route"]?.stringValue
        let dns = object["dns_profile"]?.stringValue
        var details: [String] = []
        if let route, !route.isEmpty {
            details.append("路由 \(referencedTitle(key: "routes", identifier: route))")
        }
        if let dns, !dns.isEmpty {
            details.append("DNS \(referencedTitle(key: "dns_profiles", identifier: dns))")
        }
        return details.isEmpty ? "待配置" : details.joined(separator: " · ")
    }

    private func referencedTitle(key: String, identifier: String?) -> String {
        guard let identifier, !identifier.isEmpty else { return "" }
        guard let root = parseDraft()?.objectValue,
              case let .array(values)? = root[key],
              let object = values.compactMap(\.objectValue).first(where: {
                  $0["id"]?.stringValue == identifier
              }) else { return "未命名项目" }
        let name = object["name"]?.stringValue ?? ""
        if !name.isEmpty { return name }
        let kind = object["type"]?.stringValue
            ?? object["kind"]?.stringValue
            ?? object["protocol"]?.stringValue
            ?? (object["default"]?.boolValue == true ? "default" : "")
        return draftItemFallbackTitle(key: key, kind: kind)
    }

    private func draftItemFallbackTitle(key: String, kind: String) -> String {
        if key == "routes" {
            if kind == "direct" { return "Direct" }
            if kind == "block" { return "Reject" }
            return "未命名路由"
        }
        if key == "rules", kind == "default" { return "Default" }
        switch key {
        case "nodes": return "未命名节点"
        case "dns_profiles": return "未命名 DNS Profile"
        case "local_proxies": return "未命名本地代理"
        case "rules": return "未命名规则"
        case "subscriptions": return "未命名订阅"
        default: return "未命名项目"
        }
    }
}

struct DraftItem: Identifiable {
    let id: String
    let index: Int
    let identifier: String
    let title: String
    let kind: String
    let detail: String
    let enabled: Bool
    let subscriptionOwned: Bool
    let sourceSubscription: String?
    let pinnedStale: Bool
}

private extension JSONEncoder {
    static let pretty: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return encoder
    }()
}
