// SPDX-License-Identifier: GPL-3.0-or-later

import SwiftUI
import UniformTypeIdentifiers

struct DraftActionButtons: View {
    @ObservedObject var model: AppModel

    var body: some View {
        Button {
            model.saveDraft()
        } label: {
            Label("保存", systemImage: "square.and.arrow.down")
        }
        .disabled(!model.canSaveDraft)

        Button {
            model.applySaved()
        } label: {
            Label("应用已保存配置", systemImage: "bolt")
        }
        .disabled(!model.canApplySaved)

        Button {
            model.saveAndApplyDraft()
        } label: {
            Label("保存并应用", systemImage: "bolt.fill")
        }
        .disabled(!model.canSaveAndApplyDraft)
    }
}

struct ContentView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        NavigationSplitView {
            SidebarView(model: model)
        } detail: {
            PageView(model: model)
        }
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if model.isBusy {
                    ProgressView()
                        .controlSize(.small)
                }
                Label(
                    model.runtime.healthy ? "运行正常" : (model.runtime.generationID.isEmpty ? "未运行" : "运行异常"),
                    systemImage: model.runtime.healthy && model.runtime.error.isEmpty ? "checkmark.circle.fill" : (model.runtime.generationID.isEmpty && model.runtime.error.isEmpty ? "circle" : "exclamationmark.triangle.fill")
                )
                .foregroundStyle(model.runtime.healthy && model.runtime.error.isEmpty ? .green : (model.runtime.generationID.isEmpty && model.runtime.error.isEmpty ? .secondary : .orange))
                Toggle("Steer", isOn: Binding(
                    get: { model.savedEnabled },
                    set: { model.setEnabledAndApply($0) }
                ))
                .toggleStyle(.switch)
                .disabled(!model.canToggleEnabled)
                .help(model.enableToggleHelp)
                Button { model.refreshStatus() } label: {
                    Label("刷新状态", systemImage: "arrow.clockwise")
                }
                .help("刷新服务运行状态")
                .disabled(model.isBusy || model.pendingDraftAction != nil)
                Button { model.validate() } label: {
                    Label("校验", systemImage: "checkmark.shield")
                }
                .disabled(model.isBusy || model.draftSyntaxError != nil)
                DraftActionButtons(model: model)
            }
        }
        .confirmationDialog(
            model.draftGuardTitle,
            isPresented: Binding(
                get: {
                    model.pendingDraftAction != nil && model.pendingDraftAction != .terminate
                },
                set: { isPresented in
                    guard !isPresented else { return }
                    DispatchQueue.main.async {
                        if model.pendingDraftAction != nil {
                            model.resolveDraftGuard(.cancel)
                        }
                    }
                }
            ),
            titleVisibility: .visible
        ) {
            Button("保存") { model.resolveDraftGuard(.save) }
                .disabled(!model.canSaveForPendingDraftAction)
            Button("丢弃", role: .destructive) { model.resolveDraftGuard(.discard) }
            Button("取消", role: .cancel) { model.resolveDraftGuard(.cancel) }
        } message: {
            Text(model.draftGuardExplanation)
        }
        .alert(
            "配置冲突",
            isPresented: Binding(
                get: { model.revisionConflict != nil },
                set: { _ in }
            )
        ) {
            Button("重新载入服务器配置", role: .destructive) {
                model.reloadSavedAfterRevisionConflict()
            }
            Button("保留本地工作副本", role: .cancel) {
                model.keepLocalDraftAfterRevisionConflict()
            }
            Button("覆盖服务器配置", role: .destructive) {
                model.overwriteAfterRevisionConflict()
            }
        } message: {
            Text(model.revisionConflictExplanation)
        }
    }
}

private struct SidebarView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        List(selection: Binding<AppPage?>(
            get: { model.selectedPage },
            set: { if let page = $0 { model.selectedPage = page } }
        )) {
            ForEach(SteerUISpec.contract.navigation) { group in
                Section(groupLabel(group.key, fallback: group.label)) {
                    ForEach(group.items) { item in
                        if let page = AppPage(contractKey: item.key) {
                            sidebarRow(page, count: count(for: page))
                        }
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Steer")
        .navigationSplitViewColumnWidth(min: 200, ideal: 220, max: 280)
        .safeAreaInset(edge: .bottom) {
            HStack(spacing: 8) {
                Image(systemName: model.runtime.healthy && model.runtime.error.isEmpty ? "checkmark.circle.fill" : (model.runtime.generationID.isEmpty && model.runtime.error.isEmpty ? "circle" : "exclamationmark.triangle.fill"))
                    .foregroundStyle(model.runtime.healthy && model.runtime.error.isEmpty ? .green : (model.runtime.generationID.isEmpty && model.runtime.error.isEmpty ? .secondary : .orange))
                VStack(alignment: .leading, spacing: 1) {
                    Text(model.runtimeStatusText)
                        .font(.caption.weight(.medium))
                    Text(model.isDirty ? "有未保存修改" : "配置已保存")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(.bar)
        }
    }

    @ViewBuilder
    private func sidebarRow(_ page: AppPage, count: Int? = nil) -> some View {
        HStack {
            Label(page.navigationLabel, systemImage: page.systemImage)
            Spacer()
            if let count {
                Text(count, format: .number)
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.tertiary)
            }
        }
        .tag(page)
    }

    private func count(for page: AppPage) -> Int? {
        switch page {
        case .nodes: return model.itemCount(for: "nodes")
        case .routes: return model.itemCount(for: "routes")
        case .dns: return model.itemCount(for: "dns_profiles")
        case .proxies: return model.itemCount(for: "local_proxies")
        case .rules: return model.itemCount(for: "rules")
        case .subscriptions: return model.itemCount(for: "subscriptions")
        default: return nil
        }
    }

    private func groupLabel(_ key: String, fallback: String) -> String {
        ["status": "状态", "configuration": "配置", "services": "服务", "advanced": "高级"][key] ?? fallback
    }
}

struct PageView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            PageHeader(page: model.selectedPage)
            Divider()
            switch model.selectedPage {
            case .overview:
                OverviewView(model: model)
            case .general:
                ConfigurationFormView(model: model)
            case .configuration:
                ConfigurationView(model: model)
            case .nodes:
                DraftCollectionView(model: model, descriptor: .nodes)
            case .routes:
                DraftCollectionView(model: model, descriptor: .routes)
            case .dns:
                DraftCollectionView(model: model, descriptor: .dns)
            case .rules:
                DraftCollectionView(model: model, descriptor: .rules)
            case .subscriptions:
                DraftCollectionView(model: model, descriptor: .subscriptions)
            case .proxies:
                DraftCollectionView(model: model, descriptor: .proxies)
            case .diagnostics:
                DiagnosticsView(model: model)
            case .settings:
                SystemView(model: model)
            }
        }
        .navigationTitle(model.selectedPage.navigationLabel)
    }
}

private struct PageHeader: View {
    let page: AppPage

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(page.eyebrow)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
            Text(page.navigationLabel)
                .font(.largeTitle.weight(.semibold))
            Text(page.subtitle)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 24)
        .padding(.vertical, 18)
        .background(.bar)
    }
}

struct OverviewView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        List {
            Section {
                executionContent
            } header: {
                Label("执行模型", systemImage: "point.3.filled.connected.trianglepath.dotted")
            }
            Section {
                lifecycleContent
            } header: {
                Label("配置生命周期", systemImage: "arrow.triangle.2.circlepath")
            }
            Section {
                metricGrid
            } header: {
                Label("工作副本规模", systemImage: "chart.bar.xaxis")
            }
            Section {
                validationContent
            } header: {
                Label("校验与警告摘要", systemImage: "checkmark.shield")
            }
            Section {
                lastApplyContent
            } header: {
                Label("最近应用与快捷操作", systemImage: "clock.arrow.circlepath")
            }
        }
        .listStyle(.inset)
        .onAppear {
            if model.validation == nil, model.draftSyntaxError == nil, !model.isBusy {
                model.validate()
            }
        }
    }

    private var executionContent: some View {
        HStack(spacing: 14) {
            pipelineStep(value: model.itemCount(for: "rules"), title: "匹配规则", subtitle: "首条匹配即停止", symbol: "line.3.horizontal.decrease.circle")
            pipelineArrow
            pipelineStep(value: model.itemCount(for: "dns_profiles"), title: "DNS Profile", subtitle: "独立解析路径", symbol: "network")
            pipelineArrow
            pipelineStep(value: model.itemCount(for: "routes"), title: "路由", subtitle: "直连 / 拒绝 / 节点链", symbol: "arrow.triangle.branch")
            pipelineArrow
            pipelineStep(value: nil, title: "网络出口", subtitle: "最终建立连接", symbol: "globe")
        }
    }

    private var lifecycleContent: some View {
        let saved = model.overviewLifecycle.saved
        let active = model.overviewLifecycle.active
        return Grid(alignment: .leading, horizontalSpacing: 28, verticalSpacing: 12) {
            GridRow {
                lifecycleFact("工作副本", model.isDirty ? "有未保存修改" : "已同步")
                lifecycleFact("已保存配置", saved.available ? (saved.enabled ? "启用" : "禁用") : "不可用")
            }
            GridRow {
                lifecycleFact("等待应用", model.overviewLifecycle.pendingApply ? "是" : "否")
                lifecycleFact("当前运行", active.generationID.isEmpty ? "已停止" : (active.healthy ? "正常" : "异常"))
            }
            GridRow {
                lifecycleFact("Saved / Active", lifecycleDifference)
                lifecycleFact("状态来源", "Draft · Saved · Active")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var lifecycleDifference: String {
        let saved = model.overviewLifecycle.saved
        let activeEnabled = !model.overviewLifecycle.active.generationID.isEmpty
        if !saved.available { return "Saved 不可用" }
        if model.overviewLifecycle.pendingApply || saved.enabled != activeEnabled { return "不一致，等待处理" }
        return "一致"
    }

    private func lifecycleFact(_ label: String, _ value: String) -> some View {
        LabeledContent(label, value: value)
    }

    private var metricGrid: some View {
        Grid(horizontalSpacing: 12, verticalSpacing: 12) {
            GridRow {
                metric(value: model.itemCount(for: "nodes"), label: "节点", footnote: "当前工作副本", symbol: "point.3.connected.trianglepath.dotted")
                metric(value: model.itemCount(for: "routes"), label: "路由", footnote: "当前工作副本", symbol: "arrow.triangle.branch")
                metric(value: model.itemCount(for: "dns_profiles"), label: "DNS Profile", footnote: "当前工作副本", symbol: "network")
            }
            GridRow {
                metric(value: model.itemCount(for: "local_proxies"), label: "本地入口", footnote: "当前工作副本", symbol: "rectangle.connected.to.line.below")
                metric(value: model.itemCount(for: "rules"), label: "规则", footnote: "当前工作副本", symbol: "list.number")
                metric(value: model.itemCount(for: "subscriptions"), label: "订阅", footnote: "当前工作副本", symbol: "arrow.down.circle")
            }
        }
    }

    @ViewBuilder
    private var validationContent: some View {
        if let validation = model.validation {
            HStack(spacing: 28) {
                Label("\(validation.errors.count) 个错误", systemImage: validation.errors.isEmpty ? "checkmark.circle" : "xmark.octagon")
                Label("\(validation.warnings.count) 个警告", systemImage: "exclamationmark.triangle")
                Text("当前工作副本")
                    .foregroundStyle(.secondary)
            }
            if validation.warningGroups.isEmpty {
                Text(validation.errors.isEmpty ? "当前工作副本没有聚合警告。" : "请使用工具栏“校验”定位并修复错误。")
                    .foregroundStyle(.secondary)
            } else {
                warningGroups(validation.warningGroups)
            }
        } else {
            Text(model.draftSyntaxError == nil ? "尚未完成当前工作副本校验。" : "请先修复工作副本格式错误。")
                .foregroundStyle(.secondary)
        }
    }

    private var lastApplyContent: some View {
        VStack(alignment: .leading, spacing: 12) {
            Grid(alignment: .leading, horizontalSpacing: 28, verticalSpacing: 10) {
                GridRow {
                    LabeledContent("时间", value: localizedLastApplyTime)
                    LabeledContent("结果", value: model.runtime.lastApply?.result.ok == true ? "成功" : (model.runtime.lastApply == nil ? "—" : "失败"))
                }
            }
            Text(safeLastApplySummary)
                .foregroundStyle(model.runtime.lastApply?.result.ok == false ? Color.orange : Color.secondary)
            ControlGroup {
                Button("刷新") { model.refreshStatus() }
                Button("打开诊断") { model.selectedPage = .diagnostics }
                Button("系统信息") { model.selectedPage = .settings }
            }
            .disabled(model.isBusy)
        }
    }

    private var safeLastApplySummary: String {
        guard let result = model.runtime.lastApply?.result else { return "尚无应用记录。" }
        if result.ok { return "已保存配置已成功切换到运行环境。" }
        if result.activated == true { return "运行配置已变化，但应用未完成；请打开诊断查看恢复步骤。" }
        return "运行配置未切换；已保存配置仍可重试应用。"
    }

    private var localizedLastApplyTime: String {
        guard let record = model.runtime.lastApply else { return "—" }
        if let timestamp = record.timestamp {
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            let date = formatter.date(from: timestamp) ?? ISO8601DateFormatter().date(from: timestamp)
            if let date {
                return date.formatted(date: .abbreviated, time: .standard)
            }
        }
        if let milliseconds = Double(record.sequence), record.sequence.count >= 13 {
            return Date(timeIntervalSince1970: milliseconds / 1000).formatted(date: .abbreviated, time: .standard)
        }
        return "时间未知"
    }

    private func warningGroups(_ groups: [ValidationWarningGroup]) -> some View {
        ForEach(groups) { group in
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(warningGroupLabel(group))
                        .font(.headline)
                    Text("\(group.count) 个正在使用的\(warningGroupScope(group))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 12)
                if let destination = warningGroupDestination(group) {
                    Button("查看") { model.selectedPage = destination }
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func warningGroupLabel(_ group: ValidationWarningGroup) -> String {
        if group.code == "INSECURE_TLS", group.objectType == "dns_profile" { return "DNS 证书校验已关闭" }
        if group.code == "INSECURE_TLS" { return "TLS 证书校验已关闭" }
        if group.code == "SUBSCRIPTION_NODE_STALE" { return "订阅已不再提供此节点" }
        if group.code == "DNS_REJECT_PROJECTION_SKIPPED" { return "DNS 拒绝条件无法在解析前完整执行" }
        if group.code == "DNS_PROJECTION_EMPTY" { return "DNS 将继续匹配后续规则" }
        return group.summary
    }

    private func warningGroupScope(_ group: ValidationWarningGroup) -> String {
        ["node": "节点", "route": "路由", "dns_profile": "DNS Profile", "local_proxy": "本地入口", "rule": "规则"][group.objectType] ?? "对象"
    }

    private func warningGroupDestination(_ group: ValidationWarningGroup) -> AppPage? {
        ["nodes": AppPage.nodes, "routes": .routes, "dns": .dns, "proxies": .proxies,
         "rules": .rules, "subscriptions": .subscriptions, "general": .general][group.destination ?? ""]
    }

    private func pipelineStep(value: Int?, title: String, subtitle: String, symbol: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(title, systemImage: symbol)
                .font(.headline)
            if let value {
                Text(value, format: .number)
                    .font(.title2.monospacedDigit().weight(.semibold))
            }
            Text(subtitle)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 6)
    }

    private var pipelineArrow: some View {
        Image(systemName: "chevron.right")
            .foregroundStyle(.tertiary)
    }

    private func metric(value: Int, label: String, footnote: String, symbol: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Label(label, systemImage: symbol)
                .font(.headline)
            Text(value, format: .number)
                .font(.title.monospacedDigit().weight(.semibold))
            Text(footnote)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 6)
    }
}

struct ConfigurationView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                if model.isDirty {
                    Label("有未保存修改", systemImage: "circle.fill")
                        .foregroundStyle(.orange)
                }
                Spacer()
                ControlGroup {
                    Button("重新载入") { model.loadDraft() }
                    Button("校验") { model.validate() }
                        .disabled(model.draftSyntaxError != nil)
                    DraftActionButtons(model: model)
                }
                .disabled(model.isBusy)
            }
            VStack(alignment: .leading, spacing: 8) {
                Label("JSON 配置", systemImage: "doc.plaintext")
                    .font(.headline)
                Divider()
                TextEditor(text: Binding(
                    get: { model.rawJSON },
                    set: { model.updateRawDraft($0) }
                ))
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .disabled(!model.canEditDraft)
            }
            if !model.message.isEmpty {
                Label(model.message, systemImage: "info.circle")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            if let syntaxError = model.draftSyntaxError {
                Label("JSON 语法错误：\(syntaxError)", systemImage: "xmark.octagon.fill")
                    .font(.callout)
                    .foregroundStyle(.red)
            }
        }
        .padding(24)
    }
}

struct DraftCollectionDescriptor {
    let key: String
    let title: String
    let emptyMessage: String
    let addLabel: String
    let symbol: String
    var ordered: Bool { SteerUISpec.orderingPolicy(for: key) != nil }

    static let nodes = Self(key: "nodes", title: "节点库", emptyMessage: "尚未添加节点", addLabel: "添加节点", symbol: "point.3.connected.trianglepath.dotted")
    static let routes = Self(key: "routes", title: "路由", emptyMessage: "尚未添加路由", addLabel: "添加路由", symbol: "arrow.triangle.branch")
    static let dns = Self(key: "dns_profiles", title: "DNS Profile", emptyMessage: "尚未添加 DNS Profile", addLabel: "添加 Profile", symbol: "network")
    static let rules = Self(key: "rules", title: "顺序规则", emptyMessage: "尚未添加规则", addLabel: "添加规则", symbol: "list.number")
    static let subscriptions = Self(key: "subscriptions", title: "订阅", emptyMessage: "尚未添加订阅", addLabel: "添加订阅", symbol: "arrow.down.circle")
    static let proxies = Self(key: "local_proxies", title: "本地代理", emptyMessage: "尚未添加本地代理", addLabel: "添加入口", symbol: "rectangle.connected.to.line.below")
}

private struct NodeCollectionGroup: Identifiable {
    let id: String
    let label: String
    let count: Int
}

private struct SubscriptionStaleList: View {
    @ObservedObject var model: AppModel
    let status: SubscriptionRuntimeStatus

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label("已失效节点", systemImage: "clock.badge.exclamationmark")
                    .font(.headline)
                Text(status.name ?? status.id)
                    .foregroundStyle(.secondary)
                Spacer()
                Text("\(status.stale.count) 项")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            ForEach(status.stale) { node in
                HStack(alignment: .top, spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(node.name?.isEmpty == false ? node.name! : "未命名节点")
                                .fontWeight(.medium)
                            Label("已保留", systemImage: "pin.fill")
                                .font(.caption)
                                .foregroundStyle(.orange)
                        }
                        Text(node.referencedBy.isEmpty
                             ? "未被任何路由使用，可以安全清理"
                             : "阻止原因：" + node.referencedBy.map(referenceLabel).joined(separator: "，"))
                            .font(.caption)
                            .foregroundStyle(node.referencedBy.isEmpty ? Color.secondary : Color.red)
                    }
                    Spacer()
                    Button("清理") {
                        model.cleanSubscriptionNode(subscriptionID: status.id, nodeID: node.id)
                    }
                    .disabled(model.isBusy || model.isDirty
                              || model.subscriptionOperationInProgress(status.id)
                              || !node.referencedBy.isEmpty)
                    .help(node.referencedBy.isEmpty ? "只移除这个已失效节点；不会改变当前运行配置" : "仍被路由使用，不能清理")
                }
                .padding(.vertical, 3)
            }
        }
        .padding(14)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color(nsColor: .controlBackgroundColor)))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(.quaternary))
    }

    private func referenceLabel(_ reference: SubscriptionReference) -> String {
        let type = ["route": "路由", "rule": "规则"][reference.objectType] ?? "配置"
        let label = reference.name?.isEmpty == false ? reference.name! : "未命名\(type)"
        return "\(type) \(label)"
    }
}

private struct DefaultRuleCard: View {
    let item: DraftItem
    let detail: String
    let isBusy: Bool
    let edit: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Label("Default", systemImage: "pin.fill")
                    .font(.headline)
                Text(item.title)
                    .foregroundStyle(.secondary)
                Spacer()
                Label("始终启用 · 固定在最后", systemImage: "lock.fill")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.green)
                Button(action: edit) {
                    Label("编辑决策", systemImage: "square.and.pencil")
                }
                .buttonStyle(.borderless)
                .disabled(isBusy)
            }
            Text(detail)
                .font(.callout.monospaced())
                .foregroundStyle(.secondary)
            Text("第 \(item.index + 1) 条 · 只能修改显示名称、DNS Profile 与路由")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(nsColor: .controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(.quaternary)
        )
    }
}

private struct CollectionListDropDelegate: DropDelegate {
    let targetID: String
    let entered: (String) -> Void
    let dropped: () -> Bool

    func validateDrop(info: DropInfo) -> Bool { true }
    func dropEntered(info: DropInfo) { entered(targetID) }
    func dropUpdated(info: DropInfo) -> DropProposal? { DropProposal(operation: .move) }
    func performDrop(info: DropInfo) -> Bool { dropped() }
}

func collectionDragPreviewOrder(_ ids: [String], moving sourceID: String, over targetID: String) -> [String] {
    guard sourceID != targetID,
          let sourceIndex = ids.firstIndex(of: sourceID),
          let targetIndex = ids.firstIndex(of: targetID) else { return ids }
    var order = ids
    let source = order.remove(at: sourceIndex)
    guard let adjustedTarget = order.firstIndex(of: targetID) else { return ids }
    order.insert(source, at: sourceIndex < targetIndex ? adjustedTarget + 1 : adjustedTarget)
    return order
}

struct DraftCollectionView: View {
    @ObservedObject var model: AppModel
    let descriptor: DraftCollectionDescriptor
    @State private var selection: DraftItem.ID?
    @State private var editorTarget: DraftEditorTarget?
    @State private var pendingDeletion: DraftItem?
    @State private var actionError = ""
    @State private var blockedReferences: [UIObjectReference] = []
    @State private var nodeImportPresented = false
    @State private var nodeExportTarget: DraftItem?
    @State private var selectedNodeGroup = "_manual"
    @State private var nodeSortMode = "default"
    @State private var nodeSortWorstFirst = false
    @State private var draggedItemID: DraftItem.ID?
    @State private var dragPreviewIDs: [DraftItem.ID] = []

    private var allItems: [DraftItem] { model.draftItems(for: descriptor.key) }
    private var activeNodeGroup: String {
        nodeGroups.contains(where: { $0.id == selectedNodeGroup }) ? selectedNodeGroup : "_manual"
    }
    private var configuredItems: [DraftItem] {
        if descriptor.key == "nodes" {
            let visible = allItems.filter { ($0.sourceSubscription ?? "_manual") == activeNodeGroup }
            return model.nodeItemsSortedForDisplay(visible, mode: nodeSortMode, direction: nodeSortDirection)
        }
        if descriptor.key == "rules" {
            return allItems.filter { !isDefaultRule($0) }
        }
        return allItems
    }
    private var items: [DraftItem] {
        guard !dragPreviewIDs.isEmpty else { return configuredItems }
        let byID = Dictionary(uniqueKeysWithValues: configuredItems.map { ($0.id, $0) })
        return dragPreviewIDs.compactMap { byID[$0] }
    }
    private var defaultRule: DraftItem? { allItems.first(where: isDefaultRule) }
    private var nodeSortDirection: String { nodeSortWorstFirst ? "worst_first" : "best_first" }
    private var orderingEnabled: Bool { descriptor.key != "nodes" || nodeSortMode == "default" }
    private var enabledVisibleNodeIDs: [String] { items.filter(\.enabled).map(\.identifier) }

    private var nodeGroups: [NodeCollectionGroup] {
        guard descriptor.key == "nodes" else { return [] }
        var groups = [NodeCollectionGroup(
            id: "_manual", label: "手动节点",
            count: allItems.filter { $0.sourceSubscription == nil }.count
        )]
        var known = Set(["_manual"])
        for subscription in model.draftItems(for: "subscriptions") {
            known.insert(subscription.identifier)
            groups.append(NodeCollectionGroup(
                id: subscription.identifier, label: subscription.title,
                count: allItems.filter { $0.sourceSubscription == subscription.identifier }.count
            ))
        }
        for source in allItems.compactMap(\.sourceSubscription) where !known.contains(source) {
            known.insert(source)
            groups.append(NodeCollectionGroup(
                id: source, label: "已删除的订阅",
                count: allItems.filter { $0.sourceSubscription == source }.count
            ))
        }
        return groups
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label(descriptor.title, systemImage: descriptor.symbol)
                    .font(.headline)
                Text(descriptor.key == "nodes" ? "\(items.count) / \(allItems.count) 项" : "\(allItems.count) 项")
                    .foregroundStyle(.secondary)
                if descriptor.key == "nodes" {
                    Picker("节点分组", selection: Binding(
                        get: { activeNodeGroup },
                        set: { selectedNodeGroup = $0 }
                    )) {
                        ForEach(nodeGroups) { group in
                            Text("\(group.label) · \(group.count)").tag(group.id)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(maxWidth: 220)
                    .onChange(of: selectedNodeGroup) { _ in selection = nil }
                }
                Spacer()
                Button { editSelected() } label: {
                    Label("编辑", systemImage: "square.and.pencil")
                }
                .disabled(model.isBusy || model.draftSyntaxError != nil
                          || selectedItem == nil || selectedItem?.subscriptionOwned == true)
                Button { moveSelected(offset: -1) } label: {
                    Label("上移", systemImage: "arrow.up")
                }
                .disabled(model.isBusy || model.draftSyntaxError != nil || !canMoveSelected(offset: -1))
                Button { moveSelected(offset: 1) } label: {
                    Label("下移", systemImage: "arrow.down")
                }
                .disabled(model.isBusy || model.draftSyntaxError != nil || !canMoveSelected(offset: 1))
                if descriptor.key == "nodes" {
                    Button {
                        selectedNodeGroup = "_manual"
                        nodeImportPresented = true
                    } label: {
                        Label("导入", systemImage: "square.and.arrow.down")
                    }
                    .disabled(model.isBusy || model.draftSyntaxError != nil)
                    Menu("批量测速") {
                        Button("批量连接测试") {
                            model.runAllNodeProbes(download: false, nodeIDs: enabledVisibleNodeIDs)
                        }
                        Button("批量下载测试") {
                            model.runAllNodeProbes(download: true, nodeIDs: enabledVisibleNodeIDs)
                        }
                    }
                    .disabled(model.isBusy || model.isDirty || model.isBatchNodeProbeRunning || enabledVisibleNodeIDs.isEmpty)
                }
                if descriptor.key == "subscriptions", let selectedItem {
                    Button("立即更新") { model.updateSubscription(selectedItem.identifier) }
                        .disabled(model.isBusy || model.isDirty
                                  || model.subscriptionOperationInProgress(selectedItem.identifier)
                                  || model.subscriptionStatus(selectedItem.identifier)?.enabled == false)
                        .help(model.subscriptionStatus(selectedItem.identifier)?.enabled == false
                              ? "已停用的订阅不能更新；请先启用并保存"
                              : "更新已保存的节点列表，不改变当前运行配置")
                }
                Button { addItem() } label: {
                    Label(descriptor.addLabel, systemImage: "plus")
                }
                .buttonStyle(.borderedProminent)
                .disabled(model.isBusy || model.draftSyntaxError != nil)
            }

            if descriptor.key == "subscriptions" {
                Label("订阅会定时刷新节点列表；无引用的消失节点会自动删除，仍被路由使用的节点将保留并告警。", systemImage: "info.circle")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Group {
                if descriptor.key == "nodes" {
                    nodeTable
                } else {
                    standardTable
                }
            }
            .disabled(!model.canEditDraft || model.draftSyntaxError != nil)
            .animation(.snappy(duration: 0.16), value: items.map(\.id))
            .overlay {
                if let syntaxError = model.draftSyntaxError {
                    VStack(spacing: 9) {
                        Image(systemName: "xmark.octagon.fill")
                            .font(.largeTitle)
                            .foregroundStyle(.red)
                        Text("JSON 配置格式有误")
                            .font(.headline)
                        Text(syntaxError)
                            .font(.callout.monospaced())
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .lineLimit(4)
                        Text("请先在高级配置页面修复后再继续。")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .padding(24)
                } else if items.isEmpty {
                    VStack(spacing: 9) {
                        Image(systemName: descriptor.symbol)
                            .font(.largeTitle)
                            .foregroundStyle(.tertiary)
                        Text(descriptor.emptyMessage)
                            .font(.headline)
                        Text("使用右上角的“\(descriptor.addLabel)”创建工作副本条目。")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .contextMenu(forSelectionType: DraftItem.ID.self) { selected in
                if let id = selected.first, let item = items.first(where: { $0.id == id }) {
                    if descriptor.ordered {
                        Button("上移") { move(item, offset: -1) }
                            .disabled(!canMove(item, offset: -1))
                        Button("下移") { move(item, offset: 1) }
                            .disabled(!canMove(item, offset: 1))
                        Divider()
                    }
                    if descriptor.key == "nodes" {
                        Button("导出链接") { nodeExportTarget = item }
                            .disabled(model.isBusy)
                        Divider()
                    }
                    Button("编辑") { edit(item) }
                        .disabled(item.subscriptionOwned)
                    if !isSystemRoute(item) && !isDefaultRule(item) {
                        Button("删除", role: .destructive) {
                            requestDeletion(item)
                        }
                        .disabled(item.subscriptionOwned)
                    }
                }
            } primaryAction: { selected in
                if let id = selected.first, let item = items.first(where: { $0.id == id }) {
                    edit(item)
                }
            }

            if descriptor.key == "subscriptions", let selectedItem,
               let status = model.subscriptionStatus(selectedItem.identifier), !status.stale.isEmpty {
                SubscriptionStaleList(model: model, status: status)
            }

            if let defaultRule {
                DefaultRuleCard(
                    item: defaultRule,
                    detail: runtimeDetail(defaultRule),
                    isBusy: model.isBusy
                ) {
                    edit(defaultRule)
                }
            }

            HStack {
                if !actionError.isEmpty {
                    Label(actionError, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                } else if descriptor.key == "rules" {
                    Label("从上到下严格匹配；Default 必须位于最后。", systemImage: "info.circle")
                        .foregroundStyle(.secondary)
                } else if descriptor.key == "nodes" {
                    Label("订阅节点只读；手动节点可编辑。", systemImage: "info.circle")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if model.isDirty {
                    Label("工作副本已修改", systemImage: "circle.fill")
                        .foregroundStyle(.orange)
                }
            }
            .font(.caption)
            if !blockedReferences.isEmpty {
                HStack(spacing: 8) {
                    Text("先修改引用：").foregroundStyle(.secondary)
                    ForEach(blockedReferences, id: \.sourceID) { reference in
                        Button(reference.sourceLabel) {
                            model.focusValidationIssue(ValidationIssue(
                                code: "STILL_REFERENCED", objectType: reference.sourceObjectType,
                                objectID: reference.sourceID, option: reference.field,
                                message: "对象仍引用待删除项目"
                            ))
                        }
                        .buttonStyle(.link)
                    }
                }
                .font(.caption)
            }
        }
        .padding(24)
        .sheet(item: $editorTarget) { target in
            DraftItemEditor(model: model, target: target)
        }
        .sheet(isPresented: $nodeImportPresented) {
            NodeImportSheet(model: model)
        }
        .sheet(item: $nodeExportTarget) { item in
            NodeExportSheet(model: model, item: item)
        }
        .onAppear { openValidationFocus() }
        .onChange(of: model.validationFocus?.id) { _ in openValidationFocus() }
        .alert(
            "删除“\(pendingDeletion?.title ?? "项目")”？",
            isPresented: Binding(
                get: { pendingDeletion != nil },
                set: { if !$0 { pendingDeletion = nil } }
            )
        ) {
            Button("取消", role: .cancel) { pendingDeletion = nil }
            Button("删除", role: .destructive) {
                if let item = pendingDeletion {
                    model.removeDraftItem(from: descriptor.key, at: item.index)
                    selection = nil
                }
                pendingDeletion = nil
            }
        } message: {
            Text(descriptor.key == "subscriptions"
                 ? "相关订阅节点也会从工作副本移除。"
                 : "此操作只修改工作副本，应用前仍可重新载入系统配置。")
        }
    }

    private var nodeTable: some View {
        VStack(spacing: 0) {
            nodeListHeader
            Divider()
            collectionList { item in nodeListRow(item) }
        }
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private var standardTable: some View {
        VStack(spacing: 0) {
            standardListHeader
            Divider()
            collectionList { item in standardListRow(item) }
        }
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private func collectionList<Row: View>(
        @ViewBuilder row: @escaping (DraftItem) -> Row
    ) -> some View {
        List(selection: $selection) {
            ForEach(items) { item in
                reorderableListRow(item) { row(item) }
                    .tag(item.id)
                    .contentShape(Rectangle())
            }
        }
        .listStyle(.inset(alternatesRowBackgrounds: true))
    }

    private var nodeListHeader: some View {
        HStack(spacing: 10) {
            headerLabel("状态", width: 48)
            headerLabel("名称", width: 130, alignment: .leading)
            headerLabel("类型", width: 76, alignment: .leading)
            nodeSortHeader("连接测速", mode: "connect", width: 140)
            nodeSortHeader("下载测速", mode: "download", width: 140)
            headerLabel("操作", width: 100, alignment: .leading)
            headerLabel("详情", alignment: .leading)
            headerLabel("顺序", width: 62)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .font(.caption.weight(.semibold))
        .foregroundStyle(.secondary)
    }

    private var standardListHeader: some View {
        HStack(spacing: 10) {
            headerLabel("状态", width: 48)
            headerLabel("名称", width: 180, alignment: .leading)
            headerLabel("类型", width: 90, alignment: .leading)
            headerLabel("操作", width: 200, alignment: .leading)
            headerLabel("详情", alignment: .leading)
            headerLabel(descriptor.ordered ? "顺序" : "", width: 62)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .font(.caption.weight(.semibold))
        .foregroundStyle(.secondary)
    }

    private func headerLabel(
        _ label: String,
        width: CGFloat? = nil,
        alignment: Alignment = .center
    ) -> some View {
        Text(label)
            .frame(width: width, alignment: alignment)
            .frame(maxWidth: width == nil ? .infinity : nil, alignment: alignment)
    }

    private func nodeSortHeader(_ label: String, mode: String, width: CGFloat) -> some View {
        Button {
            cycleNodeSort(mode)
        } label: {
            HStack(spacing: 4) {
                Text(label)
                if nodeSortMode == mode {
                    Image(systemName: nodeSortWorstFirst ? "arrow.down" : "arrow.up")
                }
            }
            .frame(width: width)
        }
        .buttonStyle(.plain)
    }

    private func nodeListRow(_ item: DraftItem) -> some View {
        HStack(spacing: 10) {
            statusCell(item).frame(width: 48)
            nameCell(item).frame(width: 130, alignment: .leading)
            Text(kindLabel(item)).font(.caption.weight(.medium)).frame(width: 76, alignment: .leading)
            nodeProbeCell(item, download: false).frame(width: 140, alignment: .leading)
            nodeProbeCell(item, download: true).frame(width: 140, alignment: .leading)
            collectionActions(item).frame(width: 100, alignment: .leading)
            detailCell(item).frame(maxWidth: .infinity, alignment: .leading)
            orderingCell(item).frame(width: 62)
        }
        .padding(.vertical, 3)
    }

    private func standardListRow(_ item: DraftItem) -> some View {
        HStack(spacing: 10) {
            statusCell(item).frame(width: 48)
            nameCell(item).frame(width: 180, alignment: .leading)
            Text(kindLabel(item)).font(.caption.weight(.medium)).frame(width: 90, alignment: .leading)
            collectionActions(item).frame(width: 200, alignment: .leading)
            detailCell(item).frame(maxWidth: .infinity, alignment: .leading)
            orderingCell(item).frame(width: 62)
        }
        .padding(.vertical, 3)
    }

    private var selectedItem: DraftItem? {
        guard let selection else { return nil }
        return items.first { $0.id == selection }
    }

    private var movableItems: [DraftItem] { items.filter(isMovable) }

    private func isMovable(_ item: DraftItem) -> Bool {
        guard let object = model.draftItemObject(for: descriptor.key, at: item.index) else { return false }
        return SteerUISpec.isMovable(collection: descriptor.key, object: object)
    }

    private func canMove(_ item: DraftItem, offset: Int) -> Bool {
        guard orderingEnabled, isMovable(item),
              let position = movableItems.firstIndex(where: { $0.id == item.id }) else { return false }
        return movableItems.indices.contains(position + offset)
    }

    private func canMoveSelected(offset: Int) -> Bool {
        selectedItem.map { canMove($0, offset: offset) } ?? false
    }

    private func moveSelected(offset: Int) {
        guard let selectedItem else { return }
        move(selectedItem, offset: offset)
    }

    private func move(_ item: DraftItem, offset: Int) {
        withAnimation(.snappy(duration: 0.16)) {
            if model.moveDraftItem(
                in: descriptor.key,
                identifiedBy: item.identifier,
                offset: offset,
                visibleIDs: movableItems.map(\.identifier)
            ) {
                selection = item.id
            }
        }
    }

    private func orderingRestriction(_ item: DraftItem) -> String {
        if descriptor.key == "nodes", !orderingEnabled { return "点击“顺序”列后可调整工作副本顺序" }
        if isDefaultRule(item) { return "Default 固定在最后" }
        if isSystemRoute(item) { return "系统路由顺序固定" }
        return "此项目不可移动"
    }

    private func rowDragEnabled(_ item: DraftItem) -> Bool {
        descriptor.ordered && orderingEnabled && isMovable(item)
    }

    private func cycleNodeSort(_ mode: String) {
        if nodeSortMode != mode {
            nodeSortMode = mode
            nodeSortWorstFirst = false
        } else if !nodeSortWorstFirst {
            nodeSortWorstFirst = true
        } else {
            nodeSortMode = "default"
            nodeSortWorstFirst = false
        }
    }

    private func reorderableListRow<Content: View>(
        _ item: DraftItem,
        @ViewBuilder content: () -> Content
    ) -> AnyView {
        let row = content()
        guard rowDragEnabled(item) else { return AnyView(row) }
        return AnyView(
            row
                .opacity(draggedItemID == item.id ? 0.25 : 1)
                .onDrop(
                    of: [UTType.utf8PlainText],
                    delegate: CollectionListDropDelegate(
                        targetID: item.id,
                        entered: previewListDrag(over:),
                        dropped: commitListDrag
                    )
                )
        )
    }

    private func beginListDrag(_ item: DraftItem) -> NSItemProvider {
        let previewIDs = configuredItems.map(\.id)
        withTransaction(Transaction(animation: nil)) {
            draggedItemID = item.id
            dragPreviewIDs = previewIDs
        }
        return NSItemProvider(object: item.id as NSString)
    }

    private func listDragPreview(_ item: DraftItem) -> some View {
        HStack(spacing: 12) {
            Image(systemName: item.enabled ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(item.enabled ? Color.green : Color.secondary)
            Text(item.title).fontWeight(.semibold)
            Text(kindLabel(item)).font(.caption).foregroundStyle(.secondary)
            Spacer(minLength: 12)
            Text(runtimeDetail(item))
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Image(systemName: "line.3.horizontal")
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .frame(width: 680)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private func previewListDrag(over targetID: DraftItem.ID) {
        guard let sourceID = draggedItemID, sourceID != targetID else { return }
        withAnimation(.snappy(duration: 0.16)) {
            dragPreviewIDs = collectionDragPreviewOrder(dragPreviewIDs, moving: sourceID, over: targetID)
        }
    }

    private func commitListDrag() -> Bool {
        guard let sourceID = draggedItemID,
              let source = configuredItems.first(where: { $0.id == sourceID }),
              let index = dragPreviewIDs.firstIndex(of: sourceID) else {
            cancelListDrag()
            return false
        }
        let nextID = dragPreviewIDs.indices.contains(index + 1) ? dragPreviewIDs[index + 1] : nil
        let target = nextID.flatMap { id in configuredItems.first(where: { $0.id == id }) }
        withAnimation(.snappy(duration: 0.16)) {
            _ = model.moveDraftItem(
                in: descriptor.key,
                identifiedBy: source.identifier,
                before: target?.identifier
            )
            selection = source.id
            draggedItemID = nil
            dragPreviewIDs = []
        }
        return true
    }

    private func cancelListDrag() {
        withAnimation(.snappy(duration: 0.16)) {
            draggedItemID = nil
            dragPreviewIDs = []
        }
    }

    private func addItem() {
        if descriptor.key == "nodes" { selectedNodeGroup = "_manual" }
        guard let object = model.newDraftItemObject(for: descriptor.key) else { return }
        editorTarget = DraftEditorTarget(
            key: descriptor.key, index: nil, title: descriptor.addLabel,
            object: object
        )
    }

    private func editSelected() {
        guard let selectedItem else { return }
        edit(selectedItem)
    }

    private func edit(_ item: DraftItem) {
        guard !item.subscriptionOwned,
              let object = model.draftItemObject(for: descriptor.key, at: item.index) else { return }
        editorTarget = DraftEditorTarget(
            key: descriptor.key, index: item.index, title: item.title,
            object: object
        )
    }

    private func requestDeletion(_ item: DraftItem) {
        if let reason = model.deletionBlockReason(for: descriptor.key, at: item.index) {
            actionError = reason
            blockedReferences = model.deletionReferences(for: descriptor.key, at: item.index)
            return
        }
        actionError = ""
        blockedReferences = []
        pendingDeletion = item
    }

    private func openValidationFocus() {
        let objectType = [
            "nodes": "node", "routes": "route", "dns_profiles": "dns_profile",
            "local_proxies": "local_proxy", "rules": "rule", "subscriptions": "subscription",
        ][descriptor.key]
        guard let objectType,
              let issue = model.takeValidationFocus(objectType: objectType),
              let objectID = issue.objectID,
              let item = allItems.first(where: { $0.identifier == objectID }) else { return }
        if descriptor.key == "nodes" { selectedNodeGroup = item.sourceSubscription ?? "_manual" }
        selection = item.id
        guard !item.subscriptionOwned,
              let object = model.draftItemObject(for: descriptor.key, at: item.index) else {
            actionError = "已定位只读对象 \(item.title) / \(issue.option ?? "")"
            return
        }
        editorTarget = DraftEditorTarget(
            key: descriptor.key, index: item.index, title: item.title,
            object: object, focusOption: issue.option
        )
    }

    private func isRequiredDirect(_ item: DraftItem) -> Bool {
        descriptor.key == "routes" && item.kind.caseInsensitiveCompare("direct") == .orderedSame
    }

    private func kindLabel(_ item: DraftItem) -> String {
        if descriptor.key == "routes", item.kind.caseInsensitiveCompare("block") == .orderedSame {
            return "REJECT"
        }
        return item.kind.isEmpty ? "—" : item.kind.uppercased()
    }

    private func isSystemRoute(_ item: DraftItem) -> Bool {
        descriptor.key == "routes" && ["direct", "block"].contains(item.kind.lowercased())
    }

    private func isDefaultRule(_ item: DraftItem) -> Bool {
        descriptor.key == "rules" && item.kind.caseInsensitiveCompare("default") == .orderedSame
    }

    private func statusCell(_ item: DraftItem) -> AnyView {
        if item.subscriptionOwned || isRequiredDirect(item) {
            return AnyView(
                Label(item.enabled ? "启用" : "停用", systemImage: "lock.fill")
                    .labelStyle(.iconOnly)
                    .foregroundStyle(item.enabled ? Color.green : Color.secondary)
                    .help(item.subscriptionOwned ? "订阅节点状态由订阅管理" : "系统直连路由始终启用")
            )
        }
        let enabled = Binding(
            get: {
                model.draftItemEnabled(in: descriptor.key, identifiedBy: item.identifier) ?? item.enabled
            },
            set: {
                model.setDraftItemEnabled(in: descriptor.key, identifiedBy: item.identifier, enabled: $0)
            }
        )
        return AnyView(
            Toggle("", isOn: enabled)
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(.small)
                .id("\(item.id):enabled")
        )
    }

    private func nameCell(_ item: DraftItem) -> AnyView {
        AnyView(
            HStack {
                HStack(spacing: 6) {
                    Text(item.title).fontWeight(.medium)
                    if descriptor.key == "nodes", item.pinnedStale {
                        Label("已失效", systemImage: "pin.fill")
                            .font(.caption)
                            .foregroundStyle(.orange)
                            .help("原订阅中已不再包含此节点；因仍在使用而暂时保留")
                    }
                }
                Spacer(minLength: 0)
            }
        )
    }

    private func detailCell(_ item: DraftItem) -> AnyView {
        AnyView(
            VStack(alignment: .leading, spacing: 2) {
                Text(runtimeDetail(item))
                    .font(.callout.monospaced())
                    .lineLimit(descriptor.key == "rules" ? 3 : 1)
                    .foregroundStyle(.secondary)
                    .help(runtimeDetail(item))
                if descriptor.key != "nodes" {
                    ForEach(probePresentations(item)) { result in
                        Text(result.text)
                            .font(.caption.monospaced())
                            .foregroundStyle(result.stale ? Color.orange : (result.ok ? Color.green : Color.red))
                    }
                }
            }
        )
    }

    private func orderingCell(_ item: DraftItem) -> AnyView {
        guard descriptor.ordered else { return AnyView(EmptyView()) }
        let movable = orderingEnabled && isMovable(item)
        let help = movable ? "拖动或使用上移/下移调整工作副本顺序" : orderingRestriction(item)
        let handle = HStack(spacing: 7) {
            Text(item.index + 1, format: .number)
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
            Image(systemName: movable ? "line.3.horizontal" : "pin.fill")
                .foregroundStyle(.secondary)
        }
            .frame(maxWidth: .infinity, minHeight: 32)
            .contentShape(Rectangle())
            .background(
                Color.secondary.opacity(movable ? 0.08 : 0),
                in: RoundedRectangle(cornerRadius: 6, style: .continuous)
            )
            .help(help)
            .accessibilityLabel(movable ? "拖动 \(item.title) 调整顺序" : "\(item.title) \(help)")
        guard rowDragEnabled(item) else { return AnyView(handle) }
        return AnyView(
            handle.onDrag { beginListDrag(item) } preview: { listDragPreview(item) }
        )
    }

    private func collectionActions(_ item: DraftItem) -> AnyView {
        AnyView(HStack(spacing: 8) {
            if descriptor.key == "nodes" {
                Button { nodeExportTarget = item } label: {
                    Image(systemName: "square.and.arrow.up")
                }
                .buttonStyle(.borderless)
                .disabled(model.isBusy)
                .help("导出节点分享链接")
            }
            if descriptor.key == "routes", item.kind == "single" {
                probeButton(item: item, scope: "routes", download: false)
                probeButton(item: item, scope: "routes", download: true)
            } else if descriptor.key == "subscriptions" {
                Button { model.updateSubscription(item.identifier) } label: {
                    if model.subscriptionOperationInProgress(item.identifier) {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: "arrow.clockwise")
                    }
                }
                .buttonStyle(.borderless)
                .disabled(model.isDirty || model.subscriptionOperationInProgress(item.identifier)
                          || model.subscriptionStatus(item.identifier)?.enabled == false)
                .help(model.subscriptionStatus(item.identifier)?.enabled == false
                      ? "已停用的订阅不能更新"
                      : "更新已保存的节点列表，不改变当前运行配置")
            }
            if item.subscriptionOwned {
                Image(systemName: "lock.fill")
                    .foregroundStyle(.secondary)
                    .help("订阅节点只读")
            } else {
                Button { edit(item) } label: {
                    Image(systemName: "square.and.pencil")
                }
                .buttonStyle(.borderless)
                if !isSystemRoute(item) && !isDefaultRule(item) {
                    Button(role: .destructive) {
                        requestDeletion(item)
                    } label: {
                        Image(systemName: "trash")
                    }
                    .buttonStyle(.borderless)
                }
            }
        })
    }

    private func nodeProbeCell(_ item: DraftItem, download: Bool) -> AnyView {
        let presentation = model.latestProbePresentation(
            scope: "nodes", objectID: item.identifier, download: download
        )
        let prefix = download ? "下载 · " : "连接 · "
        return AnyView(
            VStack(alignment: .leading, spacing: 3) {
                probeButton(item: item, scope: "nodes", download: download)
                if let presentation {
                    Text(presentation.text.hasPrefix(prefix)
                         ? String(presentation.text.dropFirst(prefix.count))
                         : presentation.text)
                        .font(.caption2.monospaced())
                        .lineLimit(2)
                        .foregroundStyle(
                            presentation.stale ? Color.orange : (presentation.ok ? Color.green : Color.red)
                        )
                        .help(presentation.text)
                }
            }
        )
    }

    private func probePresentations(_ item: DraftItem) -> [ProbeLatestPresentation] {
        guard descriptor.key == "nodes" || descriptor.key == "routes" else { return [] }
        return [false, true].compactMap { download in
            model.latestProbePresentation(scope: descriptor.key, objectID: item.identifier, download: download)
        }
    }

    @ViewBuilder
    private func probeButton(item: DraftItem, scope: String, download: Bool) -> some View {
        let running = model.probeInProgress(scope: scope, objectID: item.identifier, download: download)
        Button {
            model.runProbe(
                kind: "speedtest",
                nodeID: scope == "nodes" ? item.identifier : nil,
                routeID: scope == "routes" ? item.identifier : nil,
                download: download
            )
        } label: {
            if running {
                ProgressView().controlSize(.small)
            } else {
                Label(download ? "下载" : "连接", systemImage: download ? "arrow.down.circle" : "network")
                    .font(.caption)
            }
        }
        .buttonStyle(.borderless)
        .help(!item.enabled ? "已停用对象不能测试" : (download ? "下载测速" : (scope == "routes" ? "路由链连接测试" : "连接测试")))
        .disabled(model.isDirty || running || !item.enabled)
    }

    private func runtimeDetail(_ item: DraftItem) -> String {
        guard descriptor.key == "subscriptions", let status = model.subscriptionStatus(item.identifier) else {
            return item.detail.isEmpty ? "—" : item.detail
        }
        let success = status.lastSuccess ?? "—"
        let failure = status.lastFailure.map { " · 最近失败 \($0.summary)" } ?? ""
        return "\(status.stateLabel) · \(status.inventorySummary) · 最近成功 \(success)\(failure)"
    }
}

struct DiagnosticsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        List {
            Section("连通性探测") {
                Text("使用设备当前网络环境访问已保存的测试地址；即使 Steer 未启用也可以测试。成功仅表示该地址在测试时可达。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack {
                    overviewProbeButton("测试直连 URL", kind: "direct")
                    overviewProbeButton("测试代理 URL", kind: "proxy")
                    overviewProbeButton("测试下载 URL", kind: "speedtest", download: true)
                }
                overviewProbeResult("直连 URL", kind: "direct")
                overviewProbeResult("代理 URL", kind: "proxy")
                overviewProbeResult("下载 URL", kind: "speedtest")
            }
            if !model.diagnosticsWarnings.isEmpty {
                Section("诊断数据") {
                    ForEach(Array(model.diagnosticsWarnings.prefix(3)), id: \.self) { warning in
                    Label(warning, systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    }
                }
            }
            Section("配置校验") {
                if let validation = model.validation {
                    LabeledContent("结果", value: validation.ok ? "通过" : "失败")
                    ForEach(validation.errors) { issue in
                        issueRow(issue, isError: true)
                    }
                    ForEach(validation.warnings) { issue in
                        issueRow(issue, isError: false)
                    }
                } else {
                    Label("尚未校验", systemImage: "questionmark.circle")
                        .foregroundStyle(.secondary)
                }
                Button("校验当前工作副本") { model.validate() }
                    .disabled(model.isBusy)
            }
            Section("系统 DNS 接管检查") {
                if let capture = model.diagnosticsDNSCapture {
                    LabeledContent("结果", value: capture.configured ? "已配置" : "未确认")
                    LabeledContent(
                        "详情",
                        value: capture.detail == "the published Active generation contains the expected port-53 capture artifacts"
                            ? "系统 DNS 接管已配置"
                            : capture.detail
                    )
                } else {
                    Label("尚未检查系统 DNS 接管状态", systemImage: "questionmark.circle")
                        .foregroundStyle(.secondary)
                }
            }
            Section("服务状态") {
                LabeledContent("运行状态", value: model.runtime.healthy ? "正常" : (model.runtime.generationID.isEmpty ? "未运行" : "异常"))
                if !model.runtime.error.isEmpty {
                    LabeledContent("错误", value: model.runtime.error)
                }
                Button("刷新状态") { model.refreshStatus() }
                    .disabled(model.isBusy)
            }
            Section("最近应用") {
                if let apply = model.runtime.lastApply {
                    LabeledContent("结果", value: apply.result.ok ? "成功" : "失败")
                    if let error = apply.result.error, !error.isEmpty {
                        LabeledContent("错误", value: error)
                    }
                } else {
                    Text("尚无应用记录").foregroundStyle(.secondary)
                }
            }
            Section("最近消息") {
                Text(model.message.isEmpty ? "—" : model.message)
                    .textSelection(.enabled)
            }
            Section("最近日志") {
                if model.diagnosticsLog.isEmpty {
                    Label("尚未读取日志", systemImage: "doc.text.magnifyingglass")
                        .foregroundStyle(.secondary)
                } else {
                    ScrollView([.horizontal, .vertical]) {
                        Text(model.diagnosticsLog)
                            .font(.system(.caption, design: .monospaced))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .topLeading)
                    }
                    .frame(minHeight: 220, maxHeight: 320)
                }
                Button("刷新日志") { model.refreshLogs() }
                    .disabled(model.isBusy)
            }
            Section {
                Button("刷新全部诊断") { model.refreshDiagnostics() }
                    .disabled(model.isBusy)
            }
        }
        .listStyle(.inset)
        .task {
            if model.diagnosticsLog.isEmpty { model.refreshLogs() }
        }
    }

    @ViewBuilder
    private func overviewProbeButton(_ title: String, kind: String, download: Bool = false) -> some View {
        let running = model.overviewProbeInProgress(kind)
        Button {
            model.runProbe(kind: kind, download: download)
        } label: {
            if running {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text(title)
                }
            } else {
                Text(title)
            }
        }
        .disabled(running)
        .help("使用设备当前网络环境进行测试")
    }

    @ViewBuilder
    private func overviewProbeResult(_ title: String, kind: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            LabeledContent(title, value: model.overviewProbeSummary(kind))
            if let detail = model.overviewProbeDetail(kind) {
                Text(detail)
                    .font(.caption2.monospaced())
                    .foregroundStyle(model.overviewProbeIsStale(kind) ? Color.orange : Color.secondary)
                    .textSelection(.enabled)
            }
        }
    }

    private func issueRow(_ issue: ValidationIssue, isError: Bool) -> some View {
        return Button {
            model.focusValidationIssue(issue)
        } label: {
            HStack {
                Label(validationIssueMessage(issue), systemImage: isError ? "xmark.octagon.fill" : "exclamationmark.triangle.fill")
                    .foregroundStyle(isError ? .red : .orange)
                Spacer()
                if issue.destinationPage != nil {
                    Image(systemName: "chevron.right")
                        .foregroundStyle(.tertiary)
                }
            }
        }
        .buttonStyle(.plain)
        .disabled(issue.destinationPage == nil)
    }

    private func validationIssueMessage(_ issue: ValidationIssue) -> String {
        switch issue.code {
        case "REQUIRED": return "必填字段尚未填写"
        case "DANGLING_NODE": return "所选节点不存在"
        case "DANGLING_DETOUR": return "所选前置路由不存在"
        case "DANGLING_DNS_PROFILE": return "所选 DNS Profile 不存在"
        case "DANGLING_ROUTE": return "所选路由不存在"
        case "DANGLING_LOCAL_PROXY": return "所选本地代理入口不存在"
        case "DISABLED_NODE": return "所选节点已停用"
        case "DISABLED_DETOUR": return "所选前置路由已停用"
        case "DISABLED_DNS_PROFILE": return "所选 DNS Profile 已停用"
        case "DISABLED_ROUTE": return "所选路由已停用"
        case "DISABLED_LOCAL_PROXY": return "所选本地代理入口已停用"
        case "ROUTE_DETOUR_CYCLE": return "前置代理链存在循环引用"
        case "LOCAL_PROXY_AUTH_REQUIRED": return "该监听地址允许其他设备连接，必须设置用户名和密码"
        case "INVALID_DURATION": return "更新间隔必须是大于零的时长"
        case "DNS_PROJECTION_EMPTY": return "该规则仅含连接阶段条件，不影响 DNS 上游选择"
        default: return issue.message
        }
    }

}

struct SystemView: View {
    @ObservedObject var model: AppModel
    @State private var showingUninstall = false
    @State private var showingDeleteUserData = false

    var body: some View {
        List {
            Section("系统组件") {
                LabeledContent("安装状态") {
                    Label(
                        model.systemComponentsUpdateAvailable ? "可更新" : (model.systemComponentsNeedRepair ? "安装不完整" : (model.systemComponentsInstalled ? "已安装" : "未安装")),
                        systemImage: model.systemComponentsUpdateAvailable
                            ? "arrow.down.circle.fill"
                            : (model.systemComponentsNeedRepair ? "wrench.and.screwdriver.fill" : (model.systemComponentsInstalled ? "checkmark.seal.fill" : "shippingbox"))
                    )
                    .foregroundStyle(model.systemComponentsInstalled && !model.systemComponentsUpdateAvailable ? .green : .orange)
                }
                if model.systemComponentsUpdateAvailable, model.embeddedInstallerAvailable {
                    Button("更新系统组件…") { model.installSystemComponents() }
                        .buttonStyle(.borderedProminent)
                        .disabled(model.isBusy)
                    Text("更新会再次请求管理员授权；用户配置和运行状态会保留。")
                        .foregroundStyle(.secondary)
                } else if model.systemComponentsNeedRepair, model.embeddedInstallerAvailable {
                    Button("修复系统组件…") { model.installSystemComponents() }
                        .buttonStyle(.borderedProminent)
                        .disabled(model.isBusy)
                    Text("修复会补齐缺失或损坏的组件，并保留用户配置、运行状态和当前工作副本。")
                        .foregroundStyle(.secondary)
                } else if !model.systemComponentsInstalled {
                    if model.embeddedInstallerAvailable {
                        Button("安装系统组件…") { model.installSystemComponents() }
                            .buttonStyle(.borderedProminent)
                            .disabled(model.isBusy)
                        Text("首次安装会请求管理员授权，并安装运行 Steer 所需的系统组件。")
                            .foregroundStyle(.secondary)
                    } else {
                        Text("当前开发构建不包含安装组件；请使用项目提供的安装脚本。")
                            .foregroundStyle(.secondary)
                    }
                }
                if model.systemComponentsHaveArtifacts {
                    if model.embeddedUninstallerAvailable {
                        Button("卸载系统组件…", role: .destructive) { showingUninstall = true }
                            .disabled(model.isBusy)
                    } else {
                        Text("当前 App 不包含受控卸载器，不能从 GUI 卸载程序组件。")
                            .foregroundStyle(.secondary)
                    }
                }
                Text("默认卸载会保留用户配置、运行状态和日志。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !model.systemComponentFacts.isEmpty {
                Section("组件详情") {
                    ForEach(model.systemComponentFacts) { fact in
                        VStack(alignment: .leading, spacing: 5) {
                            HStack {
                                Label(fact.label, systemImage: componentSymbol(fact.state))
                                    .foregroundStyle(componentColor(fact.state))
                                Spacer()
                                Text(fact.detail)
                                    .foregroundStyle(fact.ready ? .secondary : componentColor(fact.state))
                            }
                            Text(fact.path)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                            if !fact.requiredForInstallation, fact.state == .inactive {
                                Text("运行服务未激活不影响系统组件安装完整性。")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
            Section("版本与运行时") {
                LabeledContent("Steer 版本", value: model.versions.helper)
                LabeledContent("核心版本", value: model.versions.singBox)
                LabeledContent("运行状态", value: model.runtime.healthy ? "正常" : (model.runtime.generationID.isEmpty ? "未运行" : "异常"))
                LabeledContent("上次应用", value: model.runtime.lastApply.map { $0.result.ok ? "成功" : "失败" } ?? "—")
                LabeledContent("规则数据版本", value: model.versions.geoVersion)
                LabeledContent("规则总数", value: model.versions.geoRuleCount?.formatted() ?? "—")
                LabeledContent("GeoSite 规则集", value: model.geositeNames.count.formatted())
                LabeledContent("GeoIP 分类", value: model.geoipNames.count.formatted())
            }
            Section("存储路径") {
                pathRow("配置", "/Library/Application Support/Steer/config/config.json")
                pathRow("运行目录", "/Library/Application Support/Steer/run")
                pathRow("状态目录", "/Library/Application Support/Steer/state")
                pathRow("规则数据", "/Library/Application Support/Steer/geodata-seed")
                pathRow("日志", "/Library/Logs/Steer")
            }
            Section("授权") {
                Text("首次安装系统组件需要管理员授权；日常保存和应用配置不再重复请求授权。")
                    .foregroundStyle(.secondary)
            }
        }
        .listStyle(.inset)
        .confirmationDialog("卸载 Steer 系统组件？", isPresented: $showingUninstall, titleVisibility: .visible) {
            Button("卸载并保留用户数据", role: .destructive) {
                model.uninstallSystemComponents(removeUserData: false)
            }
            Button("同时删除用户数据…", role: .destructive) {
                DispatchQueue.main.async { showingDeleteUserData = true }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("将停止 Steer 服务并删除系统组件；用户配置、运行状态和日志默认保留。")
        }
        .alert("同时删除用户数据？", isPresented: $showingDeleteUserData) {
            Button("永久删除配置、状态与日志", role: .destructive) {
                model.uninstallSystemComponents(removeUserData: true)
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("这会额外删除本机保存的配置、状态与日志，无法从 Steer 恢复。")
        }
    }

    private func pathRow(_ title: String, _ path: String) -> some View {
        LabeledContent(title) {
            Text(path)
                .font(.callout.monospaced())
                .textSelection(.enabled)
        }
    }

    private func componentSymbol(_ state: SystemComponentFact.State) -> String {
        switch state {
        case .ready: return "checkmark.circle.fill"
        case .inactive: return "pause.circle"
        case .outdated: return "arrow.down.circle.fill"
        case .missing: return "questionmark.circle"
        case .invalid: return "xmark.octagon.fill"
        }
    }

    private func componentColor(_ state: SystemComponentFact.State) -> Color {
        switch state {
        case .ready: return .green
        case .inactive: return .secondary
        case .outdated: return .orange
        case .missing, .invalid: return .red
        }
    }
}

private extension AppPage {
    init?(contractKey: String) {
        switch contractKey {
        case "overview": self = .overview
        case "general": self = .general
        case "nodes": self = .nodes
        case "routes": self = .routes
        case "dns": self = .dns
        case "proxies": self = .proxies
        case "rules": self = .rules
        case "subscriptions": self = .subscriptions
        case "diagnostics": self = .diagnostics
        case "system": self = .settings
        case "advanced": self = .configuration
        default: return nil
        }
    }

    var navigationLabel: String {
        switch self {
        case .overview: return "总览"
        case .general: return "基础设置"
        case .configuration: return "高级配置"
        case .nodes: return "节点"
        case .routes: return "路由"
        case .dns: return "DNS Profile"
        case .rules: return "规则"
        case .subscriptions: return "订阅"
        case .proxies: return "本地代理"
        case .diagnostics: return "诊断"
        case .settings: return "系统"
        }
    }

    var eyebrow: String {
        switch self {
        case .overview: return "Steer for macOS"
        case .general: return "配置"
        case .configuration: return "JSON"
        case .nodes: return "节点"
        case .routes: return "路由"
        case .dns: return "DNS Profiles"
        case .rules: return "顺序规则"
        case .subscriptions: return "订阅"
        case .proxies: return "本地代理"
        case .diagnostics: return "诊断"
        case .settings: return "系统"
        }
    }

    var subtitle: String {
        switch self {
        case .overview: return "配置概览与服务运行状态"
        case .general: return "运行、连通性探测、DNS 缓存与启动解析设置"
        case .configuration: return "高级编辑与校验；所有可视化页面共享同一份工作副本"
        case .nodes: return "手动节点可编辑，订阅节点保持只读"
        case .routes: return "管理直连、拒绝与单节点出口链路"
        case .dns: return "上游解析器与每条规则的独立 DNS 路径"
        case .rules: return "从上到下严格匹配，Default 必须位于最后"
        case .subscriptions: return "管理订阅源、节点更新与失效节点清理"
        case .proxies: return "本机 SOCKS、HTTP 与 Mixed 入口"
        case .diagnostics: return "连通性测试、最新结果、配置校验、应用结果与系统日志"
        case .settings: return "系统组件、版本与存储路径"
        }
    }
}

private extension ValidationIssue {
    var destinationPage: AppPage? {
        switch objectType {
        case "steer", "bootstrap": return .general
        case "node": return .nodes
        case "route": return .routes
        case "dns_profile": return .dns
        case "local_proxy": return .proxies
        case "rule": return .rules
        case "subscription": return .subscriptions
        default: return nil
        }
    }
}
