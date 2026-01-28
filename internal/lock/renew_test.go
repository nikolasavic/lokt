package lock

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestRenew_Success(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "renew-test", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Read initial timestamp
	path := filepath.Join(root, "locks", "renew-test.json")
	initial, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read initial lock error = %v", err)
	}

	// Small delay to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// Renew should succeed
	err = Renew(root, "renew-test", RenewOptions{})
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	// Read updated lock
	updated, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read updated lock error = %v", err)
	}

	// Timestamp should be updated
	if !updated.AcquiredAt.After(initial.AcquiredAt) {
		t.Errorf("AcquiredAt not updated: initial=%v, updated=%v", initial.AcquiredAt, updated.AcquiredAt)
	}

	// Other fields should remain the same
	if updated.Owner != initial.Owner {
		t.Errorf("Owner changed: %q -> %q", initial.Owner, updated.Owner)
	}
	if updated.Host != initial.Host {
		t.Errorf("Host changed: %q -> %q", initial.Host, updated.Host)
	}
	if updated.PID != initial.PID {
		t.Errorf("PID changed: %d -> %d", initial.PID, updated.PID)
	}
	if updated.TTLSec != initial.TTLSec {
		t.Errorf("TTLSec changed: %d -> %d", initial.TTLSec, updated.TTLSec)
	}
}

func TestRenew_NotFound(t *testing.T) {
	root := t.TempDir()

	// Renew should fail for non-existent lock
	err := Renew(root, "nonexistent", RenewOptions{})
	if err == nil {
		t.Fatal("Renew() should fail for non-existent lock")
	}
	if !os.IsNotExist(errors.Unwrap(err)) {
		// The error should indicate the lock doesn't exist
		if !errors.Is(err, os.ErrNotExist) && err.Error() != "read lock: open "+filepath.Join(root, "locks", "nonexistent.json")+": no such file or directory" {
			// Just check that there's an error related to reading
			if !containsString(err.Error(), "read lock") {
				t.Errorf("Renew() error = %v, want error about lock not found", err)
			}
		}
	}
}

func TestRenew_NotOwner(t *testing.T) {
	root := t.TempDir()

	// Create a lock with different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "other-owner",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     300,
	}
	path := filepath.Join(locksDir, "other-owner.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Renew should fail for non-owner
	err := Renew(root, "other-owner", RenewOptions{})
	if err == nil {
		t.Fatal("Renew() should fail for non-owner")
	}

	if !errors.Is(err, ErrLockStolen) {
		t.Errorf("Renew() error = %v, want ErrLockStolen", err)
	}
}

func TestRenew_DifferentHost(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "host-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Modify the lock to have different host
	path := filepath.Join(root, "locks", "host-test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	lock.Host = "different-host.example.com"
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Renew should fail for different host
	err = Renew(root, "host-test", RenewOptions{})
	if err == nil {
		t.Fatal("Renew() should fail for different host")
	}

	if !errors.Is(err, ErrLockStolen) {
		t.Errorf("Renew() error = %v, want ErrLockStolen", err)
	}
}

func TestRenew_DifferentPID(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "pid-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Modify the lock to have different PID
	path := filepath.Join(root, "locks", "pid-test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	lock.PID = 99999999 // Different PID
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Renew should fail for different PID
	err = Renew(root, "pid-test", RenewOptions{})
	if err == nil {
		t.Fatal("Renew() should fail for different PID")
	}

	if !errors.Is(err, ErrLockStolen) {
		t.Errorf("Renew() error = %v, want ErrLockStolen", err)
	}
}

func TestRenew_AuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Acquire first (without auditor to avoid extra event)
	err := Acquire(root, "renew-audit", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Renew with auditor
	err = Renew(root, "renew-audit", RenewOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	events := readRenewAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventRenew {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventRenew)
	}
	if e.Name != "renew-audit" {
		t.Errorf("Name = %q, want %q", e.Name, "renew-audit")
	}
	if e.TTLSec != 300 {
		t.Errorf("TTLSec = %d, want 300", e.TTLSec)
	}
}

func TestRenew_NilAuditorDoesNotPanic(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "nil-auditor-renew", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Renew with nil auditor should not panic
	err = Renew(root, "nil-auditor-renew", RenewOptions{Auditor: nil})
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
}

// readRenewAuditEvents reads all events from the audit log file.
func readRenewAuditEvents(t *testing.T, rootDir string) []audit.Event {
	t.Helper()
	path := filepath.Join(rootDir, "audit.log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("Open audit.log: %v", err)
	}
	defer func() { _ = f.Close() }()

	var events []audit.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e audit.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("Unmarshal audit event: %v", err)
		}
		events = append(events, e)
	}
	return events
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringImpl(s, substr))
}

func containsStringImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
