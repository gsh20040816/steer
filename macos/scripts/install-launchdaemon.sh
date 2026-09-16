#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ]; then
	printf '%s\n' 'Steer macOS LaunchDaemon must be installed on macOS.' >&2
	exit 1
fi
if [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' 'Run this installer with sudo.' >&2
	exit 1
fi

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
helper_directory="/usr/local/libexec/steer"
runtime_binary="$helper_directory/sing-box"
plist_path="/Library/LaunchDaemons/com.steer.steer.plist"
control_plist_path="/Library/LaunchDaemons/com.steer.steer.control.plist"
subscription_plist_path="/Library/LaunchDaemons/com.steer.steer.subscription.plist"
support_directory="/Library/Application Support/Steer"
sing_box_path="$(command -v sing-box || true)"

[ -n "$sing_box_path" ] || {
	printf '%s\n' 'sing-box is required; install it with Homebrew first.' >&2
	exit 1
}

install -d -o root -g wheel -m 0755 "$helper_directory"
install -d -o root -g wheel -m 0755 "/var/run/steer"
install -d -o root -g admin -m 0750 "$support_directory" \
	"$support_directory/config" \
	"$support_directory/run" \
	"$support_directory/state" \
	"$support_directory/geodata-seed" \
	"/Library/Logs/Steer"

# The GUI runs as the signed-in administrator. It may read the canonical
# configuration and sanitized generation pointer without prompting, while
# save/apply operations still require administrator authorization.
if [ -f "$support_directory/config/config.json" ]; then
	chown root:admin "$support_directory/config/config.json"
	chmod 0640 "$support_directory/config/config.json"
fi
if [ -f "$support_directory/run/current.json" ]; then
	chown root:wheel "$support_directory/run/current.json"
	chmod 0644 "$support_directory/run/current.json"
fi

(cd "$repository_root/go" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$helper_directory/steer-macos" ./cmd/steer-macos)
if [ "$sing_box_path" != "$runtime_binary" ]; then
	install -m 0755 "$sing_box_path" "$runtime_binary"
fi
install -m 0644 "$repository_root/macos/launchd/com.steer.steer.plist" "$plist_path"
install -m 0644 "$repository_root/macos/launchd/com.steer.steer.control.plist" "$control_plist_path"
install -m 0644 "$repository_root/macos/launchd/com.steer.steer.subscription.plist" "$subscription_plist_path"
plutil -lint "$plist_path" "$control_plist_path" "$subscription_plist_path" >/dev/null
chown root:wheel "$helper_directory/steer-macos" "$runtime_binary" "$plist_path" "$control_plist_path" "$subscription_plist_path"
chmod 0755 "$helper_directory/steer-macos" "$runtime_binary"
chmod 0644 "$plist_path" "$control_plist_path" "$subscription_plist_path"

# Source archives may also carry download metadata. launchd on macOS 27
# rejects quarantined plists; leave executable and unrelated metadata intact.
for installed_plist in "$plist_path" "$control_plist_path" "$subscription_plist_path"; do
	attributes="$(/usr/bin/xattr "$installed_plist")"
	if printf '%s\n' "$attributes" | /usr/bin/grep -Fxq com.apple.quarantine; then
		/usr/bin/xattr -d com.apple.quarantine "$installed_plist"
	fi
done

launchctl bootout system/com.steer.steer 2>/dev/null || true
launchctl bootout system/com.steer.steer.control 2>/dev/null || true
launchctl bootout system/com.steer.steer.subscription 2>/dev/null || true
remaining_checks=50
while launchctl print system/com.steer.steer >/dev/null 2>&1 \
	|| launchctl print system/com.steer.steer.control >/dev/null 2>&1 \
	|| launchctl print system/com.steer.steer.subscription >/dev/null 2>&1; do
	if [ "$remaining_checks" -le 0 ]; then
		printf '%s\n' 'Timed out waiting for the previous Steer LaunchDaemons to stop.' >&2
		exit 1
	fi
	remaining_checks=$((remaining_checks - 1))
	sleep 0.1
done
launchctl bootstrap system "$control_plist_path"
launchctl bootstrap system "$subscription_plist_path"
launchctl bootstrap system "$plist_path"
printf '%s\n' "Installed Steer LaunchDaemons with passwordless admin-group control IPC and a root-owned copy of $sing_box_path"
