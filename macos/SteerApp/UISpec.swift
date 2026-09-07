// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation

struct UIChoice: Decodable, Identifiable {
    let value: String
    let label: String
    var id: String { value }
}

struct UIDNSProtocolSpec: Decodable, Identifiable {
    let value: String
    let label: String
    let fields: [String]
    let requiredFields: [String]
    let defaultPort: Int
    var id: String { value }

    enum CodingKeys: String, CodingKey {
        case value, label, fields
        case requiredFields = "required_fields"
        case defaultPort = "default_port"
    }
}

struct UICondition: Decodable {
    let field: String
    let values: [String]
}

struct UIInputFormat: Decodable {
    let kind: String
    let schemes: [String]
    let absolute: Bool
    let forbidCredentials: Bool
    let forbidFragment: Bool
    let positive: Bool
    let prefix: String
    let pattern: String

    enum CodingKeys: String, CodingKey {
        case kind, schemes, absolute, positive, prefix, pattern
        case forbidCredentials = "forbid_credentials"
        case forbidFragment = "forbid_fragment"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = try container.decode(String.self, forKey: .kind)
        schemes = try container.decodeIfPresent([String].self, forKey: .schemes) ?? []
        absolute = try container.decodeIfPresent(Bool.self, forKey: .absolute) ?? false
        forbidCredentials = try container.decodeIfPresent(Bool.self, forKey: .forbidCredentials) ?? false
        forbidFragment = try container.decodeIfPresent(Bool.self, forKey: .forbidFragment) ?? false
        positive = try container.decodeIfPresent(Bool.self, forKey: .positive) ?? false
        prefix = try container.decodeIfPresent(String.self, forKey: .prefix) ?? ""
        pattern = try container.decodeIfPresent(String.self, forKey: .pattern) ?? ""
    }
}

struct UIFieldSpec: Decodable, Identifiable {
    let key: String
    let label: String
    let control: String
    let section: String
    let types: [String]
    let requiredTypes: [String]
    let options: [UIChoice]
    let when: UICondition?
    let sensitive: Bool
    let multiline: Bool
    let placeholder: String
    let defaultValue: JSONValue?

    var id: String { "\(section):\(key)" }

    enum CodingKeys: String, CodingKey {
        case key, label, control, section, types, options, when, sensitive, multiline, placeholder
        case requiredTypes = "required_types"
        case defaultValue = "default"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        key = try container.decode(String.self, forKey: .key)
        label = try container.decode(String.self, forKey: .label)
        control = try container.decode(String.self, forKey: .control)
        section = try container.decode(String.self, forKey: .section)
        types = try container.decodeIfPresent([String].self, forKey: .types) ?? []
        requiredTypes = try container.decodeIfPresent([String].self, forKey: .requiredTypes) ?? []
        options = try container.decodeIfPresent([UIChoice].self, forKey: .options) ?? []
        when = try container.decodeIfPresent(UICondition.self, forKey: .when)
        sensitive = try container.decodeIfPresent(Bool.self, forKey: .sensitive) ?? false
        multiline = try container.decodeIfPresent(Bool.self, forKey: .multiline) ?? false
        placeholder = try container.decodeIfPresent(String.self, forKey: .placeholder) ?? ""
        defaultValue = try container.decodeIfPresent(JSONValue.self, forKey: .defaultValue)
    }

    func isRequired(for nodeType: String) -> Bool {
        requiredTypes.contains(nodeType)
    }
}

struct UIPlatformCapabilities: Decodable {
    let rawEditor: Bool
    let sourceMAC: Bool
    let sourceMACReason: String?
    let systemComponents: Bool

    enum CodingKeys: String, CodingKey {
        case rawEditor = "raw_editor"
        case sourceMAC = "source_mac"
        case sourceMACReason = "source_mac_reason"
        case systemComponents = "system_components"
    }
}

struct UICollectionReference: Decodable {
    let targetCollection: String
    let sourceCollection: String
    let sourceObjectType: String
    let field: String
    let multiple: Bool

    enum CodingKeys: String, CodingKey {
        case targetCollection = "target_collection"
        case sourceCollection = "source_collection"
        case sourceObjectType = "source_object_type"
        case field, multiple
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        targetCollection = try container.decode(String.self, forKey: .targetCollection)
        sourceCollection = try container.decode(String.self, forKey: .sourceCollection)
        sourceObjectType = try container.decode(String.self, forKey: .sourceObjectType)
        field = try container.decode(String.self, forKey: .field)
        multiple = try container.decodeIfPresent(Bool.self, forKey: .multiple) ?? false
    }
}

struct UICollectionOrderingPolicy: Decodable {
    let stableIDField: String
    let moveActions: [String]
    let groupField: String?
    let movableKinds: [String]
    let pinnedLastBooleanField: String?
    let sourceOwnedRefresh: String?

    enum CodingKeys: String, CodingKey {
        case stableIDField = "stable_id_field"
        case moveActions = "move_actions"
        case groupField = "group_field"
        case movableKinds = "movable_kinds"
        case pinnedLastBooleanField = "pinned_last_boolean_field"
        case sourceOwnedRefresh = "source_owned_refresh"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        stableIDField = try container.decode(String.self, forKey: .stableIDField)
        moveActions = try container.decode([String].self, forKey: .moveActions)
        groupField = try container.decodeIfPresent(String.self, forKey: .groupField)
        movableKinds = try container.decodeIfPresent([String].self, forKey: .movableKinds) ?? []
        pinnedLastBooleanField = try container.decodeIfPresent(String.self, forKey: .pinnedLastBooleanField)
        sourceOwnedRefresh = try container.decodeIfPresent(String.self, forKey: .sourceOwnedRefresh)
    }
}

struct UINodeDisplaySortingContract: Decodable {
    let modes: [String]
    let directionModes: [String]
    let defaultDirection: String
    let connectDirection: String
    let downloadDirection: String

    enum CodingKeys: String, CodingKey {
        case modes
        case directionModes = "direction_modes"
        case defaultDirection = "default_direction"
        case connectDirection = "connect_direction"
        case downloadDirection = "download_direction"
    }
}

struct UIObjectReference: Equatable {
    let sourceCollection: String
    let sourceObjectType: String
    let sourceID: String
    let sourceLabel: String
    let field: String
}

struct UINavigationItem: Decodable, Identifiable {
    let key: String
    let label: String
    var id: String { key }
}

struct UINavigationGroup: Decodable, Identifiable {
    let key: String
    let label: String
    let items: [UINavigationItem]
    var id: String { key }
}

struct UIIDPolicy: Decodable {
    let pattern: String
    let maxLength: Int
    let autoGenerate: Bool
    let collectionPrefixes: [String: String]

    enum CodingKeys: String, CodingKey {
        case pattern
        case maxLength = "max_length"
        case autoGenerate = "auto_generate"
        case collectionPrefixes = "collection_prefixes"
    }
}

struct UIDNSBoundary: Decodable {
    let captureMode: String
    let captureScope: String
    let exclusions: [String]
    let bootstrapBoundary: String
    let encryptedDNSBoundary: String
    let diagnosticBoundary: String

    enum CodingKeys: String, CodingKey {
        case exclusions
        case captureMode = "capture_mode"
        case captureScope = "capture_scope"
        case bootstrapBoundary = "bootstrap_boundary"
        case encryptedDNSBoundary = "encrypted_dns_boundary"
        case diagnosticBoundary = "diagnostic_boundary"
    }
}

struct UISubscriptionInventoryContract: Decodable {
    let changesActiveGeneration: Bool
    let unreferencedNodes: String
    let staleReferencedNodes: String
    let notice: String

    enum CodingKeys: String, CodingKey {
        case notice
        case changesActiveGeneration = "changes_active_generation"
        case unreferencedNodes = "unreferenced_nodes"
        case staleReferencedNodes = "stale_referenced_nodes"
    }
}

struct UIContract: Decodable {
    let schemaVersion: Int
    let canonicalSchema: Int
    let subscriptionUpdateIntervalDefault: String
    let idPolicy: UIIDPolicy
    let creationDefaults: [String: [String: JSONValue]]
    let creationRequiredFields: [String: [String]]
    let inputFormats: [String: UIInputFormat]
    let nodeTypes: [UIChoice]
    let nodeFields: [UIFieldSpec]
    let logLevels: [UIChoice]
    let bootstrapProtocols: [UIChoice]
    let bootstrapStrategies: [UIChoice]
    let routeKinds: [UIChoice]
    let dnsProtocols: [UIDNSProtocolSpec]
    let localProxyProtocols: [UIChoice]
    let ruleNetworks: [UIChoice]
    let ruleProtocols: [UIChoice]
    let ruleMatchFields: [String]
    let ruleConnectionOnlyFields: [String]
    let collectionReferences: [UICollectionReference]
    let collectionOrdering: [String: UICollectionOrderingPolicy]
    let nodeDisplaySorting: UINodeDisplaySortingContract
    let domainPrefixes: [String]
    let ipPrefixes: [String]
    let platformCapabilities: [String: UIPlatformCapabilities]
    let navigation: [UINavigationGroup]
    let dnsBoundaries: [String: UIDNSBoundary]
    let subscriptionInventory: UISubscriptionInventoryContract

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case canonicalSchema = "canonical_schema"
        case subscriptionUpdateIntervalDefault = "subscription_update_interval_default"
        case idPolicy = "id_policy"
        case creationDefaults = "creation_defaults"
        case creationRequiredFields = "creation_required_fields"
        case inputFormats = "input_formats"
        case nodeTypes = "node_types"
        case nodeFields = "node_fields"
        case logLevels = "log_levels"
        case bootstrapProtocols = "bootstrap_protocols"
        case bootstrapStrategies = "bootstrap_strategies"
        case routeKinds = "route_kinds"
        case dnsProtocols = "dns_protocols"
        case localProxyProtocols = "local_proxy_protocols"
        case ruleNetworks = "rule_networks"
        case ruleProtocols = "rule_protocols"
        case ruleMatchFields = "rule_match_fields"
        case ruleConnectionOnlyFields = "rule_connection_only_fields"
        case collectionReferences = "collection_references"
        case collectionOrdering = "collection_ordering"
        case nodeDisplaySorting = "node_display_sorting"
        case domainPrefixes = "domain_prefixes"
        case ipPrefixes = "ip_prefixes"
        case platformCapabilities = "platform_capabilities"
        case navigation
        case dnsBoundaries = "dns_boundaries"
        case subscriptionInventory = "subscription_inventory"
    }
}

enum SteerUISpec {
    static let contract: UIContract = {
        guard let data = GeneratedUISpec.document.data(using: .utf8),
              let contract = try? JSONDecoder().decode(UIContract.self, from: data),
              contract.schemaVersion == 1 else {
            fatalError("Generated Steer UI specification is invalid")
        }
        return contract
    }()

    static func orderingPolicy(for collection: String) -> UICollectionOrderingPolicy? {
        contract.collectionOrdering[collection]
    }

    static func isMovable(collection: String, object: [String: JSONValue]) -> Bool {
        guard let policy = orderingPolicy(for: collection),
              object[policy.stableIDField]?.stringValue?.isEmpty == false else { return false }
        if !policy.movableKinds.isEmpty {
            guard let kind = object["kind"]?.stringValue,
                  policy.movableKinds.contains(kind) else { return false }
        }
        if let pinnedField = policy.pinnedLastBooleanField,
           object[pinnedField]?.boolValue == true { return false }
        return true
    }

    static func orderingGroup(collection: String, object: [String: JSONValue]) -> String {
        guard let field = orderingPolicy(for: collection)?.groupField else { return "" }
        return object[field]?.stringValue ?? ""
    }

    static func nodeFields(for nodeType: String, section: String? = nil) -> [UIFieldSpec] {
        contract.nodeFields.filter { field in
            field.types.contains(nodeType) && (section == nil || field.section == section)
        }
    }

    static func effectiveNodeFieldValue(
        _ field: UIFieldSpec,
        in object: [String: JSONValue]
    ) -> JSONValue? {
        if let value = object[field.key] {
            if value.stringValue == "", let defaultValue = field.defaultValue {
                return defaultValue
            }
            return value
        }
        return field.defaultValue
    }

    static func effectiveNodeFieldValue(
        key: String,
        nodeType: String,
        in object: [String: JSONValue]
    ) -> JSONValue? {
        guard let field = nodeFields(for: nodeType).first(where: { $0.key == key }) else {
            return object[key]
        }
        return effectiveNodeFieldValue(field, in: object)
    }

    static func creationObject(
        for collection: String,
        id: String? = nil,
        overrides: [String: JSONValue] = [:]
    ) -> [String: JSONValue] {
        var object = contract.creationDefaults[collection] ?? [:]
        if let id { object["id"] = .string(id) }
        for (key, value) in overrides { object[key] = value }
        return object
    }

    static func dnsProtocol(_ value: String) -> UIDNSProtocolSpec? {
        contract.dnsProtocols.first { $0.value == value }
    }

    static func applyNodeType(_ value: String, to object: inout [String: JSONValue]) {
        object["type"] = .string(value)
        for field in nodeFields(for: value) where object[field.key] == nil {
            if let defaultValue = field.defaultValue { object[field.key] = defaultValue }
        }
    }

    static func normalizeDNSProfile(_ object: inout [String: JSONValue]) {
        guard let protocolSpec = dnsProtocol(object["protocol"]?.stringValue ?? "") else { return }
        let allowed = Set(protocolSpec.fields)
        let conditionalFields = Set(contract.dnsProtocols.flatMap(\.fields))
        for field in conditionalFields where !allowed.contains(field) {
            object.removeValue(forKey: field)
        }
    }

    static func applyDNSProtocol(_ value: String, to object: inout [String: JSONValue]) {
        guard let next = dnsProtocol(value) else { return }
        let previous = dnsProtocol(object["protocol"]?.stringValue ?? "")
        let currentPort = Int(object["server_port"]?.numberValue ?? 0)
        object["protocol"] = .string(next.value)
        if currentPort < 1 || previous.map({ currentPort == $0.defaultPort }) == true {
            object["server_port"] = .number(Double(next.defaultPort))
        }
        normalizeDNSProfile(&object)
    }

    static func ruleSummaryTokens(_ object: [String: JSONValue]) -> [String] {
        if object["default"]?.boolValue == true { return ["default"] }
        return contract.ruleMatchFields.compactMap { field in
            let values = object[field]?.arrayValue ?? []
            guard !values.isEmpty else { return nil }
            if field == "network" || field == "protocol" {
                return "\(field):\(values.compactMap(\.stringValue).joined(separator: "/"))"
            }
            return "\(field):\(values.count)"
        }
    }

    static func ruleDNSContinues(_ object: [String: JSONValue]) -> Bool {
        let populated = contract.ruleMatchFields.filter { !(object[$0]?.arrayValue ?? []).isEmpty }
        return !populated.isEmpty && populated.allSatisfy(contract.ruleConnectionOnlyFields.contains)
    }

    static func inboundReferences(
        root: [String: JSONValue], targetCollection: String, targetID: String
    ) -> [UIObjectReference] {
        if targetCollection == "subscriptions" {
            let owned = Set((root["nodes"]?.arrayValue ?? []).compactMap { value -> String? in
                guard let node = value.objectValue,
                      node["source_subscription"]?.stringValue == targetID else { return nil }
                return node["id"]?.stringValue
            })
            return (root["routes"]?.arrayValue ?? []).compactMap { value in
                guard let route = value.objectValue,
                      let node = route["node"]?.stringValue, owned.contains(node),
                      let id = route["id"]?.stringValue else { return nil }
                return UIObjectReference(
                    sourceCollection: "routes", sourceObjectType: "route", sourceID: id,
                    sourceLabel: route["name"]?.stringValue ?? id, field: "node"
                )
            }
        }
        return contract.collectionReferences
            .filter { $0.targetCollection == targetCollection }
            .flatMap { relation in
                (root[relation.sourceCollection]?.arrayValue ?? []).compactMap { value in
                    guard let source = value.objectValue,
                          let id = source["id"]?.stringValue else { return nil }
                    let matched: Bool
                    if relation.multiple {
                        matched = (source[relation.field]?.arrayValue ?? []).contains { $0.stringValue == targetID }
                    } else {
                        matched = source[relation.field]?.stringValue == targetID
                    }
                    guard matched else { return nil }
                    return UIObjectReference(
                        sourceCollection: relation.sourceCollection,
                        sourceObjectType: relation.sourceObjectType,
                        sourceID: id, sourceLabel: source["name"]?.stringValue ?? id,
                        field: relation.field
                    )
                }
            }
    }
}
