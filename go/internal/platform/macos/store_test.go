// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func NewPaths(rootDirectory string) (Paths, error) {
	if rootDirectory == "" || !filepath.IsAbs(rootDirectory) {
		return Paths{}, errors.New("macOS runtime root must be an absolute path")
	}
	root := filepath.Clean(rootDirectory)
	configDirectory := filepath.Join(root, "config")
	return Paths{
		Root: root, ConfigDirectory: configDirectory,
		ConfigPath:           filepath.Join(configDirectory, "config.json"),
		GenerationsDirectory: filepath.Join(root, "generations"),
		StateDirectory:       filepath.Join(root, "state"),
		LogsDirectory:        filepath.Join(root, "logs"),
	}, nil
}

func TestIntentStoreUsesRevisionGuard(t *testing.T) {
	paths, err := NewPaths(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := IntentStore{Paths: paths}
	value := validIntent()
	revision, err := store.Save(value, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedRevision, err := store.Load()
	if err != nil || loaded.Main.ID != value.Main.ID || loadedRevision != revision {
		t.Fatalf("unexpected stored intent: %#v %q %v", loaded, loadedRevision, err)
	}
	if _, err := store.Save(value, "stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision did not fail fast: %v", err)
	}
}

func TestIntentStoreDefaultsToMacOSGeoDataDirectory(t *testing.T) {
	store := IntentStore{}
	if got := store.geoDataDirectory(); got != DefaultGeoDataDirectory {
		t.Fatalf("empty Store Geo directory = %q, want %q", got, DefaultGeoDataDirectory)
	}
}

func TestPreparePublishAndLoadCurrentGeneration(t *testing.T) {
	paths, err := NewPaths(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareTestGeneration(validIntent(), paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"intent.json", "sing-box.json", "macos.json", "generation.json"} {
		if _, err := os.Stat(filepath.Join(prepared.Directory, name)); err != nil {
			t.Fatalf("prepared generation is missing %s: %v", name, err)
		}
	}
	if err := paths.Publish(prepared); err != nil {
		t.Fatal(err)
	}
	current, err := paths.LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if current.GenerationID != prepared.Metadata.GenerationID || current.Directory != filepath.Base(prepared.Directory) {
		t.Fatalf("published generation mismatch: %#v %#v", current, prepared.Metadata)
	}
	info, err := os.Stat(filepath.Join(paths.Root, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("current generation mode = %o, want 644", info.Mode().Perm())
	}
}
