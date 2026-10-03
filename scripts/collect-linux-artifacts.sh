#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later

set -eu

project_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
binary_root="${1:-bin/linux}"
output_dir="${2:-dist-linux}"
seed_root="${3:?Geo seed directory is required}"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"
steer_version="${STEER_VERSION:?STEER_VERSION is required}"
source_revision="${SOURCE_REVISION:?SOURCE_REVISION is required}"

[ -f "$seed_root/manifest.json" ] || {
	echo "Geo seed manifest not found: $seed_root/manifest.json" >&2
	exit 1
}

case "$source_date_epoch" in
	''|*[!0-9]*)
		echo "SOURCE_DATE_EPOCH must be an unsigned integer" >&2
		exit 1
		;;
esac

work_dir="$(mktemp -d)"
trap 'find "$work_dir" -depth -delete' EXIT HUP INT TERM
if [ -d "$output_dir" ] && [ -n "$(find "$output_dir" ! -path "$output_dir" -print -quit)" ]; then
	echo "Output directory must be empty: $output_dir" >&2
	exit 1
fi
mkdir -p "$output_dir"

for target in x86_64 aarch64; do
	binary="$binary_root/steer-linux-$target"
	[ -x "$binary" ] || {
		echo "Linux binary not found: $binary" >&2
		exit 1
	}
	package_name="steer-linux-$target"
	package_root="$work_dir/$package_name"
	mkdir -p "$package_root/systemd"
	install -m 0755 "$binary" "$package_root/steer"
	install -m 0644 "$project_root"/linux/systemd/*.service "$project_root"/linux/systemd/*.timer "$package_root/systemd/"
	install -m 0644 "$project_root/linux/config.example.json" "$package_root/config.example.json"
	install -m 0644 "$project_root/linux/web.example.json" "$package_root/web.example.json"
	install -m 0644 "$project_root/LICENSE" "$package_root/LICENSE"
	cp -R "$seed_root" "$package_root/geodata-seed"
	temporary_tar="$work_dir/$package_name.tar"
	tar \
		--sort=name \
		--mtime="@$source_date_epoch" \
		--owner=0 \
		--group=0 \
		--numeric-owner \
		-cf "$temporary_tar" \
		-C "$work_dir" \
		"$package_name"
	zstd -q -19 -T0 "$temporary_tar" -o "$output_dir/$package_name.tar.zst"
done

{
	echo "Steer version: $steer_version"
	echo "Source revision: $source_revision"
	echo "Linux targets: x86_64 aarch64"
	echo "Build mode: CGO_ENABLED=0"
	echo "Geo data version: $(sed -n 's/^[[:space:]]*\"version\": \"\([^\"]*\)\",*$/\1/p' "$seed_root/manifest.json" | head -n 1)"
	echo "Geo manifest SHA256: $(sha256sum "$seed_root/manifest.json" | cut -d ' ' -f 1)"
} > "$output_dir/BUILD-METADATA.txt"

(
	cd "$output_dir"
	sha256sum ./*.tar.zst BUILD-METADATA.txt > SHA256SUMS
)
