// SPDX-License-Identifier: GPL-3.0-or-later

import SwiftUI

struct ConfigurationFormView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            Form {
                Section("运行") {
                    Picker("日志级别", selection: stringBinding("main", "log_level", required: true)) {
                        if model.draftValue(in: "main", key: "log_level")?.stringValue == nil {
                            Text("缺失（需修复）").foregroundStyle(.red).tag("")
                        }
                        ForEach(SteerUISpec.contract.logLevels) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                }

                Section("连通性探测") {
                    TextField("直连探测", text: stringBinding("main", "probe_direct", required: true), prompt: Text("https://www.example.com/"))
                    TextField("代理探测", text: stringBinding("main", "probe_proxy", required: true), prompt: Text("https://www.google.com/generate_204"))
                    TextField("代理测速", text: stringBinding("main", "speedtest_proxy", required: true), prompt: Text("https://speed.example.com/file"))
                    Text("探测地址必须使用 HTTPS，且不能包含账号密码或 # 片段。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Section("DNS 缓存") {
                    TextField(
                        "缓存容量",
                        value: optionalIntBinding("main", "dns_cache_capacity"),
                        format: .number,
                        prompt: Text("4096")
                    )
                    Toggle("持久化 DNS 缓存", isOn: boolBinding("main", "dns_cache_persist"))
                    Toggle("乐观缓存", isOn: boolBinding("main", "dns_optimistic_cache"))
                    Text("留空使用默认值；自定义范围为 1,024…10,000,000。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Section("Bootstrap DNS") {
                    Picker("协议", selection: stringBinding("bootstrap", "protocol", required: true)) {
                        if model.draftValue(in: "bootstrap", key: "protocol")?.stringValue == nil {
                            Text("缺失（需修复）").foregroundStyle(.red).tag("")
                        }
                        ForEach(SteerUISpec.contract.bootstrapProtocols) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                    TextField("服务器", text: stringBinding("bootstrap", "server", required: true), prompt: Text("1.1.1.1"))
                    TextField("端口", value: intBinding("bootstrap", "server_port"), format: .number)
                    Picker("地址策略", selection: stringBinding("bootstrap", "strategy", required: true)) {
                        if model.draftValue(in: "bootstrap", key: "strategy")?.stringValue == nil {
                            Text("缺失（需修复）").foregroundStyle(.red).tag("")
                        }
                        ForEach(SteerUISpec.contract.bootstrapStrategies) { option in
                            Text(option.displayLabel).tag(option.value)
                        }
                    }
                    Text("Bootstrap 服务器必须填写 IP 地址，避免解析环路。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .formStyle(.grouped)
            .disabled(!model.canEditDraft)

            Divider()
            HStack(spacing: 10) {
                if !model.message.isEmpty {
                    Label(model.message, systemImage: "info.circle")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                Button("重新载入") { model.loadDraft() }
                Button("校验") { model.validate() }
                    .disabled(model.draftSyntaxError != nil)
                DraftActionButtons(model: model)
            }
            .disabled(model.isBusy)
            .padding(16)
        }
    }

    private func stringBinding(
        _ section: String,
        _ key: String,
        required: Bool = false
    ) -> Binding<String> {
        Binding(
            get: {
                model.draftValue(in: section, key: key)?.stringValue ?? ""
            },
            set: { value in
                model.setDraftValue(
                    in: section,
                    key: key,
                    value: value.isEmpty && !required ? nil : .string(value)
                )
            }
        )
    }

    private func boolBinding(_ section: String, _ key: String) -> Binding<Bool> {
        Binding(
            get: { model.draftValue(in: section, key: key)?.boolValue ?? false },
            set: { model.setDraftValue(in: section, key: key, value: .bool($0)) }
        )
    }

    private func intBinding(_ section: String, _ key: String) -> Binding<Int> {
        Binding(
            get: { Int(model.draftValue(in: section, key: key)?.numberValue ?? 0) },
            set: { value in
                model.setDraftValue(
                    in: section,
                    key: key,
                    value: value == 0 ? nil : .number(Double(value))
                )
            }
        )
    }

    private func optionalIntBinding(_ section: String, _ key: String) -> Binding<Int?> {
        Binding(
            get: {
                guard let value = model.draftValue(in: section, key: key)?.numberValue,
                      value != 0 else { return nil }
                return Int(value)
            },
            set: { value in
                if let value, value != 0 {
                    model.setDraftValue(in: section, key: key, value: .number(Double(value)))
                } else {
                    model.setDraftValue(in: section, key: key, value: nil)
                }
            }
        )
    }
}
