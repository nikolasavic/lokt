package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckWritable_Success(t *testing.T) {
	// Create temp directory
	dir := t.TempDir()

	result := CheckWritable(dir)
	if result.Status != StatusOK {
		t.Errorf("CheckWritable() status = %v, want OK; message = %s", result.Status, result.Message)
	}
	if result.Name != "writable" {
		t.Errorf("CheckWritable() name = %q, want %q", result.Name, "writable")
	}

	// Verify test file was cleaned up
	testFile := filepath.Join(dir, "locks", ".lokt-doctor-test")
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("test file was not cleaned up: %v", err)
	}
}

func TestCheckWritable_NotWritable(t *testing.T) {
	// Create a read-only directory
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0500); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}

	result := CheckWritable(dir)
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() on read-only dir: status = %v, want Fail", result.Status)
	}
}

func TestCheckClock_ReasonableTime(t *testing.T) {
	result := CheckClock()
	if result.Status != StatusOK {
		t.Errorf("CheckClock() status = %v, want OK; message = %s", result.Status, result.Message)
	}
	if result.Name != "clock" {
		t.Errorf("CheckClock() name = %q, want %q", result.Name, "clock")
	}
}

func TestOverall(t *testing.T) {
	tests := []struct {
		name    string
		results []CheckResult
		want    Status
	}{
		{
			name:    "all ok",
			results: []CheckResult{{Status: StatusOK}, {Status: StatusOK}},
			want:    StatusOK,
		},
		{
			name:    "one warn",
			results: []CheckResult{{Status: StatusOK}, {Status: StatusWarn}},
			want:    StatusWarn,
		},
		{
			name:    "one fail",
			results: []CheckResult{{Status: StatusOK}, {Status: StatusFail}},
			want:    StatusFail,
		},
		{
			name:    "fail trumps warn",
			results: []CheckResult{{Status: StatusWarn}, {Status: StatusFail}},
			want:    StatusFail,
		},
		{
			name:    "empty",
			results: []CheckResult{},
			want:    StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Overall(tt.results); got != tt.want {
				t.Errorf("Overall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckLegacyFreezes_None(t *testing.T) {
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Create a regular lock (not a freeze)
	if err := os.WriteFile(filepath.Join(locksDir, "build.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	result := CheckLegacyFreezes(dir)
	if result.Status != StatusOK {
		t.Errorf("CheckLegacyFreezes() status = %v, want OK; message = %s", result.Status, result.Message)
	}
	if result.Name != "legacy_freezes" {
		t.Errorf("CheckLegacyFreezes() name = %q, want %q", result.Name, "legacy_freezes")
	}
}

func TestCheckLegacyFreezes_Present(t *testing.T) {
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Create legacy freeze files
	for _, name := range []string{"freeze-deploy.json", "freeze-build.json"} {
		if err := os.WriteFile(filepath.Join(locksDir, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Also a regular lock that should not be counted
	if err := os.WriteFile(filepath.Join(locksDir, "build.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	result := CheckLegacyFreezes(dir)
	if result.Status != StatusWarn {
		t.Errorf("CheckLegacyFreezes() status = %v, want Warn", result.Status)
	}
	if result.Message == "" {
		t.Error("CheckLegacyFreezes() message is empty, want warning with count")
	}
	// Should mention count of 2
	if !strings.Contains(result.Message, "2 legacy freeze") {
		t.Errorf("CheckLegacyFreezes() message = %q, want mention of 2 files", result.Message)
	}
}

func TestCheckLegacyFreezes_MissingDir(t *testing.T) {
	dir := t.TempDir()
	// Don't create locks/ directory at all

	result := CheckLegacyFreezes(dir)
	if result.Status != StatusOK {
		t.Errorf("CheckLegacyFreezes() status = %v, want OK for missing dir", result.Status)
	}
}

func TestStatus_Constants(t *testing.T) {
	// Verify status constants have expected string values
	if StatusOK != "ok" {
		t.Errorf("StatusOK = %q, want %q", StatusOK, "ok")
	}
	if StatusWarn != "warn" {
		t.Errorf("StatusWarn = %q, want %q", StatusWarn, "warn")
	}
	if StatusFail != "fail" {
		t.Errorf("StatusFail = %q, want %q", StatusFail, "fail")
	}
}

func TestCheckWritable_ExistingTestFile(t *testing.T) {
	// Create a temp dir with a leftover test file (simulates incomplete previous run)
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(locksDir, ".lokt-doctor-test")
	if err := os.WriteFile(testFile, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	// CheckWritable should clean up and succeed
	result := CheckWritable(dir)
	if result.Status != StatusOK {
		t.Errorf("CheckWritable() with existing test file: status = %v, want OK; message = %s",
			result.Status, result.Message)
	}
}

func TestCheckWritable_ExistingTestFile_RetryFails(t *testing.T) {
	// Create a temp dir with a leftover test file, then make dir read-only
	// so the retry after removing the existing file also fails
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(locksDir, ".lokt-doctor-test")
	if err := os.WriteFile(testFile, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	// Make dir read-only so remove fails and retry fails
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(locksDir, 0700)
	})

	result := CheckWritable(dir)
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() retry-fail: status = %v, want Fail; message = %s",
			result.Status, result.Message)
	}
}

func TestCheckWritable_CannotCreateDir(t *testing.T) {
	// Point to a path that cannot have directories created
	result := CheckWritable("/nonexistent/path/that/cannot/exist")
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() on invalid path: status = %v, want Fail", result.Status)
	}
	if result.Message == "" {
		t.Error("CheckWritable() on invalid path: message is empty")
	}
}

func TestCheckWritable_WriteError(t *testing.T) {
	old := writeStringFn
	defer func() { writeStringFn = old }()
	writeStringFn = func(_ *os.File, _ string) error {
		return fmt.Errorf("simulated write error")
	}

	dir := t.TempDir()
	result := CheckWritable(dir)
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() write error: status = %v, want Fail", result.Status)
	}
	if !strings.Contains(result.Message, "cannot write") {
		t.Errorf("CheckWritable() write error: message = %q, want 'cannot write'", result.Message)
	}
}

func TestCheckWritable_SyncError(t *testing.T) {
	old := syncFileFn
	defer func() { syncFileFn = old }()
	syncFileFn = func(_ *os.File) error {
		return fmt.Errorf("simulated sync error")
	}

	dir := t.TempDir()
	result := CheckWritable(dir)
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() sync error: status = %v, want Fail", result.Status)
	}
	if !strings.Contains(result.Message, "cannot sync") {
		t.Errorf("CheckWritable() sync error: message = %q, want 'cannot sync'", result.Message)
	}
}

func TestCheckWritable_RemoveError(t *testing.T) {
	old := removeFileFn
	defer func() { removeFileFn = old }()
	removeFileFn = func(_ string) error {
		return fmt.Errorf("simulated remove error")
	}

	dir := t.TempDir()
	result := CheckWritable(dir)
	if result.Status != StatusFail {
		t.Errorf("CheckWritable() remove error: status = %v, want Fail", result.Status)
	}
	if !strings.Contains(result.Message, "cannot remove") {
		t.Errorf("CheckWritable() remove error: message = %q, want 'cannot remove'", result.Message)
	}
}

func TestCheckClockYear_Past(t *testing.T) {
	result := checkClockYear(2019)
	if result.Status != StatusWarn {
		t.Errorf("checkClockYear(2019) status = %v, want Warn", result.Status)
	}
	if result.Message == "" {
		t.Error("checkClockYear(2019) message is empty")
	}
}

func TestCheckClockYear_Future(t *testing.T) {
	result := checkClockYear(2101)
	if result.Status != StatusWarn {
		t.Errorf("checkClockYear(2101) status = %v, want Warn", result.Status)
	}
	if result.Message == "" {
		t.Error("checkClockYear(2101) message is empty")
	}
}

func TestCheckClockYear_OK(t *testing.T) {
	result := checkClockYear(2025)
	if result.Status != StatusOK {
		t.Errorf("checkClockYear(2025) status = %v, want OK", result.Status)
	}
	if result.Name != "clock" {
		t.Errorf("checkClockYear() name = %q, want %q", result.Name, "clock")
	}
}

func TestCheckClockYear_Boundary(t *testing.T) {
	// Exactly 2020 should be OK
	result := checkClockYear(2020)
	if result.Status != StatusOK {
		t.Errorf("checkClockYear(2020) status = %v, want OK", result.Status)
	}
	// Exactly 2100 should be OK
	result = checkClockYear(2100)
	if result.Status != StatusOK {
		t.Errorf("checkClockYear(2100) status = %v, want OK", result.Status)
	}
}

func TestCheckLegacyFreezes_SubdirIgnored(t *testing.T) {
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory that looks like a freeze file (should be ignored)
	subdir := filepath.Join(locksDir, "freeze-test.json")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatal(err)
	}

	result := CheckLegacyFreezes(dir)
	if result.Status != StatusOK {
		t.Errorf("CheckLegacyFreezes() with subdir: status = %v, want OK (subdirs should be ignored)",
			result.Status)
	}
}
