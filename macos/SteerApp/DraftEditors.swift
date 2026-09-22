// SPDX-License-Identifier: GPL-3.0-or-later

import Foundation
import SwiftUI
import Darwin
import AppKit

enum LocalProxyListenClassification: String, Codable {
    case loopback
    case nonLoopback = "non_loopback"
    case invalid
}

private func parsedIPv4Bytes(_ value: String) -> [UInt8]? {
    var address = in_addr()
    guard value.withCString({ inet_pton(AF_INET, $0, &address) }) == 1 else { return nil }
    return Swift.withUnsafeBytes(of: &address) { Array($0) }
}

private func parsedIPv6Bytes(_ value: String) -> [UInt8]? {
    var literal = value
    if let zoneStart = literal.firstIndex(of: "%") {
        let zone = literal[literal.index(after: zoneStart)...]
        guard !zone.isEmpty, !zone.contains("%") else { return nil }
        literal = String(literal[..<zoneStart])
    }
    var address = in6_addr()
    guard literal.withCString({ inet_pton(AF_INET6, $0, &address) }) == 1 else { return nil }
    return Swift.withUnsafeBytes(of: &address) { Array($0) }
}

func classifyLocalProxyListen(_ value: String) -> LocalProxyListenClassification {
    if let bytes = parsedIPv4Bytes(value) {
        return bytes.first == 127 ? .loopback : .nonLoopback
    }
    guard let bytes = parsedIPv6Bytes(value) else { return .invalid }
    return bytes.dropLast().allSatisfy { $0 == 0 } && bytes.last == 1 ? .loopback : .nonLoopback
}

func validateLocalProxyAuthentication(listen: String, username: String, password: String) -> String? {
    let classification = classifyLocalProxyListen(listen)
    if classification == .invalid { return "监听地址必须是 IP 地址，不能填写域名" }
    if username.isEmpty != password.isEmpty { return "用户名和密码必须同时填写" }
    if classification == .nonLoopback && username.isEmpty {
        return "该监听地址可能允许其他设备连接，必须设置用户名和密码"
    }
    return nil
}

struct GeoCatalogPresentation: Equatable {
    let category: String
    let attribute: String?
}

func geoCatalogPresentation(_ name: String) -> GeoCatalogPresentation {
    guard let separator = name.firstIndex(of: "@") else {
        return GeoCatalogPresentation(category: name, attribute: nil)
    }
    return GeoCatalogPresentation(
        category: String(name[..<separator]),
        attribute: String(name[name.index(after: separator)...])
    )
}

func geoCatalogMatches(_ catalog: [String], query: String, limit: Int = 40) -> [String] {
    guard limit > 0 else { return [] }
    let terms = query
        .split(whereSeparator: \.isWhitespace)
        .map(String.init)
    var seen = Set<String>()
    var matches: [String] = []
    for name in catalog where seen.insert(name).inserted {
        guard terms.allSatisfy({ name.localizedCaseInsensitiveContains($0) }) else { continue }
        matches.append(name)
        if matches.count == limit { break }
    }
    return matches
}

struct DraftEditorTarget: Identifiable {
    let id = UUID()
    let key: String
    let index: Int?
    let title: String
    let object: [String: JSONValue]
    let focusOption: String?

    init(key: String, index: Int?, title: String, object: [String: JSONValue], focusOption: String? = nil) {
        self.key = key
        self.index = index
        self.title = title
        self.object = object
        self.focusOption = focusOption
    }
}

struct DraftItemEditor: View {
    @ObservedObject var model: AppModel
    let target: DraftEditorTarget
    @Environment(\.dismiss) private var dismiss
    @State private var object: [String: JSONValue]
    @State private var errorMessage = ""

    init(model: AppModel, target: DraftEditorTarget) {
        self.model = model
        self.target = target
        _object = State(initialValue: target.object)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text(target.title)
                        .font(.title2.weight(.semibold))
                    if let option = target.focusOption, !option.isEmpty {
                        Label("已定位字段：\(option)", systemImage: "scope")
                            .font(.caption.monospaced())
                            .foregroundStyle(.orange)
                    }
                }
                Spacer()
            }
            .padding(20)
            Divider()

            Form {
                switch target.key {
                case "nodes":
                    SharedNodeDraftForm(object: $object)
                case "wireguard_tunnels":
                    WireGuardDraftForm(object:$object)
                case "routes":
                    RouteDraftForm(
                        model: model,
                        object: $object,
                        editingIndex: target.index,
                        originalKind: target.object["kind"]?.stringValue ?? ""
                    )
                case "dns_profiles":
                    DNSDraftForm(object: $object)
                case "local_proxies":
                    LocalProxyDraftForm(object: $object)
                case "rules":
                    if target.object["default"]?.boolValue == true {
                        DefaultRuleDraftForm(model: model, object: $object)
                    } else {
                        RuleDraftForm(model: model, object: $object)
                    }
                case "subscriptions":
                    SubscriptionDraftForm(object: $object)
                default:
                    Section {
                        Label("该对象没有可用的原生编辑器。", systemImage: "exclamationmark.triangle")
                    }
                }
            }
            .formStyle(.grouped)

            Divider()
            HStack(spacing: 12) {
                if !errorMessage.isEmpty {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .font(.callout)
                }
                Spacer()
                Button("取消") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("保存到工作副本") { save() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
            }
            .padding(16)
        }
        .frame(minWidth: 760, minHeight: 680)
    }

    private func save() {
        let value = normalizedObject(object, key: target.key)
        if let error = validate(value) {
            errorMessage = error
            return
        }
        let saved: Bool
        if let index = target.index {
            saved = model.replaceDraftItem(in: target.key, at: index, object: value)
        } else {
            saved = model.appendDraftItem(to: target.key, object: value)
        }
        if saved {
            dismiss()
        } else {
            errorMessage = "无法更新工作副本"
        }
    }

    private func validate(_ value: [String: JSONValue]) -> String? {
        let identifier = draftString(value, "id").trimmingCharacters(in: .whitespacesAndNewlines)
        if identifier.isEmpty || identifier.contains(where: { $0.isWhitespace }) {
            return "标识无效；请取消后重新创建"
        }
        if model.draftItems(for: target.key).contains(where: {
            $0.identifier == identifier && $0.index != target.index
        }) {
            return "标识冲突；请取消后重新创建"
        }

        switch target.key {
        case "nodes":
            let type = draftString(value, "type")
            if type.isEmpty { return "请选择节点协议" }
            for field in SteerUISpec.nodeFields(for: type) where field.isRequired(for: type) {
                let missing: Bool
                switch field.control {
                case "integer", "select-integer": missing = draftInt(value, field.key) == 0
                case "string-list": missing = draftStringList(value, field.key).isEmpty
                default: missing = draftString(value, field.key).isEmpty
                }
                if missing { return "\(field.label)不能为空" }
            }
            if type != "tor" && !validPort(draftInt(value, "server_port")) { return "服务器端口必须是 1…65535" }
            if type == "ssh", draftString(value, "password").isEmpty && draftString(value, "private_key").isEmpty {
                return "SSH 需要密码或私钥"
            }
            if type == "shadowtls", draftInt(value, "version") >= 2, draftString(value, "password").isEmpty {
                return "ShadowTLS v2/v3 密码不能为空"
            }
        case "routes":
            let kind = draftString(value, "kind")
            if !["direct", "block", "single", "wireguard"].contains(kind) { return "请选择路由类型" }
            if kind == "wireguard", !model.draftItems(for:"wireguard_tunnels").contains(where: { $0.enabled && $0.identifier == draftString(value,"tunnel") }) { return "请选择已启用的 WireGuard 隧道" }
            if kind == "single" {
                let nodeID = draftString(value, "node")
                if let problem = model.nodeReferenceProblem(nodeID) { return "节点选择无效：\(problem)" }
                let detourID = draftString(value, "detour")
                if let problem = model.routeDetourProblem(routeID: identifier, detourID: detourID) {
                    return problem
                }
            }
        case "dns_profiles":
            let proto = draftString(value, "protocol")
            if draftString(value, "server").isEmpty { return "DNS 服务器不能为空" }
            if !validPort(draftInt(value, "server_port")) { return "DNS 端口必须是 1…65535" }
            if let protocolSpec = SteerUISpec.dnsProtocol(proto) {
                for field in protocolSpec.requiredFields where draftString(value, field).isEmpty {
                    return "加密 DNS 需要 TLS 服务器名"
                }
            }
        case "local_proxies":
            if draftString(value, "listen").isEmpty { return "监听地址不能为空" }
            if !validPort(draftInt(value, "listen_port")) { return "监听端口必须是 1…65535" }
            if let error = validateLocalProxyAuthentication(
                listen: draftString(value, "listen"),
                username: draftString(value, "username"),
                password: draftString(value, "password")
            ) { return error }
        case "rules":
            if draftString(value, "route").isEmpty { return "请选择路由" }
            if draftString(value, "dns_profile").isEmpty { return "请选择 DNS Profile" }
            if !draftBool(value, "default") && !ruleHasMatch(value) { return "非 Default 规则至少需要一个匹配条件" }
        case "subscriptions":
            let rawURL = draftString(value, "url")
            guard let url = URLComponents(string: rawURL),
                  ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
                  url.host?.isEmpty == false else { return "订阅 URL 必须是完整的 HTTP 或 HTTPS 地址" }
        default:
            break
        }
        return nil
    }
}

struct NodeImportSheet: View {
    @ObservedObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var document = ""
    @State private var errorMessage = ""
    @State private var previewItems: [NodeImportPreviewItem] = []
    @State private var skipped = 0
    @State private var skippedReasons: [NodeImportSkippedReason] = []
    @State private var hasPreview = false

    private var selectedCount: Int { previewItems.filter(\.selected).count }

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                Text("导入节点")
                    .font(.title2.weight(.semibold))
                Text("每行一个分享链接，也支持 Base64 包装的订阅内容。")
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(20)
            Divider()
            Form {
                if hasPreview {
                    Section {
                        HStack {
                            Label("\(previewItems.count) 个可导入节点", systemImage: "checklist")
                            Spacer()
                            Button(selectedCount == previewItems.count ? "取消全选" : "全选") {
                                let selected = selectedCount != previewItems.count
                                for index in previewItems.indices { previewItems[index].selected = selected }
                            }
                        }
                        if skipped > 0 {
                            VStack(alignment: .leading, spacing: 4) {
                                Label(
                                    "已跳过 \(skipped) 个无法识别或字段不完整的条目",
                                    systemImage: "exclamationmark.triangle.fill"
                                )
                                ForEach(Array(skippedReasons.prefix(3).enumerated()), id: \.offset) { _, reason in
                                    Text(reason.detail)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                            .foregroundStyle(.orange)
                        }
                        Text("预览只显示安全摘要；密码、Token、私钥及其他凭据不会显示。")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } header: {
                        Text("解析结果")
                    }
                    ForEach($previewItems) { $item in
                        Section {
                            HStack(alignment: .top, spacing: 12) {
                                Toggle("选择", isOn: $item.selected)
                                    .labelsHidden()
                                VStack(alignment: .leading, spacing: 8) {
                                    TextField("显示名称", text: $item.name)
                                    LabeledContent("协议", value: item.protocolName.uppercased())
                                    LabeledContent("服务器", value: item.server)
                                    LabeledContent("端口", value: item.port.formatted())
                                    LabeledContent("TLS", value: item.tlsVerification)
                                }
                            }
                        } header: {
                            Text(item.name.isEmpty ? "未命名节点" : item.name)
                        }
                    }
                } else {
                    Section("分享链接") {
                        TextEditor(text: $document)
                            .font(.system(.body, design: .monospaced))
                            .frame(minHeight: 260)
                        Text("支持 VLESS、VMess、Trojan、Hysteria、TUIC、Shadowsocks、SOCKS、HTTP 与 SSH 等分享格式。")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .formStyle(.grouped)
            Divider()
            HStack {
                if !errorMessage.isEmpty {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                }
                Spacer()
                Button("取消") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                if hasPreview {
                    Button("返回修改") {
                        hasPreview = false
                        previewItems = []
                        skipped = 0
                        skippedReasons = []
                        errorMessage = ""
                    }
                    Button("导入所选（\(selectedCount)）") {
                        let preview = NodeImportPreview(items: previewItems, skipped: skipped, skippedReasons: skippedReasons)
                        if model.confirmNodeImport(preview) {
                            dismiss()
                        } else {
                            errorMessage = model.message
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(selectedCount == 0 || model.isBusy)
                } else {
                    Button("解析并预览") {
                        Task {
                            errorMessage = ""
                            if let preview = await model.previewNodeImport(document) {
                                previewItems = preview.items
                                skipped = preview.skipped
                                skippedReasons = preview.skippedReasons
                                hasPreview = true
                            } else {
                                errorMessage = model.message
                            }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(document.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isBusy)
                }
            }
            .padding(16)
        }
        .frame(minWidth: 720, minHeight: 560)
    }
}

struct NodeExportSheet: View {
    @ObservedObject var model: AppModel
    let item: DraftItem
    @Environment(\.dismiss) private var dismiss
    @State private var link = ""
    @State private var errorMessage = ""

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                Text("导出节点链接")
                    .font(.title2.weight(.semibold))
                Text(item.title)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(20)
            Divider()
            VStack(alignment: .leading, spacing: 12) {
                Label("分享链接包含完整节点凭据，请仅通过可信渠道保存或分享。", systemImage: "exclamationmark.shield.fill")
                    .foregroundStyle(.orange)
                if link.isEmpty && errorMessage.isEmpty {
                    HStack {
                        ProgressView()
                        Text("正在生成分享链接…")
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if !errorMessage.isEmpty {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                } else {
                    ScrollView([.horizontal, .vertical]) {
                        Text(link)
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(12)
                    }
                    .background(Color(nsColor: .textBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                }
            }
            .padding(20)
            Divider()
            HStack {
                Spacer()
                Button("关闭") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("复制链接") {
                    let pasteboard = NSPasteboard.general
                    pasteboard.clearContents()
                    pasteboard.setString(link, forType: .string)
                    model.message = "节点分享链接已复制到剪贴板"
                }
                .buttonStyle(.borderedProminent)
                .disabled(link.isEmpty)
            }
            .padding(16)
        }
        .frame(minWidth: 680, minHeight: 340)
        .task {
            guard link.isEmpty else { return }
            if let exported = await model.exportNode(item) {
                link = exported
            } else {
                errorMessage = model.message
            }
        }
    }
}

// The controls remain native SwiftUI, while their field matrix, enum values,
// conditional visibility and allowed-option set come from the generated Go
// UI contract.
private struct SharedNodeDraftForm: View {
    @Binding var object: [String: JSONValue]

    private var type: String { draftString(object, "type") }
    private var typeBinding: Binding<String> {
        Binding(
            get: { type },
            set: { value in
                var updated = object
                SteerUISpec.applyNodeType(value, to: &updated)
                object = updated
            }
        )
    }

    var body: some View {
        Section("基本信息") {
            Picker("协议", selection: typeBinding) {
                if type.isEmpty { Text("缺失（需修复）").foregroundStyle(.red).tag("") }
                ForEach(SteerUISpec.contract.nodeTypes) { option in
                    Text(option.label).tag(option.value)
                }
            }
            fields(section: "general")
        }
        if hasVisibleFields(section: "protocol") {
            Section("认证与协议") { fields(section: "protocol") }
        }
        if hasVisibleFields(section: "transport") {
            Section("传输") { fields(section: "transport") }
        }
        if hasVisibleFields(section: "tls") {
            Section("TLS / REALITY") { fields(section: "tls") }
        }
        if hasVisibleFields(section: "advanced") {
            Section("高级协议参数") { fields(section: "advanced") }
        }
    }

    @ViewBuilder
    private func fields(section: String) -> some View {
        ForEach(SteerUISpec.nodeFields(for: type, section: section).filter(visible)) { field in
            fieldControl(field)
        }
    }

    @ViewBuilder
    private func fieldControl(_ field: UIFieldSpec) -> some View {
        let label = localizedLabel(field)
        switch field.control {
        case "boolean":
            Toggle(label, isOn: nodeBoolBinding(field))
        case "integer":
            TextField(label, value: nodeIntBinding(field), format: .number)
        case "select":
            Picker(label, selection: nodeStringBinding(field)) {
                ForEach(field.options) { option in Text(option.label).tag(option.value) }
            }
        case "select-integer":
            Picker(label, selection: nodeIntBinding(field)) {
                ForEach(field.options) { option in Text(option.label).tag(Int(option.value) ?? 0) }
            }
        case "string-list":
            MultilineStringListEditor(
                label: label,
                text: stringListBinding($object, field.key),
                example: field.placeholder.contains(",") ? "" : field.placeholder
            )
        case "password" where field.multiline:
            LabeledContent(label) {
                TextEditor(text: stringBinding($object, field.key))
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 76)
            }
        case "password":
            SecureField(label, text: stringBinding($object, field.key, required: field.isRequired(for: type)))
        default:
            TextField(label, text: stringBinding($object, field.key, required: field.isRequired(for: type)), prompt: prompt(field))
        }
    }

    private func visible(_ field: UIFieldSpec) -> Bool {
        guard let condition = field.when else { return true }
        let current = SteerUISpec.effectiveNodeFieldValue(
            key: condition.field,
            nodeType: type,
            in: object
        )?.stringValue ?? ""
        return condition.values.contains(current)
    }

    private func hasVisibleFields(section: String) -> Bool {
        SteerUISpec.nodeFields(for: type, section: section).contains(where: visible)
    }

    private func prompt(_ field: UIFieldSpec) -> Text? {
        field.placeholder.isEmpty ? nil : Text(field.placeholder)
    }

    private func localizedLabel(_ field: UIFieldSpec) -> String {
        let labels = [
            "enabled": "启用节点", "name": "名称", "server": "服务器", "server_port": "服务器端口",
            "username": "用户名", "password": "密码", "method": "加密方法", "plugin": "插件", "plugin_options": "插件参数",
            "security": "Security", "alter_id": "Alter ID", "network": "Network", "packet_encoding": "Packet encoding",
            "flow": "Flow", "transport": "传输类型", "transport_path": "路径", "transport_host": "Host",
            "service_name": "Service name", "server_ports": "端口范围", "hop_interval": "跳跃间隔", "obfs_type": "混淆",
            "obfs_password": "混淆密码", "up_mbps": "上传 Mbps", "down_mbps": "下载 Mbps", "version": "版本",
            "congestion_control": "拥塞控制", "udp_relay_mode": "UDP relay", "udp_over_stream": "UDP over stream",
            "zero_rtt_handshake": "Zero-RTT handshake", "heartbeat": "Heartbeat", "quic_congestion_control": "QUIC congestion control",
            "insecure_concurrency": "Insecure concurrency", "private_key": "Private key", "host_key": "Host key",
            "host_key_algorithms": "Host key algorithms", "executable_path": "可执行文件", "extra_args": "额外参数",
            "data_directory": "数据目录", "tls_server_name": "TLS 服务器名", "alpn": "ALPN", "utls_fingerprint": "uTLS 指纹",
            "insecure": "跳过证书验证", "reality_public_key": "REALITY Public key", "reality_short_id": "REALITY Short ID",
        ]
        return labels[field.key] ?? field.label
    }

    private func nodeStringBinding(_ field: UIFieldSpec) -> Binding<String> {
        Binding(
            get: { SteerUISpec.effectiveNodeFieldValue(field, in: object)?.stringValue ?? "" },
            set: { value in
                if value.isEmpty {
                    object.removeValue(forKey: field.key)
                } else {
                    object[field.key] = .string(value)
                }
            }
        )
    }

    private func nodeIntBinding(_ field: UIFieldSpec) -> Binding<Int> {
        Binding(
            get: {
                Int(SteerUISpec.effectiveNodeFieldValue(field, in: object)?.numberValue ?? 0)
            },
            set: { object[field.key] = .number(Double($0)) }
        )
    }

    private func nodeBoolBinding(_ field: UIFieldSpec) -> Binding<Bool> {
        Binding(
            get: { SteerUISpec.effectiveNodeFieldValue(field, in: object)?.boolValue ?? false },
            set: { object[field.key] = .bool($0) }
        )
    }
}

private struct RouteDraftForm: View {
    @ObservedObject var model: AppModel
    @Binding var object: [String: JSONValue]
    let editingIndex: Int?
    let originalKind: String

    private var kind: String { draftString(object, "kind") }
    private var isSystemRoute: Bool { ["direct", "block"].contains(originalKind) }
    private var routeID: String { draftString(object, "id") }
    private var selectedNodeID: String { draftString(object, "node") }
    private var selectedDetourID: String { draftString(object, "detour") }
    private var selectableNodes: [DraftItem] { model.draftItems(for: "nodes").filter(\.enabled) }
    private var selectableDetours: [DraftItem] { model.routeDetourCandidates(editingRouteID: routeID) }

    private func nodeLabel(_ identifier: String) -> String {
        guard let item = model.draftItems(for: "nodes").first(where: { $0.identifier == identifier }) else {
            return identifier
        }
        return model.draftReferenceLabel(item, in: "nodes")
    }

    private func detourLabel(_ identifier: String) -> String {
        guard let item = model.draftItems(for: "routes").first(where: { $0.identifier == identifier }) else {
            return identifier
        }
        return model.draftReferenceLabel(item, in: "routes")
    }

    var body: some View {
        Section("路由") {
            if originalKind == "direct" {
                LabeledContent("状态") {
                    Label("系统必需 · 始终启用", systemImage: "lock.fill")
                        .foregroundStyle(.green)
                }
            } else {
                Toggle("启用路由", isOn: boolBinding($object, "enabled"))
            }
            TextField("名称", text: stringBinding($object, "name"))
            if isSystemRoute {
                LabeledContent("类型", value: kind == "direct" ? "Direct" : "Reject")
            } else {
                Picker("类型", selection: stringBinding($object,"kind",required:true)) {
                    Text("Single 节点").tag("single")
                    Text("WireGuard 隧道").tag("wireguard")
                }
            }
            if isSystemRoute {
                Text("系统路由类型固定；可以修改显示名称。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        if kind == "wireguard" {
            Section("WireGuard 出口") {
                Picker("隧道",selection:stringBinding($object,"tunnel",required:true)) {
                    Text("请选择隧道").tag("")
                    ForEach(model.draftItems(for:"wireguard_tunnels").filter(\.enabled)) { item in
                        Text(item.title).tag(item.identifier)
                    }
                }
            }
        }
        if kind == "single" {
            Section("出口关系") {
                Picker("节点", selection: stringBinding($object, "node", required: true)) {
                    Text("请选择节点").tag("")
                    ForEach(selectableNodes) { item in
                        Text(model.draftReferenceLabel(item, in: "nodes")).tag(item.identifier)
                    }
                    if !selectedNodeID.isEmpty,
                       !selectableNodes.contains(where: { $0.identifier == selectedNodeID }) {
                        Text("\(nodeLabel(selectedNodeID)) · \(model.nodeReferenceProblem(selectedNodeID) ?? "无效")")
                            .foregroundStyle(.red)
                            .tag(selectedNodeID)
                    }
                }
                if let problem = model.nodeReferenceProblem(selectedNodeID), !selectedNodeID.isEmpty {
                    Label("当前节点：\(problem)", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                }
                Picker("前置路由", selection: stringBinding($object, "detour")) {
                    Text("直连（无前置）").tag("")
                    ForEach(selectableDetours) { item in
                        Text(model.draftReferenceLabel(item, in: "routes")).tag(item.identifier)
                    }
                    if !selectedDetourID.isEmpty,
                       !selectableDetours.contains(where: { $0.identifier == selectedDetourID }) {
                        Text("\(detourLabel(selectedDetourID)) · \(model.routeDetourProblem(routeID: routeID, detourID: selectedDetourID) ?? "无效")")
                            .foregroundStyle(.red)
                            .tag(selectedDetourID)
                    }
                }
                if let problem = model.routeDetourProblem(routeID: routeID, detourID: selectedDetourID) {
                    Label(problem, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                }
                Text("非空时先连接前置路由，再连接当前节点。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

private struct DNSDraftForm: View {
    @Binding var object: [String: JSONValue]
    private var proto: String { draftString(object, "protocol") }
    private var protocolSpec: UIDNSProtocolSpec? { SteerUISpec.dnsProtocol(proto) }
    private var protocolBinding: Binding<String> {
        Binding(
            get: { proto },
            set: { value in
                var updated = object
                SteerUISpec.applyDNSProtocol(value, to: &updated)
                object = updated
            }
        )
    }

    var body: some View {
        Section("DNS Profile") {
            Toggle("启用 Profile", isOn: boolBinding($object, "enabled"))
            TextField("名称", text: stringBinding($object, "name"))
            Picker("协议", selection: protocolBinding) {
                if proto.isEmpty { Text("缺失（需修复）").foregroundStyle(.red).tag("") }
                ForEach(SteerUISpec.contract.dnsProtocols) { option in
                    Text(option.label).tag(option.value)
                }
            }
            TextField("服务器", text: stringBinding($object, "server", required: true), prompt: Text("1.1.1.1"))
            TextField("端口", value: intBinding($object, "server_port"), format: .number)
        }
        if protocolSpec?.fields.isEmpty == false {
            Section("加密传输") {
                if protocolSpec?.fields.contains("tls_server_name") == true {
                    TextField("TLS 服务器名", text: stringBinding($object, "tls_server_name", required: true), prompt: Text("one.one.one.one"))
                }
                if protocolSpec?.fields.contains("path") == true {
                    TextField("HTTP 路径", text: stringBinding($object, "path"), prompt: Text("/dns-query"))
                }
                if protocolSpec?.fields.contains("insecure") == true {
                    Toggle("跳过证书验证", isOn: boolBinding($object, "insecure"))
                }
            }
        }
    }
}

private struct LocalProxyDraftForm: View {
    @Binding var object: [String: JSONValue]
    private var listen: String { draftString(object, "listen") }
    private var listenClassification: LocalProxyListenClassification { classifyLocalProxyListen(listen) }

    var body: some View {
        Section("本地入口") {
            Toggle("启用入口", isOn: boolBinding($object, "enabled"))
            TextField("名称", text: stringBinding($object, "name"))
            Picker("协议", selection: stringBinding($object, "protocol", required: true)) {
                if draftString(object, "protocol").isEmpty {
                    Text("缺失（需修复）").foregroundStyle(.red).tag("")
                }
                ForEach(SteerUISpec.contract.localProxyProtocols) { option in
                    Text(option.label).tag(option.value)
                }
            }
            TextField("监听地址", text: stringBinding($object, "listen", required: true), prompt: Text("127.0.0.1"))
            TextField("监听端口", value: intBinding($object, "listen_port"), format: .number)
        }
        if listenClassification == .nonLoopback {
            Section {
                Label("该监听地址可能允许局域网或公网客户端连接；必须同时设置用户名和密码。", systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
            } header: {
                Text("暴露范围警告")
            }
        } else if listenClassification == .invalid && !listen.isEmpty {
            Section {
                Label("监听地址必须是 IPv4 或 IPv6 地址，不能填写域名。", systemImage: "xmark.octagon.fill")
                    .foregroundStyle(.red)
            }
        }
        Section("认证（可选）") {
            TextField("用户名", text: stringBinding($object, "username"))
            SecureField("密码", text: stringBinding($object, "password"))
            Text("用户名和密码必须同时设置；仅限本机访问时可以不设置认证。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct MultilineStringListEditor: View {
    let label: String
    let example: String
    @Binding private var text: String
    @State private var draftText: String

    init(label: String, text: Binding<String>, example: String) {
        self.label = label
        self.example = example
        _text = text
        _draftText = State(initialValue: text.wrappedValue)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label)
                .font(.callout.weight(.medium))
            TextEditor(text: $draftText)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 82)
            Text(example.isEmpty ? "每行一个完整条目；逗号属于条目正文。" : "每行一个完整条目；逗号属于条目正文。示例：\(example)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .onChange(of: draftText) { text = $0 }
        .onChange(of: text) { value in
            let normalizedDraft = DraftStringListCodec.text(from: DraftStringListCodec.values(from: draftText))
            if value != normalizedDraft { draftText = value }
        }
    }
}

private struct MatchListEditor: View {
    let label: String
    let key: String
    let example: String
    let catalogLabel: String
    let catalogPrefix: String
    let catalog: [String]
    @Binding var object: [String: JSONValue]

    var body: some View {
        MultilineStringListEditor(
            label: label,
            text: stringListBinding($object, key),
            example: example
        )
        GeoCatalogCompletion(
            label: catalogLabel,
            prefix: catalogPrefix,
            catalog: catalog,
            append: appendCatalogValue
        )
    }

    private func appendCatalogValue(_ value: String) {
        let current = stringListBinding($object, key).wrappedValue
        stringListBinding($object, key).wrappedValue = DraftStringListCodec.appendingUnique(value, to: current)
    }
}

private struct GeoCatalogCompletion: View {
    let label: String
    let prefix: String
    let catalog: [String]
    let append: (String) -> Void
    @State private var query = ""
    @State private var selected: String?

    private var results: [String] { geoCatalogMatches(catalog, query: query) }

    var body: some View {
        DisclosureGroup("添加 \(label)") {
            if catalog.isEmpty {
                Label("Geo 规则列表当前不可用；仍可在上方手动输入 \(prefix)规则名。", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            } else {
                TextField("搜索 \(label) 规则", text: $query)
                    .onSubmit { appendSelection() }
                if results.isEmpty {
                    Text("没有匹配项；仍可在上方手动输入。")
                        .foregroundStyle(.secondary)
                } else {
                    List(results, id: \.self, selection: $selected) { name in
                        let presentation = geoCatalogPresentation(name)
                        HStack {
                            Text(presentation.category)
                            if let attribute = presentation.attribute {
                                Text("@\(attribute)")
                                    .font(.caption.monospaced())
                                    .foregroundStyle(.purple)
                            }
                        }
                        .tag(name)
                    }
                    .frame(minHeight: 120, maxHeight: 180)
                    HStack {
                        Text("显示最多 \(results.count) 项；搜索范围为全部 \(catalog.count) 项")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        Button("添加选择") { appendSelection() }
                            .disabled(selected == nil && results.isEmpty)
                    }
                }
            }
        }
        .onChange(of: query) { _ in selected = results.first }
        .onAppear { selected = results.first }
    }

    private func appendSelection() {
        guard let name = selected ?? results.first else { return }
        append(prefix + name)
    }
}

private struct RuleDecisionSection: View {
    @ObservedObject var model: AppModel
    @Binding var object: [String: JSONValue]

    var body: some View {
        Section("决策") {
            Picker("路由", selection: stringBinding($object, "route", required: true)) {
                Text("请选择路由").tag("")
                ForEach(model.draftItems(for: "routes")) { item in
                    Text(model.draftReferenceLabel(item, in: "routes")).tag(item.identifier)
                }
            }
            Picker("DNS Profile", selection: stringBinding($object, "dns_profile", required: true)) {
                Text("请选择 DNS Profile").tag("")
                ForEach(model.draftItems(for: "dns_profiles")) { item in
                    Text(model.draftReferenceLabel(item, in: "dns_profiles")).tag(item.identifier)
                }
            }
        }
    }
}

private struct DefaultRuleDraftForm: View {
    @ObservedObject var model: AppModel
    @Binding var object: [String: JSONValue]

    var body: some View {
        Section("Default") {
            LabeledContent("状态") {
                Label("固定启用 · 始终位于最后", systemImage: "lock.fill")
                    .foregroundStyle(.green)
            }
            TextField("名称", text: stringBinding($object, "name"))
            Text("Default 的身份、状态和顺序固定；只能修改显示名称与决策。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        RuleDecisionSection(model: model, object: $object)
    }
}

private struct RuleDraftForm: View {
    @ObservedObject var model: AppModel
    @Binding var object: [String: JSONValue]
    private var missingInbounds: [String] {
        let known = Set(model.draftItems(for: "local_proxies").map(\.identifier))
        return (object["inbound"]?.arrayValue ?? []).compactMap(\.stringValue).filter { !known.contains($0) }
    }

    var body: some View {
        Section("规则") {
            Toggle("启用规则", isOn: boolBinding($object, "enabled"))
            TextField("名称", text: stringBinding($object, "name"))
            LabeledContent("类型", value: "普通规则")
        }

        RuleDecisionSection(model: model, object: $object)

        Section("目标匹配") {
            Toggle("目标使用出口的 AllowedIPs",isOn:boolBinding($object,"allowed_ips"))
            if draftBool(object,"allowed_ips") {
                Text("选择 WireGuard 出口；网段随隧道配置更新。全网段会接管后续流量。请清空手工 IP match。").font(.caption).foregroundStyle(.secondary)
                Text(model.wireGuardAllowedIPs(routeID:draftString(object,"route")).joined(separator:"\n"))
                    .font(.caption.monospaced()).textSelection(.enabled)
            }
            MatchListEditor(
                label: "Domain match",
                key: "domain_match",
                example: "domain:example.com / geosite:cn / regexp:^api[0-9]{1,3}\\.example\\.com$",
                catalogLabel: "GeoSite",
                catalogPrefix: "geosite:",
                catalog: model.geositeNames,
                object: $object
            )
            MatchListEditor(
                label: "IP match",
                key: "ip_match",
                example: "10.0.0.0/8 / geoip:cn",
                catalogLabel: "GeoIP",
                catalogPrefix: "geoip:",
                catalog: model.geoipNames,
                object: $object
            )
            TextField("目标端口", text: intListBinding($object, "port"), prompt: Text("443, 8443"))
        }
        Section("来源匹配") {
            MultilineStringListEditor(
                label: "Source IP CIDR",
                text: stringListBinding($object, "source_ip_cidr"),
                example: "192.168.1.0/24"
            )
            if model.draftItems(for: "local_proxies").isEmpty {
                LabeledContent("本地入口", value: "无")
            } else {
                DisclosureGroup("本地入口") {
                    ForEach(model.draftItems(for: "local_proxies")) { item in
                        Toggle(model.draftReferenceLabel(item, in: "local_proxies"), isOn: membershipBinding($object, "inbound", item.identifier))
                    }
                }
            }
            ForEach(missingInbounds, id: \.self) { identifier in
                Toggle("缺失本地入口：\(identifier)", isOn: membershipBinding($object, "inbound", identifier))
                    .foregroundStyle(.red)
            }
            let capability = SteerUISpec.contract.platformCapabilities["macos"]
            Label("源 MAC 地址匹配不可用：\(capability?.sourceMACReason ?? "此平台不支持该匹配")", systemImage: "nosign")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        Section("连接条件") {
            ForEach(SteerUISpec.contract.ruleNetworks) { option in
                Toggle(option.label, isOn: membershipBinding($object, "network", option.value))
            }
            DisclosureGroup("嗅探协议") {
                ForEach(SteerUISpec.contract.ruleProtocols) { option in
                    Toggle(option.label, isOn: membershipBinding($object, "protocol", option.value))
                }
            }
            Text("提示：目标 IP、网络、协议与端口匹配仅在连接阶段生效。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct SubscriptionDraftForm: View {
    @Binding var object: [String: JSONValue]

    var body: some View {
        Section("订阅源") {
            Toggle("启用订阅", isOn: boolBinding($object, "enabled"))
            TextField("名称", text: stringBinding($object, "name"))
            TextField("URL", text: stringBinding($object, "url", required: true), prompt: Text("https://example.com/subscription"))
            TextField("更新间隔", text: stringBinding($object, "update_interval"), prompt: Text(SteerUISpec.contract.subscriptionUpdateIntervalDefault))
            Text("订阅会按设置的间隔自动更新；订阅节点在节点页保持只读。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

func stringBinding(
    _ object: Binding<[String: JSONValue]>,
    _ key: String,
    required: Bool = false
) -> Binding<String> {
    Binding(
        get: { draftString(object.wrappedValue, key) },
        set: { value in
            var copy = object.wrappedValue
            if value.isEmpty && !required {
                copy.removeValue(forKey: key)
            } else {
                copy[key] = .string(value)
            }
            object.wrappedValue = copy
        }
    )
}

func boolBinding(
    _ object: Binding<[String: JSONValue]>,
    _ key: String
) -> Binding<Bool> {
    Binding(
        get: { object.wrappedValue[key]?.boolValue ?? false },
        set: { value in
            var copy = object.wrappedValue
            copy[key] = .bool(value)
            object.wrappedValue = copy
        }
    )
}

func intBinding(
    _ object: Binding<[String: JSONValue]>,
    _ key: String
) -> Binding<Int> {
    Binding(
        get: { Int(object.wrappedValue[key]?.numberValue ?? 0) },
        set: { value in
            var copy = object.wrappedValue
            if value == 0 {
                copy.removeValue(forKey: key)
            } else {
                copy[key] = .number(Double(value))
            }
            object.wrappedValue = copy
        }
    )
}

func stringListBinding(_ object: Binding<[String: JSONValue]>, _ key: String) -> Binding<String> {
    Binding(
        get: { DraftStringListCodec.text(from: draftStringList(object.wrappedValue, key)) },
        set: { raw in
            let values = DraftStringListCodec.values(from: raw).map(JSONValue.string)
            var copy = object.wrappedValue
            if values.isEmpty { copy.removeValue(forKey: key) } else { copy[key] = .array(values) }
            object.wrappedValue = copy
        }
    )
}

private func intListBinding(_ object: Binding<[String: JSONValue]>, _ key: String) -> Binding<String> {
    Binding(
        get: { draftIntList(object.wrappedValue, key).map(String.init).joined(separator: ", ") },
        set: { raw in
            let values = splitIntegerList(raw).compactMap(Int.init).map { JSONValue.number(Double($0)) }
            var copy = object.wrappedValue
            if values.isEmpty { copy.removeValue(forKey: key) } else { copy[key] = .array(values) }
            object.wrappedValue = copy
        }
    )
}

private func membershipBinding(
    _ object: Binding<[String: JSONValue]>,
    _ key: String,
    _ value: String
) -> Binding<Bool> {
    Binding(
        get: { draftStringList(object.wrappedValue, key).contains(value) },
        set: { enabled in
            var values = draftStringList(object.wrappedValue, key)
            if enabled && !values.contains(value) { values.append(value) }
            if !enabled { values.removeAll { $0 == value } }
            var copy = object.wrappedValue
            if values.isEmpty {
                copy.removeValue(forKey: key)
            } else {
                copy[key] = .array(values.map(JSONValue.string))
            }
            object.wrappedValue = copy
        }
    )
}

private func draftString(_ object: [String: JSONValue], _ key: String) -> String {
    object[key]?.stringValue ?? ""
}

private func draftBool(_ object: [String: JSONValue], _ key: String) -> Bool {
    object[key]?.boolValue ?? false
}

private func draftInt(_ object: [String: JSONValue], _ key: String) -> Int {
    Int(object[key]?.numberValue ?? 0)
}

private func draftStringList(_ object: [String: JSONValue], _ key: String) -> [String] {
    object[key]?.arrayValue?.compactMap(\.stringValue) ?? []
}

private func draftIntList(_ object: [String: JSONValue], _ key: String) -> [Int] {
    object[key]?.arrayValue?.compactMap(\.numberValue).map(Int.init) ?? []
}

enum DraftStringListCodec {
    static func text(from values: [String]) -> String {
        values.joined(separator: "\n")
    }

    static func values(from text: String) -> [String] {
        text.components(separatedBy: .newlines).filter {
            !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    static func appendingUnique(_ value: String, to text: String) -> String {
        var values = values(from: text)
        if !values.contains(value) { values.append(value) }
        return self.text(from: values)
    }
}

private func splitIntegerList(_ raw: String) -> [String] {
    raw.split(whereSeparator: { $0 == "," || $0 == "\n" })
        .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty }
}

private func validPort(_ value: Int) -> Bool {
    (1...65_535).contains(value)
}

private func ruleHasMatch(_ object: [String: JSONValue]) -> Bool {
    object["allowed_ips"]?.boolValue == true || RuleDraftPolicy.matchKeys.contains { object[$0]?.arrayValue?.isEmpty == false }
}

private func normalizedObject(_ source: [String: JSONValue], key: String) -> [String: JSONValue] {
    var object = source
    object["id"] = .string(draftString(object, "id").trimmingCharacters(in: .whitespacesAndNewlines))

    let requiredStrings: Set<String>
    switch key {
    case "nodes": requiredStrings = ["id", "type", "server"]
    case "routes": requiredStrings = ["id", "kind"]
    case "dns_profiles": requiredStrings = ["id", "protocol", "server"]
    case "local_proxies": requiredStrings = ["id", "protocol", "listen"]
    case "rules": requiredStrings = ["id", "dns_profile", "route"]
    case "subscriptions": requiredStrings = ["id", "url"]
    default: requiredStrings = ["id"]
    }

    for field in Array(object.keys) {
        guard let value = object[field] else { continue }
        if value.stringValue?.isEmpty == true && !requiredStrings.contains(field) {
            object.removeValue(forKey: field)
        }
        if value.arrayValue?.isEmpty == true {
            object.removeValue(forKey: field)
        }
    }

    if key == "routes", draftString(object, "kind") != "single" {
        object.removeValue(forKey: "node")
        object.removeValue(forKey: "detour")
    }
    if key == "routes", draftString(object,"kind") != "wireguard" { object.removeValue(forKey:"tunnel") }
    if key == "nodes", draftString(object, "type") == "tor" {
        object.removeValue(forKey: "server")
        object.removeValue(forKey: "server_port")
    }
    if key == "nodes" {
        normalizeNodeOptionsFromSpec(&object)
    }
    if key == "dns_profiles" {
        SteerUISpec.normalizeDNSProfile(&object)
    }
    if key == "rules", draftBool(object, "default") {
        for field in RuleDraftPolicy.matchKeys { object.removeValue(forKey: field) }
    }
    return object
}

private func normalizeNodeOptionsFromSpec(_ object: inout [String: JSONValue]) {
    let type = draftString(object, "type")
    let allowed = Set(SteerUISpec.nodeFields(for: type).map(\.key))
    for field in nodeOptionFields where !allowed.contains(field) {
        object.removeValue(forKey: field)
    }
}

private let nodeOptionFields: Set<String> = [
    "uuid", "username", "password", "private_key", "host_key", "host_key_algorithms",
    "network", "transport", "transport_path", "transport_host", "service_name", "packet_encoding", "flow",
    "security", "alter_id", "version", "method", "plugin", "plugin_options",
    "congestion_control", "udp_relay_mode", "udp_over_stream", "zero_rtt_handshake", "heartbeat",
    "quic", "quic_congestion_control", "insecure_concurrency", "server_ports", "hop_interval",
    "obfs_type", "obfs_password", "up_mbps", "down_mbps", "executable_path", "extra_args", "data_directory",
    "tls_server_name", "alpn", "insecure", "reality_public_key", "reality_short_id", "utls_fingerprint",
]
