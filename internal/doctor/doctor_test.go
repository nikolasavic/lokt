package doctor

import (
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

func TestCheckNetworkFS_LocalDir(t *testing.T) {
	// Test on a local temp directory - should pass
	dir := t.TempDir()

	result := CheckNetworkFS(dir)
	// Should be OK for local filesystem (though might be "unknown" on some systems)
	if result.Status == StatusFail {
		t.Errorf("CheckNetworkFS() on local dir: status = %v, message = %s", result.Status, result.Message)
	}
	if result.Name != "network_fs" {
		t.Errorf("CheckNetworkFS() name = %q, want %q", result.Name, "network_fs")
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
