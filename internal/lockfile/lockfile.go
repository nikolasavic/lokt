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

// CurrentLockfileVersion is the schema version written to all new lock files.
const CurrentLockfileVersion = 1

// Lock represents the JSON structure of a lock file.
type Lock struct {
	Version    int        `json:"version"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner"`
	Host       string     `json:"host"`
	PID        int        `json:"pid"`
	PIDStartNS int64      `json:"pid_start_ns,omitempty"`
	AcquiredAt time.Time  `json:"acquired_ts"`
	TTLSec     int        `json:"ttl_sec,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// IsExpired returns true if the lock has a TTL and it has elapsed.
// Prefers the explicit ExpiresAt timestamp when present; falls back to
// TTLSec arithmetic for lockfiles written before the expires_at field existed.
func (l *Lock) IsExpired() bool {
	if l.ExpiresAt != nil {
		return time.Now().After(*l.ExpiresAt)
	}
	if l.TTLSec <= 0 {
		return false
	}
	return time.Since(l.AcquiredAt) > time.Duration(l.TTLSec)*time.Second
}

// Remaining returns the duration until the lock expires.
// Returns zero if the lock has no TTL, is already expired, or has no expiry info.
func (l *Lock) Remaining() time.Duration {
	if l.ExpiresAt != nil {
		rem := time.Until(*l.ExpiresAt)
		if rem < 0 {
			return 0
		}
		return rem
	}
	if l.TTLSec <= 0 {
		return 0
	}
	rem := time.Duration(l.TTLSec)*time.Second - time.Since(l.AcquiredAt)
	if rem < 0 {
		return 0
	}
	return rem
}

// Age returns the duration since the lock was acquired.
func (l *Lock) Age() time.Duration {
	return time.Since(l.AcquiredAt)
}

// ErrInvalidName is returned when a lock name fails validation.
var ErrInvalidName = errors.New("invalid lock name")

// ErrCorrupted is returned when a lock file exists but contains malformed JSON.
var ErrCorrupted = errors.New("corrupted lock file")

// ErrUnsupportedVersion is returned when a lock file has a version newer than this binary supports.
var ErrUnsupportedVersion = errors.New("unsupported lockfile version")

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
	if lock.Version > CurrentLockfileVersion {
		return nil, fmt.Errorf("%w: version %d not supported (max: %d); upgrade lokt",
			ErrUnsupportedVersion, lock.Version, CurrentLockfileVersion)
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
