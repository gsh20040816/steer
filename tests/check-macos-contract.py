#!/usr/bin/env python3
"""Static checks for macOS source contracts that do not require a Mac."""

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def read(relative: str) -> str:
    return (ROOT / relative).read_text(encoding="utf-8")


def main() -> None:
    compiler = read("go/internal/compiler/compiler.go")
    macos_plan = read("go/internal/platform/macos/plan.go")
    macos_backend = read("go/internal/platform/macos/backend.go")
    macos_validate = read("go/internal/platform/macos/validate.go")
    macos_cli = read("go/cmd/steer-macos/main.go")
    control = read("go/cmd/steer-macos/control.go")
    control_peer = read("go/cmd/steer-macos/control_peer_darwin.go")
    launchd = read("macos/launchd/com.steer.steer.plist")
    control_launchd = read("macos/launchd/com.steer.steer.control.plist")
    subscription_launchd = read("macos/launchd/com.steer.steer.subscription.plist")
    installer = read("macos/scripts/install-launchdaemon.sh")
    embedded_installer = read("macos/scripts/install-embedded-payload.sh")
    package = read("macos/Package.swift")
    ci = read(".github/workflows/ci.yml")
    app = read("macos/SteerApp/SteerApp.swift")
    app_info = read("macos/SteerApp/Info.plist")
    state = read("macos/SteerApp/AppState.swift")
    content = read("macos/SteerApp/ContentView.swift")
    editors = read("macos/SteerApp/DraftEditors.swift")
    ui_spec = read("macos/SteerApp/UISpec.swift")
    rule_tests = read("macos/SteerAppTests/RuleDraftTests.swift")
    lifecycle_tests = read("macos/SteerAppTests/AppStateDraftLifecycleTests.swift")
    general = read("macos/SteerApp/ConfigurationFormView.swift")

    assert 'DNSCaptureInboundHijack' in compiler
    assert 'DNSCaptureTUNPort53Hijack' in macos_plan
    assert '"dns_mode": "hijack"' in macos_plan
    assert 'DirectRouteAddress' in macos_plan
    for private_prefix in ("10.0.0.0/8", "100.64.0.0/10", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"):
        assert private_prefix in macos_plan
    assert '"fe80::/10"' in macos_plan
    for retired_lan_state in ("DiscoverActiveLANPrefixes", "ActiveLANPrefixes", "LANPrefixes", "monitorLANPrefixes", "reconcileLANPrefixes"):
        assert retired_lan_state not in macos_plan
        assert retired_lan_state not in macos_backend
        assert retired_lan_state not in control
    assert not (ROOT / "go/internal/platform/macos/lan.go").exists()
    assert '"auto_route": true' in macos_plan
    assert '"auto_redirect"' not in macos_plan
    assert 'launchctl' in macos_backend
    assert 'geodata.ValidateRequiredRules' in macos_backend
    assert 'PLATFORM_UNSUPPORTED_GEO_TOOLCHAIN' not in macos_validate
    assert 'case "apply"' in macos_cli
    assert 'case "parse-nodes"' in macos_cli
    assert 'case "probe"' in macos_cli and 'case "_probe-results"' in macos_cli
    assert 'case "_state"' in macos_cli
    assert 'case "geo-catalog"' in macos_cli
    assert 'case "subscription"' in macos_cli
    assert 'case "verify-geodata"' in macos_cli
    assert 'case "control"' in macos_cli
    assert 'case "_control"' in macos_cli
    assert 'case "_run"' in macos_cli
    assert 'com.steer.steer' in launchd
    assert 'com.steer.steer.control' in control_launchd
    assert 'com.steer.steer.subscription' in subscription_launchd
    assert '<key>StartInterval</key>' in subscription_launchd
    assert '<string>_control</string>' in control_launchd
    assert '<string>/var/run/steer/control.sock</string>' in control_launchd
    assert '<key>KeepAlive</key>' in control_launchd and '<true/>' in control_launchd
    assert '<string>com.steer.steer</string>' in app_info
    legacy_identifier = "com." + "gsh20040816.steer"
    assert legacy_identifier not in launchd
    assert legacy_identifier not in control_launchd
    assert 'command -v sing-box' in installer
    assert 'runtime_binary="$helper_directory/sing-box"' in installer
    assert '/usr/local/libexec/steer/sing-box' in launchd
    assert 'plutil -replace ProgramArguments' not in installer
    assert 'while launchctl print system/com.steer.steer' in installer
    assert 'Timed out waiting for the previous Steer LaunchDaemons to stop.' in installer
    assert '.executable(name: "SteerApp"' in package
    assert '.testTarget(' in package and 'name: "SteerAppTests"' in package
    assert "DraftStringListCodecTests" in rule_tests and "AppModelRulePolicyTests" in rule_tests
    assert "testCommaBearingEntriesRoundTripWithoutRewriting" in rule_tests
    assert "testDefaultCannotBeDisabledDeletedOrMoved" in rule_tests
    assert "testStableIdentifierToggleResolvesTheCurrentRuleAfterReordering" in rule_tests
    assert 'SteerAgent' not in package
    assert "SteerNetwork" not in package
    assert 'NavigationSplitView' in content
    assert 'func overviewState() async throws -> OverviewLifecycleState' in state
    assert 'model.overviewLifecycle.saved' in content and 'model.overviewLifecycle.pendingApply' in content
    assert 'navigationSplitViewColumnWidth(min: 200' in content
    assert 'List(selection: $selection)' in content
    assert 'identifiedBy: item.identifier' in content
    assert 'at: item.index, enabled:' not in content
    assert 'get: { item.enabled }' not in content
    assert '.id("\\(item.id):enabled")' in content
    assert 'func draftItemEnabled(in key: String, identifiedBy identifier: String)' in state
    assert 'requiredForInstallation: false' in state
    assert 'let installationFacts = facts.filter(\\.requiredForInstallation)' in state
    assert '运行服务未激活不影响系统组件安装完整性' in content
    assert 'var ordered: Bool { SteerUISpec.orderingPolicy(for: key) != nil }' in content
    assert 'rowDragEnabled(item)' in content
    assert '.onDrag {' in content and 'CollectionListDropDelegate' in content
    assert 'collectionDragPreviewOrder' in content
    assert 'contract.feedback == "whole_row_placeholder"' in content
    assert 'contract.singleMutationPerDrop' in content
    assert content.count('withAnimation(.snappy(duration: 0.16))') >= 2
    assert 'if model.moveDraftItem(' in content and 'selection = item.id' in content
    assert 'nodeSortWorstFirst = true' in content and 'nodeSortMode = "default"' in content
    assert 'nodeSortColumnTitle' not in content and '好 → 坏' not in content and '坏 → 好' not in content
    assert 'visibleIDs: movableItems.map(\\.identifier)' in content
    assert 'SteerUISpec.isMovable(collection: descriptor.key, object: object)' in content
    assert '.onDrop(' in content and 'DropProposal(operation: .move)' in content
    assert '"line.3.horizontal"' in content and '"pin.fill"' in content
    assert 'List {' in content and 'Section {' in content
    assert 'GroupBox' not in content
    for editor in ("NodeDraftForm", "RouteDraftForm", "DNSDraftForm", "LocalProxyDraftForm", "RuleDraftForm", "SubscriptionDraftForm"):
        assert editor in editors
    assert "MultilineStringListEditor" in editors and "MatchListEditor" in editors
    assert "DraftStringListCodec.values(from: raw)" in editors
    assert 'TextField("Domain match"' not in editors and 'TextField("IP match"' not in editors
    assert 'TextField(label, text: stringListBinding' not in editors
    assert "DefaultRuleDraftForm" in editors and 'Toggle("Default 规则"' not in editors
    assert "Default 的身份、状态和顺序固定" in editors
    for editor_control in ("TextField", "SecureField", "Picker", "Toggle", "DisclosureGroup"):
        assert editor_control in editors
    assert 'Canonical JSON 条目' not in editors
    assert 'TextField("ID"' not in editors
    assert 'Text(item.identifier)' not in content
    assert 'ConfigurationFormView' in content
    assert 'Bootstrap DNS' in general and 'DNS 缓存' in general and '连通性探测' in general
    assert 'prompt: Text("4096")' in general and 'optionalIntBinding("main", "dns_cache_capacity")' in general
    assert "if let value, value != 0" in general
    assert "留空使用默认值" in general and "容量为 0" not in general
    assert '使用设备当前网络环境' in content and '.disabled(running)' in content
    assert '.disabled(running || !model.hasActiveGeneration)' not in content
    assert '请确认服务正在运行' not in state and '请检查已保存的测试地址和当前网络' in state
    assert '<key>CFBundleIconFile</key>' in app_info and '<string>SteerAppIcon</string>' in app_info
    assert (ROOT / "macos/SteerAppIcon.png").exists()
    assert 'setEnabledAndApply($0)' not in general and 'setEnabledAndApply($0)' in content
    assert 'Toggle("Steer"' in content and 'Toggle("启用配置"' not in content
    assert 'func setEnabledAndApply' in state
    assert 'var canToggleEnabled' in state and 'canSaveAndApplyDraft' in state
    assert 'self.backend.setEnabled(enabled)' in state
    assert 'controlOperation(["state"])' in state
    assert 'controlOperation(["status"])' in state
    assert 'guard !isDirty else {' not in state
    assert 'initialStateLoadInProgress' in state
    assert 'guard !hasInitializedDraft, !initialStateLoadInProgress,' in state
    assert '!isBusy, pendingDraftAction == nil else { return }' in state
    assert 'enum DraftGuardAction' in state and 'func resolveDraftGuard' in state
    assert 'func applySaved()' in state and 'func saveAndApplyDraft()' in state
    assert 'savedApplyPending' not in state and '已保存，待 Apply' not in content
    assert 'draftMatches(document:' in state and 'draftSequence:' in state
    for global_action in ('Label("保存"', 'Label("应用已保存配置"', 'Label("保存并应用"'):
        assert global_action in content
    assert 'DraftActionButtons(model: model)' in app
    assert 'applicationShouldTerminate' in app and 'beginTerminationGuard' in app
    assert 'testInitialLoadRunsOnceAndWindowReopenPreservesDirtyDraft' in lifecycle_tests
    assert 'testInitialLoadFailureCanRetryWithoutOverwritingLaterEdits' in lifecycle_tests
    assert 'testFirstInstallCanSaveAndPreserveAnEditedDraft' in lifecycle_tests
    assert 'testDirtyEnablePreservesDraftAndAppliesLatestSaved' in lifecycle_tests
    assert 'testEnableApplyFailureKeepsSavedToggleSeparateFromActiveState' in lifecycle_tests
    assert 'testInvalidDraftDoesNotBlockSavedToggle' in lifecycle_tests
    assert 'testApplySavedPreservesAnIndependentDirtyDraft' in lifecycle_tests
    assert 'testSaveAndApplyDoesNotMarkNewerInFlightEditsClean' in lifecycle_tests
    assert 'testTerminationGuardCancelAndSaveReplyWithoutApplying' in lifecycle_tests
    assert 'deletionBlockReason' in state and '.alert(' in content
    assert 'NodeImportSheet' in editors and 'parseNodes(document:' in state
    assert 'sourceSubscription' in state and 'NodeCollectionGroup' in content
    assert 'runAllNodeProbes(download: Bool, nodeIDs: [String])' in state
    assert 'probeInProgress(scope:' in state and 'download: true' in content
    assert 'activeProbeKeys' in state and 'perform(message: "正在运行探测…")' not in state
    assert 'struct ProbeLatestResult' in state and 'result.errorSummary' in state
    for forbidden_probe_rule in ('diagnostics.reports', 'firstByteMilliseconds', 'downloadedBytes', 'savedDigest'):
        assert forbidden_probe_rule not in state
    assert 'isSystemRoute(item)' in content and 'isDefaultRule(item)' in content
    assert 'private struct DefaultRuleCard: View' in content and 'private var defaultRule: DraftItem?' in content
    assert 'Label("始终启用 · 固定在最后", systemImage: "lock.fill")' in content
    assert '只能修改显示名称、DNS Profile 与路由' in content
    assert "enum RuleDraftPolicy" in state and "RuleDraftPolicy.replacement" in state
    assert '系统必需 · 始终启用' in editors
    assert 'Picker("类型"' not in editors and 'LabeledContent("类型")' in editors and 'Text("Single 节点")' in editors
    assert 'SharedNodeDraftForm' in editors and 'SteerUISpec.nodeFields' in editors
    dns_editor = editors[editors.index("private struct DNSDraftForm"):editors.index("private struct LocalProxyDraftForm")]
    assert 'UIDNSProtocolSpec' in ui_spec
    assert 'applyDNSProtocol' in ui_spec and 'normalizeDNSProfile' in ui_spec
    assert 'defaultPort' in ui_spec and 'protocolSpec?.fields.contains' in dns_editor
    assert 'currentPort == $0.defaultPort' in ui_spec and 'defaultValue:' not in dns_editor
    assert '["tls", "https", "quic", "h3"]' not in dns_editor
    assert 'classifyLocalProxyListen' in editors
    assert 'validateLocalProxyAuthentication' in editors
    assert '暴露范围警告' in editors
    assert '.testTarget(' in package and 'SteerAppTests' in package
    assert 'func probe(kind:' in state
    assert 'func subscriptionStatuses()' in state
    assert 'func updateSubscription(id:' in state
    assert 'func geoCatalog(kind:' in state
    assert 'HelperBackendClient' in state
    assert 'with administrator privileges' in state
    assert '"--expected-revision", expectedRevision' in state
    assert 'ConfigurationSnapshot' in state and 'savedRevision' in state
    assert 'case revisionConflict(currentRevision: String)' in state
    assert state.count('let startingWasDirty = isDirty') == 2
    assert state.count('!startingWasDirty && draftMutationSequence == startingDraftSequence') == 2
    for conflict_choice in ("重新载入服务器配置", "保留本地工作副本", "覆盖服务器配置"):
        assert conflict_choice in content
    assert 'executePrivileged(Self.command([installer.path]))' in state
    assert 'executePrivileged("\\(install)' not in state
    assert 'uninstallSystemComponents(removeUserData:' in state
    assert 'Self.command(arguments)' in state and '"--remove-user-data"' in state
    assert 'SystemComponentFact' in state and 'control_socket' in state and 'runtime_service' in state
    assert 'Data(contentsOf: url)' in state
    assert 'controlOperation(["status"])' in state
    assert '"apply"' in state and '"status"' in state
    assert '/Library/Application Support/Steer/config/config.json' in state
    assert 'openWindow(id: "main")' in app
    assert '.testTarget(' in package
    assert (ROOT / "macos/SteerAppTests/AppStateRevisionTests.swift").exists()
    assert 'swift test --disable-sandbox' in ci
    assert 'install -d -o root -g admin -m 0750' in installer
    assert 'install -d -o root -g wheel -m 0755 "/var/run/steer"' in installer
    assert 'com.steer.steer.control.plist' in installer
    assert 'chmod 0640 "$support_directory/config/config.json"' in installer
    assert 'chmod 0644 "$support_directory/run/current.json"' in installer
    assert 'atomicWriteMode(filepath.Join(paths.Root, "current.json"), encoded, 0o644)' in read("go/internal/platform/macos/generation.go")
    assert 'request.Operation != "save" && request.Operation != "apply"' in control
    assert 'request.Operation != "subscription-update"' in control
    assert 'request.Operation != "subscription-clean"' in control
    assert 'ExpectedRevision' in control and 'currentControlRevision' in control
    assert 'REVISION_CONFLICT' in control and 'EXPECTED_REVISION_REQUIRED' in control
    assert 'decoder.DisallowUnknownFields()' in control
    assert 'maxControlDocument' in control and 'maxControlMessage' in control
    assert 'authorizedControlPeer' in control
    assert 'os.Geteuid() != 0' in control
    assert 'os.Chmod(*socketPath, 0o660)' in control
    assert 'os.Chown(*socketPath, 0, adminGID)' in control
    assert 'unix.GetsockoptXucred' in control_peer
    assert 'unix.LOCAL_PEERCRED' in control_peer
    assert 'command -v' not in embedded_installer
    assert 'go build' not in embedded_installer
    assert 'PAYLOAD-SHA256SUMS' in embedded_installer
    assert 'verify-geodata --directory' in embedded_installer
    assert 'if [ -f "$support_directory/config/config.json" ]' in embedded_installer
    assert 'launchctl bootstrap system "$control_plist_path"' in embedded_installer
    assert '安装系统组件' in content
    assert '修复系统组件' in content
    assert '卸载并保留用户数据' in content
    assert '永久删除配置、状态与日志' in content
    assert '日常保存和应用配置不再重复请求授权' in content
    assert (ROOT / "macos/scripts/uninstall-embedded-payload.sh").exists()
    assert (ROOT / "ui/macos-system-component-fixtures.json").exists()
    assert (ROOT / "macos/SteerAppTests/SystemComponentsStatusTests.swift").exists()
    for page in ("Overview", "Configuration", "Nodes", "Routes", "DNS", "Rules", "Subscriptions", "Diagnostics", "Settings"):
        assert page in app or page in state or page in content
    assert "Local Proxies" in state

    removed_paths = (
        "macos/SteerNetwork",
        "macos/SteerApp/NetworkExtensionController.swift",
        "macos/SteerApp/SteerApp.entitlements",
        "macos/bridge",
        "macos/scripts/build-steercore-xcframework.sh",
        "go/pkg/steermacos",
        "go/internal/platform/macos/bridge.go",
        "macos/SteerAgent",
    )
    for relative in removed_paths:
        assert not (ROOT / relative).exists(), f"obsolete macOS runtime path remains: {relative}"

    forbidden = ("NetworkExtension", "PacketTunnel", "DNSProxy", "network_extension", "dns_proxy", "App Group")
    checked_paths = (
        ROOT / "macos",
        ROOT / "go/internal/platform/macos",
        ROOT / "go/cmd/steer-macos",
    )
    for base in checked_paths:
        for path in base.rglob("*"):
            if not path.is_file() or ".build" in path.parts or path.suffix.lower() in {".png", ".icns"}:
                continue
            content = path.read_text(encoding="utf-8")
            for token in forbidden:
                assert token not in content, f"obsolete token {token!r} remains in {path.relative_to(ROOT)}"
    print("macOS source contracts passed")


if __name__ == "__main__":
    main()
