#!/bin/sh
# Build the local feed with the official OpenWrt SDK image.
# Download phases are intentionally serial; compilation may use all CPUs.

set -eu

CCACHE_DIR=/builder/.ccache
export CCACHE_DIR
export CCACHE_CONFIGPATH2=staging_dir/host/etc/ccache.conf

cd /builder

phase() {
  printf '[sdk-phase] %s %s\n' "$(date -u +%FT%TZ)" "$1"
}

(umask 077 && printf '%s\n' "$OPENWRT_APK_PRIVATE_KEY" > private-key.pem)

phase feeds-update
cat > feeds.conf <<EOF
src-git --root=package base https://git.openwrt.org/openwrt/openwrt.git^f0a60eee2fe051741c643ea6118718aae1ef17fb
src-git packages https://github.com/openwrt/packages.git^5caa62e0bc9f7fb9b0c12a23267bceb7724214dd
src-git luci https://github.com/openwrt/luci.git^128a7812f4be233c5dd7f7466f534fd888785caf
src-link $FEEDNAME /feed/
EOF
./scripts/feeds update -a

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

mkdir -p "$CCACHE_DIR" /go-build-cache
printf '%s\n' \
  'compiler_type=gcc' \
  'max_size=8G' \
  'depend_mode=true' \
  'sloppiness=file_macro,locale,time_macros,include_file_ctime,include_file_mtime' \
  'compiler_check=string:openwrt-sdk-x86_64-25.12.5:c8a248ce2411962a89f227db444bf5cea022829b049e6326c7d1032d9762982a' \
  > "$CCACHE_CONFIGPATH2"
staging_dir/host/bin/ccache --zero-stats

# Keep the Steer package source archives on the official serial make download
# path. Only the actual compilation is parallelised.
phase package-download
for package in $PACKAGES; do
  make "package/$package/download" V=s
done

# Compile one top-level dependency closure.  OpenWrt's dependency graph pulls
# steer into the LuCI package build.
phase package-compile
make package/luci-app-steer/compile V=s -j "$(nproc)" CONFIG_AUTOREMOVE=y $kmod_overrides

phase mirror-sing-box
package_arch="$(make --no-print-directory val.ARCH_PACKAGES)"
repository_dir="bin/packages/$package_arch/$FEEDNAME"
mirrored_sing_box="$repository_dir/sing-box-${SING_BOX_MIRROR_PACKAGE_VERSION}.apk"
cp "$SING_BOX_UPSTREAM_APK" "$mirrored_sing_box"
staging_dir/host/bin/apk adbdump "$mirrored_sing_box" | sed '/^# sig /d' > upstream.dump
staging_dir/host/bin/apk --allow-untrusted adbsign \
  --reset-signatures \
  --sign-key private-key.pem \
  "$mirrored_sing_box"
staging_dir/host/bin/apk adbdump "$mirrored_sing_box" | sed '/^# sig /d' > mirrored.dump
# Re-signing must not change the official package metadata or payload.
cmp upstream.dump mirrored.dump
staging_dir/host/bin/apk --keys-dir /feed/keys verify "$mirrored_sing_box"

phase package-index
make package/index
staging_dir/host/bin/apk --keys-dir /feed/keys verify "$repository_dir/packages.adb"
# The signed index must contain exactly the four published packages.
staging_dir/host/bin/apk adbdump "$repository_dir/packages.adb" \
  | sed -n 's/^  - name: //p' | sort > index-packages.txt
printf '%s\n' luci-app-steer luci-i18n-steer-zh-cn sing-box steer | cmp - index-packages.txt

phase ccache-stats
staging_dir/host/bin/ccache --show-stats
mv bin logs "$ARTIFACTS_DIR/"
