#!/usr/bin/env python3
"""Static contracts for native macOS DMG packaging and tag publication."""

from pathlib import Path
import os
import re
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
BUNDLER = (ROOT / "macos/scripts/build-app-bundle.sh").read_text(encoding="utf-8")
INSTALLER = (ROOT / "macos/scripts/install-embedded-payload.sh").read_text(encoding="utf-8")
SOURCE_INSTALLER = (ROOT / "macos/scripts/install-launchdaemon.sh").read_text(encoding="utf-8")
UNINSTALLER = (ROOT / "macos/scripts/uninstall-embedded-payload.sh").read_text(encoding="utf-8")


def fail(message: str) -> None:
    print(f"check-macos-packaging: {message}", file=sys.stderr)
    raise SystemExit(1)


for path in (
    ROOT / "macos/scripts/build-app-bundle.sh",
    ROOT / "macos/scripts/install-embedded-payload.sh",
    ROOT / "macos/scripts/uninstall-embedded-payload.sh",
    ROOT / "macos/launchd/com.steer.steer.control.plist",
    ROOT / "macos/launchd/com.steer.steer.subscription.plist",
    ROOT / "macos/SteerAppIcon.png",
):
    if not path.exists():
        fail(f"missing required file: {path.relative_to(ROOT)}")

for fragment in (
    "runner: xcode-27",
    "arch: arm64",
    "DEVELOPER_DIR: /Applications/Xcode_27.0.app/Contents/Developer",
    "xcodebuild -version | grep -Fx 'Xcode 27.0'",
    "xcrun vtool -show-build",
    "grep -E '^(macOS SDK|Xcode|Swift|Go): '",
    "CGO_ENABLED=0 GOOS=darwin GOARCH=${{ matrix.goarch }} go build",
    "swift build -c release --disable-sandbox --build-system native",
    "swift build -c release --show-bin-path --build-system native",
    "sing-box-$SING_BOX_MACOS_VERSION-darwin-${{ matrix.upstream_arch }}.tar.gz",
    "b9024642ef7b4848252df5469b7f60ef3c18bb5e217a16a0934f0174f8ad11b4",
    'arm64) expected_sha256="$SING_BOX_DARWIN_ARM64_SHA256"',
    'STEER_SING_BOX_ARCHIVE_SHA256: ${{ steps.sing_box.outputs.sha256 }}',
    "macos/scripts/build-app-bundle.sh",
    "steer-macos-arm64.dmg",
    "codesign --verify --deep --strict",
    "plutil -extract CFBundleExecutable raw",
    "name: macos-${{ matrix.arch }}",
    "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6",
):
    if fragment not in WORKFLOW:
        fail(f"tag workflow is missing macOS contract: {fragment}")

for forbidden in (
    "macos-26-intel",
    "steer-macos-x86_64.dmg",
    "SING_BOX_DARWIN_AMD64_SHA256",
    "Xcode_26.6.app",
    "GOOS=darwin GOARCH=amd64 swift",
    "notarytool",
    "altool",
    "Developer ID Application",
    "steer-macos-arm64.zip",
    "steer-macos-x86_64.zip",
    "matrix.sing_box_sha256",
    "0c57457917ad529da4af939a3da5e0ad1cfa639c140dd3de7b6248aef2170bcd",
    "b0c45037c369616e744b8276bfc3be74f246d889531b73ca592a67c0e06bb432",
):
    if forbidden in WORKFLOW or forbidden in BUNDLER:
        fail(f"unsupported macOS release behavior present: {forbidden}")

if 'arm64|x86_64)' in BUNDLER:
    fail("macOS release bundler must only accept the supported arm64 target")

for fragment in (
    'app="$work_directory/Steer.app"',
    '"$app/Contents/MacOS/SteerApp"',
    '"$app/Contents/Resources/Installer"',
    '"$app/Contents/Resources/geodata-seed"',
    '"$app/Contents/Resources/LICENSES"',
    'app_icon="$repository_root/macos/SteerAppIcon.png"',
    'iconutil -c icns "$iconset" -o "$resources_directory/SteerAppIcon.icns"',
    "<key>CFBundleIconFile</key>",
    "<string>SteerAppIcon</string>",
    "<string>SteerApp</string>",
    "<string>com.steer.steer</string>",
    "<string>13.0</string>",
    "STEER_BUILD_NUMBER must contain digits only",
    "PAYLOAD-SHA256SUMS",
    "uninstall-embedded-payload.sh",
    "verify-geodata --directory",
    "validate --config",
    "parse-nodes --input",
    "com.steer.steer.subscription.plist",
    "lipo -archs",
    "xcrun vtool -show-build",
    "SteerApp must be linked against the macOS 27 SDK",
    "codesign --force --sign - --timestamp=none",
    "codesign --verify --deep --strict",
    "hdiutil create",
    "hdiutil verify",
    "ln -s /Applications",
    "Signing: ad-hoc",
    "macOS SDK: $app_sdk",
    "Notarization: none",
    "BUILD-METADATA.txt",
    "SHA256SUMS",
):
    if fragment not in BUNDLER:
        fail(f"bundle script is missing: {fragment}")

helper_sign = BUNDLER.index('codesign --force --sign - --timestamp=none "$installer_directory/steer-macos"')
payload_hash = BUNDLER.index("\t\t> PAYLOAD-SHA256SUMS")
app_sign = BUNDLER.index('codesign --force --sign - --timestamp=none "$app"')
if not helper_sign < payload_hash < app_sign:
    fail("embedded executables must be signed before payload checksums and the App must be signed last")
if 'codesign --force --deep --sign - --timestamp=none "$app"' in BUNDLER:
    fail("final App signing must not recursively mutate already-hashed embedded executables")

for fragment in (
    'helper_payload="$script_dir/steer-macos"',
    'sing_box_payload="$script_dir/sing-box"',
    'control_plist_payload="$script_dir/com.steer.steer.control.plist"',
    'subscription_plist_payload="$script_dir/com.steer.steer.subscription.plist"',
    'geodata_payload="$resources_dir/geodata-seed"',
    "[ ! -L \"$1\" ]",
    "/usr/bin/shasum -a 256 -c PAYLOAD-SHA256SUMS",
    "/usr/bin/codesign --verify --deep --strict \"$app_bundle\"",
    "/usr/bin/file \"$binary\"",
    "verify-geodata --directory",
    "/usr/local/libexec/steer",
    "/Library/LaunchDaemons/com.steer.steer.control.plist",
    "/Library/LaunchDaemons/com.steer.steer.subscription.plist",
    "/var/run/steer",
    "-o root -g wheel -m 0755 \"$socket_directory\"",
    "-o root -g wheel -m 0755",
    "-o root -g wheel -m 0644",
    "if [ -f \"$support_directory/config/config.json\" ]",
    "launchctl bootout system/com.steer.steer.control",
    "launchctl bootstrap system \"$control_plist_path\"",
    "launchctl bootstrap system \"$subscription_plist_path\"",
    '"$script_dir/uninstall-embedded-payload.sh"',
):
    if fragment not in INSTALLER:
        fail(f"embedded installer is missing: {fragment}")

if "command -v" in INSTALLER or "go build" in INSTALLER:
    fail("embedded installer must not depend on PATH discovery or source compilation")

# Exercise the actual installer cleanup against real macOS extended attributes.
# Temporary service definitions never get registered with launchd or need root.
for name, script, runtime_variable in (
    ("embedded", INSTALLER, "runtime_plist_path"),
    ("source", SOURCE_INSTALLER, "plist_path"),
):
    cleanup = re.search(r"^for installed_plist in [^\n]+; do\n.*?^done$", script, re.M | re.S)
    if cleanup is None or "com.apple.quarantine" not in cleanup.group():
        fail(f"{name} installer must handle quarantined service definitions")
    if cleanup.end() > script.index("launchctl bootstrap"):
        fail(f"{name} installer must clean plist quarantine before bootstrap")
    if name == "embedded" and cleanup.start() < script.index("/usr/bin/codesign --verify"):
        fail("embedded installer must verify the App before removing plist quarantine")
    if sys.platform != "darwin":
        continue
    with tempfile.TemporaryDirectory(prefix="steer quarantine test ") as directory:
        root = Path(directory)
        environment = os.environ.copy()
        files = []
        for index, variable in enumerate((runtime_variable, "control_plist_path", "subscription_plist_path")):
            source = root / f"payload-{index}.plist"
            source.write_bytes((ROOT / "macos/launchd/com.steer.steer.plist").read_bytes())
            subprocess.run(["/usr/bin/xattr", "-w", "com.steer.packaging-test", "preserve", str(source)], check=True)
            if index != 1:  # A mixed install also covers files without quarantine.
                subprocess.run(["/usr/bin/xattr", "-w", "com.apple.quarantine", "0081;00000000;SteerTest;", str(source)], check=True)
            destination = root / f"installed-{index}.plist"
            subprocess.run(["/usr/bin/install", "-m", "0644", str(source), str(destination)], check=True)
            environment[variable] = str(destination)
            files.append((source, destination))
        for _ in range(2):  # Repair is idempotent.
            subprocess.run(["/bin/sh", "-eu"], input=cleanup.group(), text=True, env=environment, check=True)
            for index, (source, destination) in enumerate(files):
                attributes = subprocess.check_output(["/usr/bin/xattr", str(destination)], text=True).splitlines()
                if "com.apple.quarantine" in attributes:
                    fail(f"{name} installer left a service definition quarantined")
                if destination.read_bytes() != source.read_bytes():
                    fail(f"{name} installer changed plist contents")
                metadata = subprocess.check_output(["/usr/bin/xattr", "-p", "com.steer.packaging-test", str(destination)], text=True).strip()
                if metadata != "preserve":
                    fail(f"{name} installer removed unrelated metadata")
                source_attributes = subprocess.check_output(["/usr/bin/xattr", str(source)], text=True).splitlines()
                if index != 1 and "com.apple.quarantine" not in source_attributes:
                    fail(f"{name} installer modified the source payload")
        files[0][1].unlink()
        result = subprocess.run(["/bin/sh", "-eu"], input=cleanup.group(), text=True,
                                env=environment, capture_output=True)
        if result.returncode == 0:
            fail(f"{name} installer ignored a plist metadata read failure")

for fragment in (
    '"--remove-user-data"',
    'helper_directory="/usr/local/libexec/steer"',
    'runtime_plist_path="/Library/LaunchDaemons/com.steer.steer.plist"',
    'control_plist_path="/Library/LaunchDaemons/com.steer.steer.control.plist"',
    'subscription_plist_path="/Library/LaunchDaemons/com.steer.steer.subscription.plist"',
    'support_directory="/Library/Application Support/Steer"',
    'logs_directory="/Library/Logs/Steer"',
    'socket_directory="/var/run/steer"',
    'Refusing to uninstall through a symbolic-link directory',
    "/usr/bin/shasum -a 256 -c PAYLOAD-SHA256SUMS",
    '/usr/bin/codesign --verify --deep --strict "$app_bundle"',
    'launchctl bootout "system/$label"',
    '/bin/rm -rf "$helper_directory"',
    '/bin/rm -f "$socket_directory/control.sock"',
    '/bin/rm -rf "$support_directory/run" "$support_directory/geodata-seed"',
    'Preserved configuration, state, and logs.',
):
    if fragment not in UNINSTALLER:
        fail(f"embedded uninstaller is missing: {fragment}")

for forbidden in ("eval ", "sh -c", "command -v", "go build"):
    if forbidden in UNINSTALLER:
        fail(f"embedded uninstaller accepts an unsafe execution path: {forbidden}")

print("macOS native DMG packaging checks passed")
