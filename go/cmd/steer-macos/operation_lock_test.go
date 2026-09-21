// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
)

func TestApplyRecordIncludesTimestamp(t *testing.T) {
	for name, operationErr := range map[string]error{"success": nil, "failure": errors.New("apply failed")} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			before := time.Now()
			err := runApplyOperation(directory, func() (coreapply.Result, error) {
				return coreapply.Result{OK: true}, operationErr
			}, io.Discard)
			if !errors.Is(err, operationErr) {
				t.Fatalf("unexpected error: %v", err)
			}
			content, err := os.ReadFile(filepath.Join(directory, "last-apply.json"))
			if err != nil {
				t.Fatal(err)
			}
			var record coreapply.Record
			if err := json.Unmarshal(content, &record); err != nil {
				t.Fatal(err)
			}
			timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
			if err != nil {
				t.Fatal(err)
			}
			if timestamp.Before(before) || timestamp.After(time.Now()) {
				t.Fatalf("timestamp outside operation interval: %s", record.Timestamp)
			}
			if record.Sequence != strconv.FormatInt(timestamp.UnixNano(), 10) {
				t.Fatalf("sequence and timestamp disagree: %+v", record)
			}
			if record.Result.OK != (operationErr == nil) {
				t.Fatalf("unexpected result: %+v", record.Result)
			}
		})
	}
}
