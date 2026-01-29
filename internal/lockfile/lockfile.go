// Package lockfile handles reading, writing, and parsing lock files.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Lock represents the JSON structure of a lock file.
type Lock struct {
	Name       string    `json:"name"`
	Owner      string    `json:"owner"`
	Host       string    `json:"host"`
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_ts"`
	TTLSec     int       `json:"ttl_sec,omitempty"`
}

// IsExpired returns true if the lock has a TTL and it has elapsed.
func (l *Lock) IsExpired() bool {
	if l.TTLSec <= 0 {
		return false
	}
	return time.Since(l.AcquiredAt) > time.Duration(l.TTLSec)*time.Second
}

// Age returns the duration since the lock was acquired.
func (l *Lock) Age() time.Duration {
	return time.Since(l.AcquiredAt)
}

// ErrInvalidName is returned when a lock name fails validation.
var ErrInvalidName = errors.New("invalid lock name")

// ErrCorrupted is returned when a lock file exists but contains malformed JSON.
var ErrCorrupted = errors.New("corrupted lock file")

// validNamePattern matches allowed lock name characters: alphanumeric, dots, hyphens, underscores.
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateName checks if a lock name is safe and valid.
// Returns nil if valid, or an error describing the problem.
//
// Valid names:
//   - Contain only alphanumeric characters, dots, hyphens, and underscores
//   - Are not empty
//   - Do not contain path traversal sequences (..)
//   - Do not start with /
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidName)
	}

	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: absolute paths not allowed", ErrInvalidName)
	}

	if strings.Contains(name, "..") {
		return fmt.Errorf("%w: path traversal not allowed", ErrInvalidName)
	}

	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("%w: must contain only alphanumeric characters, dots, hyphens, and underscores", ErrInvalidName)
	}

	return nil
}

// Read parses a lock file from the given path.
func Read(path string) (*Lock, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Path is validated by caller
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		// Empty file — likely a race (file created but not yet written).
		// Return a generic error, not ErrCorrupted, so callers retry.
		return nil, fmt.Errorf("empty lock file")
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupted, err)
	}
	return &lock, nil
}

// Write atomically writes a lock file to the given path.
// Uses write-to-temp + rename for atomicity, with fsync for durability.
func Write(path string, lock *Lock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".lock-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return SyncDir(path)
}

// SyncDir fsyncs the parent directory of the given path to ensure
// the directory entry (create, rename, or delete) is durably persisted.
// Without this, a power loss could leave ghost or phantom entries.
func SyncDir(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
