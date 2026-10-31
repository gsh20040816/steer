#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later

import ast
import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LUCI_ROOT = ROOT / "luci-app-steer"
PO_PATH = LUCI_ROOT / "po" / "zh_Hans" / "steer.po"
MENU_PATH = LUCI_ROOT / "root" / "usr" / "share" / "luci" / "menu.d" / "luci-app-steer.json"
UI_SPEC_PATH = ROOT / "ui" / "steer-ui-spec.json"
FORM_FIXTURES_PATH = ROOT / "ui" / "form-input-fixtures.json"


def fail(message: str) -> None:
    print(f"check-luci-i18n: {message}", file=sys.stderr)
    raise SystemExit(1)


menu = json.loads(MENU_PATH.read_text())
source_ids = {entry["title"] for entry in menu.values() if "title" in entry}
for path in (LUCI_ROOT / "htdocs").rglob("*.js"):
    source_ids.update(re.findall(r"_\('([^']*)'\)", path.read_text()))

ui_spec = json.loads(UI_SPEC_PATH.read_text())


def collect_labels(value) -> set[str]:
    labels = set()
    if isinstance(value, dict):
        if isinstance(value.get("label"), str):
            labels.add(value["label"])
        for child in value.values():
            labels.update(collect_labels(child))
    elif isinstance(value, list):
        for child in value:
            labels.update(collect_labels(child))
    return labels


generated_labels = collect_labels(ui_spec)
source_ids.update(generated_labels)
openwrt_dns_boundary = ui_spec.get("dns_boundaries", {}).get("openwrt", {})
for field in (
    "capture_scope", "bootstrap_boundary", "encrypted_dns_boundary",
    "diagnostic_boundary",
):
    if isinstance(openwrt_dns_boundary.get(field), str):
        source_ids.add(openwrt_dns_boundary[field])
source_ids.update(openwrt_dns_boundary.get("exclusions", []))

formats = ui_spec.get("input_formats", {})
expected_formats = {"probe_url", "subscription_url", "positive_duration", "dns_http_path"}
if set(formats) != expected_formats:
    fail(f"shared input format metadata drifted: {sorted(formats)!r}")
fixtures = json.loads(FORM_FIXTURES_PATH.read_text())
if fixtures.get("schema_version") != 1 or not fixtures.get("cases"):
    fail("invalid shared form input fixtures")
for fixture in fixtures["cases"]:
    if fixture.get("format") not in formats or not isinstance(fixture.get("value"), str) or not isinstance(fixture.get("valid"), bool):
        fail(f"invalid shared form input fixture: {fixture!r}")

ucode_source = (LUCI_ROOT / "root" / "usr" / "share" / "rpcd" / "ucode" / "luci.steer").read_text()
if re.search(r"[\u3400-\u9fff]", ucode_source):
    fail("ucode RPC contains a hard-coded single-language error")

for relative in [
    "view/steer/overview.js", "view/steer/nodes.js", "view/steer/dns.js",
    "view/steer/local-proxies.js", "view/steer/rules.js"
]:
    content = (LUCI_ROOT / "htdocs" / "luci-static" / "resources" / relative).read_text()
    if re.search(r"o\.value\(item\.value,\s*item\.label\)", content):
        fail(f"{relative} bypasses generated choice-label localization")

translations = {}
current_id = None
for line_number, line in enumerate(PO_PATH.read_text().splitlines(), 1):
    if line.startswith("msgid "):
        current_id = ast.literal_eval(line[6:])
    elif line.startswith("msgstr ") and current_id:
        if current_id in translations:
            fail(f"duplicate msgid {current_id!r} at line {line_number}")
        translations[current_id] = ast.literal_eval(line[7:])
        current_id = None

missing = sorted(source_ids - translations.keys())
untranslated = sorted(key for key in source_ids if not translations.get(key))
obsolete = sorted(translations.keys() - source_ids)
bad_placeholders = sorted(
    key
    for key in source_ids
    if re.findall(r"%(?:\d+\$)?[a-zA-Z%]", key)
    != re.findall(r"%(?:\d+\$)?[a-zA-Z%]", translations.get(key, ""))
)

required_localized = {
    "Configuration lifecycle": "配置生命周期",
    "Working copy": "工作副本",
    "Unsaved changes": "有未保存修改",
    "Saved configuration": "已保存配置",
    "Active status": "运行状态",
    "Running status": "运行状态",
    "Recent application and shortcuts": "最近应用与快捷操作",
    "Canonical Preview": "规范预览",
    "Single-node routes": "单节点路由",
    "Add single-node route": "添加单节点路由",
    "Working copy warning summary": "工作副本警告摘要",
    "Saved configuration warning summary": "已保存配置警告摘要",
    "TLS certificate verification is disabled": "TLS 证书校验已关闭",
    "DNS certificate verification is disabled": "DNS 证书校验已关闭",
    "DNS continues matching later rules": "DNS 将继续匹配后续规则",
    "View affected items": "查看受影响项",
    "Steer status": "Steer 状态",
    "Enable Steer": "启用 Steer",
    "Active configuration": "运行配置",
    "Running normally": "运行正常",
}
unlocalized_required = sorted(
    key for key, expected in required_localized.items()
    if translations.get(key) != expected
)



def collect_shared_texts(value) -> list[tuple[str, str]]:
    """UI texts shared by the three frontends carry the LuCI msgid and zh_cn."""
    texts = []
    if isinstance(value, dict):
        if isinstance(value.get("label"), str) and isinstance(value.get("zh_cn"), str):
            texts.append((value["label"], value["zh_cn"]))
        for child in value.values():
            texts.extend(collect_shared_texts(child))
    elif isinstance(value, list):
        for child in value:
            texts.extend(collect_shared_texts(child))
    return texts


# LuCI must show exactly the wording the Linux web console and macOS app use.
drifted_shared = sorted(
    f"{label!r}: {translations.get(label)!r} != {zh_cn!r}"
    for label, zh_cn in collect_shared_texts(ui_spec)
    if translations.get(label) != zh_cn
)

if missing:
    fail("missing translations:\n" + "\n".join(missing))
if untranslated:
    fail("empty translations:\n" + "\n".join(untranslated))
if obsolete:
    fail("obsolete translations:\n" + "\n".join(obsolete))
if bad_placeholders:
    fail("placeholder mismatch:\n" + "\n".join(bad_placeholders))
if unlocalized_required:
    fail("key user-facing labels are not localized:\n" + "\n".join(unlocalized_required))
if drifted_shared:
    fail("shared UI text translations differ from zh_cn:\n" + "\n".join(drifted_shared))

print(f"LuCI Simplified Chinese coverage: {len(source_ids)}/{len(source_ids)}")
