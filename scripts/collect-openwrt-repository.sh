#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later

set -eu

package_dir="${1:-bin/packages/x86_64/steer}"
output_dir="${2:-openwrt-repository}"
public_key="${3:-keys/steer-apk.pem}"

[ -d "$package_dir" ] || {
	echo "OpenWrt package repository not found: $package_dir" >&2
	exit 1
}
[ -f "$package_dir/packages.adb" ] || {
	echo "Signed OpenWrt package index not found: $package_dir/packages.adb" >&2
	exit 1
}
[ -f "$public_key" ] || {
	echo "OpenWrt repository public key not found: $public_key" >&2
	exit 1
}
[ ! -e "$output_dir" ] || {
	echo "OpenWrt repository output already exists: $output_dir" >&2
	exit 1
}

mkdir -p "$output_dir"

for pattern in \
	'sing-box-*.apk' \
	'steer-[0-9]*.apk' \
	'luci-app-steer-*.apk' \
	'luci-i18n-steer-zh-cn-*.apk'; do
	set -- $(find "$package_dir" -maxdepth 1 -type f -name "$pattern" | sort)
	if [ "$#" -ne 1 ]; then
		echo "Expected exactly one repository APK matching $pattern, found $#" >&2
		exit 1
	fi
	cp "$1" "$output_dir/"
done

cp "$package_dir/packages.adb" "$output_dir/"
cp "$public_key" "$output_dir/steer-apk.pem"

{
	echo "OpenWrt release: ${OPENWRT_RELEASE:-unknown}"
	echo "OpenWrt target: ${OPENWRT_TARGET:-unknown}"
	echo "Source revision: ${SOURCE_REVISION:-unknown}"
	echo "Geo data version: ${GEODATA_VERSION:-unknown}"
	echo "Geo manifest SHA256: ${GEODATA_MANIFEST_SHA256:-unknown}"
	echo "sing-box mirrored SHA256: ${SING_BOX_MIRRORED_SHA256:-unknown}"
	echo "Repository key SHA256: $(sha256sum "$public_key" | cut -d ' ' -f 1)"
} > "$output_dir/BUILD-METADATA.txt"

(
	cd "$output_dir"
	sha256sum ./*.apk packages.adb steer-apk.pem BUILD-METADATA.txt > SHA256SUMS
	sha256sum -c SHA256SUMS
)
