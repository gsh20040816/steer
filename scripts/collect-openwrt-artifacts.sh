#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later

set -eu

package_root="${1:-bin/packages/x86_64/steer}"
output_dir="${2:-dist}"

[ -d "$package_root" ] || {
	echo "Package directory not found: $package_root" >&2
	exit 1
}

mkdir -p "$output_dir"

for pattern in \
	'sing-box-*.apk' \
	'steer-[0-9]*.apk' \
	'luci-app-steer-*.apk' \
	'luci-i18n-steer-zh-cn-*.apk'; do
	set -- $(find "$package_root" -maxdepth 1 -type f -name "$pattern" | sort)
	if [ "$#" -ne 1 ]; then
		echo "Expected exactly one APK matching $pattern, found $#" >&2
		exit 1
	fi
	cp "$1" "$output_dir/"
done

{
	echo "OpenWrt release: ${OPENWRT_RELEASE:-unknown}"
	echo "OpenWrt target: ${OPENWRT_TARGET:-unknown}"
	echo "Source revision: ${SOURCE_REVISION:-unknown}"
	echo "Geo data version: ${GEODATA_VERSION:-unknown}"
	echo "Geo manifest SHA256: ${GEODATA_MANIFEST_SHA256:-unknown}"
	echo "sing-box mirrored SHA256: ${SING_BOX_MIRRORED_SHA256:-unknown}"
} > "$output_dir/BUILD-METADATA.txt"

(
	cd "$output_dir"
	sha256sum ./*.apk BUILD-METADATA.txt > SHA256SUMS
)
