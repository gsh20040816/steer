// SPDX-License-Identifier: GPL-3.0-or-later
import SwiftUI
import AppKit

struct WireGuardImportResult: Decodable, Sendable {
    let tunnel: JSONValue
    let dns: [String]?
    let warnings: [String]?
}

struct WireGuardPage: View {
    @ObservedObject var model: AppModel
    @State private var importing = false
    var body: some View {
        VStack(spacing:0) {
            HStack {
                Text("出站通过路由与规则选择；远端只能访问明确开放的本机服务。")
                    .font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button("导入 .conf") { importing = true }.disabled(!model.canEditDraft)
            }.padding(.horizontal,24).padding(.top,12)
            ForEach(Array(model.runtime.wireguard.enumerated()),id:\.offset) { _, peer in
                HStack {
                    Text(peer.server)
                    Text(peer.address ?? "等待解析")
                    if let error = peer.error, !error.isEmpty { Text(error).foregroundStyle(.orange) }
                    Spacer()
                }.font(.caption).padding(.horizontal,24)
            }
            if !model.runtime.wireguard.isEmpty {
                Text("以上为 Endpoint 解析状态，不代表握手成功。域名每 60 秒检查；地址变化会重载核心，现有连接可能中断。")
                    .font(.caption).foregroundStyle(.secondary).padding(.horizontal,24)
            }
            DraftCollectionView(model:model,descriptor:.wireguard)
        }.sheet(isPresented:$importing) { WireGuardImportSheet(model:model) }
    }
}

struct WireGuardImportSheet: View {
    @ObservedObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var document = ""
    @State private var name = "wg_direct"
    @State private var createRule = true
    @State private var preview: WireGuardImportResult?
    @State private var error = ""
    @State private var busy = false
    var body: some View {
        VStack(alignment:.leading,spacing:12) {
            Text("导入 WireGuard").font(.title2)
            TextField("隧道名称",text:$name)
            if let preview {
                Text("\(preview.tunnel.objectValue?["peers"]?.arrayValue?.count ?? 0) 个 Peer")
                Text(prefixes(preview).joined(separator:"\n")).font(.body.monospaced()).textSelection(.enabled)
                if prefixes(preview).contains("0.0.0.0/0") || prefixes(preview).contains("::/0") {
                    Text("包含全网段：自动分流规则会接管其后的剩余流量。").foregroundStyle(.orange)
                }
                ForEach(preview.warnings ?? [],id:\.self) { Text($0).font(.caption).foregroundStyle(.secondary) }
                Toggle("创建 AllowedIPs 分流规则（位于默认规则之前）",isOn:$createRule)
                Text("将创建一条隧道和一个 WireGuard 出口。保存并应用后生效。").font(.caption)
            } else {
                Text("粘贴配置或选择文件。私钥仅写入配置，不显示在导入摘要中。")
                TextEditor(text:$document).font(.body.monospaced()).frame(minHeight:280)
                Button("选择 .conf 文件") {
                    let panel = NSOpenPanel(); panel.allowsMultipleSelection = false
                    if panel.runModal() == .OK, let url = panel.url {
                        do { document = try String(contentsOf:url,encoding:.utf8) }
                        catch { self.error = error.localizedDescription }
                    }
                }
            }
            if !error.isEmpty { Text(error).foregroundStyle(.red) }
            Spacer()
            HStack {
                Button("取消") { dismiss() }
                Spacer()
                if let preview {
                    Button("返回") { self.preview = nil }
                    Button("导入到工作副本") {
                        if model.importWireGuard(preview,name:name,createRule:createRule) { dismiss() }
                        else { error = "无法导入；请确保配置包含默认规则及 DNS Profile" }
                    }.keyboardShortcut(.defaultAction)
                } else {
                    Button(busy ? "正在解析…" : "预览") {
                        busy = true; error = ""
                        Task {
                            do { preview = try await model.previewWireGuard(document) }
                            catch { self.error = error.localizedDescription }
                            busy = false
                        }
                    }.disabled(busy || document.isEmpty)
                }
            }
        }.padding(24).frame(width:720,height:620)
    }
    private func prefixes(_ value: WireGuardImportResult) -> [String] {
        (value.tunnel.objectValue?["peers"]?.arrayValue ?? []).flatMap {
            ($0.objectValue?["allowed_ips"]?.arrayValue ?? []).compactMap(\.stringValue)
        }
    }
}

struct WireGuardDraftForm: View {
    @Binding var object: [String:JSONValue]
    var body: some View {
        Section("隧道") {
            Toggle("启用",isOn:boolBinding($object,"enabled"))
            TextField("名称",text:stringBinding($object,"name"))
            TextField("本机隧道地址（每行一个 /32 或 /128）",text:stringListBinding($object,"address"),axis:.vertical)
            SecureField("私钥",text:stringBinding($object,"private_key",required:true))
            Picker("Endpoint 地址策略",selection:stringBinding($object,"endpoint_strategy")) {
                Text("继承 Bootstrap").tag("")
                ForEach(SteerUISpec.contract.bootstrapStrategies) { option in Text(option.label).tag(option.value) }
            }
            TextField("监听 UDP 端口（0 自动）",value:intBinding($object,"listen_port"),format:.number)
            TextField("MTU（0 使用默认）",value:intBinding($object,"mtu"),format:.number)
        }
        ForEach(Array((object["peers"]?.arrayValue ?? []).indices),id:\.self) { index in
            Section("Peer \(index+1)") {
                WireGuardPeerForm(object:element("peers",index))
                Button("删除 Peer",role:.destructive) { remove("peers",index) }
            }
        }
        Section { Button("添加 Peer") { append("peers",["public_key":.string(""),"allowed_ips":.array([])]) } }
        Section("远端访问本机") {
            Text("只开放下列服务；其他 WG 入站一律拒绝。本机服务通常看到的是回环源地址。").font(.caption)
            ForEach(Array((object["access"]?.arrayValue ?? []).indices),id:\.self) { index in
                WireGuardAccessForm(object:element("access",index))
                Button("删除服务",role:.destructive) { remove("access",index) }
                Divider()
            }
            Button("开放本机服务") { append("access",["source_ip_cidr":.array([]),"network":.string("tcp"),"port":.number(22),"target":.string("127.0.0.1"),"target_port":.number(22)]) }
        }
    }
    private func element(_ key:String,_ index:Int) -> Binding<[String:JSONValue]> {
        Binding(get:{ let a=object[key]?.arrayValue ?? []; return a.indices.contains(index) ? a[index].objectValue ?? [:] : [:] },set:{ value in
            var a=object[key]?.arrayValue ?? []; guard a.indices.contains(index) else {return}; a[index] = .object(value); object[key] = .array(a)
        })
    }
    private func append(_ key:String,_ value:[String:JSONValue]) { var a=object[key]?.arrayValue ?? [];a.append(.object(value));object[key] = .array(a) }
    private func remove(_ key:String,_ index:Int) {var a=object[key]?.arrayValue ?? [];guard a.indices.contains(index) else{return};a.remove(at:index);object[key] = .array(a)}
}
private struct WireGuardPeerForm: View {
    @Binding var object:[String:JSONValue]
    var body: some View {
        TextField("公钥",text:stringBinding($object,"public_key",required:true))
        SecureField("预共享密钥（可选）",text:stringBinding($object,"pre_shared_key"))
        TextField("Endpoint 域名或 IP",text:stringBinding($object,"server"))
        TextField("Endpoint 端口",value:intBinding($object,"server_port"),format:.number)
        TextField("AllowedIPs（每行一个 CIDR）",text:stringListBinding($object,"allowed_ips"),axis:.vertical)
        TextField("Keepalive 秒数（0 关闭）",value:intBinding($object,"persistent_keepalive"),format:.number)
    }
}
private struct WireGuardAccessForm: View {
    @Binding var object:[String:JSONValue]
    var body: some View {
        TextField("允许的远端源地址（CIDR）",text:stringListBinding($object,"source_ip_cidr"),axis:.vertical)
        Picker("协议",selection:stringBinding($object,"network",required:true)) {Text("TCP").tag("tcp");Text("UDP").tag("udp")}
        TextField("WG 目标端口",value:intBinding($object,"port"),format:.number)
        TextField("本机回环地址",text:stringBinding($object,"target",required:true))
        TextField("本机服务端口",value:intBinding($object,"target_port"),format:.number)
    }
}
