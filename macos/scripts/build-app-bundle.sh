#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
set -eu

app_binary="${STEER_APP_BINARY:?STEER_APP_BINARY is required}"
helper_binary="${STEER_HELPER_BINARY:?STEER_HELPER_BINARY is required}"
sing_box_binary="${STEER_SING_BOX_BINARY:?STEER_SING_BOX_BINARY is required}"
sing_box_license="${STEER_SING_BOX_LICENSE:?STEER_SING_BOX_LICENSE is required}"
geodata_directory="${STEER_GEODATA_DIRECTORY:?STEER_GEODATA_DIRECTORY is required}"
output_directory="${STEER_OUTPUT_DIRECTORY:?STEER_OUTPUT_DIRECTORY is required}"
version="${STEER_VERSION:?STEER_VERSION is required}"
marketing_version="${version%%-*}"
build_number="${STEER_BUILD_NUMBER:?STEER_BUILD_NUMBER is required}"
target_arch="${STEER_TARGET_ARCH:?STEER_TARGET_ARCH is required}"
source_revision="${STEER_SOURCE_REVISION:?STEER_SOURCE_REVISION is required}"
source_tag="${STEER_SOURCE_TAG:?STEER_SOURCE_TAG is required}"
sing_box_version="${STEER_SING_BOX_VERSION:?STEER_SING_BOX_VERSION is required}"
sing_box_revision="${STEER_SING_BOX_REVISION:?STEER_SING_BOX_REVISION is required}"
sing_box_archive_sha256="${STEER_SING_BOX_ARCHIVE_SHA256:?STEER_SING_BOX_ARCHIVE_SHA256 is required}"
geo_manifest_sha256="${STEER_GEO_MANIFEST_SHA256:?STEER_GEO_MANIFEST_SHA256 is required}"
xcode_version="${STEER_XCODE_VERSION:?STEER_XCODE_VERSION is required}"
swift_version="${STEER_SWIFT_VERSION:?STEER_SWIFT_VERSION is required}"
go_version="${STEER_GO_VERSION:?STEER_GO_VERSION is required}"

case "$target_arch" in
	arm64) ;;
	*)
		printf 'Unsupported target architecture: %s\n' "$target_arch" >&2
		exit 1
		;;
esac
case "$build_number" in
	''|*[!0-9]*)
		printf '%s\n' 'STEER_BUILD_NUMBER must contain digits only.' >&2
		exit 1
		;;
esac

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
runtime_plist="$repository_root/macos/launchd/com.steer.steer.plist"
control_plist="$repository_root/macos/launchd/com.steer.steer.control.plist"
subscription_plist="$repository_root/macos/launchd/com.steer.steer.subscription.plist"
embedded_installer="$repository_root/macos/scripts/install-embedded-payload.sh"
embedded_uninstaller="$repository_root/macos/scripts/uninstall-embedded-payload.sh"
config_example="$repository_root/linux/config.example.json"
steer_license="$repository_root/LICENSE"
app_icon="$repository_root/macos/SteerAppIcon.png"

require_regular_file() {
	[ -f "$1" ] && [ ! -L "$1" ] || {
		printf 'Expected a regular input file: %s\n' "$1" >&2
		exit 1
	}
}

for input in \
	"$app_binary" \
	"$helper_binary" \
	"$sing_box_binary" \
	"$sing_box_license" \
	"$runtime_plist" \
	"$control_plist" \
	"$subscription_plist" \
	"$embedded_installer" \
	"$embedded_uninstaller" \
	"$config_example" \
	"$steer_license" \
	"$app_icon" \
	"$geodata_directory/manifest.json"; do
	require_regular_file "$input"
done
if find "$geodata_directory" -type l -print -quit | grep -q .; then
	printf '%s\n' 'Geo seed must not contain symbolic links.' >&2
	exit 1
fi
actual_geo_sha256="$(shasum -a 256 "$geodata_directory/manifest.json" | awk '{print $1}')"
[ "$actual_geo_sha256" = "$geo_manifest_sha256" ] || {
	printf 'Geo manifest SHA mismatch: expected %s, found %s\n' "$geo_manifest_sha256" "$actual_geo_sha256" >&2
	exit 1
}

for binary in "$app_binary" "$helper_binary" "$sing_box_binary"; do
	architectures="$(lipo -archs "$binary")"
	[ "$architectures" = "$target_arch" ] || {
		printf 'Expected %s binary, found %s: %s\n' "$target_arch" "$architectures" "$binary" >&2
		exit 1
	}
done

app_build_version="$(xcrun vtool -show-build "$app_binary")"
app_minos="$(printf '%s\n' "$app_build_version" | awk '$1 == "minos" {print $2; exit}')"
app_sdk="$(printf '%s\n' "$app_build_version" | awk '$1 == "sdk" {print $2; exit}')"
[ "$app_minos" = "13.0" ] || {
	printf 'SteerApp deployment target must be macOS 13.0, found %s\n' "$app_minos" >&2
	exit 1
}
[ "${app_sdk%%.*}" = "27" ] || {
	printf 'SteerApp must be linked against the macOS 27 SDK, found %s\n' "$app_sdk" >&2
	exit 1
}

helper_version="$($helper_binary version)"
[ "$helper_version" = "$version" ] || {
	printf 'steer-macos version mismatch: expected %s, found %s\n' "$version" "$helper_version" >&2
	exit 1
}
sing_box_version_output="$($sing_box_binary version)"
printf '%s\n' "$sing_box_version_output" | grep -Fqx "sing-box version $sing_box_version"
printf '%s\n' "$sing_box_version_output" | grep -Fqx "Revision: $sing_box_revision"
printf '%s\n' "$sing_box_version_output" | grep -Eq '^Tags: .+'
"$helper_binary" verify-geodata --directory "$geodata_directory"
"$helper_binary" validate --config "$config_example" >/dev/null
work_directory="$(mktemp -d)"
cleanup() {
	rm -rf "$work_directory"
}
trap cleanup EXIT HUP INT TERM
node_input="$work_directory/nodes.txt"
printf '%s\n' 'socks://user:pass@127.0.0.1:1080#PackagingSmoke' > "$node_input"
"$helper_binary" parse-nodes --input "$node_input" | grep -Fq 'PackagingSmoke'
rm -f "$node_input"

app="$work_directory/Steer.app"
resources_directory="$app/Contents/Resources"
installer_directory="$app/Contents/Resources/Installer"
licenses_directory="$app/Contents/Resources/LICENSES"
mkdir -p "$app/Contents/MacOS" "$installer_directory" "$licenses_directory"
install -m 0755 "$app_binary" "$app/Contents/MacOS/SteerApp"
install -m 0755 "$helper_binary" "$installer_directory/steer-macos"
install -m 0755 "$sing_box_binary" "$installer_directory/sing-box"
install -m 0755 "$embedded_installer" "$installer_directory/install-embedded-payload.sh"
install -m 0755 "$embedded_uninstaller" "$installer_directory/uninstall-embedded-payload.sh"
install -m 0644 "$runtime_plist" "$installer_directory/com.steer.steer.plist"
install -m 0644 "$control_plist" "$installer_directory/com.steer.steer.control.plist"
install -m 0644 "$subscription_plist" "$installer_directory/com.steer.steer.subscription.plist"
install -m 0644 "$config_example" "$installer_directory/config.example.json"
install -m 0644 "$steer_license" "$licenses_directory/Steer-GPL-3.0.txt"
install -m 0644 "$sing_box_license" "$licenses_directory/sing-box-GPL-3.0.txt"
cp -R "$geodata_directory" "$app/Contents/Resources/geodata-seed"

iconset="$work_directory/SteerAppIcon.iconset"
mkdir -p "$iconset"
render_icon() {
	size="$1"
	output="$2"
	sips --resampleHeightWidth "$size" "$size" "$app_icon" --out "$iconset/$output" >/dev/null
}
render_icon 16 icon_16x16.png
render_icon 32 icon_16x16@2x.png
render_icon 32 icon_32x32.png
render_icon 64 icon_32x32@2x.png
render_icon 128 icon_128x128.png
render_icon 256 icon_128x128@2x.png
render_icon 256 icon_256x256.png
render_icon 512 icon_256x256@2x.png
render_icon 512 icon_512x512.png
render_icon 1024 icon_512x512@2x.png
iconutil -c icns "$iconset" -o "$resources_directory/SteerAppIcon.icns"
require_regular_file "$resources_directory/SteerAppIcon.icns"

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
	<string>$build_number</string>
	<key>LSMinimumSystemVersion</key>
	<string>13.0</string>
</dict>
</plist>
EOF
plutil -lint "$app/Contents/Info.plist" >/dev/null
if grep -Eq '\$\((MARKETING_VERSION|CURRENT_PROJECT_VERSION)\)' "$app/Contents/Info.plist"; then
	printf '%s\n' 'App Info.plist contains an unexpanded build setting.' >&2
	exit 1
fi

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
	shasum -a 256 -c PAYLOAD-SHA256SUMS
)

if find "$app" -type f \( -name '*.dat' -o -name '*.go' -o -name '*.key' -o -name '*.pem' \) -print -quit | grep -q .; then
	printf '%s\n' 'App bundle contains a forbidden source, key, PEM, or DAT file.' >&2
	exit 1
fi

codesign --force --sign - --timestamp=none "$app"
codesign --verify --deep --strict --verbose=2 "$app"

mkdir -p "$output_directory"
dmg_name="steer-macos-$target_arch.dmg"
dmg_path="$output_directory/$dmg_name"
[ ! -e "$dmg_path" ] || {
	printf 'Refusing to overwrite existing output: %s\n' "$dmg_path" >&2
	exit 1
}
dmg_root="$work_directory/dmg"
mkdir -p "$dmg_root"
ditto "$app" "$dmg_root/Steer.app"
ln -s /Applications "$dmg_root/Applications"
hdiutil create \
	-volname "Steer $version" \
	-srcfolder "$dmg_root" \
	-format UDZO \
	-ov \
	"$dmg_path"
hdiutil verify "$dmg_path"

cat > "$output_directory/BUILD-METADATA.txt" <<EOF
Steer version: $version
Source tag: $source_tag
Source revision: $source_revision
Target architecture: $target_arch
Minimum macOS: 13.0
macOS SDK: $app_sdk
Xcode: $xcode_version
Swift: $swift_version
Go: $go_version
sing-box version: $sing_box_version
sing-box revision: $sing_box_revision
sing-box upstream archive SHA256: $sing_box_archive_sha256
Geo manifest SHA256: $geo_manifest_sha256
Signing: ad-hoc
Notarization: none
EOF
(
	cd "$output_directory"
	shasum -a 256 "$dmg_name" BUILD-METADATA.txt > SHA256SUMS
	shasum -a 256 -c SHA256SUMS
)
