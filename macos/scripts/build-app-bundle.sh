#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
set -eu

version="$STEER_VERSION"
marketing_version="${version%%-*}"
target_arch=arm64

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
launchd_directory="$repository_root/macos/launchd"

# Swift's default build system records the wrong SDK in the binary, which is
# why the release builds with --build-system native. Catch a regression here.
app_build_version="$(xcrun vtool -show-build "$STEER_APP_BINARY")"
app_minos="$(printf '%s\n' "$app_build_version" | awk '$1 == "minos" {print $2; exit}')"
app_sdk="$(printf '%s\n' "$app_build_version" | awk '$1 == "sdk" {print $2; exit}')"
[ "$app_minos" = "13.0" ] && [ "${app_sdk%%.*}" = "27" ] || {
	printf 'SteerApp must target macOS 13.0 with the macOS 27 SDK, found minos %s sdk %s\n' "$app_minos" "$app_sdk" >&2
	exit 1
}

work_directory="$(mktemp -d)"
trap 'rm -rf "$work_directory"' EXIT HUP INT TERM

app="$work_directory/Steer.app"
resources_directory="$app/Contents/Resources"
installer_directory="$resources_directory/Installer"
licenses_directory="$resources_directory/LICENSES"
mkdir -p "$app/Contents/MacOS" "$installer_directory" "$licenses_directory"
install -m 0755 "$STEER_APP_BINARY" "$app/Contents/MacOS/SteerApp"
install -m 0755 "$STEER_HELPER_BINARY" "$installer_directory/steer-macos"
install -m 0755 "$STEER_SING_BOX_BINARY" "$installer_directory/sing-box"
install -m 0755 "$script_dir/install-embedded-payload.sh" "$installer_directory/install-embedded-payload.sh"
install -m 0755 "$script_dir/uninstall-embedded-payload.sh" "$installer_directory/uninstall-embedded-payload.sh"
install -m 0644 "$launchd_directory/com.steer.steer.plist" "$installer_directory/com.steer.steer.plist"
install -m 0644 "$launchd_directory/com.steer.steer.control.plist" "$installer_directory/com.steer.steer.control.plist"
install -m 0644 "$launchd_directory/com.steer.steer.subscription.plist" "$installer_directory/com.steer.steer.subscription.plist"
install -m 0644 "$repository_root/linux/config.example.json" "$installer_directory/config.example.json"
install -m 0644 "$repository_root/LICENSE" "$licenses_directory/Steer-GPL-3.0.txt"
install -m 0644 "$STEER_SING_BOX_LICENSE" "$licenses_directory/sing-box-GPL-3.0.txt"
cp -R "$STEER_GEODATA_DIRECTORY" "$resources_directory/geodata-seed"

iconset="$work_directory/SteerAppIcon.iconset"
mkdir -p "$iconset"
for spec in 16:16x16 32:16x16@2x 32:32x32 64:32x32@2x 128:128x128 \
	256:128x128@2x 256:256x256 512:256x256@2x 512:512x512 1024:512x512@2x; do
	sips --resampleHeightWidth "${spec%%:*}" "${spec%%:*}" "$repository_root/macos/SteerAppIcon.png" \
		--out "$iconset/icon_${spec#*:}.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$resources_directory/SteerAppIcon.icns"

cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>Steer</string>
	<key>CFBundleExecutable</key>
	<string>SteerApp</string>
	<key>CFBundleIdentifier</key>
	<string>com.steer.steer</string>
	<key>CFBundleIconFile</key>
	<string>SteerAppIcon</string>
	<key>CFBundleName</key>
	<string>Steer</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$marketing_version</string>
	<key>CFBundleVersion</key>
	<string>$STEER_BUILD_NUMBER</string>
	<key>LSMinimumSystemVersion</key>
	<string>13.0</string>
</dict>
</plist>
EOF

# Sign every embedded executable before hashing the installer payload. Signing
# changes Mach-O bytes, so checksums generated earlier would make every first
# install fail before it can write any system component.
codesign --force --sign - --timestamp=none "$installer_directory/steer-macos"
codesign --force --sign - --timestamp=none "$installer_directory/sing-box"
codesign --force --sign - --timestamp=none "$app/Contents/MacOS/SteerApp"

(
	cd "$installer_directory"
	shasum -a 256 \
		steer-macos \
		sing-box \
		install-embedded-payload.sh \
		uninstall-embedded-payload.sh \
		com.steer.steer.plist \
		com.steer.steer.control.plist \
		com.steer.steer.subscription.plist \
		config.example.json \
		> PAYLOAD-SHA256SUMS
)

codesign --force --sign - --timestamp=none "$app"

mkdir -p "$STEER_OUTPUT_DIRECTORY"
dmg_root="$work_directory/dmg"
mkdir -p "$dmg_root"
ditto "$app" "$dmg_root/Steer.app"
ln -s /Applications "$dmg_root/Applications"
hdiutil create \
	-volname "Steer $version" \
	-srcfolder "$dmg_root" \
	-format UDZO \
	-ov \
	"$STEER_OUTPUT_DIRECTORY/steer-macos-$target_arch.dmg"

cat > "$STEER_OUTPUT_DIRECTORY/BUILD-METADATA.txt" <<EOF
Steer version: $version
Source tag: $STEER_SOURCE_TAG
Source revision: $STEER_SOURCE_REVISION
Target architecture: $target_arch
Minimum macOS: 13.0
macOS SDK: $app_sdk
Xcode: $STEER_XCODE_VERSION
Swift: $STEER_SWIFT_VERSION
Go: $STEER_GO_VERSION
sing-box version: $STEER_SING_BOX_VERSION
sing-box revision: $STEER_SING_BOX_REVISION
sing-box upstream archive SHA256: $STEER_SING_BOX_ARCHIVE_SHA256
Geo manifest SHA256: $STEER_GEO_MANIFEST_SHA256
Signing: ad-hoc
Notarization: none
EOF
