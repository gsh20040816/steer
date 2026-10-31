// SPDX-License-Identifier: GPL-3.0-or-later

// Package generation owns the platform-neutral files in one prepared runtime
// generation. Platform adapters may add files but must not change these names.
package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Candidate struct {
	Directory string `json:"directory"`
}

func Create(root string, value model.Intent, singBox map[string]any) (candidate Candidate, returnErr error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Candidate{}, fmt.Errorf("create generation root: %w", err)
	}
	directory, err := os.MkdirTemp(root, "candidate.")
	if err != nil {
		return Candidate{}, fmt.Errorf("create candidate generation: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.Chmod(directory, 0o700); err != nil {
		return Candidate{}, fmt.Errorf("protect candidate generation: %w", err)
	}
	intentFile, err := os.OpenFile(filepath.Join(directory, "intent.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Candidate{}, fmt.Errorf("create intent.json: %w", err)
	}
	if err := model.EncodeJSON(intentFile, value); err != nil {
		intentFile.Close()
		return Candidate{}, err
	}
	if err := intentFile.Close(); err != nil {
		return Candidate{}, fmt.Errorf("close intent.json: %w", err)
	}
	if err := WriteJSON(filepath.Join(directory, "sing-box.json"), singBox); err != nil {
		return Candidate{}, err
	}
	return Candidate{Directory: directory}, nil
}

func WriteJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		file.Close()
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	return nil
}

// RuntimeFile names one platform file whose content decides how the running
// service behaves. Normalize, when set, removes generation identity that may
// differ between two otherwise equivalent generations.
type RuntimeFile struct {
	Name      string
	Normalize func([]byte) ([]byte, error)
}

// Unchanged reports whether Activate may keep the running service and only
// switch current to the candidate: the current generation exists, sing-box.json
// and every platform runtime file are byte-identical, and the running service
// is healthy. intent.json is never compared, so edits that do not reach the
// runtime (names, unreferenced nodes, subscription metadata) never restart.
// Any read failure means "changed" and keeps the ordinary restart path.
func Unchanged(ctx context.Context, current string, candidate Candidate, platformFiles []RuntimeFile, healthy func(context.Context) error) bool {
	if current == "" || candidate.Directory == "" {
		return false
	}
	files := append([]RuntimeFile{{Name: "sing-box.json"}}, platformFiles...)
	for _, file := range files {
		left, err := readRuntimeFile(current, file)
		if err != nil {
			return false
		}
		right, err := readRuntimeFile(candidate.Directory, file)
		if err != nil || !bytes.Equal(left, right) {
			return false
		}
	}
	return healthy(ctx) == nil
}

func readRuntimeFile(directory string, file RuntimeFile) ([]byte, error) {
	content, err := os.ReadFile(filepath.Join(directory, file.Name))
	if err != nil || file.Normalize == nil {
		return content, err
	}
	return file.Normalize(content)
}

// PublishLink atomically points runDirectory/current at the candidate. It is
// the whole switch when the runtime is unchanged and the last step before the
// service starts otherwise.
func PublishLink(runDirectory string, candidate Candidate) error {
	link := filepath.Join(runDirectory, "current")
	temporary := filepath.Join(runDirectory, ".current."+strconv.Itoa(os.Getpid()))
	_ = os.Remove(temporary)
	if err := os.Symlink(candidate.Directory, temporary); err != nil {
		return fmt.Errorf("create current generation link: %w", err)
	}
	if err := os.Rename(temporary, link); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish current generation: %w", err)
	}
	return nil
}

func ReadIntent(directory string) (model.Intent, error) {
	file, err := os.Open(filepath.Join(directory, "intent.json"))
	if err != nil {
		return model.Intent{}, fmt.Errorf("open current intent: %w", err)
	}
	defer file.Close()
	return model.DecodeJSON(file)
}
