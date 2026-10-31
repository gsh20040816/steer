// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func validIntent() model.Intent {
	return model.Intent{
		Main:        model.Main{ID: "main", SchemaVersion: model.SchemaVersion, Enabled: false, LogLevel: "warn", ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/", SpeedtestProxyURL: "https://speed.example/"},
		Bootstrap:   model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Routes:      []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "block", Enabled: true, Kind: "block"}},
		DNSProfiles: []model.DNSProfile{{ID: "public", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:       []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "public", Route: "direct"}},
	}
}

func TestIntentStoreRoundTripAndRevisionConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := SavedStore(path, filepath.Join(filepath.Dir(path), "state"))
	value := validIntent()
	rev, err := store.Save(value, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedRev, err := store.Load()
	if err != nil || loaded.Main.ID != value.Main.ID || loadedRev != rev {
		t.Fatalf("round trip failed: value=%#v revision=%q err=%v", loaded, loadedRev, err)
	}
	if _, err := store.Save(value, `"sha256-invalid"`); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision returned %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions are %o", info.Mode().Perm())
	}
}

func TestIntentStorePreservesPrivateKeyBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := SavedStore(path, filepath.Join(filepath.Dir(path), "state"))
	value := validIntent()
	privateKey := "-----BEGIN OPENSSH PRIVATE KEY-----\r\nline one  \r\n\r\nline two\r\n-----END OPENSSH PRIVATE KEY-----\r\n"
	value.Nodes = []model.Node{{
		ID: "ssh-node", Enabled: true, Type: "ssh", Server: "ssh.example", ServerPort: 22,
		NodeCredentials: model.NodeCredentials{Username: "root", PrivateKey: privateKey},
	}}

	if _, err := store.Save(value, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Nodes) != 1 || loaded.Nodes[0].PrivateKey != privateKey {
		t.Fatal("private key changed during save/load round trip")
	}
}

func TestLinuxValidationAcceptsNativeSourceMAC(t *testing.T) {
	value := validIntent()
	macRule := model.Rule{ID: "mac", Enabled: true, SourceMACAddress: []string{"02:00:00:00:00:10"}, DNSProfile: "public", Route: "direct"}
	value.Rules = append(value.Rules[:len(value.Rules)-1], macRule, value.Rules[len(value.Rules)-1])
	validation := Validate(value)
	if !validation.OK {
		t.Fatalf("sing-box 1.14 native source MAC rule was rejected on Linux: %#v", validation.Errors)
	}
}
