#!/usr/bin/env python3
"""Verify generated UI specs and the shared native information architecture."""

import json
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parents[1]


def require(content: str, fragment: str, owner: str) -> None:
    if fragment not in content:
        raise SystemExit(f"check-ui-contract: {owner} does not consume {fragment}")


subprocess.run(
    ["go", "run", "./cmd/steer-ui-spec", "--root", "..", "--check"],
    cwd=ROOT / "go",
    check=True,
)

contract = json.loads((ROOT / "ui/steer-ui-spec.json").read_text())
local_proxy_fixtures = json.loads(
    (ROOT / "ui/local-proxy-listen-fixtures.json").read_text()
)
if local_proxy_fixtures.get("schema_version") != 1:
    raise SystemExit("check-ui-contract: invalid local proxy fixture schema")
fixture_by_listen = {
    item["listen"]: item for item in local_proxy_fixtures.get("cases", [])
}
for listen, classification in {
    "127.0.0.1": "loopback", "::1": "loopback",
    "0.0.0.0": "non_loopback", "::": "non_loopback",
    "192.168.50.1": "non_loopback", "fd12:3456:789a::1": "non_loopback",
    "router.lan": "invalid",
}.items():
    if fixture_by_listen.get(listen, {}).get("classification") != classification:
        raise SystemExit(f"check-ui-contract: missing local proxy fixture {listen!r}")
subscription_status_fixtures = json.loads(
    (ROOT / "ui/subscription-status-fixtures.json").read_text()
)
probe_diagnostics_fixtures = json.loads(
    (ROOT / "ui/probe-diagnostics-fixtures.json").read_text()
)
state_lifecycle_fixtures = json.loads(
    (ROOT / "ui/state-lifecycle-fixtures.json").read_text()
)
validation_issue_fixtures = json.loads(
    (ROOT / "ui/validation-issue-fixtures.json").read_text()
)
collection_reference_fixtures = json.loads(
    (ROOT / "ui/collection-reference-fixtures.json").read_text()
)
rule_summary_fixtures = json.loads(
    (ROOT / "ui/rule-summary-fixtures.json").read_text()
)
form_input_fixtures = json.loads(
    (ROOT / "ui/form-input-fixtures.json").read_text()
)
creation_policy_fixtures = json.loads(
    (ROOT / "ui/creation-policy-fixtures.json").read_text()
)
collection_ordering_fixtures = json.loads(
    (ROOT / "ui/collection-ordering-fixtures.json").read_text()
)
collection_drag_fixtures = json.loads(
    (ROOT / "ui/collection-drag-fixtures.json").read_text()
)
node_display_sorting_fixtures = json.loads(
    (ROOT / "ui/node-display-sorting-fixtures.json").read_text()
)
for name, fixture in {
    "validation issue": validation_issue_fixtures,
    "collection reference": collection_reference_fixtures,
    "rule summary": rule_summary_fixtures,
}.items():
    if fixture.get("schema_version") != 1:
        raise SystemExit(f"check-ui-contract: invalid {name} fixture schema")
if not contract.get("collection_references") or contract.get("rule_connection_only_fields") != [
    "ip_match", "network", "protocol", "port"
]:
    raise SystemExit("check-ui-contract: missing shared reference or DNS-stage contract")
if form_input_fixtures.get("schema_version") != 1 or not form_input_fixtures.get("cases"):
    raise SystemExit("check-ui-contract: invalid shared form input fixtures")
if set(contract.get("input_formats", {})) != {"probe_url", "subscription_url", "positive_duration", "dns_http_path"}:
    raise SystemExit("check-ui-contract: shared input format metadata drifted")
if creation_policy_fixtures.get("schema_version") != 1:
    raise SystemExit("check-ui-contract: invalid creation policy fixture schema")
ordered_collections = {
    "nodes", "routes", "dns_profiles", "local_proxies", "rules", "subscriptions"
}
if collection_ordering_fixtures.get("schema_version") != 1 or set(
    collection_ordering_fixtures.get("collections", [])
) != ordered_collections:
    raise SystemExit("check-ui-contract: invalid collection ordering fixture schema")
ordering = contract.get("collection_ordering", {})
if set(ordering) != ordered_collections or any(
    policy.get("stable_id_field") != "id" or policy.get("move_actions") != ["up", "down"]
    for policy in ordering.values()
):
    raise SystemExit("check-ui-contract: collection ordering policy drift")
if ordering["nodes"].get("group_field") != "source_subscription":
    raise SystemExit("check-ui-contract: Node ordering must stay inside source groups")
if ordering["routes"].get("movable_kinds") != ["single"]:
    raise SystemExit("check-ui-contract: system Route ordering boundary drift")
if ordering["rules"].get("pinned_last_boolean_field") != "default":
    raise SystemExit("check-ui-contract: Default ordering boundary drift")
drag = contract.get("collection_drag", {})
if (
    collection_drag_fixtures.get("schema_version") != 1
    or drag.get("states") != collection_drag_fixtures.get("states")
    or drag.get("feedback") != "whole_row_placeholder"
    or drag.get("commit") != "draft_move_on_drop"
    or drag.get("cancel") != "restore_without_mutation"
    or drag.get("fallback_actions") != ["up", "down"]
    or set(drag.get("pointer_inputs", [])) != {"mouse", "touch", "pen"}
    or drag.get("ordering_policy_source") != "collection_ordering"
    or drag.get("single_mutation_per_drop") is not True
):
    raise SystemExit("check-ui-contract: collection drag lifecycle drift")
node_sorting = contract.get("node_display_sorting", {})
if (
    node_display_sorting_fixtures.get("schema_version") != 1
    or node_sorting.get("modes") != node_display_sorting_fixtures.get("modes")
    or node_sorting.get("header_columns") != ["connect", "download"]
    or node_sorting.get("direction_modes") != node_display_sorting_fixtures.get("direction_modes")
    or node_sorting.get("default_direction") != "best_first"
    or node_sorting.get("repeat_click") != "best_worst_default_cycle"
    or node_sorting.get("result_source") != "probe_results.latest_results"
    or node_sorting.get("metric_field") != "summary"
    or node_sorting.get("connect_direction") != "ascending"
    or node_sorting.get("download_direction") != "descending"
    or node_sorting.get("unranked_placement") != "last_stable"
    or node_sorting.get("tie_breaker") != "original_index"
    or node_sorting.get("scope") != "visible_group"
    or node_sorting.get("ordering_actions_mode") != "default_only"
    or node_sorting.get("mutates_draft") is not False
):
    raise SystemExit("check-ui-contract: Node display sorting contract drift")
id_policy = contract.get("id_policy", {})
if not id_policy.get("auto_generate") or id_policy.get("max_length") != 32:
    raise SystemExit("check-ui-contract: automatic ID policy is missing")
id_pattern = re.compile(id_policy.get("pattern", ""))
for case in creation_policy_fixtures.get("cases", []):
    collection = case["collection"]
    actual = dict(contract.get("creation_defaults", {}).get(collection, {}))
    actual["id"] = case["id"]
    actual.update(case.get("overrides", {}))
    if actual != case["expected"]:
        raise SystemExit(f"check-ui-contract: {collection} creation Canonical drifted: {actual!r}")
    if not id_pattern.fullmatch(case["id"]):
        raise SystemExit(f"check-ui-contract: fixture ID violates shared policy: {case['id']!r}")
if state_lifecycle_fixtures.get("schema_version") != 1:
    raise SystemExit("check-ui-contract: invalid state lifecycle fixture schema")
if [item.get("name") for item in state_lifecycle_fixtures.get("cases", [])] != [
    "fresh", "pending-disable", "failed-apply", "active"
]:
    raise SystemExit("check-ui-contract: state lifecycle fixture drift")
if probe_diagnostics_fixtures.get("schema_version") != 2:
    raise SystemExit("check-ui-contract: invalid probe diagnostics fixture schema")
dns_capture_fixture = probe_diagnostics_fixtures.get("diagnostics", {}).get("dns_capture", {})
if not dns_capture_fixture.get("configured") or dns_capture_fixture.get("mode") != "native_hijack_with_local_shim":
    raise SystemExit("check-ui-contract: probe diagnostics DNS capture fixture drift")
latest_probe_results = probe_diagnostics_fixtures.get("probe_results", {}).get("latest_results", [])
if [result.get("scope") for result in latest_probe_results] != ["overview", "nodes", "routes"]:
    raise SystemExit("check-ui-contract: probe diagnostics fixture scope drift")
required_latest_fields = {"scope", "kind", "tested_at", "ok", "stale", "summary", "error_summary"}
if any(not required_latest_fields.issubset(result) for result in latest_probe_results):
    raise SystemExit("check-ui-contract: latest probe result fixture field drift")
if "reports" in probe_diagnostics_fixtures.get("diagnostics", {}):
    raise SystemExit("check-ui-contract: ordinary diagnostics fixture retained raw reports")
for collection in ("nodes", "routes", "subscriptions"):
    enabled_values = [item.get("enabled") for item in probe_diagnostics_fixtures.get("objects", {}).get(collection, [])]
    if enabled_values != [True, False]:
        raise SystemExit(f"check-ui-contract: {collection} enabled/disabled probe fixture drift")
if subscription_status_fixtures.get("schema_version") != 1:
    raise SystemExit("check-ui-contract: invalid subscription status fixture schema")
subscription_cases = {item.get("name"): item.get("status", {}) for item in subscription_status_fixtures.get("cases", [])}
required_subscription_cases = {
    "never-fetched", "success", "success-with-skipped",
    "failed-after-success-with-referenced-stale", "disabled",
}
if set(subscription_cases) != required_subscription_cases:
    raise SystemExit(f"check-ui-contract: subscription lifecycle fixture drift: {set(subscription_cases)!r}")
for name, status in subscription_cases.items():
    for field in ("never_fetched", "last_success", "last_failure", "node_count", "current", "added", "skipped", "stale"):
        if field not in status:
            raise SystemExit(f"check-ui-contract: fixture {name!r} lacks {field!r}")
expected_navigation = [
    ("status", ["overview"]),
    ("configuration", ["general", "nodes", "routes", "dns", "proxies", "rules"]),
    ("services", ["subscriptions", "diagnostics", "system"]),
    ("advanced", ["advanced"]),
]
actual_navigation = [
    (group["key"], [item["key"] for item in group["items"]])
    for group in contract["navigation"]
]
if actual_navigation != expected_navigation:
    raise SystemExit(f"check-ui-contract: navigation drift: {actual_navigation!r}")

expected_page_facts = {
    "overview": {"execution_model", "draft", "saved", "active", "object_counts", "validation_summary", "warning_summary", "last_apply", "quick_actions"},
    "diagnostics": {"validation", "probes", "latest_results", "dns_capture", "last_apply", "logs"},
    "system": {"versions", "last_apply", "geo", "paths", "platform_components", "access"},
}
page_responsibilities = contract.get("page_responsibilities", {})
if set(page_responsibilities) != set(expected_page_facts):
    raise SystemExit("check-ui-contract: page responsibility keys drifted")
for page, facts in expected_page_facts.items():
    if set(page_responsibilities[page].get("facts", [])) != facts:
        raise SystemExit(f"check-ui-contract: {page} responsibility drifted")
overview_contract = page_responsibilities["overview"]
if overview_contract.get("object_count_source") != "draft" or overview_contract.get("validation_source") != "draft_validation":
    raise SystemExit("check-ui-contract: Overview counts and validation must use the current Draft")
if [region.get("key") for region in overview_contract.get("regions", [])] != [
    "execution_model", "configuration_lifecycle", "object_scale", "validation_summary", "last_apply_and_actions"
]:
    raise SystemExit("check-ui-contract: Overview five-region contract drifted")
if overview_contract["regions"][2].get("facts") != ["nodes", "routes", "dns_profiles", "local_proxies", "rules", "subscriptions"]:
    raise SystemExit("check-ui-contract: Overview Draft scale is incomplete")
if overview_contract["regions"][4].get("actions") != ["refresh", "diagnostics", "system", "save", "apply_saved", "save_and_apply", "discard"]:
    raise SystemExit("check-ui-contract: Overview lifecycle actions drifted")
if overview_contract.get("forbidden_facts") != ["probe_history", "raw_error_chain", "object_ids", "digests", "generation_paths"]:
    raise SystemExit("check-ui-contract: Overview forbidden technical facts drifted")
dns_boundaries = contract.get("dns_boundaries", {})
if set(dns_boundaries) != {"linux", "openwrt", "macos"}:
    raise SystemExit("check-ui-contract: DNS platform boundaries are incomplete")
for platform, boundary in dns_boundaries.items():
    for field in ("capture_mode", "capture_scope", "exclusions", "bootstrap_boundary", "encrypted_dns_boundary", "diagnostic_boundary"):
        if not boundary.get(field):
            raise SystemExit(f"check-ui-contract: {platform} DNS boundary lacks {field}")
    if "does not prove" not in boundary["diagnostic_boundary"] or "Port-53 capture alone" not in boundary["encrypted_dns_boundary"]:
        raise SystemExit(f"check-ui-contract: {platform} DNS boundary makes an unverifiable claim")
subscription_inventory = contract.get("subscription_inventory", {})
if (subscription_inventory.get("changes_active_generation") is not False
        or subscription_inventory.get("unreferenced_nodes") != "removed"
        or subscription_inventory.get("stale_referenced_nodes") != "preserved"):
    raise SystemExit("check-ui-contract: subscription inventory lifecycle drifted")
probe_results_contract = contract.get("probe_results", {})
if probe_results_contract.get("key_fields") != ["scope", "object_id", "kind"] or probe_results_contract.get("result_fields") != [
    "scope", "object_id", "kind", "tested_at", "ok", "stale", "summary", "error_summary"
]:
    raise SystemExit("check-ui-contract: latest probe result contract drifted")
global_status = contract.get("global_status", {})
if (
    global_status.get("visible_on_every_page") is not True
    or global_status.get("enable_action") != "set_enabled_on_latest_saved"
    or global_status.get("includes_current_draft") is not False
    or global_status.get("blocking_conditions") != ["write_in_progress"]
    or global_status.get("facts") != ["draft", "saved_enabled", "active", "pending_apply"]
    or global_status.get("actions") != ["enable", "save", "apply_saved", "save_and_apply", "discard"]
):
    raise SystemExit("check-ui-contract: global status and Enable contract drifted")

luci_menu = json.loads(
    (ROOT / "luci-app-steer/root/usr/share/luci/menu.d/luci-app-steer.json").read_text()
)
luci_root = "admin/services/steer"
expected_luci_items = [
    item for _, items in expected_navigation for item in items
]
luci_children = sorted(
    (
        (entry["order"], path.rsplit("/", 1)[-1])
        for path, entry in luci_menu.items()
        if path.startswith(luci_root + "/")
        and "/" not in path[len(luci_root) + 1:]
    ),
    key=lambda item: item[0],
)
actual_luci_items = [key for _, key in luci_children]
if actual_luci_items != expected_luci_items:
    raise SystemExit(
        f"check-ui-contract: LuCI flat navigation drift: {actual_luci_items!r}"
    )
for group_key, _ in expected_navigation:
    if group_key not in expected_luci_items and f"{luci_root}/{group_key}" in luci_menu:
        raise SystemExit(
            f"check-ui-contract: LuCI restored redundant group menu {group_key!r}"
        )

node_types = {item["value"] for item in contract["node_types"]}
if node_types != {
    "socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria",
    "hysteria2", "shadowtls", "tuic", "anytls", "naive", "ssh", "tor",
}:
    raise SystemExit("check-ui-contract: node protocol matrix is incomplete")
route_kind_labels = {item["value"]: item["label"] for item in contract["route_kinds"]}
if route_kind_labels.get("block") != "Reject":
    raise SystemExit("check-ui-contract: deprecated Block label returned instead of Reject")
if contract.get("subscription_update_interval_default") != "6h":
    raise SystemExit("check-ui-contract: shared subscription interval default drifted")

expected_dns_protocols = {
    "udp": ([], [], 53),
    "tcp": ([], [], 53),
    "tls": (["tls_server_name", "insecure"], ["tls_server_name"], 853),
    "https": (["tls_server_name", "path", "insecure"], ["tls_server_name"], 443),
    "quic": (["tls_server_name", "insecure"], ["tls_server_name"], 853),
    "h3": (["tls_server_name", "path", "insecure"], ["tls_server_name"], 443),
}
actual_dns_protocols = {
    item["value"]: (item["fields"], item["required_fields"], item["default_port"])
    for item in contract["dns_protocols"]
}
if actual_dns_protocols != expected_dns_protocols:
    raise SystemExit(f"check-ui-contract: DNS protocol field matrix drift: {actual_dns_protocols!r}")

linux_ui = (ROOT / "go/cmd/steer-linux/web/js/ui.js").read_text()
linux_web_api = (ROOT / "go/cmd/steer-linux/web_api.go").read_text()
linux_lib = (ROOT / "go/cmd/steer-linux/web/js/lib.js").read_text()
linux_nodes = (ROOT / "go/cmd/steer-linux/web/js/views/nodes.js").read_text()
linux_routes = (ROOT / "go/cmd/steer-linux/web/js/views/routes.js").read_text()
linux_subscriptions = (ROOT / "go/cmd/steer-linux/web/js/views/subscriptions.js").read_text()
linux_diagnostics = (ROOT / "go/cmd/steer-linux/web/js/views/diagnostics.js").read_text()
linux_lib = (ROOT / "go/cmd/steer-linux/web/js/lib.js").read_text()
linux_overview = (ROOT / "go/cmd/steer-linux/web/js/views/overview.js").read_text()
linux_system = (ROOT / "go/cmd/steer-linux/web/js/views/system.js").read_text()
linux_dns = (ROOT / "go/cmd/steer-linux/web/js/views/dns.js").read_text()
linux_proxies = (ROOT / "go/cmd/steer-linux/web/js/views/proxies.js").read_text()
linux_rules = (ROOT / "go/cmd/steer-linux/web/js/views/rules.js").read_text()
luci_nodes = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/nodes.js").read_text()
luci_rules = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/rules.js").read_text()
luci_overview = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/overview.js").read_text()
luci_system = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/system.js").read_text()
luci_helper = (ROOT / "luci-app-steer/htdocs/luci-static/resources/steer.js").read_text()
luci_dns = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/dns.js").read_text()
luci_proxies = (ROOT / "luci-app-steer/htdocs/luci-static/resources/view/steer/local-proxies.js").read_text()
mac_content = (ROOT / "macos/SteerApp/ContentView.swift").read_text()
mac_state = (ROOT / "macos/SteerApp/AppState.swift").read_text()
mac_editors = (ROOT / "macos/SteerApp/DraftEditors.swift").read_text()
mac_configuration = (ROOT / "macos/SteerApp/ConfigurationFormView.swift").read_text()
mac_ui_spec = (ROOT / "macos/SteerApp/UISpec.swift").read_text()
openwrt_cli = (ROOT / "go/cmd/steer-openwrt/main.go").read_text()
mac_control = (ROOT / "go/cmd/steer-macos/control.go").read_text()
shared_probe_archive = (ROOT / "go/internal/probe/archive.go").read_text()
linux_diagnostics_backend = (ROOT / "go/internal/platform/linux/diagnostics.go").read_text()
openwrt_diagnostics_backend = (ROOT / "go/internal/platform/openwrt/diagnostics.go").read_text()
mac_diagnostics_backend = (ROOT / "go/internal/platform/macos/diagnostics.go").read_text()

require(linux_ui, "S.uiSpec.navigation", "Linux navigation")
require(linux_nodes, "S.uiSpec.node_fields", "Linux node form")
require(luci_nodes, "steer.ui-spec", "LuCI node form")
require(luci_nodes, "uiSpec.node_types", "LuCI protocol picker")
require(luci_nodes, "uiSpec.node_fields", "LuCI field matrix")
require(luci_nodes, "addGeneratedNodeField", "LuCI generated controls")
require(mac_content, "SteerUISpec.contract.navigation", "macOS navigation")
require(mac_ui_spec, "pageResponsibilities", "macOS page responsibility contract")
require(linux_ui, "onToggleEnabled", "Linux global Enable")
require(luci_helper, "setGlobalEnabled", "LuCI global Enable")
require(mac_content, 'Toggle("Steer"', "macOS global Enable")
require(linux_overview, "执行模型、配置生命周期与当前工作副本概览", "Linux Overview responsibility")
for content, owner in (
    (linux_diagnostics, "Linux Diagnostics"),
    (linux_system, "Linux System"),
    (linux_dns, "Linux DNS"),
    (luci_overview, "LuCI Diagnostics"),
    (luci_system, "LuCI System"),
    (luci_dns, "LuCI DNS"),
    (mac_content, "macOS pages"),
):
    if "dns_boundaries" in content or "dnsBoundaries" in content:
        raise SystemExit(f"check-ui-contract: {owner} still renders the internal DNS boundary copy")
for content, owner, fragments in (
    (linux_overview, "Linux Overview", ("lastApply", "intent.nodes.length", "validation.warnings")),
    (linux_diagnostics, "Linux Diagnostics", ("probeResults.latest_results", "S.api.logs", "diagnostics.dns_capture")),
    (linux_system, "Linux System", ("runtime.sing_box", "status.last_apply", "/run/steer")),
    (luci_overview, "LuCI Overview/Diagnostics", ("renderLifecycleOverview", "diagnostics?.logs", "diagnostics?.dns_capture")),
    (luci_system, "LuCI System", ("singBox.version", "status.last_apply", "/run/steer")),
    (mac_content, "macOS pages", ("diagnosticsDNSCapture", "geoVersion", "/Library/Application Support/Steer/run")),
):
    for fragment in fragments:
        require(content, fragment, owner)
require(mac_editors, "SharedNodeDraftForm", "macOS node form")
require(mac_editors, "SteerUISpec.nodeFields", "macOS field matrix")
require(linux_subscriptions, "S.uiSpec.subscription_update_interval_default", "Linux subscription default")
require(linux_subscriptions, "ui.creationDraft('subscriptions')", "Linux subscription creation")
require(luci_nodes, "uiSpec.subscription_update_interval_default", "LuCI subscription default")
require(luci_nodes, "steer.creationDefaults('subscriptions')", "LuCI subscription creation")
require(mac_ui_spec, "subscriptionUpdateIntervalDefault", "macOS subscription default")
require(mac_state, "SteerUISpec.creationObject", "macOS subscription creation")
require(linux_dns, "item.fields", "Linux DNS field matrix")
require(linux_dns, "next.default_port", "Linux DNS port matrix")
require(luci_dns, "protocol.fields", "LuCI DNS field matrix")
require(luci_dns, "next.default_port", "LuCI DNS port matrix")
require(mac_ui_spec, "UIDNSProtocolSpec", "macOS DNS field matrix")
require(mac_ui_spec, "next.defaultPort", "macOS DNS port matrix")
require(mac_ui_spec, "currentPort == $0.defaultPort", "macOS DNS custom-port preservation")
require(mac_editors, "protocolSpec?.fields.contains", "macOS DNS field visibility")
require(linux_ui, "classifyLocalProxyListen", "Linux local proxy address classification")
require(linux_proxies, "PROTOCOL_LABEL", "Linux shared local proxy labels")
require(linux_proxies, "保持已保存密码", "Linux explicit password retention")
require(linux_proxies, "替换密码", "Linux explicit password replacement")
require(linux_proxies, "移除认证", "Linux explicit authentication removal")
require(luci_proxies, "classifyLocalProxyListen", "LuCI local proxy address classification")
require(luci_proxies, "steer-exposure-warning", "LuCI local proxy exposure warning")
require(mac_editors, "classifyLocalProxyListen", "macOS local proxy address classification")
require(mac_editors, "validateLocalProxyAuthentication", "macOS local proxy authentication gate")
if "value: draft.password" in linux_proxies:
    raise SystemExit("check-ui-contract: Linux local proxy editor redisplays a saved password")

fixture_consumers = (
    "go/internal/intent/validate_test.go",
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/LocalProxyAddressTests.swift",
)
for consumer in fixture_consumers:
    consumer_content = (ROOT / consumer).read_text()
    require(consumer_content, "local-proxy-listen-fixtures.json", consumer)

subscription_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/SubscriptionStatusTests.swift",
)
for consumer in subscription_fixture_consumers:
    consumer_content = (ROOT / consumer).read_text()
    require(consumer_content, "subscription-status-fixtures.json", consumer)

probe_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/ProbeDiagnosticsTests.swift",
)
for consumer in probe_fixture_consumers:
    consumer_content = (ROOT / consumer).read_text()
    require(consumer_content, "probe-diagnostics-fixtures.json", consumer)

state_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/StateLifecycleTests.swift",
)
for consumer in state_fixture_consumers:
    consumer_content = (ROOT / consumer).read_text()
    require(consumer_content, "state-lifecycle-fixtures.json", consumer)

ui_safety_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/UISafetyContractTests.swift",
)
for consumer in ui_safety_fixture_consumers:
    consumer_content = (ROOT / consumer).read_text()
    for fixture in (
        "validation-issue-fixtures.json",
        "collection-reference-fixtures.json",
        "rule-summary-fixtures.json",
    ):
        require(consumer_content, fixture, consumer)

form_input_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/steer_helper_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/UISafetyContractTests.swift",
)
for consumer in form_input_fixture_consumers:
    require((ROOT / consumer).read_text(), "form-input-fixtures.json", consumer)

creation_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/UISafetyContractTests.swift",
)
for consumer in creation_fixture_consumers:
    require((ROOT / consumer).read_text(), "creation-policy-fixtures.json", consumer)

ordering_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/CollectionOrderingTests.swift",
)
for consumer in ordering_fixture_consumers:
    require((ROOT / consumer).read_text(), "collection-ordering-fixtures.json", consumer)

drag_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/CollectionOrderingTests.swift",
)
for consumer in drag_fixture_consumers:
    require((ROOT / consumer).read_text(), "collection-drag-fixtures.json", consumer)

node_sorting_fixture_consumers = (
    "tests/node/linux_web_test.js",
    "tests/node/luci_view_test.js",
    "macos/SteerAppTests/CollectionOrderingTests.swift",
)
for consumer in node_sorting_fixture_consumers:
    require((ROOT / consumer).read_text(), "node-display-sorting-fixtures.json", consumer)

require((ROOT / "go/cmd/steer-linux/web/js/store.js").read_text(), "collection_ordering", "Linux ordering store")
for view in ("nodes", "routes", "dns", "proxies", "rules", "subscriptions"):
    source = (ROOT / f"go/cmd/steer-linux/web/js/views/{view}.js").read_text()
    require(source, "collectionOrderToolbar", f"Linux {view} ordering controls")
    require(source, "collectionRowAttributes", f"Linux {view} row selection")

require(luci_helper, "uiSpec.collection_ordering", "LuCI ordering helper")
for view in ("dns", "local-proxies", "nodes", "rules"):
    require(
        (ROOT / f"luci-app-steer/htdocs/luci-static/resources/view/steer/{view}.js").read_text(),
        "configureOrdering", f"LuCI {view} ordering controls"
    )

macos_content = (ROOT / "macos/SteerApp/ContentView.swift").read_text()
require(macos_content, "orderingPolicy", "macOS shared ordering policy")
require(macos_content, "moveSelected", "macOS ordering controls")
require(linux_ui, "collectionDragHandle", "Linux whole-row drag controls")
require((ROOT / "go/cmd/steer-linux/web/js/store.js").read_text(), "moveCollectionItemTo", "Linux shared drop mutation")
require(luci_helper, "steer-touchsort-whole-row", "LuCI whole-row touch preview")
require(luci_helper, "handleTouchCancel", "LuCI touch cancellation")
require(macos_content, "CollectionListDropDelegate", "macOS whole-row List drag")
require(macos_content, "collectionDragPreviewOrder", "macOS live row-yield preview")
require(macos_content, ".snappy(duration: 0.16)", "macOS native move animation")
require(linux_nodes, "sortNodesForDisplay", "Linux Node display sorting")
require(linux_nodes, "table-sort__direction", "Linux clickable sort headers")
require(luci_nodes, "sortNodeSectionIDs", "LuCI Node display sorting")
require(luci_nodes, "decorateNodeSortHeaders", "LuCI clickable sort headers")
require(luci_nodes, "s.handleSort = function() {}", "LuCI display-only header sorting")
require(macos_content, "nodeListHeader", "macOS clickable Node list headers")
require(macos_content, "nodeSortHeader", "macOS per-column Node sorting")
require(macos_content, "cycleNodeSort", "macOS three-state Node sorting")
if "nodeSortBar" in macos_content:
    raise SystemExit("check-ui-contract: macOS Node sorting must use actual table headers, not an adjacent sort bar")
require((ROOT / "macos/SteerApp/AppState.swift").read_text(), "NodeDisplaySorting.sortedIDs", "macOS Node display sorting")
for source, label in ((linux_nodes, "Linux Node sorting"), (luci_nodes, "LuCI Node sorting"), (macos_content, "macOS Node sorting")):
    if "diagnostics.reports" in source:
        raise SystemExit(f"check-ui-contract: {label} must consume backend latest_results only")

require(linux_ui, "creationDraft", "Linux automatic creation policy")
require(linux_ui, "referenceOptions", "Linux disambiguated references")
require(luci_helper, "nextSectionID", "LuCI automatic ID policy")
require(luci_helper, "disambiguateReferences", "LuCI disambiguated references")
require(mac_ui_spec, "creationObject", "macOS creation policy")
require(mac_state, "draftReferenceLabel", "macOS disambiguated references")
if "defaultValue:" in mac_editors or "defaultValue:" in mac_configuration:
    raise SystemExit("check-ui-contract: macOS still renders a field-missing phantom default")

require(linux_ui, "配置开关", "Linux working-copy state")
require(linux_ui, "已保存开关", "Linux saved state")
require(linux_ui, "重新载入", "Linux external revision reload")
for region in ("execution_model", "configuration_lifecycle", "object_scale", "validation_summary", "last_apply_and_actions"):
    require(luci_overview, f"'data-overview-region': '{region}'", f"LuCI Overview {region} region")
    require(linux_overview, f"'data-overview-region': '{region}'", f"Linux Overview {region} region")
require(luci_overview, "Save, Apply Saved, Save and Apply, and Discard are available in the global status area above.", "LuCI global Overview actions")
require(luci_helper, "steer-lifecycle-global", "LuCI global lifecycle actions")
require(luci_helper, "applySaved", "LuCI Apply Saved action")
require(mac_content, "DraftActionButtons", "macOS lifecycle actions")
require(linux_ui, "guardCollectionDeletion", "Linux shared reference guard")
require(luci_helper, "configureRemovalGuard", "LuCI shared reference guard")
require(luci_helper, "uiSpec.collection_references", "LuCI shared reference relations")
require(mac_state, "SteerUISpec.inboundReferences", "macOS shared reference guard")
require(linux_rules, "rule_connection_only_fields", "Linux DNS-stage boundary")
require(luci_rules, "rule_connection_only_fields", "LuCI DNS-stage boundary")
require(mac_editors, "sourceMACReason", "macOS source-MAC capability reason")

require(linux_diagnostics, "成功仅表示该地址在测试时可达", "Linux accurate probe boundary")
require(luci_overview, "Success only means the target was reachable", "LuCI accurate probe boundary")
require(mac_content, "成功仅表示该地址在测试时可达", "macOS accurate probe boundary")
require(linux_diagnostics, "当前网络环境", "Linux disabled overview probes")
require(luci_overview, "current network environment", "LuCI disabled overview probes")
require(mac_content, "当前网络环境", "macOS disabled overview probes")
require(shared_probe_archive, "type LatestProbeResult struct", "shared latest probe result DTO")
require(shared_probe_archive, "ReadLatestProbeResults", "shared latest probe result reader")
require(shared_probe_archive, "reportIsStale", "backend stale policy")
require(shared_probe_archive, "coreMetric", "backend probe metric policy")
require(shared_probe_archive, "safeErrorSummary", "backend safe probe error policy")
require(linux_diagnostics_backend, "ReadLatestProbeResults", "Linux latest-result capability")
require(openwrt_diagnostics_backend, "ReadLatestProbeResults", "OpenWrt latest-result capability")
require(mac_diagnostics_backend, "ReadLatestProbeResults", "macOS latest-result capability")
require(linux_diagnostics, "probeResults.latest_results", "Linux persisted latest probe results")
require(luci_overview, "probeResults?.latest_results", "LuCI persisted latest probe results")
require(mac_state, "latestProbePresentation", "macOS persisted latest probe results")
ordinary_probe_sources = (
    (linux_lib + linux_nodes + linux_routes + linux_diagnostics, "Linux ordinary UI"),
    (luci_nodes + luci_overview, "LuCI ordinary UI"),
    (mac_state + mac_content, "macOS ordinary UI"),
)
for source, owner in ordinary_probe_sources:
    for forbidden in ("diagnostics.reports", "saved_digest", "active_digest", "first_byte_milliseconds", "downloaded_bytes"):
        if forbidden in source:
            raise SystemExit(f"check-ui-contract: {owner} still derives latest probe results from {forbidden}")
require(luci_nodes, "probeOperationGate", "LuCI pending probe gate")
require(mac_content, "!item.enabled", "macOS disabled probe action")
if "服务运行后才能测试" in mac_content or ".disabled(running || !model.hasActiveGeneration)" in mac_content:
    raise SystemExit("check-ui-contract: macOS still disables Overview probes without Active")

require(linux_subscriptions, "last_failure", "Linux subscription failure state")
require(linux_subscriptions, "status.stale", "Linux subscription stale state")
require(linux_subscriptions, "当前运行配置未改变", "Linux no-Apply inventory warning")
require(linux_subscriptions, "无引用的消失节点已自动删除", "Linux automatic unreferenced subscription cleanup notice")
require(linux_subscriptions, "仍被路由使用的节点已自动保留并应尽快解除引用", "Linux referenced stale inventory warning")
require(luci_nodes, "(status.stale || []).length ? 'warning' : 'info'", "LuCI warns only when referenced stale nodes remain")
require(luci_nodes, "subscriptionOperationGate", "LuCI pending subscription gate")
require(luci_nodes, "running configuration was not changed", "LuCI no-Apply inventory warning")
require(luci_nodes, "nodes still used by Routes were kept", "LuCI referenced stale inventory warning")
require(mac_state, "SubscriptionStaleNode", "macOS stale node contract")
require(mac_state, "当前运行配置未改变", "macOS no-Apply inventory warning")
require(mac_content, "SubscriptionStaleList", "macOS per-node stale management")
require(mac_content, "已失效", "macOS stale node badge")
for source, owner in (
    (linux_web_api, "Linux subscription API"),
    (openwrt_cli, "OpenWrt subscription CLI"),
    (mac_control, "macOS subscription control"),
):
    if 'json:"snapshots"' in source or 'json:"snapshot"' in source:
        raise SystemExit(f"check-ui-contract: {owner} exposes credential-bearing internal snapshots")
    require(source, '"subscriptions"', owner)

for content, owner in (
    (linux_nodes, "Linux nodes"),
    (luci_nodes, "LuCI nodes"),
    (mac_content, "macOS nodes"),
):
    require(content, "source_subscription" if owner != "macOS nodes" else "sourceSubscription", owner)

require(linux_nodes, "S.api.importNodes", "Linux multi-node import")
require(luci_nodes, "steer.importNodes", "LuCI multi-node import")
require(mac_state, "parseNodes(document:", "macOS multi-node import")
require(linux_nodes, "testButton('下载', true", "Linux node download test")
require(luci_nodes, "_download_speedtest", "LuCI node download test")
require(mac_content, "nodeProbeCell(item, download: true)", "macOS node download test column")
require(linux_routes, "draft.kind = 'single'", "Linux fixed system routes")
require(luci_nodes, "addSystemRouteSection", "LuCI fixed system routes")
require(mac_content, "isSystemRoute(item)", "macOS fixed system routes")
require(mac_state, "result.errorSummary", "macOS backend probe failure summary")
require(linux_lib, "result.error_summary", "Linux backend probe failure summary")
require(luci_nodes, "See diagnostic logs for details.", "LuCI sanitized probe summary")
if "report?.error || results.map" in linux_lib or "report?.error || results.map" in luci_nodes:
    raise SystemExit("check-ui-contract: a node list still exposes raw probe backend errors")

if (ROOT / "luci-app-steer/htdocs/luci-static/resources/steer/share-url.js").exists():
    raise SystemExit("check-ui-contract: LuCI retained its duplicate share URL parser")

print("shared native UI contract checks passed")
