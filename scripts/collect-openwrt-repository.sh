#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later

set -eu

package_dir="${1:-bin/packages/x86_64/steer}"
output_dir="${2:-openwrt-repository}"
public_key="${3:-keys/steer-apk.pem}"

mkdir -p "$output_dir"
# The SDK build checked that the signed index holds exactly these packages.
cp "$package_dir"/*.apk "$package_dir/packages.adb" "$output_dir/"
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
)
