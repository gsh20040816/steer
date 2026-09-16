#!/usr/bin/env python3
"""Enforce all-commit platform CI and one-run tag release boundaries."""

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
CI_PATH = ROOT / ".github/workflows/ci.yml"
RELEASE_PATH = ROOT / ".github/workflows/release.yml"
CI = CI_PATH.read_text(encoding="utf-8")
RELEASE = RELEASE_PATH.read_text(encoding="utf-8")
ENTRYPOINT = (ROOT / ".github/actions/openwrt-sdk/entrypoint.sh").read_text(encoding="utf-8")
REPOSITORY_COLLECTOR = (ROOT / "scripts/collect-openwrt-repository.sh").read_text(encoding="utf-8")
SYSTEMD_STARTER = (ROOT / "tests/integration/start-linux-system-container.sh").read_text(encoding="utf-8")


def fail(message: str) -> None:
    print(f"check-build-cache: {message}", file=sys.stderr)
    raise SystemExit(1)


if not CI_PATH.exists() or not RELEASE_PATH.exists():
    fail("ci.yml and release.yml are both required")
if (ROOT / ".github/workflows/publish.yml").exists():
    fail("publish.yml must be absorbed into the tag release workflow")
if (ROOT / ".github/workflows/macos-ci.yml").exists():
    fail("macos-ci.yml must be absorbed into verify-only ci.yml")

for fragment in (
    "name: CI",
    "pull_request:",
    "push:",
    "workflow_dispatch:",
    "linux-openwrt:",
    "name: Test Linux and OpenWrt",
    "linux-system:",
    "name: Test Linux system integration",
    "macos:",
    "name: Test macOS (${{ matrix.arch }})",
    "runner: xcode-27",
    "DEVELOPER_DIR: /Applications/Xcode_27.0.app/Contents/Developer",
    "xcrun vtool -show-build",
    "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
    "go-version-file: go/go.mod",
    "go test -race ./...",
    "go vet ./...",
    "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build",
    'CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build',
    "CGO_ENABLED=0 GOOS=darwin GOARCH=${{ matrix.goarch }} go build",
    "/workspace/tests/integration/run-linux-system.sh",
    "tests/integration/start-linux-system-container.sh",
    "swift test --disable-sandbox",
    "swift build -c release --disable-sandbox",
    "python3 tests/check-macos-packaging.py",
):
    if fragment not in CI:
        fail(f"all-commit platform CI is missing: {fragment}")

ci_trigger = CI.split("permissions:", 1)[0]
if "branches:" in ci_trigger or "tags:" in ci_trigger:
    fail("CI push trigger must cover every branch commit")
if "concurrency:" in CI:
    fail("CI must not impose a concurrency group or cancel older commits")
if "macos-26-intel" in CI or "arch: x86_64" in CI:
    fail("macOS CI must only build the supported arm64 target")

for fragment in (
    "max_start_attempts=3",
    "readiness_checks=30",
    "running|degraded",
    "systemctl is-active --quiet systemd-resolved",
    "systemctl --failed --no-pager",
    "journalctl -b --no-pager -n 200",
):
    if fragment not in SYSTEMD_STARTER:
        fail(f"Linux system container starter is missing bounded readiness behavior: {fragment}")
if "run-linux-system.sh" in SYSTEMD_STARTER:
    fail("Linux system container starter must not retry the product integration test")

for forbidden in (
    "actions/upload-artifact@",
    "actions/upload-pages-artifact@",
    "actions/deploy-pages@",
    "gh release create",
    "collect-openwrt-artifacts.sh",
    "collect-linux-artifacts.sh",
    "ghcr.io/openwrt/sdk@",
    "release-bundle",
):
    if forbidden in CI:
        fail(f"all-commit CI must not publish release assets: {forbidden}")

release_trigger = RELEASE.split("concurrency:", 1)[0]
if "tags:\n      - 'v*'" not in release_trigger or "workflow_dispatch:" not in release_trigger:
    fail("release workflow must support v* tag pushes and explicit degraded-service dispatch")
for forbidden in ("branches:", "paths-ignore:"):
    if forbidden in release_trigger:
        fail(f"release trigger must not contain {forbidden}")

for fragment in (
    "group: release-${{ github.ref_name }}",
    "cancel-in-progress: false",
    "source-gate:",
    'test "$GITHUB_REF_TYPE" = tag',
    'git merge-base --is-ancestor "$GITHUB_SHA" origin/master',
    "actions/workflows/ci.yml/runs?branch=master&status=success",
    'select(.head_sha == \\\"$SOURCE_REVISION\\\" and .event == \\\"push\\\")',
    "name: Require a successful master CI push run for a stable release",
    "if: ${{ !contains(github.ref_name, '-') }}",
    "name: Record locally verified prerelease source gate",
    "if: ${{ contains(github.ref_name, '-') }}",
    "^v[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$",
    'version="${GITHUB_REF_NAME#v}"',
    'base_version="${version%%-*}"',
    'openwrt_version="${base_version}_${package_suffix}"',
    'arch_version="${base_version}${package_suffix}"',
    "name: Resolve verified Geo seed",
    "name: OpenWrt 25.12.5 x86_64",
    "name: Generic Linux x86_64 and aarch64",
    "name: macOS ${{ matrix.arch }} DMG",
    "name: Assemble verified release bundle",
    "name: Attest final release assets",
    "name: Publish GitHub Release",
    "name: Deploy stable OpenWrt repository",
    "name: geodata-seed",
    "name: linux-generic",
    "name: macos-${{ matrix.arch }}",
    "name: release-bundle",
    "name: openwrt-repository",
    'gh run download "$GITHUB_RUN_ID"',
    "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6",
    "attestations: write",
    "id-token: write",
    "contents: write",
    "!contains(github.ref_name, '-')",
    "group: pages-site",
):
    if fragment not in RELEASE:
        fail(f"tag release workflow is missing: {fragment}")

for forbidden in (
    "Publish verified master artifact",
    "Find successful build for tagged commit",
    "steps.build.outputs.run_id",
    "Download exact release bundle",
    "release-artifact-build",
    "name: Linux system integration",
    "/workspace/tests/integration/run-linux-system.sh",
):
    if forbidden in RELEASE:
        fail(f"tag release must not reuse master artifacts: {forbidden}")

for path in sorted((ROOT / ".github/workflows").glob("*.yml")):
    content = path.read_text(encoding="utf-8")
    for line in content.splitlines():
        stripped = line.strip()
        if not stripped.startswith("uses: actions/"):
            continue
        reference = stripped.split("@", 1)[-1]
        if not re.fullmatch(r"[0-9a-f]{40}", reference):
            fail(f"GitHub action is not pinned to a full commit in {path.name}: {stripped}")

required_release_fragments = (
    "ghcr.io/openwrt/sdk@sha256:c8a248ce2411962a89f227db444bf5cea022829b049e6326c7d1032d9762982a",
    '--volume "$source_dir:/feed:ro"',
    '--volume "$RUNNER_TEMP/steer-sdk-artifacts:/artifacts"',
    '--volume "$RUNNER_TEMP/steer-sdk-dl:/builder/dl"',
    '--volume "$RUNNER_TEMP/steer-sdk-ccache:/builder/.ccache"',
    '--volume "$RUNNER_TEMP/steer-sdk-go-cache:/go-build-cache"',
    "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9",
    "actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9",
    '--env PACKAGES="steer luci-app-steer"',
    '--env SING_BOX_MIRROR_PACKAGE_VERSION',
    "./scripts/collect-openwrt-artifacts.sh",
    "./scripts/collect-openwrt-repository.sh",
    "./scripts/collect-linux-artifacts.sh",
)
for fragment in required_release_fragments:
    if fragment not in RELEASE:
        fail(f"release workflow is missing official build path: {fragment}")

for forbidden in (
    "docker/build-push-action",
    "docker/setup-buildx-action",
    "aria2c",
    ".github/actions/openwrt-builder",
    "CONFIG_GOLANG_EXTERNAL_BOOTSTRAP_ROOT",
    "dpkg-buildpackage",
    "rpmbuild",
):
    if forbidden in RELEASE:
        fail(f"release workflow reintroduced a custom builder/downloader: {forbidden}")

if RELEASE.count("actions/cache/restore@") != 2 or RELEASE.count("actions/cache/save@") != 2:
    fail("release workflow must persist exactly OpenWrt ccache and GOCACHE")

required_entrypoint_fragments = (
    "./scripts/feeds update -a",
    "CONFIG_ALL_KMODS=n",
    "CONFIG_ALL_NONSHARED=n",
    "--allow-untrusted adbsign",
    "--reset-signatures",
    "CONFIG_CCACHE=y",
    "make defconfig",
    'make "package/$package/download" V=s',
    'make package/luci-app-steer/compile V="${V:-s}" -j "$(nproc)"',
    "OPENWRT_APK_PRIVATE_KEY is required",
    "SING_BOX_MIRROR_PACKAGE_VERSION must be a safe package version",
    'mirrored_sing_box="$repository_dir/sing-box-${SING_BOX_MIRROR_PACKAGE_VERSION}.apk"',
    "cmp generated-public-key.pem /feed/keys/steer-apk.pem",
    'verify "$repository_dir/packages.adb"',
)
for fragment in required_entrypoint_fragments:
    if fragment not in ENTRYPOINT:
        fail(f"official SDK entrypoint is missing: {fragment}")

for fragment in ("packages.adb", "steer-apk.pem", "BUILD-METADATA.txt", "SHA256SUMS"):
    if fragment not in REPOSITORY_COLLECTOR:
        fail(f"OpenWrt repository collector is missing: {fragment}")

print("all-commit platform CI and one-run tag release checks passed")
