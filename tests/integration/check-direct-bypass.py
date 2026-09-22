#!/usr/bin/env python3
"""Check compiled IPv6 TCP bypass in isolated Linux network/mount namespaces.

Usage: [unshare -Ur] python3 check-direct-bypass.py fixtures platform
Only the private router/client/server topology is changed. No Internet needed.
"""
import ctypes
import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import time


def run(*args):
    try:
        return subprocess.check_output(args, stderr=subprocess.STDOUT, text=True).strip()
    except subprocess.CalledProcessError as exc:
        print(exc.output, file=sys.stderr)
        raise


SERVER = r'''
import socket, threading, time
def exact(c,n):
 b=b''
 while len(b)<n:
  x=c.recv(n-len(b))
  if not x: raise EOFError()
  b+=x
 return b
def handle(c, address, proxy):
 try:
  c.settimeout(5)
  if proxy:
   version,n=exact(c,2); exact(c,n); c.sendall(b'\x05\x00')
   version,command,reserved,kind=exact(c,4)
   exact(c,4 if kind==1 else 16 if kind==4 else exact(c,1)[0]); exact(c,2)
   c.sendall(b'\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x01')
  c.recv(4096)
  body='proxy' if proxy else address[0]
  c.sendall(('HTTP/1.1 200 OK\r\nContent-Length: '+str(len(body))+'\r\nConnection: close\r\n\r\n'+body).encode())
 except (OSError,EOFError): pass
 finally: c.close()
def serve(port):
 s=socket.socket(socket.AF_INET6);s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 s.bind(('::',port));s.listen()
 while True:
  c,a=s.accept(); threading.Thread(target=handle,args=(c,a,port==19080),daemon=True).start()
for port in [18080,18081,18083,18084,18085,19080]:
 threading.Thread(target=serve,args=(port,),daemon=True).start()
print('ready',flush=True)
while True: time.sleep(60)
'''

CLIENT = r'''
import socket,sys
address,port,host=sys.argv[1:4]
try:
 with socket.create_connection((address,int(port)),timeout=4) as c:
  c.sendall(('GET / HTTP/1.1\r\nHost: '+host+'\r\nConnection: close\r\n\r\n').encode())
  b=b''
  while True:
   data=c.recv(4096)
   if not data:break
   b+=data
  print(b.decode().split('\r\n\r\n')[-1] or 'rejected')
except OSError:print('rejected')
'''


def main():
    fixtures, platform = pathlib.Path(sys.argv[1]).resolve(), sys.argv[2]
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(0x40000000 | 0x00020000):
        raise OSError(ctypes.get_errno(), "cannot isolate network/mount namespaces")
    run("mount", "--make-rprivate", "/")
    resolver = pathlib.Path(os.path.realpath("/etc/resolv.conf"))
    run("mount", "-t", "tmpfs", "tmpfs", "/run")
    if str(resolver).startswith("/run/"):
        resolver.parent.mkdir(parents=True, exist_ok=True)
        resolver.touch()
    pathlib.Path("/run/bypass-resolv.conf").write_text("nameserver 127.0.0.1\n")
    run("mount", "--bind", "/run/bypass-resolv.conf", str(resolver))
    pathlib.Path("/run/steer-bypass").mkdir()
    os.environ["DBUS_SYSTEM_BUS_ADDRESS"] = "unix:path=/run/no-host-bus"
    run("sysctl", "-w", "net.ipv4.ip_forward=1", "net.ipv6.conf.all.forwarding=1")
    run("ip", "link", "set", "lo", "up")
    for side, segment in [("client", "1"), ("server", "2")]:
        namespace, device, peer = "b-" + side, "b-" + side + "-host", "b-" + side + "-peer"
        run("ip", "netns", "add", namespace)
        run("ip", "link", "add", device, "type", "veth", "peer", "name", peer)
        run("ip", "link", "set", peer, "netns", namespace)
        if side == "client":
            run("ip", "-n", namespace, "link", "set", peer, "address", "02:00:00:00:00:10")
        for prefix, interface, last in [(["ip"], device, "1"), (["ip", "-n", namespace], peer, "2")]:
            run(*prefix, "link", "set", "lo", "up")
            run(*prefix, "link", "set", interface, "up")
            run(*prefix, "-6", "addr", "add", "2001:4860:77:" + segment + "::" + last + "/64", "dev", interface, "nodad")
        run("ip", "-n", namespace, "-6", "route", "add", "default", "via", "2001:4860:77:" + segment + "::1")
    run("ip", "-6", "route", "add", "default", "via", "2001:4860:77:2::2")
    run("ip", "-6", "route", "add", "2001:4860:77:3::/64", "via", "2001:4860:77:2::2")
    for last in ["10", "11", "12", "13", "14", "20"]:
        run("ip", "-n", "b-server", "-6", "addr", "add", "2001:4860:77:3::" + last + "/128", "dev", "b-server-peer", "nodad")
    server = subprocess.Popen(["ip", "netns", "exec", "b-server", "python3", "-u", "-c", SERVER], stdout=subprocess.PIPE, text=True)
    assert server.stdout.readline().strip() == "ready"
    client_address, router_address = "2001:4860:77:1::2", "2001:4860:77:2::1"

    def request(last, port, host):
        return run("ip", "netns", "exec", "b-client", "python3", "-c", CLIENT, "2001:4860:77:3::" + last, str(port), host)

    try:
        for mode in ["off", "static", "dns"]:
            config = str(fixtures / (platform + "-" + mode + ".json"))
            run("sing-box", "check", "-c", config)
            with tempfile.TemporaryFile(mode="w+") as log:
                core = subprocess.Popen(["sing-box", "run", "-c", config], stdout=log, stderr=log)
                try:
                    for _ in range(50):
                        if core.poll() is not None:
                            raise RuntimeError("core exited on startup")
                        try:
                            with socket.create_connection(("::1", 19090), timeout=.1):
                                break
                        except OSError:
                            time.sleep(.1)
                    else:
                        raise RuntimeError("core listener did not start")

                    # Warm neighbors using a real client flow. A later MAC rule
                    # must work before sniff even without a DNS mapping.
                    request("20", 18081, "unmapped.test")
                    expected = client_address if mode != "off" else router_address
                    assert request("20", 18081, "unmapped.test") == expected, "management IP/port direct"
                    assert request("20", 18085, "unmapped.test") == expected, "source MAC direct"
                    # A missing reverse map must not turn an earlier domain
                    # proxy/reject into FALSE and bypass the later IP rule.
                    assert request("20", 18080, "unmapped.proxy.test") == "proxy", "missing-domain proxy barrier"
                    assert request("20", 18080, "unmapped.reject.test") == "rejected", "missing-domain reject barrier"
                    assert request("20", 18080, "unmapped.direct.test") == router_address, "sniff fallback remains L4 direct"

                    for host, last in [("mapped.direct.test", "10"), ("mapped.proxy.test", "11"), ("protocol.direct.test", "12"), ("mapped.other.test", "13")]:
                        answer = run("ip", "netns", "exec", "b-client", "dig", "+short", "+time=2", "+tries=1", "@2001:4860:77:9::99", host, "AAAA")
                        assert answer == "2001:4860:77:3::" + last, (host, answer)
                    expected = client_address if mode == "dns" else router_address
                    assert request("10", 18080, "mapped.direct.test") == expected, "mapped domain direct"
                    assert request("11", 18080, "mapped.proxy.test") == "proxy", "earlier mapped proxy"
                    assert request("12", 18083, "protocol.direct.test") == "proxy", "protocol barrier"
                    assert request("10", 18084, "mapped.direct.test") == expected, "UDP-only barrier cannot block TCP"
                    assert request("13", 18080, "mapped.other.test") == expected, "mapped domain excludes proxies for IP direct"

                    # An explicit HTTP proxy connection must never kernel-bypass.
                    explicit = CLIENT.replace("GET / HTTP/1.1", "GET http://[2001:4860:77:3::10]:18080/ HTTP/1.1")
                    result = run("ip", "netns", "exec", "b-client", "python3", "-c", explicit, "2001:4860:77:1::1", "19090", "mapped.direct.test")
                    assert result == router_address, "explicit proxy remains L4 direct"
                    print("PASS:", platform, mode, "source IPv6, MAC, DNS mapping, proxy/reject/protocol barriers, L4 fallback and HTTP proxy")
                except BaseException:
                    log.seek(0)
                    print(log.read(), file=sys.stderr)
                    raise
                finally:
                    core.terminate()
                    try: core.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        core.kill(); core.wait()
    finally:
        server.terminate()
        server.wait(timeout=5)


if __name__ == "__main__":
    main()
