package lockfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockIsExpired(t *testing.T) {
	tests := []struct {
		name     string
		lock     Lock
		expected bool
	}{
		{
			name:     "no TTL",
			lock:     Lock{TTLSec: 0},
			expected: false,
		},
		{
			name:     "not expired",
			lock:     Lock{TTLSec: 3600, AcquiredAt: time.Now()},
			expected: false,
		},
		{
			name:     "expired",
			lock:     Lock{TTLSec: 1, AcquiredAt: time.Now().Add(-2 * time.Second)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lock.IsExpired(); got != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	lock := &Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		AcquiredAt: time.Now().Truncate(time.Millisecond),
		TTLSec:     300,
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.Name != lock.Name {
		t.Errorf("Name = %q, want %q", got.Name, lock.Name)
	}
	if got.Owner != lock.Owner {
		t.Errorf("Owner = %q, want %q", got.Owner, lock.Owner)
	}
	if got.PID != lock.PID {
		t.Errorf("PID = %d, want %d", got.PID, lock.PID)
	}
	if got.TTLSec != lock.TTLSec {
		t.Errorf("TTLSec = %d, want %d", got.TTLSec, lock.TTLSec)
	}
}

func TestReadNotFound(t *testing.T) {
	_, err := Read("/nonexistent/path/lock.json")
	if err == nil {
		t.Error("Read() expected error for nonexistent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Read() error = %v, want os.IsNotExist", err)
	}
}

func TestReadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lock")

	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path)
	if err == nil {
		t.Error("Read() expected error for invalid JSON")
	}
	if !errors.Is(err, ErrCorrupted) {
		t.Errorf("Read() error should wrap ErrCorrupted, got %v", err)
	}
}

func TestReadCorruptedVariants(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr error
	}{
		{"partial JSON", []byte(`{"name":"test"`), ErrCorrupted},
		{"binary garbage", []byte{0x00, 0xFF, 0xFE, 0x89}, ErrCorrupted},
		{"array instead of object", []byte(`[1,2,3]`), ErrCorrupted},
		{"plain text", []byte("hello world"), ErrCorrupted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "corrupt.lock")
			if err := os.WriteFile(path, tt.content, 0600); err != nil {
				t.Fatal(err)
			}

			_, err := Read(path)
			if err == nil {
				t.Fatal("Read() expected error for corrupted content")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Read() error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

func TestReadEmptyFileIsNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.lock")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read() expected error for empty file")
	}
	// Empty file should NOT be ErrCorrupted (it's a race, not corruption)
	if errors.Is(err, ErrCorrupted) {
		t.Error("Read() empty file should not return ErrCorrupted")
	}
}

func TestReadNotFoundIsNotCorrupted(t *testing.T) {
	_, err := Read("/nonexistent/path/lock.json")
	if err == nil {
		t.Fatal("Read() expected error for nonexistent file")
	}
	if errors.Is(err, ErrCorrupted) {
		t.Error("Read() missing file should not return ErrCorrupted")
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"simple", "deploy", false},
		{"with-hyphen", "deploy-prod", false},
		{"with-underscore", "deploy_prod", false},
		{"with-dot", "deploy.prod", false},
		{"alphanumeric", "deploy123", false},
		{"leading-dot", ".hidden", false},
		{"complex-valid", "my-app_v1.2.3", false},

		// Invalid names
		{"empty", "", true},
		{"absolute-path", "/tmp/evil", true},
		{"path-traversal", "../etc/passwd", true},
		{"path-traversal-mid", "foo/../bar", true},
		{"double-dot-only", "..", true},
		{"contains-double-dot", "foo..bar", true},
		{"space", "foo bar", true},
		{"semicolon", "foo;rm -rf", true},
		{"pipe", "foo|cat", true},
		{"ampersand", "foo&bar", true},
		{"backtick", "foo`id`", true},
		{"dollar", "foo$HOME", true},
		{"slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidName) {
				t.Errorf("ValidateName(%q) error should wrap ErrInvalidName, got %v", tt.input, err)
			}
		})
	}
}

func TestWriteAndRead_PIDStartNS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	lock := &Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		PIDStartNS: 1706400000000000000, // example nanosecond timestamp
		AcquiredAt: time.Now().Truncate(time.Millisecond),
		TTLSec:     300,
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.PIDStartNS != lock.PIDStartNS {
		t.Errorf("PIDStartNS = %d, want %d", got.PIDStartNS, lock.PIDStartNS)
	}
}

func TestRead_BackwardCompat_NoPIDStartNS(t *testing.T) {
	// Simulate an old lock file without pid_start_ns field.
	dir := t.TempDir()
	path := filepath.Join(dir, "old.lock")

	oldJSON := `{
  "name": "test",
  "owner": "testuser",
  "host": "testhost",
  "pid": 12345,
  "acquired_ts": "2026-01-28T10:00:00Z",
  "ttl_sec": 300
}`
	if err := os.WriteFile(path, []byte(oldJSON), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.PIDStartNS != 0 {
		t.Errorf("PIDStartNS should be 0 for old lock, got %d", got.PIDStartNS)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}

func TestWriteOmitsPIDStartNS_WhenZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nopid.lock")

	lock := &Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		PIDStartNS: 0, // zero — should be omitted from JSON
		AcquiredAt: time.Now().Truncate(time.Millisecond),
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if _, exists := raw["pid_start_ns"]; exists {
		t.Error("pid_start_ns should be omitted when zero (omitempty)")
	}
}

func TestWriteAndRead_Version(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		AcquiredAt: time.Now().Truncate(time.Millisecond),
		TTLSec:     300,
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.Version != CurrentLockfileVersion {
		t.Errorf("Version = %d, want %d", got.Version, CurrentLockfileVersion)
	}
}

func TestRead_BackwardCompat_NoVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.lock")

	// Pre-v1.0 lockfile with no version field
	oldJSON := `{
  "name": "test",
  "owner": "testuser",
  "host": "testhost",
  "pid": 12345,
  "acquired_ts": "2026-01-28T10:00:00Z",
  "ttl_sec": 300
}`
	if err := os.WriteFile(path, []byte(oldJSON), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() should accept lockfile without version, got error: %v", err)
	}

	if got.Version != 0 {
		t.Errorf("Version should be 0 for old lockfile, got %d", got.Version)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}

func TestRead_UnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.lock")

	futureJSON := `{
  "version": 99,
  "name": "test",
  "owner": "testuser",
  "host": "testhost",
  "pid": 12345,
  "acquired_ts": "2026-01-28T10:00:00Z"
}`
	if err := os.WriteFile(path, []byte(futureJSON), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read() should reject lockfile with unsupported version")
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Read() error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

func TestWriteAlwaysIncludesVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ver.lock")

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		AcquiredAt: time.Now().Truncate(time.Millisecond),
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if _, exists := raw["version"]; !exists {
		t.Error("version field should always be present in JSON (no omitempty)")
	}
}

func TestVersionIsFirstJSONField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.lock")

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "testuser",
		Host:       "testhost",
		PID:        12345,
		AcquiredAt: time.Now().Truncate(time.Millisecond),
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// In JSON output, "version" should appear before "name"
	str := string(data)
	vIdx := indexOf(str, `"version"`)
	nIdx := indexOf(str, `"name"`)

	if vIdx < 0 {
		t.Fatal("version field not found in JSON")
	}
	if nIdx < 0 {
		t.Fatal("name field not found in JSON")
	}
	if vIdx >= nIdx {
		t.Errorf("version field (pos %d) should appear before name field (pos %d) in JSON", vIdx, nIdx)
	}
}

// indexOf returns the byte offset of the first occurrence of substr in s, or -1.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
