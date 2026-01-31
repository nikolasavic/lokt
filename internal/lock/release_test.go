package lock

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestRelease(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Release should succeed
	err = Release(root, "test", ReleaseOptions{})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Lock file should be gone
	path := filepath.Join(root, "locks", "test.json")
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted")
	}
}

func TestReleaseNotFound(t *testing.T) {
	root := t.TempDir()

	err := Release(root, "nonexistent", ReleaseOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Release() error = %v, want ErrNotFound", err)
	}
}

func TestReleaseNotOwner(t *testing.T) {
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
	}
	path := filepath.Join(locksDir, "other-owner.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Release without force should fail
	err := Release(root, "other-owner", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() should fail for non-owner")
	}

	var notOwner *NotOwnerError
	if !errors.As(err, &notOwner) {
		t.Fatalf("error should be *NotOwnerError, got %T", err)
	}

	if !errors.Is(err, ErrNotOwner) {
		t.Error("error should wrap ErrNotOwner")
	}
}

func TestReleaseForce(t *testing.T) {
	root := t.TempDir()

	// Create a lock with different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "force-test",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	path := filepath.Join(locksDir, "force-test.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Force release should succeed
	err := Release(root, "force-test", ReleaseOptions{Force: true})
	if err != nil {
		t.Fatalf("Release(force=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after force release")
	}
}

func TestReleaseBreakStale_ExpiredTTL(t *testing.T) {
	root := t.TempDir()

	// Create a lock with expired TTL
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "stale-ttl",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-2 * time.Hour), // 2 hours ago
		TTLSec:     60,                             // 1 minute TTL (expired)
	}
	path := filepath.Join(locksDir, "stale-ttl.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should succeed for expired TTL
	err := Release(root, "stale-ttl", ReleaseOptions{BreakStale: true})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after break-stale")
	}
}

func TestReleaseBreakStale_DeadPID(t *testing.T) {
	root := t.TempDir()

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Create a lock with dead PID on same host
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "stale-pid",
		Owner:      "someone-else",
		Host:       hostname,
		PID:        99999999, // Very unlikely to exist
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "stale-pid.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should succeed for dead PID
	err = Release(root, "stale-pid", ReleaseOptions{BreakStale: true})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after break-stale")
	}
}

func TestReleaseBreakStale_NotStale(t *testing.T) {
	root := t.TempDir()

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Create a lock with alive PID on same host
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "not-stale",
		Owner:      "someone-else",
		Host:       hostname,
		PID:        os.Getpid(), // Current process - definitely alive
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "not-stale.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should fail for non-stale lock
	err = Release(root, "not-stale", ReleaseOptions{BreakStale: true})
	if err == nil {
		t.Fatal("Release(BreakStale=true) should fail for non-stale lock")
	}

	var notStale *NotStaleError
	if !errors.As(err, &notStale) {
		t.Fatalf("error should be *NotStaleError, got %T", err)
	}

	if !errors.Is(err, ErrNotStale) {
		t.Error("error should wrap ErrNotStale")
	}

	// Lock file should still exist
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		t.Error("Lock file should NOT be deleted for non-stale lock")
	}
}

func TestReleaseBreakStale_CrossHost(t *testing.T) {
	root := t.TempDir()

	// Create a lock on different host (cannot verify PID)
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "cross-host",
		Owner:      "someone-else",
		Host:       "definitely-not-this-host.example.com",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "cross-host.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should fail for cross-host lock without TTL
	err := Release(root, "cross-host", ReleaseOptions{BreakStale: true})
	if err == nil {
		t.Fatal("Release(BreakStale=true) should fail for cross-host lock without TTL")
	}

	var notStale *NotStaleError
	if !errors.As(err, &notStale) {
		t.Fatalf("error should be *NotStaleError, got %T", err)
	}

	// Lock file should still exist
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		t.Error("Lock file should NOT be deleted for cross-host lock")
	}
}

// readReleaseAuditEvents reads all events from the audit log file.
func readReleaseAuditEvents(t *testing.T, rootDir string) []audit.Event {
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

func TestReleaseEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Acquire first
	err := Acquire(root, "release-audit", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Release with auditor
	err = Release(root, "release-audit", ReleaseOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	events := readReleaseAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventRelease {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventRelease)
	}
	if e.Name != "release-audit" {
		t.Errorf("Name = %q, want %q", e.Name, "release-audit")
	}
}

func TestReleaseForceEmitsForceBreakEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create a lock with different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "force-audit",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	path := filepath.Join(locksDir, "force-audit.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Force release with auditor
	err := Release(root, "force-audit", ReleaseOptions{Force: true, Auditor: auditor})
	if err != nil {
		t.Fatalf("Release(force=true) error = %v", err)
	}

	events := readReleaseAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventForceBreak {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventForceBreak)
	}
}

func TestReleaseBreakStaleEmitsStaleBreakEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create a lock with expired TTL
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "stale-audit",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-2 * time.Hour),
		TTLSec:     60, // Expired
	}
	path := filepath.Join(locksDir, "stale-audit.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale release with auditor
	err := Release(root, "stale-audit", ReleaseOptions{BreakStale: true, Auditor: auditor})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	events := readReleaseAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventStaleBreak {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventStaleBreak)
	}
}

func TestReleaseNilAuditorDoesNotPanic(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "nil-auditor-release", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Release with nil auditor should not panic
	err = Release(root, "nil-auditor-release", ReleaseOptions{Auditor: nil})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

// Corrupted lock file tests for L-203

func TestReleaseBreakStale_CorruptedLock(t *testing.T) {
	root := t.TempDir()

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-stale.json")
	if err := os.WriteFile(path, []byte("not json{{{"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// BreakStale should remove corrupted lock
	err := Release(root, "corrupt-stale", ReleaseOptions{BreakStale: true})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v, want nil for corrupted lock", err)
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Corrupted lock file should be deleted after break-stale")
	}
}

func TestReleaseForce_CorruptedLock(t *testing.T) {
	root := t.TempDir()

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-force.json")
	if err := os.WriteFile(path, []byte(`[1,2,3]`), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Force release should remove corrupted lock
	err := Release(root, "corrupt-force", ReleaseOptions{Force: true})
	if err != nil {
		t.Fatalf("Release(Force=true) error = %v, want nil for corrupted lock", err)
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Corrupted lock file should be deleted after force release")
	}
}

func TestReleaseNormal_CorruptedLockReturnsError(t *testing.T) {
	root := t.TempDir()

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-normal.json")
	if err := os.WriteFile(path, []byte("garbage"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Normal release should return error for corrupted lock
	err := Release(root, "corrupt-normal", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() should fail for corrupted lock without force/break-stale")
	}

	if !errors.Is(err, lockfile.ErrCorrupted) {
		t.Errorf("Release() error should wrap ErrCorrupted, got %v", err)
	}

	// File should still exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Corrupted lock file should NOT be deleted during normal release")
	}
}

func TestReleaseBreakStale_CorruptedLockEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-audit.json")
	if err := os.WriteFile(path, []byte("bad data"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// BreakStale with auditor
	err := Release(root, "corrupt-audit", ReleaseOptions{BreakStale: true, Auditor: auditor})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	events := readReleaseAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventCorruptBreak {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventCorruptBreak)
	}
	if e.Name != "corrupt-audit" {
		t.Errorf("Name = %q, want %q", e.Name, "corrupt-audit")
	}
}

// createTestLock is a helper that creates a lock file with the given name and owner.
func createTestLock(t *testing.T, rootDir, name, owner string) {
	t.Helper()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	lf := &lockfile.Lock{
		Name:       name,
		Owner:      owner,
		Host:       "test-host",
		PID:        12345,
		AcquiredAt: time.Now(),
	}
	path := filepath.Join(locksDir, name+".json")
	if err := lockfile.Write(path, lf); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}
}

func TestReleaseByOwner_MatchAll(t *testing.T) {
	root := t.TempDir()

	createTestLock(t, root, "lock-a", "agent-1")
	createTestLock(t, root, "lock-b", "agent-1")
	createTestLock(t, root, "lock-c", "agent-1")

	released, err := ReleaseByOwner(root, "agent-1", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}

	sort.Strings(released)
	if len(released) != 3 {
		t.Fatalf("released = %v, want 3 locks", released)
	}
	want := []string{"lock-a", "lock-b", "lock-c"}
	for i, name := range want {
		if released[i] != name {
			t.Errorf("released[%d] = %q, want %q", i, released[i], name)
		}
	}

	// All lock files should be gone
	for _, name := range want {
		path := filepath.Join(root, "locks", name+".json")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("lock file %q should be deleted", name)
		}
	}
}

func TestReleaseByOwner_MatchSome(t *testing.T) {
	root := t.TempDir()

	createTestLock(t, root, "mine-1", "agent-1")
	createTestLock(t, root, "theirs", "agent-2")
	createTestLock(t, root, "mine-2", "agent-1")

	released, err := ReleaseByOwner(root, "agent-1", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}

	sort.Strings(released)
	if len(released) != 2 {
		t.Fatalf("released = %v, want 2 locks", released)
	}
	if released[0] != "mine-1" || released[1] != "mine-2" {
		t.Errorf("released = %v, want [mine-1, mine-2]", released)
	}

	// Other owner's lock should still exist
	path := filepath.Join(root, "locks", "theirs.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("lock owned by agent-2 should NOT be deleted")
	}
}

func TestReleaseByOwner_MatchNone(t *testing.T) {
	root := t.TempDir()

	createTestLock(t, root, "lock-a", "agent-1")
	createTestLock(t, root, "lock-b", "agent-1")

	released, err := ReleaseByOwner(root, "nobody", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want empty", released)
	}

	// All locks should still exist
	for _, name := range []string{"lock-a", "lock-b"} {
		path := filepath.Join(root, "locks", name+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("lock %q should still exist", name)
		}
	}
}

func TestReleaseByOwner_MissingLocksDir(t *testing.T) {
	root := t.TempDir()
	// Don't create locks directory

	released, err := ReleaseByOwner(root, "anyone", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v, want nil", err)
	}
	if released != nil {
		t.Errorf("released = %v, want nil", released)
	}
}

func TestReleaseByOwner_SkipsCorruptedLocks(t *testing.T) {
	root := t.TempDir()

	createTestLock(t, root, "good", "agent-1")

	// Create a corrupted lock file
	corruptPath := filepath.Join(root, "locks", "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("not json"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	released, err := ReleaseByOwner(root, "agent-1", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}
	if len(released) != 1 || released[0] != "good" {
		t.Errorf("released = %v, want [good]", released)
	}

	// Corrupted file should still exist (not our owner, can't read it)
	if _, err := os.Stat(corruptPath); os.IsNotExist(err) {
		t.Error("corrupted lock file should still exist")
	}
}

func TestReleaseByOwner_EmitsAuditEvents(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	createTestLock(t, root, "audit-a", "agent-1")
	createTestLock(t, root, "audit-b", "agent-1")

	released, err := ReleaseByOwner(root, "agent-1", ReleaseOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}
	if len(released) != 2 {
		t.Fatalf("released = %v, want 2 locks", released)
	}

	events := readReleaseAuditEvents(t, root)
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events, got %d", len(events))
	}
	for _, e := range events {
		if e.Event != audit.EventRelease {
			t.Errorf("Event = %q, want %q", e.Event, audit.EventRelease)
		}
	}
}

func TestReleaseByOwner_SkipsNonJSONFiles(t *testing.T) {
	root := t.TempDir()

	createTestLock(t, root, "real", "agent-1")

	// Create a non-JSON file in locks directory
	nonJSON := filepath.Join(root, "locks", "readme.txt")
	if err := os.WriteFile(nonJSON, []byte("not a lock"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	released, err := ReleaseByOwner(root, "agent-1", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}
	if len(released) != 1 || released[0] != "real" {
		t.Errorf("released = %v, want [real]", released)
	}
}
