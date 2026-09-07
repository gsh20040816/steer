#!/usr/bin/env python3
"""Verify generated UI specifications and the installed LuCI navigation."""

import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
subprocess.run(
    ["go", "run", "./cmd/steer-ui-spec", "--root", "..", "--check"],
    cwd=ROOT / "go", check=True,
)
contract = json.loads((ROOT / "ui/steer-ui-spec.json").read_text())
luci_menu = json.loads(
    (ROOT / "luci-app-steer/root/usr/share/luci/menu.d/luci-app-steer.json").read_text()
)
luci_root = "admin/services/steer"
expected_luci_items = [item["key"] for group in contract["navigation"] for item in group["items"]]
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

print("generated UI specifications and LuCI navigation checks passed")
