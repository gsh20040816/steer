// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
)

const applyLockTimeout = 2 * time.Minute

func runLockedApply(runDirectory string, operation func() (coreapply.Result, error), stdout io.Writer) error {
	lock, err := acquireLock(runDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	return runApplyOperation(runDirectory, operation, stdout)
}

// runApplyOperation records an Apply while the caller already owns the
// cross-process operation lock. The control daemon uses this to keep saving
// the canonical document and switching runtime state in one transaction.
func runApplyOperation(runDirectory string, operation func() (coreapply.Result, error), stdout io.Writer) error {
	result, err := operation()
	if err != nil {
		result.OK = false
		result.Error = err.Error()
	}
	now := time.Now().UTC()
	if recordErr := writeApplyRecord(runDirectory, coreapply.Record{
		Sequence: strconv.FormatInt(now.UnixNano(), 10), Timestamp: now.Format(time.RFC3339Nano), Result: result,
	}); recordErr != nil {
		if err != nil {
			err = errors.Join(err, recordErr)
		} else {
			err = recordErr
		}
	}
	if writeErr := writeJSON(stdout, result); writeErr != nil && err == nil {
		err = writeErr
	}
	return err
}

func acquireLock(runDirectory string) (*os.File, error) {
	ctx, cancel := context.WithTimeout(context.Background(), applyLockTimeout)
	defer cancel()
	if err := os.MkdirAll(runDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create macOS runtime directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(runDirectory, "operation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open macOS Apply lock: %w", err)
	}
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			lock.Close()
			return nil, fmt.Errorf("lock macOS Apply transaction: %w", err)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			lock.Close()
			return nil, fmt.Errorf("lock macOS Apply transaction: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func writeApplyRecord(runDirectory string, record coreapply.Record) error {
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return macOSAtomicWrite(filepath.Join(runDirectory, "last-apply.json"), append(encoded, '\n'))
}

func macOSAtomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".steer.result.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
