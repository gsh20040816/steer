#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
set -eu

if [ "$(/usr/bin/uname -s)" != "Darwin" ]; then
	printf '%s\n' 'Steer embedded payload can only be uninstalled on macOS.' >&2
	exit 1
fi
if [ "$(/usr/bin/id -u)" -ne 0 ]; then
	printf '%s\n' 'Steer embedded payload uninstaller must run as root.' >&2
	exit 1
fi

remove_user_data=0
case "$#" in
	0) ;;
	1)
		[ "$1" = "--remove-user-data" ] || {
			printf 'Unsupported uninstaller argument: %s\n' "$1" >&2
			exit 2
		}
		remove_user_data=1
		;;
	*)
		printf '%s\n' 'Steer uninstaller accepts only --remove-user-data.' >&2
		exit 2
		;;
esac

script_dir="$(CDPATH= cd -- "$(/usr/bin/dirname -- "$0")" && /bin/pwd)"
resources_dir="$(CDPATH= cd -- "$script_dir/.." && /bin/pwd)"
app_bundle="$(CDPATH= cd -- "$resources_dir/../.." && /bin/pwd)"
payload_sums="$script_dir/PAYLOAD-SHA256SUMS"
[ -f "$payload_sums" ] && [ ! -L "$payload_sums" ] || {
	printf '%s\n' 'Steer payload checksums are unavailable.' >&2
	exit 1
}
(
	cd "$script_dir"
	/usr/bin/shasum -a 256 -c PAYLOAD-SHA256SUMS
)
/usr/bin/codesign --verify --deep --strict "$app_bundle"

helper_directory="/usr/local/libexec/steer"
runtime_plist_path="/Library/LaunchDaemons/com.steer.steer.plist"
control_plist_path="/Library/LaunchDaemons/com.steer.steer.control.plist"
subscription_plist_path="/Library/LaunchDaemons/com.steer.steer.subscription.plist"
support_directory="/Library/Application Support/Steer"
logs_directory="/Library/Logs/Steer"
socket_directory="/var/run/steer"

for directory in "$helper_directory" "$support_directory" "$logs_directory" "$socket_directory"; do
	if [ -L "$directory" ]; then
		printf 'Refusing to uninstall through a symbolic-link directory: %s\n' "$directory" >&2
		exit 1
	fi
done

for label in com.steer.steer com.steer.steer.control com.steer.steer.subscription; do
	/bin/launchctl bootout "system/$label" 2>/dev/null || true
done
remaining_checks=50
while /bin/launchctl print system/com.steer.steer >/dev/null 2>&1 \
	|| /bin/launchctl print system/com.steer.steer.control >/dev/null 2>&1 \
	|| /bin/launchctl print system/com.steer.steer.subscription >/dev/null 2>&1; do
	if [ "$remaining_checks" -le 0 ]; then
		printf '%s\n' 'Timed out waiting for Steer LaunchDaemons to stop.' >&2
		exit 1
	fi
	remaining_checks=$((remaining_checks - 1))
	/bin/sleep 0.1
done

# Recover a journal left by a killed runtime before deleting the helper/state.
if [ -x "$helper_directory/steer-macos" ]; then
	"$helper_directory/steer-macos" cleanup
fi

/bin/rm -f "$runtime_plist_path" "$control_plist_path" "$subscription_plist_path"
/bin/rm -rf "$helper_directory"
/bin/rm -f "$socket_directory/control.sock"
/bin/rmdir "$socket_directory" 2>/dev/null || true

if [ "$remove_user_data" -eq 1 ]; then
	/bin/rm -rf "$support_directory" "$logs_directory"
	printf '%s\n' 'Uninstalled Steer system components and removed user configuration, state, and logs.'
else
	/bin/rm -rf "$support_directory/run" "$support_directory/geodata-seed"
	printf '%s\n' 'Uninstalled Steer system components. Preserved configuration, state, and logs.'
fi
