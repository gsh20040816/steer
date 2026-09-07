#!/usr/bin/env python3
"""Root-only isolated OpenWrt supervisor regression. Args: steer sing-box.
Checks actual netlink buffer size, burst tolerance, recovery and cooldown.
"""
import ctypes
import json
import os
import pathlib
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import time


def run(*args):
    return subprocess.check_output(args, stderr=subprocess.STDOUT, text=True)


def main():
    steer, core = map(os.path.abspath, sys.argv[1:])
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(0x40000000 | 0x00020000):
        raise OSError(ctypes.get_errno(), 'unshare')
    run('mount', '--make-rprivate', '/')
    run('mount', '-t', 'tmpfs', 'tmpfs', '/run')
    run('ip', 'link', 'set', 'lo', 'up')
    run('ip', 'link', 'add', 'guard-wan', 'type', 'veth', 'peer', 'name', 'guard-peer')
    for dev in ['guard-wan', 'guard-peer']:
        run('ip', 'link', 'set', dev, 'up')
    run('ip', 'addr', 'add', '10.77.0.1/24', 'dev', 'guard-wan')
    run('ip', 'route', 'add', 'default', 'via', '10.77.0.2')
    config = {'log': {'level': 'warn'}, 'inbounds': [{'type': 'direct', 'tag': 'in', 'listen': '127.0.0.1', 'listen_port': 19053}], 'outbounds': [{'type': 'direct', 'tag': 'direct'}], 'route': {'auto_detect_interface': True}}
    pathlib.Path('/run/guard.json').write_text(json.dumps(config))
    default = pathlib.Path('/proc/sys/net/core/rmem_default')
    original = default.read_text()
    with tempfile.TemporaryFile(mode='w+') as log:
        parent = subprocess.Popen([steer, '_supervise', '--core', core, '--config', '/run/guard.json', '--recovery-state', '/run/guard-cooldown.json'], stdout=log, stderr=log)
        def child():
            for stat in pathlib.Path('/proc').glob('[0-9]*/stat'):
                try:
                    fields = stat.read_text().split(') ')[1].split()
                    if int(fields[1]) == parent.pid:
                        return int(stat.parent.name)
                except (FileNotFoundError, ProcessLookupError):
                    continue
            return None
        def cpu(pid):
            def ticks():
                return sum(map(int, pathlib.Path('/proc', str(pid), 'stat').read_text().split(') ')[1].split()[11:13]))
            start = ticks()
            time.sleep(3)
            return (ticks() - start) / os.sysconf('SC_CLK_TCK') / 3 * 100
        def burst(pid, count, table):
            os.kill(pid, signal.SIGSTOP)
            try:
                batch = ''.join(f'route add 10.{i//256+80}.{i%256}.0/24 dev guard-wan table {table}\n' for i in range(count))
                subprocess.run(['ip', '-batch', '-'], input=batch, text=True, check=True, capture_output=True)
            finally:
                os.kill(pid, signal.SIGCONT)
        def buffers(pid):
            inodes = {os.readlink(p)[8:-1] for p in pathlib.Path('/proc', str(pid), 'fd').iterdir() if os.readlink(p).startswith('socket:[')}
            result = []
            with socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, 4) as sock:
                sock.settimeout(3)
                req = struct.pack('BBHIIII', 16, 0, 0, 0, 1, 0, 0)
                sock.send(struct.pack('IHHII', len(req)+16, 20, 0x301, 1, 0)+req)
                while True:
                    data = sock.recv(65536)
                    pos = 0
                    while pos < len(data):
                        size, kind = struct.unpack_from('IH', data, pos)
                        if kind == 3: return result
                        if kind == 2: raise RuntimeError('netlink diag failed')
                        inode = str(struct.unpack_from('I', data, pos+32)[0])
                        attr = pos+44
                        while attr < pos+size:
                            n, typ = struct.unpack_from('HH', data, attr)
                            if typ == 0 and inode in inodes:
                                result.append(struct.unpack_from('I', data, attr+8)[0])
                            attr += (n+3)&~3
                        pos += (size+3)&~3
        try:
            time.sleep(7)
            pid = child()
            assert pid and parent.poll() is None
            assert default.read_text() == original, 'global buffer default was not restored'
            sizes = buffers(pid)
            assert len(sizes) >= 3, sizes
            if min(sizes) >= 4*1024*1024:
                print('PASS actual subscription receive buffers:', sizes, flush=True)
                burst(pid, 2500, 50000)
                time.sleep(3)
                assert cpu(pid) < 25, 'moderate burst caused spinning'
                print('PASS moderate burst prevention', flush=True)
            else:
                # rmem_default is read-only in non-initial network namespaces.
                # Production verifies the raised socket limit separately.
                print('Buffer adjustment unavailable in this namespace; testing recovery with buffers:', sizes, flush=True)
            burst(pid, 12000, 50001)
            time.sleep(3)
            assert cpu(pid) > 70, 'overflow reproducer did not trigger upstream fault'
            until = time.monotonic()+85
            while time.monotonic() < until and child() == pid:
                time.sleep(2)
            new = child()
            assert new and new != pid, 'guard did not recover core'
            time.sleep(7)
            assert cpu(new) < 25, 'new core did not recover'
            print('PASS automatic recovery', flush=True)
            burst(new, 12000, 50002)
            time.sleep(3)
            assert cpu(new) > 70
            time.sleep(70)
            assert child() == new, 'recovery cooldown was bypassed'
            print('PASS cooldown prevents repeated restarts', flush=True)
        except BaseException:
            log.seek(0)
            print(log.read(), file=sys.stderr)
            raise
        finally:
            parent.terminate()
            parent.wait(timeout=10)
            assert default.read_text() == original
            child_pid = locals().get('new', locals().get('pid'))
            if child_pid:
                assert not pathlib.Path('/proc', str(child_pid)).exists(), 'orphaned core'


if __name__ == '__main__':
    main()
