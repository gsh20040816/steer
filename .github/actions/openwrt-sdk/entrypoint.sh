#!/bin/sh
# Build the local feed with the official OpenWrt SDK image.
# Download phases are intentionally serial; compilation may use all CPUs.

set -eu

FEEDNAME="${FEEDNAME:-steer}"
ARTIFACTS_DIR="${ARTIFACTS_DIR:-/artifacts}"
PACKAGES="${PACKAGES:-steer luci-app-steer}"
CCACHE_DIR="${CCACHE_DIR:-/builder/.ccache}"
GO_BUILD_CACHE_DIR="${GO_BUILD_CACHE_DIR:-/go-build-cache}"
export CCACHE_DIR
export CCACHE_CONFIGPATH2=staging_dir/host/etc/ccache.conf

cd /builder

phase() {
  printf '[sdk-phase] %s %s\n' "$(date -u +%FT%TZ)" "$1"
}

[ -n "${OPENWRT_APK_PRIVATE_KEY:-}" ] || {
  echo 'OPENWRT_APK_PRIVATE_KEY is required to sign the OpenWrt repository.' >&2
  exit 1
}
[ -n "${SING_BOX_UPSTREAM_APK:-}" ] && [ -f "$SING_BOX_UPSTREAM_APK" ] || {
  echo 'SING_BOX_UPSTREAM_APK must name the mounted official APK.' >&2
  exit 1
}
[ -n "${SING_BOX_UPSTREAM_SHA256:-}" ] || {
  echo 'SING_BOX_UPSTREAM_SHA256 is required.' >&2
  exit 1
}
case "${SING_BOX_MIRROR_PACKAGE_VERSION:-}" in
  ''|*[!0-9A-Za-z._-]*)
    echo 'SING_BOX_MIRROR_PACKAGE_VERSION must be a safe package version.' >&2
    exit 1
    ;;
esac

old_umask="$(umask)"
umask 077
printf '%s\n' "$OPENWRT_APK_PRIVATE_KEY" > private-key.pem
umask "$old_umask"
staging_dir/host/bin/openssl ec \
  -in private-key.pem \
  -pubout \
  -out generated-public-key.pem 2>/dev/null
cmp generated-public-key.pem /feed/keys/steer-apk.pem || {
  echo 'OPENWRT_APK_PRIVATE_KEY does not match keys/steer-apk.pem.' >&2
  exit 1
}
rm -f generated-public-key.pem

phase feeds-update
cat > feeds.conf <<EOF
src-git --root=package base https://git.openwrt.org/openwrt/openwrt.git^f0a60eee2fe051741c643ea6118718aae1ef17fb
src-git packages https://github.com/openwrt/packages.git^5caa62e0bc9f7fb9b0c12a23267bceb7724214dd
src-git luci https://github.com/openwrt/luci.git^128a7812f4be233c5dd7f7466f534fd888785caf
src-link $FEEDNAME /feed/
EOF
printf '%s\n' '--- feeds.conf ---'
cat feeds.conf

./scripts/feeds update -a
printf '%s\n' '--- package/base layout ---'
find feeds/base_root/package -maxdepth 2 -type f -name Makefile -print | sort | head -20

# The stock SDK defconfig enables every kernel module and device profile.  Set
# the two guards before the first defconfig, then prune profile selections.
cat > .config <<'EOF'
CONFIG_ALL_KMODS=n
CONFIG_ALL_NONSHARED=n
CONFIG_CCACHE=y
CONFIG_LUCI_LANG_zh_Hans=y
EOF
phase initial-defconfig
make defconfig
grep -qx '# CONFIG_ALL_KMODS is not set' .config
grep -qx '# CONFIG_ALL_NONSHARED is not set' .config

# Preserve the SDK device-profile selections as explicit make-time unsets.
# The profile menu selects MODULE_DEFAULT_* symbols again while make builds
# package metadata; passing these overrides keeps that firmware-only set out
# of the single package dependency closure.
kmod_overrides="$(grep '^CONFIG_PACKAGE_kmod-.*=[my]' .config | sed 's/=.*/=/')"

phase feed-install
for package in $PACKAGES; do
  ./scripts/feeds install -p "$FEEDNAME" -f "$package"
done

# Keep the SDK's non-kernel target package selections so their headers and
# staging metadata remain available to LuCI dependencies.  Only the generic
# device kmods are removed after the final defconfig below.
cat >> .config <<'EOF'
CONFIG_ALL_KMODS=n
CONFIG_ALL_NONSHARED=n
CONFIG_CCACHE=y
CONFIG_PACKAGE_luci-app-steer=m
CONFIG_LUCI_LANG_zh_Hans=y
CONFIG_GOLANG_BUILD_CACHE_DIR="/go-build-cache"
EOF
phase final-defconfig
make defconfig

# Final defconfig selects the real package dependency closure, but it also
# recreates the SDK's generic x86 device profile. Remove only the kmods that
# were present before this package build; the make-time overrides below enforce
# the same boundary during package metadata traversal.
profile_kmods="$(mktemp)"
printf '%s\n' "$kmod_overrides" | sed 's/=$//' > "$profile_kmods"
minimal_config="$(mktemp)"
awk '
  NR == FNR { drop[$1] = 1; next }
  /^CONFIG_PACKAGE_kmod-.*=[my]$/ {
    symbol = $0
    sub(/=.*/, "", symbol)
    if (drop[symbol]) {
      print "# " symbol " is not set"
      next
    }
  }
  { print }
' "$profile_kmods" .config > "$minimal_config"
mv "$minimal_config" .config
rm -f "$profile_kmods"

# These target-profile modules are not in Steer's dependency closure.  Seeing
# either one means the SDK config still selected unrelated target state.
if grep -Eq '^CONFIG_PACKAGE_(kmod-r8169|kmod-video[^=]*)=[ym]' .config; then
  echo 'Unexpected unrelated kmod selected by SDK defconfig:' >&2
  grep -E '^CONFIG_PACKAGE_(kmod-r8169|kmod-video[^=]*)=[ym]' .config >&2
  exit 1
fi

selected_kmods="$(grep -c '^CONFIG_PACKAGE_kmod-.*=[my]' .config || true)"
if [ "$selected_kmods" -gt 100 ]; then
  echo "Minimal package config unexpectedly selected $selected_kmods kernel modules." >&2
  exit 1
fi
echo "Selected kernel module packages: $selected_kmods"

mkdir -p "$CCACHE_DIR" "$GO_BUILD_CACHE_DIR"
touch "$CCACHE_CONFIGPATH2"
printf '%s\n' \
  'compiler_type=gcc' \
  'max_size=8G' \
  'depend_mode=true' \
  'sloppiness=file_macro,locale,time_macros,include_file_ctime,include_file_mtime' \
  'compiler_check=string:openwrt-sdk-x86_64-25.12.5:c8a248ce2411962a89f227db444bf5cea022829b049e6326c7d1032d9762982a' \
  > "$CCACHE_CONFIGPATH2"
resolved_ccache_dir="$(make --no-print-directory val.CCACHE_DIR)"
[ "$resolved_ccache_dir" = "$CCACHE_DIR" ] || {
  echo "OpenWrt resolved CCACHE_DIR to $resolved_ccache_dir instead of $CCACHE_DIR." >&2
  exit 1
}
staging_dir/host/bin/ccache --zero-stats || true

# Keep the Steer package source archives on the official serial make download
# path. Only the actual compilation is parallelised.
phase package-download
for package in $PACKAGES; do
  make "package/$package/download" V=s
done

# Compile one top-level dependency closure.  OpenWrt's dependency graph pulls
# steer into the LuCI package build.
phase package-compile
make package/luci-app-steer/compile V="${V:-s}" -j "$(nproc)" CONFIG_AUTOREMOVE=y $kmod_overrides

phase mirror-sing-box
printf '%s  %s\n' "$SING_BOX_UPSTREAM_SHA256" "$SING_BOX_UPSTREAM_APK" | sha256sum -c
package_arch="$(make --no-print-directory val.ARCH_PACKAGES)"
repository_dir="bin/packages/$package_arch/$FEEDNAME"
mkdir -p "$repository_dir"
mirrored_sing_box="$repository_dir/sing-box-${SING_BOX_MIRROR_PACKAGE_VERSION}.apk"
cp "$SING_BOX_UPSTREAM_APK" "$mirrored_sing_box"
upstream_dump="$(mktemp)"
mirrored_dump="$(mktemp)"
staging_dir/host/bin/apk adbdump "$mirrored_sing_box" | sed '/^# sig /d' > "$upstream_dump"
staging_dir/host/bin/apk --allow-untrusted adbsign \
  --reset-signatures \
  --sign-key private-key.pem \
  "$mirrored_sing_box"
staging_dir/host/bin/apk adbdump "$mirrored_sing_box" | sed '/^# sig /d' > "$mirrored_dump"
cmp "$upstream_dump" "$mirrored_dump" || {
  echo 'Re-signing changed official sing-box package metadata or payload.' >&2
  exit 1
}
rm -f "$upstream_dump" "$mirrored_dump"
staging_dir/host/bin/apk --keys-dir /feed/keys verify "$mirrored_sing_box"

phase package-index
make package/index

staging_dir/host/bin/apk \
  --keys-dir /feed/keys \
  verify "$repository_dir/packages.adb"

actual_packages="$(mktemp)"
expected_packages="$(mktemp)"
staging_dir/host/bin/apk adbdump "$repository_dir/packages.adb" \
  | sed -n 's/^  - name: //p' \
  | sort > "$actual_packages"
printf '%s\n' \
  luci-app-steer \
  luci-i18n-steer-zh-cn \
  sing-box \
  steer \
  | sort > "$expected_packages"
cmp "$expected_packages" "$actual_packages" || {
  echo 'OpenWrt repository index does not contain the exact Steer package set.' >&2
  exit 1
}
rm -f "$actual_packages" "$expected_packages"

phase ccache-stats
staging_dir/host/bin/ccache -vv --show-stats || true
mkdir -p "$ARTIFACTS_DIR"
mv bin "$ARTIFACTS_DIR/"
[ ! -d logs ] || mv logs "$ARTIFACTS_DIR/"
