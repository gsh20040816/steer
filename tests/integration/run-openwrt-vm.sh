#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH='' cd -- "$SCRIPT_DIR/../.." && pwd)"
CONTROL_SOURCE="${STEER_BIN:-$REPO_DIR/bin/steer-openwrt-amd64}"
SING_BOX_BIN="${SING_BOX_BIN:-/usr/bin/sing-box}"
TEST_DIR="$(mktemp -d /tmp/steer-m1-integration.XXXXXX)"
ORIGINAL_CONFIG="$TEST_DIR/original-steer"

cleanup() {
	status="$?"
	trap - EXIT INT TERM
	/etc/init.d/steer stop >/dev/null 2>&1 || true
	uci -q delete firewall.steertest
	uci -q commit firewall
	ip link delete br-steer type bridge >/dev/null 2>&1 || true
	if [ -f "$ORIGINAL_CONFIG" ]; then
		cp "$ORIGINAL_CONFIG" /etc/config/steer
	fi
	rm -f /usr/sbin/steer
	rm -rf /run/steer "$TEST_DIR" /tmp/steer-m1-state
	exit "$status"
}
trap cleanup EXIT INT TERM

[ "$(ubus call system board | jsonfilter -e '@.release.target')" = 'x86/64' ] || {
	echo 'This integration test requires an OpenWrt x86/64 VM.' >&2
	exit 1
}
for executable in "$CONTROL_SOURCE" "$SING_BOX_BIN" /usr/sbin/nft; do
	[ -x "$executable" ] || { echo "Required executable is missing: $executable" >&2; exit 1; }
done
[ -s /usr/share/steer/geodata-seed/manifest.json ] || { echo 'Geo seed manifest is missing.' >&2; exit 1; }
find /usr/share/steer/geodata-seed/rules -type f -name '*.srs' -print -quit | grep -q . || {
	echo 'Geo SRS seed is empty.' >&2
	exit 1
}

[ ! -f /etc/config/steer ] || cp /etc/config/steer "$ORIGINAL_CONFIG"
/etc/init.d/steer stop >/dev/null 2>&1 || true
ln -sf "$CONTROL_SOURCE" /usr/sbin/steer
cp "$REPO_DIR/steer/files/etc/init.d/steer" /etc/init.d/steer
chmod 0755 /usr/sbin/steer /etc/init.d/steer

mkdir -p /usr/share/rpcd/ucode
ucode -c "$REPO_DIR/luci-app-steer/root/usr/share/rpcd/ucode/luci.steer" >/dev/null
cp "$REPO_DIR/luci-app-steer/root/usr/share/rpcd/ucode/luci.steer" /usr/share/rpcd/ucode/luci.steer
chmod 0755 /usr/share/rpcd/ucode/luci.steer
/etc/init.d/rpcd restart
for attempt in 1 2 3 4 5; do
	ubus -t 1 list luci.steer >/dev/null 2>&1 && break
	[ "$attempt" -lt 5 ] || { echo 'luci.steer RPC did not become ready.' >&2; exit 1; }
	sleep 1
done
ubus -v list luci.steer > "$TEST_DIR/rpc-methods.txt"
for method in commit_candidate geodata_catalog node_export node_import node_speedtest overview_probe probe_results route_speedtest status subscriptions validate; do
	grep -q "\"$method\"" "$TEST_DIR/rpc-methods.txt"
done
for removed in plan rollback; do
	if grep -q "\"$removed\"" "$TEST_DIR/rpc-methods.txt"; then
		echo "Removed RPC method is still public: $removed" >&2
		exit 1
	fi
done

ubus call luci.steer geodata_catalog > "$TEST_DIR/geodata-catalog.json"
[ "$(jsonfilter -q -i "$TEST_DIR/geodata-catalog.json" -e '@.geosite.ok')" = 'true' ]
[ "$(jsonfilter -q -i "$TEST_DIR/geodata-catalog.json" -e '@.geoip.ok')" = 'true' ]

# The public LuCI RPC must launch the shared parser on the target ucode, which
# only accepts a string command for fs.popen(). Documents remain RPC data and
# are written to the parser's stdin instead of being interpolated into shell.
. /usr/share/libubox/jshn.sh
node_import_rpc() {
	json_init
	json_add_string document "$1"
	ubus call luci.steer node_import "$(json_dump)"
}
single_node='vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example.com&type=ws&path=%2Fproxy#fixture'
node_import_rpc "$single_node" > "$TEST_DIR/node-import-single.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-single.json" -e '@.nodes[0].type')" = 'vless' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-single.json" -e '@.skipped')" = '0' ]

tuic_node='tuic://00000000-0000-4000-8000-000000000001@example.com:39823?congestion_control=bbr&alpn=h3%2Ch2&sni=example.com&udp_relay_mode=quic&allow_insecure=0#tuic-fixture'
node_import_rpc "$tuic_node" > "$TEST_DIR/node-import-tuic.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-tuic.json" -e '@.nodes[0].type')" = 'tuic' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-tuic.json" -e '@.nodes[0].uuid')" = '00000000-0000-4000-8000-000000000001' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-tuic.json" -e '@.nodes[0].alpn[0]')" = 'h3' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-tuic.json" -e '@.nodes[0].alpn[1]')" = 'h2' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-tuic.json" -e '@.skipped')" = '0' ]

multi_node="$single_node
not-a-node-link
ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@example.com:8388#fixture"
node_import_rpc "$multi_node" > "$TEST_DIR/node-import-multi.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-multi.json" -e '@.nodes[0].type')" = 'vless' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-multi.json" -e '@.nodes[1].type')" = 'shadowsocks' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-multi.json" -e '@.skipped')" = '1' ]

base64_node='dm1lc3M6Ly9leUoySWpvaU1pSXNJbkJ6SWpvaVptbDRkSFZ5WlNJc0ltRmtaQ0k2SW5adFpYTnpMbVY0WVcxd2JHVXVZMjl0SWl3aWNHOXlkQ0k2SWpRME15SXNJbWxrSWpvaU1EQXdNREF3TURBdE1EQXdNQzAwTURBd0xUZ3dNREF0TURBd01EQXdNREF3TURBeElpd2lZV2xrSWpvaU1DSXNJbk5qZVNJNkltRjFkRzhpTENKdVpYUWlPaUozY3lJc0luUnNjeUk2SW5Sc2N5SXNJbk51YVNJNkltVmtaMlV1WlhoaGJYQnNaUzVqYjIwaUxDSm9iM04wSWpvaVpXUm5aUzVsZUdGdGNHeGxMbU52YlNJc0luQmhkR2dpT2lJdmQzTWlMQ0owZVhCbElqb2libTl1WlNKOQ=='
node_import_rpc "$base64_node" > "$TEST_DIR/node-import-base64.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-base64.json" -e '@.nodes[0].type')" = 'vmess' ]
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-base64.json" -e '@.skipped')" = '0' ]

node_import_rpc 'not-a-node-link' > "$TEST_DIR/node-import-invalid.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-import-invalid.json" -e '@.error_code')" = 'IMPORT_PARSE_FAILED' ]

# Representative and detour fixtures remain valid through the public semantic
# validator. Native compilation is exercised by normal Apply below and by the
# shared Go compiler tests; no engineering compiler command is public.
/usr/sbin/steer validate --config "$REPO_DIR/tests/fixtures/m1-representative-valid/steer" > "$TEST_DIR/representative-validation.json"
/usr/sbin/steer validate --config "$REPO_DIR/tests/fixtures/detour-valid/steer" > "$TEST_DIR/detour-validation.json"

cp "$REPO_DIR/tests/fixtures/m1-openwrt-direct-valid/steer" /etc/config/steer
uci set steer.export_fixture='node'
uci set steer.export_fixture.enabled='1'
uci set steer.export_fixture.name='Export fixture'
uci set steer.export_fixture.type='vless'
uci set steer.export_fixture.server='proxy.example'
uci set steer.export_fixture.server_port='443'
uci set steer.export_fixture.uuid='00000000-0000-4000-8000-000000000001'
uci set steer.export_fixture.tls_server_name='edge.example'
uci commit steer
export_session="$(ubus call session login '{"username":"root","password":"","timeout":300}' | jsonfilter -e '@.ubus_rpc_session')"
ubus call luci.steer node_export "{\"node\":\"export_fixture\",\"ubus_rpc_session\":\"$export_session\"}" > "$TEST_DIR/node-export.json"
[ "$(jsonfilter -q -i "$TEST_DIR/node-export.json" -e '@.ok')" = 'true' ]
exported_node="$(jsonfilter -q -i "$TEST_DIR/node-export.json" -e '@.uri')"
case "$exported_node" in
	vless://*) ;;
	*) echo 'Node export did not return a VLESS share link.' >&2; exit 1 ;;
esac
ubus call session destroy "{\"ubus_rpc_session\":\"$export_session\"}"
uci delete steer.export_fixture
uci commit steer

# Package installation/boot establishes the procd service and its config
# trigger before LuCI can edit an already running configuration.
	/usr/sbin/steer apply > "$TEST_DIR/initial-apply.json"
	[ "$(jsonfilter -q -i "$TEST_DIR/initial-apply.json" -e '@.ok')" = 'true' ]
	grep -Fq '"dns_mode": "hijack"' /run/steer/current/sing-box.json
grep -Fq '"initial_path"' /run/steer/current/sing-box.json
grep -Fq '"type": "remote"' /run/steer/current/sing-box.json
[ -s /var/lib/steer/cache.db ]

# The LuCI transaction validates the authenticated session overlay before the
# standard rpcd UCI commit. An unknown Geo selector must remain pending, retain
# its object-level diagnostic and never reach /etc/config/steer.
invalid_geo_session="$(ubus call session login '{"username":"root","password":"","timeout":300}' | jsonfilter -e '@.ubus_rpc_session')"
ubus call uci set "{\"config\":\"steer\",\"section\":\"geo_cn\",\"values\":{\"domain_match\":[\"geosite:not-installed\"]},\"ubus_rpc_session\":\"$invalid_geo_session\"}"
ubus call luci.steer commit_candidate "{\"ubus_rpc_session\":\"$invalid_geo_session\"}" > "$TEST_DIR/invalid-geo-commit.json"
[ "$(jsonfilter -q -i "$TEST_DIR/invalid-geo-commit.json" -e '@.committed')" = 'false' ]
[ "$(jsonfilter -q -i "$TEST_DIR/invalid-geo-commit.json" -e '@.validation.errors[0].code')" = 'GEO_CATEGORY_NOT_FOUND' ]
[ "$(jsonfilter -q -i "$TEST_DIR/invalid-geo-commit.json" -e '@.validation.errors[0].object_id')" = 'geo_cn' ]
[ "$(jsonfilter -q -i "$TEST_DIR/invalid-geo-commit.json" -e '@.validation.errors[0].option')" = 'domain_match' ]
[ "$(uci get steer.geo_cn.domain_match)" = 'geosite:cn' ]
ubus call session destroy "{\"ubus_rpc_session\":\"$invalid_geo_session\"}"

# Reproduce the LuCI form lifecycle with a real authenticated UCI session.
# The disk value must remain old until the session-scoped commit completes,
# and the first Steer Apply after that commit must compile the new value.
luci_session="$(ubus call session login '{"username":"root","password":"","timeout":300}' | jsonfilter -e '@.ubus_rpc_session')"
ubus call uci set "{\"config\":\"steer\",\"section\":\"main\",\"values\":{\"log_level\":\"error\"},\"ubus_rpc_session\":\"$luci_session\"}"
[ "$(uci get steer.main.log_level)" = 'warn' ]
luci_apply_sequence_before="$(ubus call luci.steer status | jsonfilter -q -e '@.last_apply.sequence' || true)"
ubus call luci.steer commit_candidate "{\"ubus_rpc_session\":\"$luci_session\"}" > "$TEST_DIR/luci-commit.json"
[ "$(jsonfilter -q -i "$TEST_DIR/luci-commit.json" -e '@.committed')" = 'true' ]
[ "$(jsonfilter -q -i "$TEST_DIR/luci-commit.json" -e '@.validation.ok')" = 'true' ]
[ "$(uci get steer.main.log_level)" = 'error' ]
for wait_attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	sleep 1
	ubus call luci.steer status > "$TEST_DIR/apply-status.json"
	luci_apply_sequence="$(jsonfilter -q -i "$TEST_DIR/apply-status.json" -e '@.last_apply.sequence' || true)"
	[ -z "$luci_apply_sequence" ] || [ "$luci_apply_sequence" = "$luci_apply_sequence_before" ] || break
done
ubus call session destroy "{\"ubus_rpc_session\":\"$luci_session\"}"
[ -n "$luci_apply_sequence" ] && [ "$luci_apply_sequence" != "$luci_apply_sequence_before" ]
[ "$(jsonfilter -q -i "$TEST_DIR/apply-status.json" -e '@.last_apply.result.ok')" = 'true' ]
[ "$(jsonfilter -q -i /run/steer/current/sing-box.json -e '@.log.level')" = 'error' ]
sleep 3
ubus call luci.steer status > "$TEST_DIR/apply-stable-status.json"
[ "$(jsonfilter -q -i "$TEST_DIR/apply-stable-status.json" -e '@.last_apply.sequence')" = "$luci_apply_sequence" ]
/usr/sbin/steer health --timeout 10s
/usr/sbin/steer status > "$TEST_DIR/status.json"
[ "$(jsonfilter -q -i "$TEST_DIR/status.json" -e '@.healthy')" = 'true' ]
core_pid="$(ubus call service list '{"name":"steer"}' | jsonfilter -e '@.steer.instances["sing-box"].pid')"
grep -Eq "^[[:space:]]*100[[:space:]]+$core_pid[[:space:]]" /proc/net/netfilter/nfnetlink_queue

nft list table inet steer > "$TEST_DIR/steer.nft"
grep -q 'dnat ip to 127.0.0.1:1053' "$TEST_DIR/steer.nft"
grep -q 'dnat ip6 to \[::1\]:1053' "$TEST_DIR/steer.nft"
grep -q 'snat ip to 127.0.0.1' "$TEST_DIR/steer.nft"
grep -q 'snat ip6 to ::1' "$TEST_DIR/steer.nft"
mac_rule_count="$(grep -c '"source_mac_address"' /run/steer/current/sing-box.json)"
[ "$mac_rule_count" -ge 2 ]
grep -q '02:00:00:00:00:10' /run/steer/current/sing-box.json
if grep -Eq 'steer-mac-|"type": "tproxy"' /run/steer/current/sing-box.json; then
	echo 'Compiled configuration retained the pre-1.14 MAC inbound shim.' >&2
	exit 1
fi
if grep -Eq 'ether saddr|mac_tproxy|tproxy to :20000|0x2026' "$TEST_DIR/steer.nft"; then
	echo 'OpenWrt firewall retained the pre-1.14 MAC shim.' >&2
	exit 1
fi
if grep -Eq 'udp dport 443.*(drop|reject)' "$TEST_DIR/steer.nft"; then
	echo 'Steer blocks QUIC instead of routing it.' >&2
	exit 1
fi
if ip -4 rule show | grep -Eq '8999:.*lookup 2023' || ip -6 rule show | grep -Eq '8999:.*lookup 2023'; then
	echo 'OpenWrt retained the pre-1.14 MAC policy route.' >&2
	exit 1
fi
nslookup openwrt.org 1.1.1.1 > "$TEST_DIR/dns.txt"
grep -q 'Address' "$TEST_DIR/dns.txt"

# A second authenticated UCI commit must still route through the same
# transactional reload_service path without requiring a second Apply.
trigger_sequence_before="$(jsonfilter -q -i "$TEST_DIR/status.json" -e '@.last_apply.sequence')"
trigger_session="$(ubus call session login '{"username":"root","password":"","timeout":300}' | jsonfilter -e '@.ubus_rpc_session')"
ubus call uci set "{\"config\":\"steer\",\"section\":\"main\",\"values\":{\"log_level\":\"warn\"},\"ubus_rpc_session\":\"$trigger_session\"}"
ubus call luci.steer commit_candidate "{\"ubus_rpc_session\":\"$trigger_session\"}" > "$TEST_DIR/trigger-commit.json"
[ "$(jsonfilter -q -i "$TEST_DIR/trigger-commit.json" -e '@.committed')" = 'true' ]
trigger_sequence=''
for wait_attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	sleep 1
	/usr/sbin/steer status > "$TEST_DIR/trigger-status.json"
	trigger_sequence="$(jsonfilter -q -i "$TEST_DIR/trigger-status.json" -e '@.last_apply.sequence' || true)"
	[ "$trigger_sequence" = "$trigger_sequence_before" ] || break
done
ubus call session destroy "{\"ubus_rpc_session\":\"$trigger_session\"}"
[ -n "$trigger_sequence" ] && [ "$trigger_sequence" != "$trigger_sequence_before" ]
[ "$(jsonfilter -q -i "$TEST_DIR/trigger-status.json" -e '@.healthy')" = 'true' ]
[ "$(jsonfilter -q -i /run/steer/current/sing-box.json -e '@.log.level')" = 'warn' ]

# fw4 must not delete Steer's separately owned table. A service restart must
# rebuild the current UCI through the private init hook.
fw4 reload
nft list table inet steer >/dev/null
/etc/init.d/steer restart
/usr/sbin/steer health --timeout 10s
/etc/init.d/steer reload
/usr/sbin/steer health --timeout 10s

# Invalid schema and unknown legacy fields fail before the running generation
# is stopped or replaced.
uci set steer.main.router_proxy='1'
uci commit steer
if /usr/sbin/steer apply > "$TEST_DIR/rejected-schema.json"; then
	echo 'Legacy schema field was accepted.' >&2
	exit 1
fi
/usr/sbin/steer status > "$TEST_DIR/invalid-status.json"
[ "$(jsonfilter -q -i "$TEST_DIR/invalid-status.json" -e '@.healthy')" = 'true' ]
uci -q delete steer.main.router_proxy
uci commit steer

disabled_sequence_before="$(ubus call luci.steer status | jsonfilter -q -e '@.last_apply.sequence')"
uci set steer.main.enabled='0'
uci commit steer
for wait_attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	sleep 1
	ubus call luci.steer status > "$TEST_DIR/disabled-status.json"
	disabled_sequence="$(jsonfilter -q -i "$TEST_DIR/disabled-status.json" -e '@.last_apply.sequence' || true)"
	[ -z "$disabled_sequence" ] || [ "$disabled_sequence" = "$disabled_sequence_before" ] || break
done
[ -n "$disabled_sequence" ] && [ "$disabled_sequence" != "$disabled_sequence_before" ]
[ "$(jsonfilter -q -i "$TEST_DIR/disabled-status.json" -e '@.last_apply.result.ok')" = 'true' ]
if nft list table inet steer >/dev/null 2>&1; then
	echo 'Disabled Apply retained the Steer nftables table.' >&2
	exit 1
fi
[ ! -e /run/steer/current ]

# Disabling removes runtime resources but must not unregister the config
# trigger needed for the next LuCI commit to enable Steer again.
disabled_service="$(ubus call service list '{"name":"steer","verbose":true}')"
echo "$disabled_service" | grep -q '"steer"'
echo "$disabled_service" | grep -q '"triggers"'
if echo "$disabled_service" | grep -q '"running":true'; then
	echo 'Disabled Apply left the sing-box instance running.' >&2
	exit 1
fi
reenable_session="$(ubus call session login '{"username":"root","password":"","timeout":300}' | jsonfilter -e '@.ubus_rpc_session')"
ubus call uci set "{\"config\":\"steer\",\"section\":\"main\",\"values\":{\"enabled\":\"1\"},\"ubus_rpc_session\":\"$reenable_session\"}"
ubus call luci.steer commit_candidate "{\"ubus_rpc_session\":\"$reenable_session\"}" > "$TEST_DIR/reenable-commit.json"
[ "$(jsonfilter -q -i "$TEST_DIR/reenable-commit.json" -e '@.committed')" = 'true' ]
for wait_attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	sleep 1
	ubus call luci.steer status > "$TEST_DIR/reenabled-status.json"
	reenabled_sequence="$(jsonfilter -q -i "$TEST_DIR/reenabled-status.json" -e '@.last_apply.sequence' || true)"
	[ -z "$reenabled_sequence" ] || [ "$reenabled_sequence" = "$disabled_sequence" ] || break
done
ubus call session destroy "{\"ubus_rpc_session\":\"$reenable_session\"}"
[ "$(jsonfilter -q -i "$TEST_DIR/reenabled-status.json" -e '@.last_apply.result.ok')" = 'true' ]
[ "$(jsonfilter -q -i "$TEST_DIR/reenabled-status.json" -e '@.healthy')" = 'true' ]

echo 'OpenWrt cross-platform-preparation VM integration tests passed.'
