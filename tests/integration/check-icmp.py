#!/usr/bin/env python3
"""Exercise real ICMP through compiled fixtures in disposable network namespaces.

Requires root/CAP_SYS_ADMIN, iproute2, nftables, Python and sing-box. The script
unshares network and mount state before making changes; no host routes change.
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


PING = r'''
import socket, struct, sys, time
address, source, hops = sys.argv[1:]
fam = socket.AF_INET6 if ':' in address else socket.AF_INET
s = socket.socket(fam, socket.SOCK_RAW, socket.IPPROTO_ICMPV6 if fam == socket.AF_INET6 else socket.IPPROTO_ICMP)
s.settimeout(1.5)
s.setsockopt(socket.IPPROTO_IPV6 if fam == socket.AF_INET6 else socket.IPPROTO_IP,
             socket.IPV6_UNICAST_HOPS if fam == socket.AF_INET6 else socket.IP_TTL, int(hops))
if source: s.bind((source, 0))
p = struct.pack('!BBHHH', 128 if fam == socket.AF_INET6 else 8, 0, 0, 17329, 1) + b'steer-real-icmp-test!!'
if fam == socket.AF_INET:
 total=sum(struct.unpack('!%dH' % (len(p)//2),p)); total=(total>>16)+(total&65535); total+=(total>>16)
 p=p[:2]+struct.pack('!H',~total&65535)+p[4:]
s.sendto(p,(address,0))
end=time.monotonic()+1.5
while time.monotonic()<end:
 try: data,peer=s.recvfrom(65535)
 except socket.timeout: break
 if fam==socket.AF_INET: data=data[(data[0]&15)*4:]
 if peer[0]==address and data[0]==(129 if fam==socket.AF_INET6 else 0) and data[4:8]==p[4:8] and data[8:]==p[8:]:
  print('echo');sys.exit(0)
 if data[0]==(3 if fam==socket.AF_INET6 else 11) and p[4:8] in data:
  print('time-exceeded');sys.exit(0)
print('no-echo')
'''


def main():
    fixtures = pathlib.Path(sys.argv[1]).resolve()
    platforms = sys.argv[2:] or ['linux', 'openwrt', 'macos']
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(0x40000000 | 0x00020000):
        raise OSError(ctypes.get_errno(), 'cannot isolate network/mount namespaces')
    run('mount', '--make-rprivate', '/')
    run('mount', '-t', 'tmpfs', 'tmpfs', '/run')
    pathlib.Path('/run/steer-icmp').mkdir()
    os.environ['DBUS_SYSTEM_BUS_ADDRESS'] = 'unix:path=/run/no-host-bus'
    run('ip', 'link', 'set', 'lo', 'up')
    run('sysctl', '-w', 'net.ipv4.ip_forward=1', 'net.ipv6.conf.all.forwarding=1')
    for side, v4, v6 in [('client','10.77.0','2001:4860:77:1'),('server','192.168.77','2001:4860:77:2')]:
        ns,dev,peer='i-'+side,'i-'+side+'-host','i-'+side+'-peer'
        run('ip','netns','add',ns)
        run('ip','link','add',dev,'type','veth','peer','name',peer)
        run('ip','link','set',peer,'netns',ns)
        for prefix, interface, last in [(['ip'],dev,'1'),(['ip','-n',ns],peer,'2')]:
            run(*prefix,'link','set','lo','up')
            run(*prefix,'link','set',interface,'up')
            run(*prefix,'addr','add',v4+'.'+last+'/24','dev',interface)
            run(*prefix,'-6','addr','add',v6+'::'+last+'/64','dev',interface,'nodad')
        run('ip','-n',ns,'route','add','default','via',v4+'.1')
        run('ip','-n',ns,'-6','route','add','default','via',v6+'::1')
    targets=['11.77.0.2','2001:4860:77:3::2']
    run('ip','-n','i-server','addr','add',targets[0]+'/32','dev','lo')
    run('ip','-n','i-server','-6','addr','add',targets[1]+'/128','dev','lo','nodad')
    run('ip','route','add','default','via','192.168.77.2')
    # Source-specific IPv6 defaults reproduce unbound OpenWrt ping selecting
    # the TUN ULA source through the fallback table before OUTPUT pre-match.
    for prefix in ['2001:4860:77:1::/64','2001:4860:77:2::/64']:
        run('ip','-6','route','add','default','from',prefix,'via','2001:4860:77:2::2')

    def ping(ns, address, source='', hops=64):
        prefix=['ip','netns','exec',ns] if ns else []
        return run(*prefix,'python3','-c',PING,address,source,str(hops))

    for address in targets:
        assert ping('',address)=='echo', ('baseline host',address)
        assert ping('i-client',address)=='echo', ('baseline client',address)
    for platform in platforms:
        for variant in ['plain','wireguard']:
            config=str(fixtures/(platform+'-'+variant+'.json'))
            run('sing-box','check','-c',config)
            with tempfile.TemporaryFile(mode='w+') as log:
                core=subprocess.Popen(['sing-box','run','-c',config],stdout=log,stderr=log)
                try:
                    for _ in range(50):
                        if core.poll() is not None: raise RuntimeError('core exited')
                        try:
                            with socket.create_connection(('127.0.0.1',19090),timeout=.1): break
                        except OSError: time.sleep(.1)
                    else: raise RuntimeError('core not ready')
                    for ns in ([''] if platform == 'macos' else ['', 'i-client']):
                        for address in targets:
                            assert ping(ns,address)=='echo', (platform,variant,ns,address,'no real echo')
                    if platform != 'macos':
                        for address,source in zip(targets,['198.18.0.1','fdfe:dcba:9876::1']):
                            assert ping('',address,source)=='echo', (platform,variant,source,'TUN source must use Direct')
                        for address in targets:
                            assert ping('i-client',address,hops=1)=='time-exceeded', (platform,variant,address,'TTL/hop-limit')
                    # Block actual requests at the destination. A local TUN
                    # echo emulator would still reply and fails this assertion.
                    run('ip','netns','exec','i-server','nft','add','table','inet','icmp_test')
                    run('ip','netns','exec','i-server','nft','add','chain','inet','icmp_test','input','{ type filter hook input priority 0; policy accept; }')
                    run('ip','netns','exec','i-server','nft','add','rule','inet','icmp_test','input','meta','l4proto','{ 1, 58 }','counter','drop')
                    try:
                        for ns in ([''] if platform == 'macos' else ['', 'i-client']):
                            for address in targets:
                                assert ping(ns,address)=='no-echo', (platform,variant,ns,address,'FAKE echo')
                    finally: run('ip','netns','exec','i-server','nft','delete','table','inet','icmp_test')
                    print('PASS:',platform,variant,'IPv4/IPv6', 'host' if platform == 'macos' else 'host+forwarded+TUN-source+hop-limit', 'real echo and destination-drop checks',flush=True)
                except BaseException:
                    log.seek(0);print(log.read(),file=sys.stderr);raise
                finally:
                    core.terminate()
                    try: core.wait(timeout=5)
                    except subprocess.TimeoutExpired: core.kill();core.wait()


if __name__=='__main__': main()
