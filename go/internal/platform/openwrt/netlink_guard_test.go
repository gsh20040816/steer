// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNetlinkGuardEvidenceAndCooldown(t *testing.T) {
	for _, mode := range []string{"stalled", "busy-traffic", "idle-dropped", "draining", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			g := NetlinkGuard{}
			start := time.Unix(10000, 0)
			triggered := false
			for i := 0; i <= 15; i++ {
				s := NetlinkSample{At: start.Add(time.Duration(i) * 5 * time.Second), CPU: time.Duration(i) * 5 * time.Second, Inode: "42", Queue: 212160, Drops: 10}
				switch mode {
				case "busy-traffic":
					s.Drops = 0
				case "idle-dropped":
					s.CPU /= 10
				case "draining":
					s.Queue -= uint64(i) * 100
				case "replaced":
					s.Inode = string(rune('a' + i))
				}
				if g.Observe(s) {
					triggered = true
				}
			}
			if triggered != (mode == "stalled") {
				t.Fatalf("triggered=%v", triggered)
			}
			g.LastRecovery = start.Add(75 * time.Second)
			if g.Observe(NetlinkSample{At: start.Add(80 * time.Second), CPU: 80 * time.Second, Inode: "42", Queue: 212160, Drops: 10}) {
				t.Fatal("cooldown bypassed")
			}
		})
	}
}
func TestReceiveDefaultRestore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "net/core/rmem_default")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("212992\n"), 0600)
	restore, err := RaiseReceiveDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "4194304" {
		t.Fatal(string(b))
	}
	restore()
	b, _ = os.ReadFile(path)
	if string(b) != "212992\n" {
		t.Fatal(string(b))
	}
	restore, err = RaiseReceiveDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("8388608"), 0600)
	restore()
	b, _ = os.ReadFile(path)
	if string(b) != "8388608" {
		t.Fatal("overwrote concurrent change")
	}
}
func TestReadNetlinkSampleIgnoresOtherProcesses(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "42")
	os.MkdirAll(filepath.Join(p, "fd"), 0700)
	os.MkdirAll(filepath.Join(p, "net"), 0700)
	os.WriteFile(filepath.Join(p, "stat"), []byte("42 (core with space) S 1 1 1 0 0 0 0 0 0 0 100 20"), 0600)
	os.Symlink("socket:[123]", filepath.Join(p, "fd/6"))
	os.WriteFile(filepath.Join(p, "net/netlink"), []byte("sk Eth Pid Groups Rmem Wmem Dump Locks Drops Inode\n0 0 42 00000440 212160 0 0 2 8 123\n0 0 9 00000440 4000000 0 0 2 99 999\n"), 0600)
	s, err := ReadNetlinkSample(root, 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.Inode != "123" || s.Drops != 8 || s.CPU != 1200*time.Millisecond {
		t.Fatalf("%+v", s)
	}
}
