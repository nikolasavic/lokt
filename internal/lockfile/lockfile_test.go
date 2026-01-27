package lockfile

import (
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
