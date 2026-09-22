#!/usr/bin/env python3
"""Keep generic Linux delivery platform-neutral and install-name consistent."""

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> None:
    print(f"check-linux-packaging: {message}", file=sys.stderr)
    raise SystemExit(1)


for path in (
    ROOT / "linux/config.example.json",
    ROOT / "linux/web.example.json",
    ROOT / "scripts/collect-linux-artifacts.sh",
    ROOT / "tests/integration/linux-system.Dockerfile",
    ROOT / "tests/integration/run-linux-system.sh",
    ROOT / "tests/integration/start-linux-system-container.sh",
    ROOT / "go/cmd/steer-openwrt/main.go",
    ROOT / "go/cmd/steer-linux/commands.go",
    ROOT / "go/cmd/steer-linux/command_apply.go",
    ROOT / "go/cmd/steer-linux/command_probe.go",
    ROOT / "go/cmd/steer-linux/command_subscription.go",
    ROOT / "go/cmd/steer-linux/operation_lock.go",
    ROOT / "go/cmd/steer-linux/web_server.go",
    ROOT / "go/cmd/steer-linux/web_api.go",
    ROOT / "go/cmd/steer-linux/web_auth.go",
    ROOT / "go/cmd/steer-linux/web_config.go",
):
    if not path.is_file():
        fail(f"required structure is missing: {path.relative_to(ROOT)}")

for retired in (
    ROOT / "linux/config.json",
    ROOT / "linux/platform.json",
    ROOT / "linux/platform.example.json",
    ROOT / "go/cmd/steer/main.go",
    ROOT / "scripts/collect-release-artifacts.sh",
    ROOT / "packaging/archlinux/geoview",
    ROOT / "packaging/archlinux/steer/steer.install",
    ROOT / "go/internal/intent/migrate.go",
    ROOT / "go/internal/intent/migrate_test.go",
    ROOT / "go/internal/platform/openwrt/migrate.go",
    ROOT / "go/internal/platform/openwrt/migrate_test.go",
):
    if retired.exists():
        fail(f"retired path still exists: {retired.relative_to(ROOT)}")

for path in (
    ROOT / "go/cmd/steer-openwrt/main.go",
    ROOT / "go/cmd/steer-linux/commands.go",
    ROOT / "go/cmd/steer-linux/command_apply.go",
):
    content = path.read_text()
    if '"migrate"' in content or "runMigrate" in content or "MigrateSchema8" in content:
        fail(f"retired schema migration command remains in {path.relative_to(ROOT)}")

main_lines = (ROOT / "go/cmd/steer-linux/main.go").read_text().splitlines()
if len(main_lines) >= 100:
    fail("Linux main.go must remain a small process entry point")

units = "\n".join(
    path.read_text() for path in sorted((ROOT / "linux/systemd").glob("steer*.service"))
)
if "/usr/bin/steer-linux" in units:
    fail("systemd exposes the source target name steer-linux")
for command in (
    "/usr/bin/steer _run",
    "/usr/bin/steer cleanup",
    "/usr/bin/steer web",
    "/usr/bin/steer subscription update",
):
    if command not in units:
        fail(f"systemd is missing public executable command: {command}")
if "PartOf=nftables.service" not in units:
    fail("steer.service must restart with nftables.service")

collector = (ROOT / "scripts/collect-linux-artifacts.sh").read_text()
for required in (
    "for target in x86_64 aarch64",
    'package_name="steer-linux-$target"',
    "config.example.json",
    "web.example.json",
    "geodata-seed",
    "linux/systemd/*.service",
    "linux/systemd/*.timer",
    "CGO_ENABLED=0",
    "SOURCE_DATE_EPOCH",
):
    if required not in collector:
        fail(f"Linux collector is missing: {required}")
for bundled in ("sing-box", "geoview", "geosite.dat", "geoip.dat"):
    if f'install -m 0755 "$project_root/{bundled}' in collector:
        fail(f"Linux collector bundles external resource: {bundled}")

workflow = (ROOT / ".github/workflows/release.yml").read_text()
ci_workflow = (ROOT / ".github/workflows/ci.yml").read_text()
geodata_workflow = (ROOT / ".github/workflows/geodata.yml").read_text()
geodata_contract = (ROOT / "go/internal/geodata/geodata.go").read_text()
sing_box_version = "1.14.1"
sing_box_linux_sha256 = "b907365b154e4a7e3e40be15c2cd83433c0fa65c7dc736bdb1b5face2afe4501"
geoview_ref = "3c91926d360b8f49d47520639e574608318baf12"
if f'SingBoxCompiler       = "{sing_box_version}"' not in geodata_contract:
    fail("Geo manifest compiler identity does not match the verified sing-box baseline")
if f'GeoViewCommit         = "{geoview_ref}"' not in geodata_contract:
    fail("Geo manifest compiler identity does not match the verified geoview baseline")
for name, content in (("CI", ci_workflow), ("Geo", geodata_workflow)):
    for required in (
        f"SING_BOX_VERSION: {sing_box_version}",
        f"SING_BOX_LINUX_AMD64_MUSL_SHA256: {sing_box_linux_sha256}",
    ):
        if required not in content:
            fail(f"{name} workflow does not pin the verified sing-box runtime: {required}")
if f"GEOVIEW_REF: {geoview_ref}" not in geodata_workflow:
    fail("Geo workflow does not pin the verified geoview compiler identity")
for required in (
    ".tools.sing_box_version // empty",
    ".tools.geoview_ref // empty",
    '"$current_sing_box_version" = "$SING_BOX_VERSION"',
    '"$current_geoview_ref" = "$GEOVIEW_REF"',
):
    if required not in geodata_workflow:
        fail(f"Geo workflow does not rebuild on compiler identity changes: {required}")
for required in (
    "SING_BOX_OPENWRT_VERSION: 1.14.1",
    "SING_BOX_OPENWRT_X86_64_SHA256: 796a5eb87d2d5f47e291778ba273065e31d125164d1076d773e3e53b343301f8",
    "SING_BOX_MACOS_VERSION: 1.14.1",
    "SING_BOX_MACOS_REVISION: 1ac1a339cb1223e9c70eae14c44411c75033c02d",
):
    if required not in workflow:
        fail(f"release workflow does not pin the verified sing-box runtime: {required}")
for required in (
    "needs: source-gate",
    "Generic Linux x86_64 and aarch64",
    'CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build',
    "./scripts/collect-linux-artifacts.sh",
    "name: linux-generic",
    "name: release-bundle",
):
    if required not in workflow:
        fail(f"release workflow is missing: {required}")
for archive in ("steer-linux-x86_64.tar.zst", "steer-linux-aarch64.tar.zst"):
    if workflow.count(archive) < 2:
        fail(f"release pipeline does not enforce archive: {archive}")
for required in (
    "name: Test Linux system integration",
    "/workspace/tests/integration/run-linux-system.sh",
    "tests/integration/start-linux-system-container.sh",
):
    if required not in ci_workflow:
        fail(f"all-commit CI is missing Linux integration coverage: {required}")
    if required in workflow:
        fail(f"tag release must package rather than repeat Linux integration: {required}")
for distro_tool in ("makepkg", "dpkg-buildpackage", "rpmbuild"):
    if distro_tool in workflow:
        fail(f"upstream CI must not build distribution package with {distro_tool}")

arch_pkgbuild = (ROOT / "packaging/archlinux/steer/PKGBUILD").read_text()
arch_srcinfo = (ROOT / "packaging/archlinux/steer/.SRCINFO").read_text()
version_match = re.search(r"^pkgver=(\S+)$", arch_pkgbuild, re.MULTILINE)
if version_match is None:
    fail("Arch steer PKGBUILD has no scalar pkgver")
arch_version = version_match.group(1)
commit_match = re.search(r"^_commit=([0-9a-f]{40})$", arch_pkgbuild, re.MULTILINE)
if commit_match is None:
    fail("Arch steer PKGBUILD must pin a full source commit")
arch_commit = commit_match.group(1)
for required in (
    "  'git'\n",
    f"pkgver = {arch_version}\n",
    "\tmakedepends = git\n",
    f"\tsource = steer::git+https://github.com/gsh20040816/steer.git#commit={arch_commit}\n",
):
    source = arch_pkgbuild if required == "  'git'\n" else arch_srcinfo
    if required not in source:
        fail(f"Arch steer metadata is missing or stale: {required.strip()}")

for required in (
    "steer-geodata.tar.zst::https://gsh20040816.github.io/steer/geodata/latest/steer-geodata.tar.zst",
    "go run ./cmd/steer-geodata-build verify",
    'rm -rf -- "$srcdir/geodata-seed"',
    'mkdir -p "$srcdir/geodata-seed"',
):
    if required not in arch_pkgbuild:
        fail(f"Arch steer package is missing an idempotent Geo seed contract: {required}")
if "install=steer.install" in arch_pkgbuild or (ROOT / "packaging/archlinux/steer/steer.install").exists():
    fail("Arch steer package retained the removed schema migration hook")
clear_seed = arch_pkgbuild.index('rm -rf -- "$srcdir/geodata-seed"')
create_seed = arch_pkgbuild.index('mkdir -p "$srcdir/geodata-seed"')
extract_seed = arch_pkgbuild.index('bsdtar -xf "$srcdir/steer-geodata.tar.zst"')
if not clear_seed < create_seed < extract_seed:
    fail("Arch prepare must replace stale Geo seed before extracting the current archive")
if "  'sing-box'\n" not in arch_pkgbuild or "sing-box>=1.14" in arch_pkgbuild or "sing-box<1.15" in arch_pkgbuild:
    fail("Arch steer must depend only on the virtual sing-box provider; native config check decides compatibility")
if "\tdepends = sing-box\n" not in arch_srcinfo:
    fail("Arch steer .SRCINFO is missing the virtual sing-box dependency")

print("generic Linux packaging checks passed")
