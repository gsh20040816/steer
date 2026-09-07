// SPDX-License-Identifier: GPL-3.0-or-later

package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxArchivedReportSize = 1 << 20

// Identity is the Saved/Active configuration identity used by the backend to
// decide whether a persisted probe result still describes the current state.
// It is deliberately not part of the ordinary UI payload.
type Identity struct {
	SavedDigest      string
	ActiveGeneration string
	ActiveDigest     string
}

// LatestProbeResult is the only persisted-probe contract intended for an
// ordinary UI. Raw Report values remain an internal diagnostic artifact.
type LatestProbeResult struct {
	Scope    string    `json:"scope"`
	ObjectID string    `json:"object_id,omitempty"`
	Kind     string    `json:"kind"`
	TestedAt time.Time `json:"tested_at"`
	OK       bool      `json:"ok"`
	Stale    bool      `json:"stale"`
	Summary  string    `json:"summary"`
	// MetricValue is latency in ms for connect probes and throughput in Mbps for downloads.
	MetricValue  *float64 `json:"metric_value,omitempty"`
	ErrorSummary string   `json:"error_summary"`
}

type LatestProbeResults struct {
	Results  []LatestProbeResult `json:"latest_results"`
	Warnings []string            `json:"warnings"`
}

// Diagnostics contains non-probe-history diagnostics. Latest probe results
// are exposed through the separate LatestProbeResults capability.
type Diagnostics struct {
	DNSCapture DNSCaptureDiagnostic `json:"dns_capture"`
	Warnings   []string             `json:"warnings"`
}

func FailureReport(scope, objectID, kind string, err error) Report {
	return SanitizeReport(Report{
		Scope: scope, ObjectID: objectID, Kind: kind,
		Error: SafeError(err), TestedAt: time.Now().UTC(), Results: []Result{},
	})
}

func BindReportIdentity(report Report, identity Identity) Report {
	report.SavedDigest = identity.SavedDigest
	if report.Scope == "overview" {
		report.ActiveGeneration = identity.ActiveGeneration
		report.ActiveDigest = identity.ActiveDigest
	}
	return report
}

// SanitizeReport removes credentials, query values and process diagnostics
// before a report crosses a public UI boundary or is persisted for later UI
// display.
func SanitizeReport(report Report) Report {
	report.Error = SafeError(errors.New(report.Error))
	if report.Results == nil {
		report.Results = []Result{}
	}
	for index := range report.Results {
		report.Results[index].URL = SafeURL(report.Results[index].URL)
		if report.Results[index].Error != "" {
			report.Results[index].Error = SafeError(errors.New(report.Results[index].Error))
		}
	}
	return report
}

func SafeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.Fragment = ""
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		parsed.Path = "/REDACTED"
		parsed.RawPath = ""
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		for key := range query {
			values := query[key]
			for index := range values {
				values[index] = "REDACTED"
			}
			query[key] = values
		}
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

func SafeError(err error) string {
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		return ""
	}
	existing := strings.TrimSpace(err.Error())
	switch existing {
	case "probe timed out", "probe was cancelled", "TLS verification failed", "probe connection was refused", "probe target could not be resolved", "probe failed":
		return existing
	}
	if strings.HasPrefix(existing, "probe target returned HTTP ") {
		fields := strings.Fields(strings.TrimPrefix(existing, "probe target returned HTTP "))
		if len(fields) > 0 {
			if status, parseErr := strconv.Atoi(fields[0]); parseErr == nil && status >= 100 && status <= 599 {
				return fmt.Sprintf("probe target returned HTTP %d", status)
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "timed out") {
		return "probe timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "probe was cancelled"
	}
	message := strings.ToLower(err.Error())
	if marker := "unexpected http status "; strings.Contains(message, marker) {
		remainder := message[strings.Index(message, marker)+len(marker):]
		fields := strings.Fields(remainder)
		if len(fields) > 0 {
			if status, parseErr := strconv.Atoi(fields[0]); parseErr == nil && status >= 100 && status <= 599 {
				return fmt.Sprintf("probe target returned HTTP %d", status)
			}
		}
	}
	if strings.Contains(message, "x509") || strings.Contains(message, "tls") {
		return "TLS verification failed"
	}
	if strings.Contains(message, "connection refused") {
		return "probe connection was refused"
	}
	if strings.Contains(message, "no such host") {
		return "probe target could not be resolved"
	}
	return "probe failed"
}

func SaveReport(stateDirectory string, report Report) error {
	path, err := reportPath(stateDirectory, report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create test log directory: %w", err)
	}
	report = SanitizeReport(report)
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open test report lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock test report: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if current, err := readReportFile(path); err == nil && current.TestedAt.After(report.TestedAt) {
		return nil
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode test report: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return fmt.Errorf("create temporary test report: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish test report: %w", err)
	}
	return nil
}

// ReadLatestProbeResults returns one result for every persisted scope/object/kind key.
func ReadLatestProbeResults(stateDirectory string, identity Identity) LatestProbeResults {
	reports, warnings := readReports(stateDirectory)
	results := make([]LatestProbeResult, 0, len(reports))
	for _, report := range reports {
		results = append(results, PresentLatestProbeResult(report, identity))
	}
	return LatestProbeResults{Results: results, Warnings: warnings}
}

func PresentLatestProbeResult(report Report, identity Identity) LatestProbeResult {
	report = SanitizeReport(report)
	result := LatestProbeResult{
		Scope: report.Scope, ObjectID: report.ObjectID, Kind: report.Kind,
		TestedAt: report.TestedAt, OK: report.OK, Stale: reportIsStale(report, identity),
	}
	if report.OK {
		result.Summary, result.MetricValue = coreMetric(report)
	} else {
		result.ErrorSummary = safeErrorSummary(report)
	}
	return result
}

func FindLatestProbeResult(results LatestProbeResults, scope, objectID, kind string) (LatestProbeResult, bool) {
	for _, result := range results.Results {
		if result.Scope == scope && result.ObjectID == objectID && result.Kind == kind {
			return result, true
		}
	}
	return LatestProbeResult{}, false
}

func readReports(stateDirectory string) ([]Report, []string) {
	root := filepath.Join(normalizeStateDirectory(stateDirectory), "logs", "tests")
	reports := []Report{}
	warnings := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root && errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			warnings = append(warnings, "a saved probe result is unavailable")
			return nil
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			warnings = append(warnings, "a saved probe result is invalid")
			return nil
		}
		if info, err := entry.Info(); err != nil || info.Size() > maxArchivedReportSize {
			warnings = append(warnings, "a saved probe result is invalid")
			return nil
		}
		report, err := readReportFile(path)
		if err != nil || !validReportIdentity(report) {
			warnings = append(warnings, "a saved probe result is invalid")
			return nil
		}
		reports = append(reports, SanitizeReport(report))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		warnings = append(warnings, "saved probe results are unavailable")
	}
	sort.Slice(reports, func(left, right int) bool {
		return reports[left].TestedAt.After(reports[right].TestedAt)
	})
	return reports, warnings
}

func readReportFile(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxArchivedReportSize))
	decoder.DisallowUnknownFields()
	var report Report
	decodeErr := decoder.Decode(&report)
	if decodeErr == nil {
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			decodeErr = errors.New("probe report contains trailing data")
		}
	}
	closeErr := file.Close()
	if decodeErr != nil {
		return Report{}, decodeErr
	}
	return report, closeErr
}

func reportIsStale(report Report, identity Identity) bool {
	if report.SavedDigest == "" || identity.SavedDigest == "" || report.SavedDigest != identity.SavedDigest {
		return true
	}
	if report.Scope != "overview" {
		return false
	}
	return report.ActiveGeneration != identity.ActiveGeneration || report.ActiveDigest != identity.ActiveDigest
}

func coreMetric(report Report) (string, *float64) {
	for _, result := range report.Results {
		if !result.OK {
			continue
		}
		if report.Kind == "download" || report.Kind == "speedtest" {
			if result.DownloadedBytes > 0 && result.DownloadMilliseconds > 0 {
				megabitsPerSecond := float64(result.DownloadedBytes) * 8 / float64(result.DownloadMilliseconds) / 1000
				return fmt.Sprintf("%.1f Mbps", megabitsPerSecond), &megabitsPerSecond
			}
			continue
		}
		milliseconds := result.FirstByteMilliseconds
		if milliseconds == 0 {
			milliseconds = result.TLSMilliseconds
		}
		if milliseconds == 0 {
			milliseconds = result.ConnectMilliseconds
		}
		if milliseconds > 0 {
			value := float64(milliseconds)
			return fmt.Sprintf("%d ms", milliseconds), &value
		}
		if result.Status > 0 {
			return fmt.Sprintf("HTTP %d", result.Status), nil
		}
	}
	return "成功", nil
}

func safeErrorSummary(report Report) string {
	errorValue := report.Error
	if errorValue == "" {
		for _, result := range report.Results {
			if result.Error != "" {
				errorValue = result.Error
				break
			}
		}
	}
	safe := errorValue
	if safe != "probe timed out" && safe != "probe was cancelled" && safe != "TLS verification failed" &&
		safe != "probe connection was refused" && safe != "probe target could not be resolved" &&
		!strings.HasPrefix(safe, "probe target returned HTTP ") {
		safe = SafeError(errors.New(errorValue))
	}
	switch safe {
	case "probe timed out":
		return "连接超时"
	case "probe was cancelled":
		return "测试已取消"
	case "TLS verification failed":
		return "TLS 校验失败"
	case "probe connection was refused":
		return "连接被拒绝"
	case "probe target could not be resolved":
		return "目标无法解析"
	case "probe failed", "":
		return "请查看诊断日志"
	default:
		if strings.HasPrefix(safe, "probe target returned HTTP ") {
			return strings.Replace(safe, "probe target returned HTTP ", "目标返回 HTTP ", 1)
		}
		return "请查看诊断日志"
	}
}

func reportPath(stateDirectory string, report Report) (string, error) {
	if !validReportIdentity(report) {
		return "", errors.New("invalid probe report identity")
	}
	directory := filepath.Join(normalizeStateDirectory(stateDirectory), "logs", "tests", report.Scope)
	if report.ObjectID != "" {
		directory = filepath.Join(directory, report.ObjectID)
	}
	return filepath.Join(directory, report.Kind+".json"), nil
}

func validReportIdentity(report Report) bool {
	if report.Scope != "overview" && report.Scope != "nodes" && report.Scope != "routes" {
		return false
	}
	if report.Kind != "direct" && report.Kind != "proxy" && report.Kind != "speedtest" && report.Kind != "connect" && report.Kind != "download" {
		return false
	}
	if report.Scope == "overview" {
		return report.ObjectID == ""
	}
	return report.ObjectID != "" && filepath.Base(report.ObjectID) == report.ObjectID && report.ObjectID != "." && report.ObjectID != ".."
}

func normalizeStateDirectory(stateDirectory string) string {
	if stateDirectory == "" {
		return "/var/lib/steer"
	}
	return stateDirectory
}
