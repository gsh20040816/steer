#!/usr/bin/env python3
"""Reject package ownership and retired-runtime regressions after M1 cutover."""

from pathlib import Path
import sys


ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> None:
    print(f"check-package-boundaries: {message}", file=sys.stderr)
    raise SystemExit(1)


makefile = (ROOT / "steer/Makefile").read_text()
luci_makefile = (ROOT / "luci-app-steer/Makefile").read_text()
init_script = (ROOT / "steer/files/etc/init.d/steer").read_text()
rpc = (ROOT / "luci-app-steer/root/usr/share/rpcd/ucode/luci.steer").read_text()
acl = (ROOT / "luci-app-steer/root/usr/share/rpcd/acl.d/luci-app-steer.json").read_text()

for dependency in (
    "ip",
    "kmod-nft-queue",
    "kmod-nft-tproxy",
    "kmod-tun",
):
    if f"+{dependency}" not in makefile:
        fail(f"steer must declare external dependency: {dependency}")

for retired_dependency in ("steer-geodata", "geoview"):
    if f"+{retired_dependency}" in makefile:
        fail(f"steer retained device-side Geo dependency: {retired_dependency}")

for retired in ("smartdns", "curl", "ucode-mod-uci", "bind-dig"):
    if f"+{retired}" in makefile:
        fail(f"steer retained retired dependency: {retired}")

for concrete_ip_provider in ("ip-tiny", "ip-full"):
    if f"+{concrete_ip_provider}" in makefile:
        fail(f"steer must use the virtual ip provider, not {concrete_ip_provider}")

if "+sing-box" not in makefile:
    fail("steer must depend on the unversioned sing-box provider")
if "EXTRA_DEPENDS:=" in makefile:
    fail("steer must not use version-constrained EXTRA_DEPENDS for sing-box")

for metadata in (
    "PROVIDES:=steer-openwrt",
    "CONFLICTS:=steer-openwrt",
    "REPLACES:=steer-openwrt",
):
    if metadata not in makefile:
        fail(f"steer-openwrt package replacement metadata is missing: {metadata}")

for forbidden_path in ("usr/bin/sing-box", "usr/bin/geoview"):
    if forbidden_path in makefile:
        fail(f"steer must not install third-party binary: {forbidden_path}")

if "$(1)/usr/sbin/steer" not in makefile:
    fail("steer must install the public CLI as /usr/sbin/steer")
if "$(PKG_INSTALL_DIR)/usr/bin/steer-openwrt $(1)/usr/sbin/steer" not in makefile:
    fail("OpenWrt source target must be renamed to the public steer executable during packaging")
if "$(1)/usr/sbin/steer-openwrt" in makefile or "/usr/sbin/steer-openwrt" in rpc:
    fail("retired steer-openwrt CLI name is still user-visible")

for retired_migration in (
    '/usr/sbin/steer migrate --config "$$config"',
    "for option in probe_direct probe_proxy speedtest_proxy",
    "uci set steer.main.schema_version='7'",
    "_connect_speedtest _download_speedtest",
    "/var/lib/steer/logs/speedtests",
    "/var/lib/steer/rollback.uci",
):
    if retired_migration in makefile:
        fail(f"package retained removed migration behavior: {retired_migration}")
if "PKG_NAME:=steer" not in makefile or "define Package/steer" not in makefile:
    fail("the OpenWrt controller package must be named steer")
if "PKG_VERSION:=0.10.1\n" not in makefile or "PKG_RELEASE:=1\n" not in makefile:
	fail("steer package version must be 0.10.1-r1")
if "PKG_VERSION:=0.10.1\n" not in luci_makefile or "PKG_RELEASE:=1\n" not in luci_makefile:
	fail("LuCI packages must use 0.10.1-r1")
if 'extra_command "stop_runtime"' not in init_script or 'ubus call service state \'{"name":"steer","spawn":false}\'' not in init_script:
	fail("OpenWrt disable path must preserve the procd config trigger")
if "github.com/gsh20040816/steer/go" not in makefile or "$(CURDIR)/../go/." not in makefile:
    fail("steer must build the repository-level Go module")
for stale_repair in ("repaired_subscription_network", "uci -q delete steer.$$section.network"):
    if stale_repair in makefile:
        fail(f"package retained the expired subscription network repair: {stale_repair}")
if "*/15 * * * * /usr/sbin/steer subscription update" not in makefile:
    fail("subscription cron dispatcher is not package-managed")
if "[ -x /etc/init.d/cron ]" not in makefile:
    fail("subscription dispatcher must fail fast when BusyBox crond is unavailable")
if "PKG_UPGRADE=0 /usr/sbin/steer apply" not in makefile:
    fail("post-upgrade must switch the schema 9 intent through verified Apply")
if "PKG_UPGRADE=0 /etc/init.d/steer start" in makefile:
    fail("post-upgrade must not leave an already-running sing-box instance unchanged")
if "subscription_update" not in rpc or "subscription_update" not in acl:
    fail("LuCI subscription update must be implemented and authorized explicitly")
for removed_contract in ("steer rollback", "steer plan", "method: 'rollback'", "method: 'plan'"):
    if removed_contract in rpc:
        fail(f"LuCI RPC retained removed public contract: {removed_contract}")
if "$(CURDIR)/../generated/geodata-seed" not in makefile or "$(1)/usr/share/steer" not in makefile:
    fail("steer package does not own the verified SRS seed")
for retired_package in (ROOT / "steer-geodata", ROOT / "geoview"):
    if retired_package.exists() and any(path.is_file() for path in retired_package.rglob("*")):
        fail(f"retired package directory still contains files: {retired_package.relative_to(ROOT)}")

if (ROOT / "steer-openwrt").exists() and any(
    path.is_file() for path in (ROOT / "steer-openwrt").rglob("*")
):
    fail("retired steer-openwrt package directory still exists")

retired_tokens = (
    "smartdns",
    "last-known-good",
    "/usr/sbin/steerctl",
    "/usr/libexec/steer/runtime",
)
for path in (ROOT / "steer").rglob("*"):
    if path.is_file() and not path.name.endswith("_test.go"):
        content = path.read_text(errors="ignore").lower()
        for token in retired_tokens:
            if token.lower() in content:
                fail(f"{path.relative_to(ROOT)} retained retired runtime token: {token}")

for forbidden in ("/bin/sh", "sh -c", "eval("):
    if forbidden in rpc:
        fail(f"LuCI RPC must call fixed controller commands, found: {forbidden}")

print("package boundary checks passed")
