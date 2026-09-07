// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NetlinkSample describes only route notification sockets owned by one core.
type NetlinkSample struct {
	At           time.Time
	CPU          time.Duration
	Inode        string
	Queue, Drops uint64
}

// NetlinkGuard requires sustained CPU use and an unchanged, dropped route queue.
// Ordinary traffic, drained queues and a single overloaded sample cannot recover.
type NetlinkGuard struct {
	previous     NetlinkSample
	stalledSince time.Time
	LastRecovery time.Time
}

func (g *NetlinkGuard) Observe(s NetlinkSample) bool {
	p := g.previous
	g.previous = s
	elapsed := s.At.Sub(p.At)
	busy := elapsed > 0 && s.CPU >= p.CPU && float64(s.CPU-p.CPU)/float64(elapsed) >= .8
	stuck := s.Inode != "" && s.Inode == p.Inode && s.Queue >= 128*1024 && s.Queue == p.Queue && s.Drops > 0
	if !busy || !stuck {
		g.stalledSince = time.Time{}
		return false
	}
	if g.stalledSince.IsZero() {
		g.stalledSince = p.At
	}
	return s.At.Sub(g.stalledSince) >= time.Minute && s.At.Sub(g.LastRecovery) >= 10*time.Minute
}

func ReadNetlinkSample(proc string, pid int, now time.Time) (NetlinkSample, error) {
	root := filepath.Join(proc, strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return NetlinkSample{}, err
	}
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return NetlinkSample{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(stat)[end+2:])
	if len(fields) < 13 {
		return NetlinkSample{}, fmt.Errorf("short process stat")
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return NetlinkSample{}, err
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return NetlinkSample{}, err
	}
	// Linux exports /proc process times in USER_HZ (100 on supported x86_64).
	sample := NetlinkSample{At: now, CPU: time.Duration(user+system) * 10 * time.Millisecond}
	entries, err := os.ReadDir(filepath.Join(root, "fd"))
	if err != nil {
		return sample, err
	}
	sockets := map[string]bool{}
	for _, entry := range entries {
		target, e := os.Readlink(filepath.Join(root, "fd", entry.Name()))
		if e == nil {
			sockets[target] = true
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "net", "netlink"))
	if err != nil {
		return sample, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[1] != "0" || !sockets["socket:["+f[9]+"]"] {
			continue
		}
		groups, e := strconv.ParseUint(f[3], 16, 64)
		if e != nil || groups&0x440 == 0 {
			continue
		}
		queue, e := strconv.ParseUint(f[4], 10, 64)
		if e != nil {
			return sample, e
		}
		drops, e := strconv.ParseUint(f[8], 10, 64)
		if e != nil {
			return sample, e
		}
		if sample.Inode == "" || queue > sample.Queue {
			sample.Inode = f[9]
			sample.Queue = queue
			sample.Drops = drops
		}
	}
	return sample, nil
}

// RaiseReceiveDefault changes the default only while the child creates sockets.
// Existing sockets keep their buffers; restoration does not overwrite later edits.
func RaiseReceiveDefault(root string) (func(), error) {
	path := filepath.Join(root, "net/core/rmem_default")
	data, err := os.ReadFile(path)
	if err != nil {
		return func() {}, err
	}
	old, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return func() {}, err
	}
	const target = 4 * 1024 * 1024
	if old >= target {
		return func() {}, nil
	}
	if err = os.WriteFile(path, []byte(strconv.Itoa(target)), 0600); err != nil {
		return func() {}, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			current, e := os.ReadFile(path)
			if e == nil && strings.TrimSpace(string(current)) == strconv.Itoa(target) {
				_ = os.WriteFile(path, data, 0600)
			}
		})
	}, nil
}
