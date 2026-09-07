#!/usr/bin/env python3
"""Validate generated DNS artifacts in an isolated Linux network+mount namespace.

Run as root, or inside `unshare -Ur` for an unprivileged desktop test.
Arguments: generated sing-box.json and firewall.nft. No host network mutation.
"""
import ctypes
import os
import pathlib
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time


def run(*args):
    try:
        return subprocess.check_output(args, stderr=subprocess.STDOUT, text=True)
    except subprocess.CalledProcessError as exc:
        print(exc.output, file=sys.stderr)
        raise


def main():
    config, firewall = map(os.path.abspath, sys.argv[1:3])
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(0x40000000 | 0x00020000):  # CLONE_NEWNET | CLONE_NEWNS
        raise OSError(ctypes.get_errno(), "cannot isolate network/mount namespaces")
    run("mount", "--make-rprivate", "/")
    # Do not let sing-box contact host systemd-resolved over the host D-Bus.
    resolver_path = pathlib.Path(os.path.realpath("/etc/resolv.conf"))
    run("mount", "-t", "tmpfs", "tmpfs", "/run")
    if str(resolver_path).startswith("/run/"):
        resolver_path.parent.mkdir(parents=True, exist_ok=True)
        resolver_path.write_text("nameserver 127.0.0.1\n")
    # Host resolv.conf may contain IPv6 interface scopes absent in this netns.
    pathlib.Path("/run/test-resolv.conf").write_text("nameserver 127.0.0.1\n")
    run("mount", "--bind", "/run/test-resolv.conf", str(resolver_path))
    os.environ["DBUS_SYSTEM_BUS_ADDRESS"] = "unix:path=/run/no-host-bus"
    run("sysctl", "-w", "net.ipv4.ip_forward=1", "net.ipv6.conf.all.forwarding=1")
    run("ip", "link", "set", "lo", "up")
    run("ip", "netns", "add", "dns-client")
    run("ip", "link", "add", "dns-host", "type", "veth", "peer", "name", "dns-peer")
    run("ip", "link", "set", "dns-peer", "netns", "dns-client")
    for namespace, device, suffix in [(None, "dns-host", "1"), ("dns-client", "dns-peer", "2")]:
        prefix = ["ip"] if namespace is None else ["ip", "-n", namespace]
        run(*prefix, "link", "set", "lo", "up")
        run(*prefix, "link", "set", device, "up")
        run(*prefix, "addr", "add", "10.77.0." + suffix + "/24", "dev", device)
        run(*prefix, "-6", "addr", "add", "fd77::" + suffix + "/64", "dev", device, "nodad")
    run("ip", "route", "add", "default", "via", "10.77.0.2")
    run("ip", "-6", "route", "add", "default", "via", "fd77::2")
    run("ip", "-n", "dns-client", "route", "add", "default", "via", "10.77.0.1")
    run("ip", "-n", "dns-client", "-6", "route", "add", "default", "via", "fd77::1")
    run("sing-box", "check", "-c", config)
    run("nft", "-c", "-f", firewall)
    run("nft", "-f", firewall)
    echo = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    echo.bind(("127.0.0.1", 19000))

    def echo_loop():
        while True:
            data, address = echo.recvfrom(4096)
            echo.sendto(data, address)

    threading.Thread(target=echo_loop, daemon=True).start()
    with tempfile.TemporaryFile(mode="w+") as log:
        core = subprocess.Popen(["sing-box", "run", "-c", config], stdout=log, stderr=log)
        try:
            time.sleep(2)
            if core.poll() is not None:
                raise RuntimeError("core exited on startup")
            for forwarded in [False, True]:
                prefix = ["ip", "netns", "exec", "dns-client"] if forwarded else []
                # Nonexistent remote DNS: replies can only come from hijacking.
                for server in ["11.77.0.99", "2001:4860:77::99"]:
                    for transport in ["+notcp", "+tcp"]:
                        answer = run(*prefix, "dig", "+short", "+time=2", "+tries=1", transport, "@" + server, "steer.test", "A").strip()
                        assert answer == "203.0.113.7", (forwarded, server, transport, answer)
            # Local destination exception: LAN query to the host/router itself.
            for server in ["10.77.0.1", "fd77::1"]:
                for transport in ["+notcp", "+tcp"]:
                    answer = run("ip", "netns", "exec", "dns-client", "dig", "+short", "+time=2", "+tries=1", transport, "@" + server, "steer.test", "A").strip()
                    assert answer == "203.0.113.7", ("local", server, transport, answer)
            # Host-local DNS must be captured even without a local resolver.
            for server in ["127.0.0.1", "127.0.0.53", "::1", "10.77.0.1", "fd77::1"]:
                for transport in ["+notcp", "+tcp"]:
                    answer = run("dig", "+short", "+time=2", "+tries=1", transport, "@" + server, "steer.test", "A").strip()
                    assert answer == "203.0.113.7", ("host-local", server, transport, answer)
            # Reuse exactly the same UDP socket/port for DNS and non-DNS traffic.
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
                client.settimeout(3)
                query = struct.pack("!6H", 1234, 256, 1, 0, 0, 0) + b"\x05steer\x04test\0\0\x01\0\x01"
                client.sendto(query, ("11.77.0.99", 53))
                assert client.recvfrom(4096)[0][:2] == query[:2]
                stun = b"\x00\x01\x00\x00\x21\x12\xa4\x42" + b"0123456789ab"
                client.sendto(stun, ("11.77.0.99", 19000))
                assert client.recvfrom(4096)[0] == stun, "source-port reuse misrouted STUN"
            # Relay topology: the LAN has only a link-local IPv6 address,
            # while the router's non-link-local address belongs to the WAN.
            # A wildcard UDP redirect listener may choose that WAN source
            # on replies and miss reverse NAT. Exercise real client queries.
            run("ip", "link", "add", "dns-wan", "type", "veth", "peer", "name", "dns-uplink")
            run("ip", "link", "set", "dns-wan", "up")
            run("ip", "link", "set", "dns-uplink", "up")
            run("ip", "-6", "addr", "add", "fe80::77:1/64", "dev", "dns-host", "nodad")
            run("ip", "-6", "addr", "del", "fd77::1/64", "dev", "dns-host")
            run("ip", "-6", "addr", "add", "fd77::1/64", "dev", "dns-wan", "nodad")
            run("ip", "-6", "route", "replace", "fd77::2/128", "dev", "dns-host")
            run("ip", "-n", "dns-client", "-6", "route", "replace", "fd77::1/128", "via", "fe80::77:1", "dev", "dns-peer")
            run("ip", "-n", "dns-client", "-6", "route", "replace", "default", "via", "fe80::77:1", "dev", "dns-peer")
            for server in ["fdfe:dcba:9876::1", "fd77::1", "fe80::77:1%dns-peer"]:
                for transport in ["+notcp", "+tcp"]:
                    answer = run("ip", "netns", "exec", "dns-client", "dig", "+short", "+time=2", "+tries=1", transport, "@" + server, "steer.test", "A").strip()
                    assert answer == "203.0.113.7", ("relay", server, transport, answer)
            print("PASS: host/forwarded IPv4+IPv6 UDP+TCP DNS, host-local+LAN-local DNS shim, DNS→STUN source-port reuse, IPv6 relay DNS")
        except BaseException:
            log.seek(0)
            print(log.read(), file=sys.stderr)
            raise
        finally:
            core.terminate()
            try:
                core.wait(timeout=5)
            except subprocess.TimeoutExpired:
                core.kill()
                core.wait()
            run("ip", "netns", "delete", "dns-client")


if __name__ == "__main__":
    main()
