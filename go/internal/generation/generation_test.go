// SPDX-License-Identifier: GPL-3.0-or-later

package generation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestCreateWritesOnlySharedFiles(t *testing.T) {
	value := model.Intent{Main: model.Main{ID: "main", SchemaVersion: model.SchemaVersion}}
	candidate, err := Create(filepath.Join(t.TempDir(), "generations"), value, map[string]any{"log": map[string]any{"level": "warn"}})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(candidate.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "intent.json" || entries[1].Name() != "sing-box.json" {
		t.Fatalf("unexpected shared generation files: %#v", entries)
	}
	if _, err := ReadIntent(candidate.Directory); err != nil {
		t.Fatal(err)
	}
}

func TestUnchangedComparesRuntimeFilesButNotIntent(t *testing.T) {
	root := t.TempDir()
	write := func(directory, name, content string) {
		t.Helper()
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	current, candidate := filepath.Join(root, "current"), filepath.Join(root, "candidate")
	write(current, "intent.json", `{"old":true}`)
	write(candidate, "intent.json", `{"new":true}`)
	for _, directory := range []string{current, candidate} {
		write(directory, "sing-box.json", `{"log":{}}`)
		write(directory, "platform.json", `{"tun":"steer"}`)
	}
	healthy := func(context.Context) error { return nil }
	files := []RuntimeFile{{Name: "platform.json"}}
	if !Unchanged(context.Background(), current, Candidate{Directory: candidate}, files, healthy) {
		t.Fatal("intent-only change must not require a restart")
	}
	if Unchanged(context.Background(), current, Candidate{Directory: candidate}, files, func(context.Context) error { return errors.New("down") }) {
		t.Fatal("an unhealthy runtime must restart")
	}
	if Unchanged(context.Background(), "", Candidate{Directory: candidate}, files, healthy) {
		t.Fatal("a missing current generation must restart")
	}
	write(candidate, "platform.json", `{"tun":"other"}`)
	if Unchanged(context.Background(), current, Candidate{Directory: candidate}, files, healthy) {
		t.Fatal("a platform file change must restart")
	}
	normalized := []RuntimeFile{{Name: "platform.json", Normalize: func([]byte) ([]byte, error) { return []byte("same"), nil }}}
	if !Unchanged(context.Background(), current, Candidate{Directory: candidate}, normalized, healthy) {
		t.Fatal("normalized identity must not force a restart")
	}
	write(candidate, "sing-box.json", `{"log":{"level":"debug"}}`)
	if Unchanged(context.Background(), current, Candidate{Directory: candidate}, normalized, healthy) {
		t.Fatal("a sing-box.json change must restart")
	}
	if Unchanged(context.Background(), current, Candidate{Directory: candidate}, []RuntimeFile{{Name: "missing.json"}}, healthy) {
		t.Fatal("an unreadable runtime file must restart")
	}
}

func TestPublishLinkSwitchesCurrent(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two"} {
		directory := filepath.Join(root, "generations", name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := PublishLink(root, Candidate{Directory: directory}); err != nil {
			t.Fatal(err)
		}
		if target, err := os.Readlink(filepath.Join(root, "current")); err != nil || target != directory {
			t.Fatalf("current points at %q, want %q: %v", target, directory, err)
		}
	}
}
