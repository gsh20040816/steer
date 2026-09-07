#!/bin/sh
set -eu

fail() {
	echo "linux-system: $*" >&2
	exit 1
}

[ "${STEER_LINUX_SYSTEM_TEST:-}" = "1" ] || fail "STEER_LINUX_SYSTEM_TEST=1 is required"
[ "$(id -u)" -eq 0 ] || fail "must run as root in a disposable container"
systemd-detect-virt --quiet --container || fail "must run inside a disposable systemd container"

source_root=${STEER_SOURCE_ROOT:-/workspace}
steer_binary=${STEER_BINARY:-/artifacts/steer}
sing_box_binary=${SING_BOX_BINARY:-/artifacts/sing-box}
geodata_seed=${STEER_GEODATA_SEED:-/artifacts/geodata-seed}
fixture_root="$source_root/tests/fixtures/linux-system"
run_root=$(mktemp -d /run/steer-linux-system.XXXXXX)

cleanup() {
	set +e
	systemctl stop steer.service
	for namespace in steer-client steer-upstream; do
		ip netns pids "$namespace" 2>/dev/null | xargs -r kill
		ip netns delete "$namespace" 2>/dev/null
	done
	rm -rf "$run_root"
}

on_exit() {
	status=$?
	trap - EXIT INT TERM
	if [ "$status" -ne 0 ]; then
		systemctl status steer.service --no-pager || true
		journalctl -u steer.service --no-pager -n 100 || true
		ip address show || true
		ip route show table main || true
		ip -6 route show table main || true
		ip rule show || true
		ip -6 rule show || true
		ip netns exec steer-upstream ip address show || true
		ip netns exec steer-upstream ip route show table main || true
		ip netns exec steer-upstream ip -6 route show table main || true
		ip netns exec steer-upstream ss -lnptu || true
		nft list ruleset || true
		for log in "$run_root"/*.log; do
			echo "== $log ==" >&2
			cat "$log" >&2
		done
	fi
	cleanup
	exit "$status"
}
trap on_exit EXIT
trap 'exit 130' INT TERM

[ -x "$steer_binary" ] || fail "Steer binary is not executable: $steer_binary"
[ -x "$sing_box_binary" ] || fail "sing-box binary is not executable: $sing_box_binary"
[ -f "$geodata_seed/manifest.json" ] || fail "Geo seed manifest is missing: $geodata_seed/manifest.json"
[ -d "$fixture_root" ] || fail "fixture directory is missing: $fixture_root"

install -Dm755 "$steer_binary" /usr/bin/steer
install -Dm755 "$sing_box_binary" /usr/bin/sing-box
install -d -m755 /usr/share/steer
cp -a "$geodata_seed" /usr/share/steer/geodata-seed
install -Dm644 "$source_root"/linux/systemd/steer.service /usr/lib/systemd/system/steer.service
install -d -m700 /etc/steer
install -Dm600 "$fixture_root/config-enabled.json" /etc/steer/config.json
install -Dm644 "$fixture_root/nftables.conf" /etc/nftables.conf

ip netns add steer-upstream
ip link add st-up type veth peer name up0
ip link set up0 netns steer-upstream
ip address add 192.168.77.1/24 dev st-up
ip -6 address add fd76::1/64 dev st-up nodad
ip link set st-up up
ip netns exec steer-upstream ip link set lo up
ip netns exec steer-upstream ip link set up0 up
ip netns exec steer-upstream ip address add 192.168.77.2/24 dev up0
ip netns exec steer-upstream ip -6 address add fd76::2/64 dev up0 nodad
ip netns exec steer-upstream ip address add 11.77.0.2/32 dev lo
ip netns exec steer-upstream ip -6 address add 2001:4860:77::2/128 dev lo nodad
ip route add 11.77.0.2/32 via 192.168.77.2 dev st-up
ip -6 route add 2001:4860:77::2/128 via fd76::2 dev st-up
ip route replace default via 192.168.77.2 dev st-up
ip -6 route replace default via fd76::2 dev st-up
ip netns exec steer-upstream ip route add default via 192.168.77.1
ip netns exec steer-upstream ip -6 route add default via fd76::1

ip netns add steer-client
ip link add st-client type veth peer name client0
ip link set client0 netns steer-client
ip netns exec steer-client ip link set dev client0 address 02:00:00:00:00:77
ip address add 10.77.0.1/24 dev st-client
ip -6 address add fd77::1/64 dev st-client nodad
ip link set st-client up
ip netns exec steer-client ip link set lo up
ip netns exec steer-client ip link set client0 up
ip netns exec steer-client ip address add 10.77.0.2/24 dev client0
ip netns exec steer-client ip -6 address add fd77::2/64 dev client0 nodad
ip netns exec steer-client ip route add default via 10.77.0.1
ip netns exec steer-client ip -6 route add default via fd77::1
sysctl -qw net.ipv4.ip_forward=1
sysctl -qw net.ipv6.conf.all.forwarding=1

ip netns exec steer-upstream python3 -c 'import socket, sys
family = socket.AF_INET6 if sys.argv[1] == "6" else socket.AF_INET
s = socket.socket(family, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((sys.argv[2], 18080))
s.listen()
while True:
    connection, _ = s.accept()
    connection.recv(65535)
    connection.sendall(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
    connection.close()' 4 11.77.0.2 >"$run_root/http4.log" 2>&1 &
ip netns exec steer-upstream python3 -c 'import socket, sys
family = socket.AF_INET6 if sys.argv[1] == "6" else socket.AF_INET
s = socket.socket(family, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((sys.argv[2], 18080))
s.listen()
while True:
    connection, _ = s.accept()
    connection.recv(65535)
    connection.sendall(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
    connection.close()' 6 2001:4860:77::2 >"$run_root/http6.log" 2>&1 &
ip netns exec steer-upstream python3 -c 'import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("11.77.0.2", 19090))
while True:
    data, peer = s.recvfrom(65535)
    s.sendto(data, peer)' >"$run_root/udp4.log" 2>&1 &
ip netns exec steer-upstream python3 -c 'import socket
s = socket.socket(socket.AF_INET6, socket.SOCK_DGRAM)
s.bind(("2001:4860:77::2", 19090))
while True:
    data, peer = s.recvfrom(65535)
    s.sendto(data, peer)' >"$run_root/udp6.log" 2>&1 &
ip netns exec steer-upstream dnsmasq \
	--keep-in-foreground \
	--no-hosts \
	--no-resolv \
	--bind-interfaces \
	--interface=lo \
	--listen-address=11.77.0.2 \
	--listen-address=2001:4860:77::2 \
	--address=/steer.test/11.77.0.2 \
	--address=/steer6.test/2001:4860:77::2 \
	--log-facility=- >"$run_root/dns.log" 2>&1 &
sleep 1

# Prove the isolated upstream and forwarding topology before Steer changes the
# data plane. The same requests are repeated after activation below.
curl --fail --silent --show-error --max-time 5 http://11.77.0.2:18080/ >/dev/null
curl --fail --silent --show-error --max-time 5 'http://[2001:4860:77::2]:18080/' >/dev/null
ip netns exec steer-client curl --fail --silent --show-error --max-time 5 http://11.77.0.2:18080/ >/dev/null
ip netns exec steer-client curl --fail --silent --show-error --max-time 5 'http://[2001:4860:77::2]:18080/' >/dev/null

systemctl daemon-reload
systemctl restart nftables.service
steer geo-catalog --kind geosite | jq -e '.names | index("cn") != null' >/dev/null
systemctl start steer.service

wait_healthy() {
	attempt=0
	until steer health >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		if [ "$attempt" -ge 20 ]; then
			systemctl status steer.service --no-pager || true
			journalctl -u steer.service --no-pager -n 100 || true
			fail "Steer did not become healthy"
		fi
		sleep 1
	done
}

expect_udp() {
	namespace=$1
	family=$2
	address=$3
	if [ "$namespace" = host ]; then
		prefix=""
	else
		prefix="ip netns exec $namespace"
	fi
	$prefix python3 -c 'import socket, sys
family = socket.AF_INET6 if sys.argv[1] == "6" else socket.AF_INET
s = socket.socket(family, socket.SOCK_DGRAM)
s.settimeout(5)
s.sendto(b"steer-udp", (sys.argv[2], 19090))
data, _ = s.recvfrom(64)
raise SystemExit(0 if data == b"steer-udp" else 1)' "$family" "$address"
}

expect_dns() {
	namespace=$1
	transport=$2
	server=$3
	name=$4
	expected=$5
	if [ "$namespace" = host ]; then
		prefix=""
	else
		prefix="ip netns exec $namespace"
	fi
	result=$($prefix dig $transport +short +time=3 +tries=1 "@$server" "$name" A)
	[ "$result" = "$expected" ] || fail "$namespace DNS $transport via $server returned $result"
}

chain_packets() {
	nft -j list chain inet steer "$1" \
		| jq '[.nftables[].rule.expr[]?.counter.packets?] | add // 0'
}

wait_healthy
ip link show dev steer0 >/dev/null
nft list table inet steer >/dev/null
grep -Fq '"dns_mode": "hijack"' /run/steer/current/sing-box.json || fail "TUN native DNS ownership was not enabled"
grep -Fq '"initial_path"' /run/steer/current/sing-box.json || fail "compiled Geo rule-set has no initial_path"
grep -Fq '"type": "remote"' /run/steer/current/sing-box.json || fail "compiled Geo rule-set is not remote"
mac_rule_count=$(grep -c '"source_mac_address"' /run/steer/current/sing-box.json)
[ "$mac_rule_count" -ge 2 ] || fail "native source MAC rule was not projected to route and DNS"
grep -Fq '02:00:00:00:00:77' /run/steer/current/sing-box.json || fail "native source MAC address was not preserved"
[ -s /var/lib/steer/cache.db ] || fail "sing-box cache database was not created"

systemctl is-active --quiet systemd-resolved || fail "systemd-resolved is not active"
resolvectl dns steer0 | grep -Fq '198.18.0.2' || fail "native IPv4 DNS was not registered"
resolvectl dns steer0 | grep -Fq 'fdfe:dcba:9876::2' || fail "native IPv6 DNS was not registered"

curl --fail --silent --show-error --max-time 5 http://11.77.0.2:18080/ >/dev/null
curl --fail --silent --show-error --max-time 5 'http://[2001:4860:77::2]:18080/' >/dev/null
ip netns exec steer-client curl --fail --silent --show-error --max-time 5 http://11.77.0.2:18080/ >/dev/null
ip netns exec steer-client curl --fail --silent --show-error --max-time 5 'http://[2001:4860:77::2]:18080/' >/dev/null
expect_udp host 4 11.77.0.2
expect_udp host 6 2001:4860:77::2
expect_udp steer-client 4 11.77.0.2
expect_udp steer-client 6 2001:4860:77::2

expect_dns host "" 11.77.0.99 steer.test 11.77.0.2
expect_dns host +tcp 11.77.0.99 steer.test 11.77.0.2
expect_dns host "" 2001:4860:77::99 steer.test 11.77.0.2
expect_dns host +tcp 2001:4860:77::99 steer.test 11.77.0.2
expect_dns steer-client "" 11.77.0.99 steer.test 11.77.0.2
expect_dns steer-client +tcp 11.77.0.99 steer.test 11.77.0.2
expect_dns steer-client "" 2001:4860:77::99 steer.test 11.77.0.2
expect_dns steer-client +tcp 2001:4860:77::99 steer.test 11.77.0.2

# Non-local DNS above must use native interception. The compatibility shim is
# only for packets addressed to the host itself.
local_dns_before=$(chain_packets dns_prerouting)
expect_dns steer-client "" 10.77.0.1 steer.test 11.77.0.2
expect_dns steer-client +tcp 10.77.0.1 steer.test 11.77.0.2
local_dns_after=$(chain_packets dns_prerouting)
[ "$local_dns_after" -gt "$local_dns_before" ] || fail "local-destination DNS missed the compatibility shim"

local_output_before=$(chain_packets dns_output)
for local_dns in 127.0.0.1 127.0.0.53 ::1 10.77.0.1; do
    expect_dns host "" "$local_dns" steer.test 11.77.0.2
    expect_dns host +tcp "$local_dns" steer.test 11.77.0.2
done
local_output_after=$(chain_packets dns_output)
[ "$local_output_after" -gt "$local_output_before" ] || fail "host-local DNS missed OUTPUT interception"

python3 -c 'import socket
s = socket.socket()
s.settimeout(2)
try:
    s.connect(("127.0.0.1", 1053))
except OSError:
    raise SystemExit(0)
raise SystemExit("direct access to 127.0.0.1:1053 was accepted")'
ip netns exec steer-client python3 -c 'import socket
s = socket.socket()
s.settimeout(2)
try:
    s.connect(("10.77.0.1", 1053))
except OSError:
    raise SystemExit(0)
raise SystemExit("direct access to host:1053 was accepted")'

systemctl restart steer.service
wait_healthy

before_pid=$(systemctl show steer.service --property=MainPID --value)
systemctl restart nftables.service
wait_healthy
after_pid=$(systemctl show steer.service --property=MainPID --value)
[ "$after_pid" -gt 0 ] || fail "Steer has no main process after nftables restart"
[ "$before_pid" != "$after_pid" ] || fail "nftables restart did not restart Steer"
nft list table inet steer >/dev/null

install -Dm600 "$fixture_root/config-disabled.json" /etc/steer/config.json
steer apply
if systemctl is-active --quiet steer.service; then
	fail "disabled configuration left steer.service active"
fi
if nft list table inet steer >/dev/null 2>&1; then
	fail "disabled configuration left the Steer nftables table"
fi

install -Dm600 "$fixture_root/config-enabled.json" /etc/steer/config.json
steer apply
wait_healthy
expect_dns steer-client "" 11.77.0.99 steer.test 11.77.0.2

echo "Linux system integration tests passed."
