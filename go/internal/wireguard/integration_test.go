// SPDX-License-Identifier: GPL-3.0-or-later
package wireguard

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

// Two real userspace endpoints, no TUN, root privileges or host route changes.
// This exercises crypto, outbound AllowedIPs, inbound source ACL, loopback
// remapping, TCP replies and default-deny using the installed native runtime.
func TestNativeWireGuardRemoteAccess(t *testing.T) {
	binary := os.Getenv("STEER_WG_SING_BOX")
	if binary == "" {
		t.Skip("set STEER_WG_SING_BOX to test real WireGuard endpoints")
	}
	key := func() (string, string) {
		k, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
	}
	privA, pubA := key()
	privB, pubB := key()
	udpPort := func() int {
		c, e := net.ListenPacket("udp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	portA, portB := udpPort(), udpPort()
	for portB == portA {
		portB = udpPort()
	}
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	echoPort := echo.Addr().(*net.TCPAddr).Port
	udpEcho, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udpEcho.Close()
	go func() {
		data := make([]byte, 2048)
		for {
			n, addr, e := udpEcho.ReadFrom(data)
			if e != nil {
				return
			}
			_, _ = udpEcho.WriteTo(data[:n], addr)
		}
	}()
	udpEchoPort := udpEcho.LocalAddr().(*net.UDPAddr).Port
	udpEntrance := udpPort()
	for udpEntrance == portA || udpEntrance == portB {
		udpEntrance = udpPort()
	}
	socks, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	socksPort := socks.Addr().(*net.TCPAddr).Port
	socks.Close()
	makeIntent := func(id, address, private, public string, listen, remote int, allowed string) model.Intent {
		return model.Intent{Main: model.Main{ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "debug"}, Bootstrap: model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "ipv4_only"}, Routes: []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "wg", Enabled: true, Kind: "wireguard", Tunnel: id}}, DNSProfiles: []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}}, Rules: []model.Rule{{ID: "allowed", Enabled: true, AllowedIPs: true, Route: "wg", DNSProfile: "dns"}, {ID: "default", Enabled: true, Default: true, Route: "direct", DNSProfile: "dns"}}, WireGuardTunnels: []model.WireGuardTunnel{{ID: id, Enabled: true, Address: []string{address}, PrivateKey: private, ListenPort: listen, Peers: []model.WireGuardPeer{{PublicKey: public, Server: "127.0.0.1", ServerPort: remote, AllowedIPs: []string{allowed}}}}}}
	}
	a := makeIntent("client", "10.77.0.1/32", privA, pubB, portA, portB, "10.77.0.2/32")
	a.LocalProxies = []model.LocalProxy{{ID: "socks", Enabled: true, Protocol: "socks", Listen: "127.0.0.1", ListenPort: socksPort}}
	b := makeIntent("server", "10.77.0.2/32", privB, pubA, portB, portA, "10.77.0.1/32")
	b.WireGuardTunnels[0].Peers[0].Server = ""
	b.WireGuardTunnels[0].Peers[0].ServerPort = 0
	b.WireGuardTunnels[0].Access = []model.WireGuardAccess{{SourceIPCIDR: []string{"10.77.0.1/32"}, Network: "tcp", Port: 2222, Target: "127.0.0.1", TargetPort: echoPort}}
	b.WireGuardTunnels[0].Access = append(b.WireGuardTunnels[0].Access, model.WireGuardAccess{SourceIPCIDR: []string{"10.77.0.1/32"}, Network: "udp", Port: 3333, Target: "127.0.0.1", TargetPort: udpEchoPort})
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func(name string, v model.Intent) {
		out := compiler.Compile(v, compiler.Options{StateDirectory: dir})
		if name == "client" {
			out.SingBox["inbounds"] = append(out.SingBox["inbounds"].([]any), map[string]any{"type": "direct", "tag": "test-udp", "listen": "127.0.0.1", "listen_port": udpEntrance, "network": "udp"})
			route := out.SingBox["route"].(map[string]any)
			route["rules"] = append([]any{map[string]any{"inbound": []string{"test-udp"}, "action": "route", "outbound": "steer-route-wg", "override_address": "10.77.0.2", "override_port": 3333}}, route["rules"].([]any)...)
		}
		data, _ := json.Marshal(out.SingBox)
		path := filepath.Join(dir, name+".json")
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		if output, e := exec.Command(binary, "check", "-c", path).CombinedOutput(); e != nil {
			t.Fatalf("native check %s: %v %s", name, e, output)
		}
		log, e := os.Create(filepath.Join(dir, name+".log"))
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, binary, "run", "-c", path)
		cmd.Stdout = log
		cmd.Stderr = log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			cmd.Process.Kill()
			_ = cmd.Wait()
			log.Close()
			if t.Failed() {
				data, _ := os.ReadFile(log.Name())
				t.Logf("%s: %s", name, data)
			}
		})
	}
	start("server", b)
	start("client", a)
	connect := func(port int) (net.Conn, error) {
		c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), time.Second)
		if e != nil {
			return nil, e
		}
		_ = c.SetDeadline(time.Now().Add(4 * time.Second))
		fail := func(e error) (net.Conn, error) { c.Close(); return nil, e }
		if _, e = c.Write([]byte{5, 1, 0}); e != nil {
			return fail(e)
		}
		reply := make([]byte, 2)
		if _, e = io.ReadFull(c, reply); e != nil {
			return fail(e)
		}
		if _, e = c.Write([]byte{5, 1, 0, 1, 10, 77, 0, 2, byte(port >> 8), byte(port)}); e != nil {
			return fail(e)
		}
		header := make([]byte, 4)
		if _, e = io.ReadFull(c, header); e != nil {
			return fail(e)
		}
		if header[1] != 0 {
			return fail(fmt.Errorf("SOCKS reply %d", header[1]))
		}
		n := 6
		if header[3] == 4 {
			n = 18
		}
		if _, e = io.CopyN(io.Discard, c, int64(n)); e != nil {
			return fail(e)
		}
		return c, nil
	}
	var c net.Conn
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		c, e = connect(2222)
		if e == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, e = c.Write([]byte("wg-roundtrip")); e != nil {
		t.Fatal(e)
	}
	payload := make([]byte, 12)
	if _, e = io.ReadFull(c, payload); e != nil || string(payload) != "wg-roundtrip" {
		t.Fatalf("remote access failed: %q %v", payload, e)
	}
	udp, e := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", udpEntrance))
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(4 * time.Second))
	if _, e = udp.Write([]byte("udp-echo")); e != nil {
		t.Fatal(e)
	}
	reply := make([]byte, 64)
	n, e := udp.Read(reply)
	if e != nil || string(reply[:n]) != "udp-echo" {
		t.Fatalf("UDP remote access failed: %q %v", reply[:n], e)
	}
	denied, e := connect(echoPort)
	if e == nil {
		defer denied.Close()
		_, _ = denied.Write([]byte("forbidden"))
		payload := make([]byte, 9)
		if _, err := io.ReadFull(denied, payload); err == nil {
			t.Fatal("unlisted local service was exposed")
		}
	}
}
