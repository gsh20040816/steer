#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
set -eu

if [ "$(/usr/bin/uname -s)" != "Darwin" ]; then
	printf '%s\n' 'Steer embedded payload can only be installed on macOS.' >&2
	exit 1
fi
if [ "$(/usr/bin/id -u)" -ne 0 ]; then
	printf '%s\n' 'Steer embedded payload installer must run as root.' >&2
	exit 1
fi

script_dir="$(CDPATH= cd -- "$(/usr/bin/dirname -- "$0")" && /bin/pwd)"
resources_dir="$(CDPATH= cd -- "$script_dir/.." && /bin/pwd)"
app_bundle="$(CDPATH= cd -- "$resources_dir/../.." && /bin/pwd)"
helper_payload="$script_dir/steer-macos"
sing_box_payload="$script_dir/sing-box"
runtime_plist_payload="$script_dir/com.steer.steer.plist"
control_plist_payload="$script_dir/com.steer.steer.control.plist"
subscription_plist_payload="$script_dir/com.steer.steer.subscription.plist"
config_payload="$script_dir/config.example.json"
payload_sums="$script_dir/PAYLOAD-SHA256SUMS"
geodata_payload="$resources_dir/geodata-seed"

helper_directory="/usr/local/libexec/steer"
runtime_plist_path="/Library/LaunchDaemons/com.steer.steer.plist"
control_plist_path="/Library/LaunchDaemons/com.steer.steer.control.plist"
subscription_plist_path="/Library/LaunchDaemons/com.steer.steer.subscription.plist"
support_directory="/Library/Application Support/Steer"
geodata_directory="$support_directory/geodata-seed"
socket_directory="/var/run/steer"

require_regular_file() {
	[ -f "$1" ] && [ ! -L "$1" ] || {
		printf 'Embedded payload is missing a regular file: %s\n' "$1" >&2
		exit 1
	}
}

for payload in \
	"$helper_payload" \
	"$sing_box_payload" \
	"$runtime_plist_payload" \
	"$control_plist_payload" \
	"$subscription_plist_payload" \
	"$config_payload" \
	"$payload_sums" \
	"$script_dir/install-embedded-payload.sh" \
	"$script_dir/uninstall-embedded-payload.sh" \
	"$geodata_payload/manifest.json"; do
	require_regular_file "$payload"
done

if /usr/bin/find "$geodata_payload" -type l -print -quit | /usr/bin/grep -q .; then
	printf '%s\n' 'Embedded Geo seed must not contain symbolic links.' >&2
	exit 1
fi

(
	cd "$script_dir"
	/usr/bin/shasum -a 256 -c PAYLOAD-SHA256SUMS
)
/usr/bin/codesign --verify --deep --strict "$app_bundle"
/usr/bin/plutil -lint "$runtime_plist_payload" "$control_plist_payload" "$subscription_plist_payload" >/dev/null

machine_arch="$(/usr/bin/uname -m)"
case "$machine_arch" in
	arm64|x86_64) ;;
	*)
		printf 'Unsupported macOS architecture: %s\n' "$machine_arch" >&2
		exit 1
		;;
esac
for binary in "$helper_payload" "$sing_box_payload"; do
	/usr/bin/file "$binary" | /usr/bin/grep -Eq "Mach-O .*${machine_arch}" || {
		printf 'Embedded binary does not match %s: %s\n' "$machine_arch" "$binary" >&2
		exit 1
	}
done
"$helper_payload" verify-geodata --directory "$geodata_payload"

/usr/bin/install -d -o root -g wheel -m 0755 "$helper_directory"
/usr/bin/install -d -o root -g wheel -m 0755 "$socket_directory"
/usr/bin/install -d -o root -g admin -m 0750 \
	"$support_directory" \
	"$support_directory/config" \
	"$support_directory/run" \
	"$support_directory/state" \
	"/Library/Logs/Steer"

/bin/launchctl bootout system/com.steer.steer 2>/dev/null || true
/bin/launchctl bootout system/com.steer.steer.control 2>/dev/null || true
/bin/launchctl bootout system/com.steer.steer.subscription 2>/dev/null || true
remaining_checks=50
while /bin/launchctl print system/com.steer.steer >/dev/null 2>&1 \
	|| /bin/launchctl print system/com.steer.steer.control >/dev/null 2>&1 \
	|| /bin/launchctl print system/com.steer.steer.subscription >/dev/null 2>&1; do
	if [ "$remaining_checks" -le 0 ]; then
		printf '%s\n' 'Timed out waiting for previous Steer LaunchDaemons to stop.' >&2
		exit 1
	fi
	remaining_checks=$((remaining_checks - 1))
	/bin/sleep 0.1
done

/usr/bin/install -o root -g wheel -m 0755 "$helper_payload" "$helper_directory/steer-macos"
/usr/bin/install -o root -g wheel -m 0755 "$sing_box_payload" "$helper_directory/sing-box"
/usr/bin/install -o root -g wheel -m 0644 "$runtime_plist_payload" "$runtime_plist_path"
/usr/bin/install -o root -g wheel -m 0644 "$control_plist_payload" "$control_plist_path"
/usr/bin/install -o root -g wheel -m 0644 "$subscription_plist_payload" "$subscription_plist_path"

# install preserves download metadata. macOS 27 launchd rejects quarantined
# plists (error 155), even after the App and payload above have been verified.
# Clear only this attribute on the three installed service definitions.
for installed_plist in "$runtime_plist_path" "$control_plist_path" "$subscription_plist_path"; do
	attributes="$(/usr/bin/xattr "$installed_plist")"
	if printf '%s\n' "$attributes" | /usr/bin/grep -Fxq com.apple.quarantine; then
		/usr/bin/xattr -d com.apple.quarantine "$installed_plist"
	fi
done

if [ -f "$support_directory/config/config.json" ]; then
	/usr/sbin/chown root:admin "$support_directory/config/config.json"
	/bin/chmod 0640 "$support_directory/config/config.json"
else
	/usr/bin/install -o root -g admin -m 0640 "$config_payload" "$support_directory/config/config.json"
fi

geodata_stage="$(/usr/bin/mktemp -d "$support_directory/.geodata-stage.XXXXXX")"
geodata_backup="$support_directory/.geodata-backup"
cleanup_stage() {
	if [ -n "${geodata_stage:-}" ] && [ -d "$geodata_stage" ]; then
		/bin/rm -rf "$geodata_stage"
	fi
}
trap cleanup_stage EXIT HUP INT TERM
/bin/cp -R "$geodata_payload/." "$geodata_stage/"
"$helper_payload" verify-geodata --directory "$geodata_stage"
/usr/sbin/chown -R root:wheel "$geodata_stage"
/usr/bin/find "$geodata_stage" -type d -exec /bin/chmod 0755 {} \;
/usr/bin/find "$geodata_stage" -type f -exec /bin/chmod 0644 {} \;
/bin/rm -rf "$geodata_backup"
if [ -e "$geodata_directory" ]; then
	/bin/mv "$geodata_directory" "$geodata_backup"
fi
if ! /bin/mv "$geodata_stage" "$geodata_directory"; then
	if [ -d "$geodata_backup" ]; then
		/bin/mv "$geodata_backup" "$geodata_directory"
	fi
	exit 1
fi
geodata_stage=""
/bin/rm -rf "$geodata_backup"

/bin/launchctl bootstrap system "$control_plist_path"
/bin/launchctl bootstrap system "$subscription_plist_path"
/bin/launchctl bootstrap system "$runtime_plist_path"
/bin/launchctl print system/com.steer.steer.control >/dev/null
remaining_checks=50
while [ ! -S "$socket_directory/control.sock" ]; do
	if [ "$remaining_checks" -le 0 ]; then
		printf '%s\n' 'Timed out waiting for the Steer control socket.' >&2
		exit 1
	fi
	remaining_checks=$((remaining_checks - 1))
	/bin/sleep 0.1
done
printf '%s\n' 'Installed Steer system components. Future GUI Save/Apply operations use restricted passwordless control IPC.'
