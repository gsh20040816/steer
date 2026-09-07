// SPDX-License-Identifier: GPL-3.0-or-later

package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLatestProbeResultsSanitizeReportsAndReturnNewestFirst(t *testing.T) {
	stateDirectory := t.TempDir()
	older := Report{
		Scope: "nodes", ObjectID: "node_a", Kind: "connect", TestedAt: time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC),
		SavedDigest: "saved-a",
		Error:       "temporary sing-box: password=secret outbound={private_key: hidden}", Results: []Result{{
			URL: "https://user:token@example.test/probe?token=secret&bytes=1000#private", Error: "dial tcp: password=secret",
		}},
	}
	newer := Report{
		Scope: "overview", Kind: "proxy", OK: true, TestedAt: older.TestedAt.Add(time.Hour),
		SavedDigest: "saved-a", ActiveGeneration: "generation-a", ActiveDigest: "active-a",
		Results: []Result{{URL: "https://example.test/", OK: true, Status: 204}},
	}
	if err := SaveReport(stateDirectory, older); err != nil {
		t.Fatal(err)
	}
	if err := SaveReport(stateDirectory, newer); err != nil {
		t.Fatal(err)
	}
	results := ReadLatestProbeResults(stateDirectory, Identity{SavedDigest: "saved-a", ActiveGeneration: "generation-a", ActiveDigest: "active-a"})
	if len(results.Results) != 2 || results.Results[0].Kind != "proxy" {
		t.Fatalf("latest result order drifted: %#v", results.Results)
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"user:", "token@", "secret", "private_key", "outbound="} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("latest result leaked %q: %s", secret, encoded)
		}
	}
	if results.Results[1].ErrorSummary != "请查看诊断日志" {
		t.Fatalf("unsafe failure was not reduced to a product summary: %#v", results.Results[1])
	}
}

func TestReportArchiveKeepsOnlyLatestScopeObjectKind(t *testing.T) {
	stateDirectory := t.TempDir()
	first := Report{
		Scope: "nodes", ObjectID: "node_a", Kind: "connect", TestedAt: time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC),
		SavedDigest: "saved-a",
		Results:     []Result{{OK: false, Error: "timeout"}},
	}
	latest := first
	latest.OK = true
	latest.TestedAt = first.TestedAt.Add(time.Hour)
	latest.Results = []Result{{OK: true, FirstByteMilliseconds: 42}}
	if err := SaveReport(stateDirectory, first); err != nil {
		t.Fatal(err)
	}
	if err := SaveReport(stateDirectory, latest); err != nil {
		t.Fatal(err)
	}
	if err := SaveReport(stateDirectory, first); err != nil {
		t.Fatal(err)
	}
	results := ReadLatestProbeResults(stateDirectory, Identity{SavedDigest: "saved-a"})
	if len(results.Results) != 1 || !results.Results[0].OK ||
		results.Results[0].TestedAt != latest.TestedAt || results.Results[0].Summary != "42 ms" {
		t.Fatalf("scope/object/kind did not retain exactly the latest result: %#v", results.Results)
	}
}

func TestReportArchiveWarnsAndContinuesPastInvalidState(t *testing.T) {
	stateDirectory := t.TempDir()
	if err := SaveReport(stateDirectory, Report{Scope: "overview", Kind: "direct", TestedAt: time.Now(), SavedDigest: "saved-a", Results: []Result{}}); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(stateDirectory, "logs", "tests", "overview", "invalid.json")
	if err := os.WriteFile(invalid, []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := ReadLatestProbeResults(stateDirectory, Identity{SavedDigest: "saved-a"})
	if len(results.Results) != 1 || len(results.Warnings) != 1 {
		t.Fatalf("invalid state hid valid results or lacked a warning: %#v", results)
	}
}

func TestLatestProbeResultsAreUnboundedAndBackendComputesStale(t *testing.T) {
	stateDirectory := t.TempDir()
	for index := 0; index < 140; index++ {
		for _, kind := range []string{"connect", "download"} {
			report := Report{
				Scope: "nodes", ObjectID: fmt.Sprintf("node_%03d", index), Kind: kind,
				OK: true, TestedAt: time.Date(2026, 8, 26, 1, index%60, 0, 0, time.UTC), SavedDigest: "saved-a",
				Results: []Result{{OK: true, FirstByteMilliseconds: 17, DownloadedBytes: 1_000_000, DownloadMilliseconds: 500}},
			}
			if err := SaveReport(stateDirectory, report); err != nil {
				t.Fatal(err)
			}
		}
	}
	results := ReadLatestProbeResults(stateDirectory, Identity{SavedDigest: "saved-a"})
	if len(results.Results) != 280 {
		t.Fatalf("latest results were globally truncated: got %d, want 280", len(results.Results))
	}
	for _, result := range results.Results {
		if result.Stale {
			t.Fatalf("matching Saved identity was marked stale: %#v", result)
		}
	}
	stale := ReadLatestProbeResults(stateDirectory, Identity{SavedDigest: "saved-b"})
	if !stale.Results[0].Stale {
		t.Fatal("changed Saved identity did not stale a persisted Node result")
	}
}

func TestLatestProbeResultUsesBackendMetricAndSafeFailureSummary(t *testing.T) {
	identity := Identity{SavedDigest: "saved-a"}
	download := PresentLatestProbeResult(Report{
		Scope: "nodes", ObjectID: "node_a", Kind: "download", OK: true, SavedDigest: "saved-a", TestedAt: time.Now(),
		Results: []Result{{OK: true, DownloadedBytes: 1_000_000, DownloadMilliseconds: 500}},
	}, identity)
	if download.Summary != "16.0 Mbps" || download.ErrorSummary != "" {
		t.Fatalf("download summary drifted: %#v", download)
	}
	failure := PresentLatestProbeResult(Report{
		Scope: "routes", ObjectID: "route_a", Kind: "connect", SavedDigest: "saved-a", TestedAt: time.Now(),
		Error: "dial tcp 192.0.2.1:443: connection refused; password=secret",
	}, identity)
	if failure.OK || failure.ErrorSummary != "连接被拒绝" || failure.Summary != "" {
		t.Fatalf("failure summary drifted: %#v", failure)
	}
}

func TestOverviewLatestResultUsesSavedAndActiveIdentity(t *testing.T) {
	report := Report{
		Scope: "overview", Kind: "proxy", OK: true, TestedAt: time.Now(),
		SavedDigest: "saved-a", ActiveGeneration: "generation-a", ActiveDigest: "active-a",
		Results: []Result{{OK: true, FirstByteMilliseconds: 21}},
	}
	matching := PresentLatestProbeResult(report, Identity{
		SavedDigest: "saved-a", ActiveGeneration: "generation-a", ActiveDigest: "active-a",
	})
	if matching.Stale {
		t.Fatal("matching Saved/Active identity was marked stale")
	}
	changed := PresentLatestProbeResult(report, Identity{
		SavedDigest: "saved-a", ActiveGeneration: "generation-b", ActiveDigest: "active-b",
	})
	if !changed.Stale {
		t.Fatal("changed Active identity did not stale an Overview result")
	}
}

func TestSafeErrorKeepsOnlyStableDiagnosticCategories(t *testing.T) {
	for input, expected := range map[string]string{
		"unexpected HTTP status 503":             "probe target returned HTTP 503",
		"x509: certificate is invalid":           "TLS verification failed",
		"dial tcp: connection refused":           "probe connection was refused",
		"temporary sing-box: private_key=secret": "probe failed",
	} {
		if actual := SafeError(errors.New(input)); actual != expected {
			t.Fatalf("SafeError(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestLatestMetricPreservesPrecisionAndOmitsUnmeasuredValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   string
		ok     bool
		result Result
		want   *float64
	}{
		{"first byte", "connect", true, Result{OK: true, FirstByteMilliseconds: 42, TLSMilliseconds: 10}, metricPointer(42)},
		{"TLS fallback", "connect", true, Result{OK: true, TLSMilliseconds: 10}, metricPointer(10)},
		{"connection fallback", "connect", true, Result{OK: true, ConnectMilliseconds: 7}, metricPointer(7)},
		{"download precision", "download", true, Result{OK: true, DownloadedBytes: 1001, DownloadMilliseconds: 8}, metricPointer(1.001)},
		{"HTTP only", "connect", true, Result{OK: true, Status: 204}, nil},
		{"empty", "connect", true, Result{OK: true}, nil},
		{"failed", "connect", false, Result{OK: true, FirstByteMilliseconds: 42}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := PresentLatestProbeResult(Report{Kind: test.kind, OK: test.ok, Results: []Result{test.result}}, Identity{})
			if test.want == nil {
				if result.MetricValue != nil {
					t.Fatalf("unexpected metric: %v", *result.MetricValue)
				}
			} else if result.MetricValue == nil || *result.MetricValue != *test.want {
				t.Fatalf("metric = %v, want %v", result.MetricValue, *test.want)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			value, exists := payload["metric_value"]
			if exists != (test.want != nil) {
				t.Fatalf("unexpected metric presence: %s", encoded)
			}
			if test.want != nil && value != *test.want {
				t.Fatalf("metric must serialize as a number: %s", encoded)
			}
		})
	}
}

func metricPointer(value float64) *float64 { return &value }
