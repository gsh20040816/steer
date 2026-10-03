#!/usr/bin/env python3
"""Check that versions and pins repeated across files agree."""

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
errors = []


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def value(path: str, pattern: str) -> str:
    match = re.search(pattern, read(path), re.MULTILINE)
    if match is None:
        errors.append(f"{path}: no match for {pattern}")
        return ""
    return match.group(1)


def same(label: str, *pairs: tuple[str, str]) -> None:
    found = {path: value(path, pattern) for path, pattern in pairs}
    if len(set(found.values())) != 1:
        errors.append(f"{label} disagrees: {found}")


# Third-party actions run with release secrets; tags can be moved, commits cannot.
for workflow in sorted((ROOT / ".github/workflows").glob("*.yml")):
    for line in workflow.read_text(encoding="utf-8").splitlines():
        reference = re.match(r"\s*(?:-\s*)?uses:\s*([^@\s]+)@(\S+)", line)
        if reference and not reference.group(1).startswith("./") and not re.fullmatch(r"[0-9a-f]{40}", reference.group(2)):
            errors.append(f"{workflow.name}: action not pinned to a commit: {line.strip()}")

# The Geo manifest records which compiler built it; CI must build with the same one.
same(
    "sing-box Geo compiler version",
    ("go/internal/geodata/geodata.go", r'SingBoxCompiler\s*=\s*"([^"]+)"'),
    (".github/workflows/geodata.yml", r"^\s*SING_BOX_VERSION: (\S+)"),
    (".github/workflows/ci.yml", r"^\s*SING_BOX_VERSION: (\S+)"),
)
same(
    "sing-box linux-amd64-musl SHA256",
    (".github/workflows/geodata.yml", r"^\s*SING_BOX_LINUX_AMD64_MUSL_SHA256: (\S+)"),
    (".github/workflows/ci.yml", r"^\s*SING_BOX_LINUX_AMD64_MUSL_SHA256: (\S+)"),
)
same(
    "geoview commit",
    ("go/internal/geodata/geodata.go", r'GeoViewCommit\s*=\s*"([^"]+)"'),
    (".github/workflows/geodata.yml", r"^\s*GEOVIEW_REF: (\S+)"),
)

same(
    "OpenWrt package version",
    ("steer/Makefile", r"^PKG_VERSION:=(\S+)"),
    ("luci-app-steer/Makefile", r"^PKG_VERSION:=(\S+)"),
)
same(
    "OpenWrt package release",
    ("steer/Makefile", r"^PKG_RELEASE:=(\S+)"),
    ("luci-app-steer/Makefile", r"^PKG_RELEASE:=(\S+)"),
)

# .SRCINFO is generated from PKGBUILD and goes stale when only one is edited.
for label, pkgbuild, srcinfo in (
    ("Arch pkgver", r"^pkgver=(\S+)", r"^\tpkgver = (\S+)"),
    ("Arch pkgrel", r"^pkgrel=(\S+)", r"^\tpkgrel = (\S+)"),
    ("Arch source commit", r"^_commit=([0-9a-f]{40})$", r"#commit=([0-9a-f]{40})$"),
):
    same(label, ("packaging/archlinux/steer/PKGBUILD", pkgbuild), ("packaging/archlinux/steer/.SRCINFO", srcinfo))

# rpcd runs as root; the LuCI bridge must only exec fixed argv, never a shell.
rpc = read("luci-app-steer/root/usr/share/rpcd/ucode/luci.steer")
for forbidden in ("/bin/sh", "sh -c", "eval("):
    if forbidden in rpc:
        errors.append(f"luci.steer must not run a shell: found {forbidden!r}")

if errors:
    print("\n".join(f"check-release-consistency: {error}" for error in errors), file=sys.stderr)
    raise SystemExit(1)
print("release consistency checks passed")
