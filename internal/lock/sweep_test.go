package lock

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

func setupSweepRoot(t *testing.T) string {
	t.Helper()
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "locks"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "freezes"), 0750); err != nil {
		t.Fatal(err)
	}
	return rootDir
}

func writeLock(t *testing.T, dir, name string, lf *lockfile.Lock) {
	t.Helper()
	if err := lockfile.Write(filepath.Join(dir, name+".json"), lf); err != nil {
		t.Fatal(err)
	}
}

// readSweepAuditEvents reads audit events from the audit log.
// Separate from readAuditEvents in acquire_test.go to avoid redeclaration.
func readSweepAuditEvents(t *testing.T, rootDir string) []audit.Event {
	t.Helper()
	path := filepath.Join(rootDir, "audit.log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var events []audit.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e audit.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal audit event: %v", err)
		}
		events = append(events, e)
	}
	return events
}

func TestSweep_EmptyDir(t *testing.T) {
	rootDir := setupSweepRoot(t)

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_NonExistentDir(t *testing.T) {
	rootDir := t.TempDir() // no locks/ or freezes/ subdirs

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_ExpiredTTL(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	expired := time.Now().Add(-2 * time.Minute)
	writeLock(t, locksDir, "stale", &lockfile.Lock{
		Version:    1,
		Name:       "stale",
		Owner:      "other",
		Host:       "other-host",
		PID:        12345,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired, // already passed
	})

	auditor := audit.NewWriter(rootDir)
	pruned, errs := PruneAllExpired(rootDir, auditor)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	// Lock file should be gone
	if _, err := os.Stat(filepath.Join(locksDir, "stale.json")); !os.IsNotExist(err) {
		t.Error("stale lock file should have been removed")
	}

	// Audit event should exist
	events := readSweepAuditEvents(t, rootDir)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Event != audit.EventAutoPrune {
		t.Errorf("event type = %q, want %q", events[0].Event, audit.EventAutoPrune)
	}
	if events[0].Name != "stale" {
		t.Errorf("event name = %q, want %q", events[0].Name, "stale")
	}
}

func TestSweep_DeadPID_SameHost(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	hostname, _ := os.Hostname()
	writeLock(t, locksDir, "dead-pid", &lockfile.Lock{
		Version:    1,
		Name:       "dead-pid",
		Owner:      "departed",
		Host:       hostname,
		PID:        999999, // almost certainly not running
		AcquiredAt: time.Now(),
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_LiveLock_Untouched(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	hostname, _ := os.Hostname()
	writeLock(t, locksDir, "alive", &lockfile.Lock{
		Version:    1,
		Name:       "alive",
		Owner:      "me",
		Host:       hostname,
		PID:        os.Getpid(), // this process is alive
		AcquiredAt: time.Now(),
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	// Lock should still exist
	if _, err := os.Stat(filepath.Join(locksDir, "alive.json")); err != nil {
		t.Error("live lock should not be removed")
	}
}

func TestSweep_CrossHost_NoTTL_Untouched(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	writeLock(t, locksDir, "remote", &lockfile.Lock{
		Version:    1,
		Name:       "remote",
		Owner:      "remote-agent",
		Host:       "other-host.example.com",
		PID:        42,
		AcquiredAt: time.Now(),
		// No TTL — intentionally permanent
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	if _, err := os.Stat(filepath.Join(locksDir, "remote.json")); err != nil {
		t.Error("cross-host no-TTL lock should not be removed")
	}
}

func TestSweep_CorruptedFile(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	// Write garbage data
	if err := os.WriteFile(filepath.Join(locksDir, "corrupt.json"), []byte("{bad json"), 0600); err != nil {
		t.Fatal(err)
	}

	auditor := audit.NewWriter(rootDir)
	pruned, errs := PruneAllExpired(rootDir, auditor)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	events := readSweepAuditEvents(t, rootDir)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	reason, ok := events[0].Extra["sweep_reason"]
	if !ok || reason != "corrupted" {
		t.Errorf("sweep_reason = %v, want 'corrupted'", reason)
	}
}

func TestSweep_UnsupportedVersion_Untouched(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	// Write a lock with a future version
	data := []byte(`{"version":99,"name":"future","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(locksDir, "future.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	if _, err := os.Stat(filepath.Join(locksDir, "future.json")); err != nil {
		t.Error("unsupported version lock should not be removed")
	}
}

func TestSweep_ExpiredFreeze(t *testing.T) {
	rootDir := setupSweepRoot(t)
	freezesDir := filepath.Join(rootDir, "freezes")

	expired := time.Now().Add(-5 * time.Minute)
	writeLock(t, freezesDir, "deploy", &lockfile.Lock{
		Version:    1,
		Name:       "deploy",
		Owner:      "admin",
		Host:       "ci-host",
		PID:        1,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_MixedDirectory(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	hostname, _ := os.Hostname()
	expired := time.Now().Add(-2 * time.Minute)

	// Stale: expired TTL
	writeLock(t, locksDir, "expired", &lockfile.Lock{
		Version:    1,
		Name:       "expired",
		Owner:      "old",
		Host:       "other-host",
		PID:        1,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	// Stale: dead PID
	writeLock(t, locksDir, "dead", &lockfile.Lock{
		Version:    1,
		Name:       "dead",
		Owner:      "gone",
		Host:       hostname,
		PID:        999998,
		AcquiredAt: time.Now(),
	})

	// Healthy: live process
	writeLock(t, locksDir, "healthy", &lockfile.Lock{
		Version:    1,
		Name:       "healthy",
		Owner:      "me",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
	})

	// Healthy: cross-host, no TTL
	writeLock(t, locksDir, "remote", &lockfile.Lock{
		Version:    1,
		Name:       "remote",
		Owner:      "remote",
		Host:       "far-away",
		PID:        1,
		AcquiredAt: time.Now(),
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	// Stale locks gone
	for _, name := range []string{"expired", "dead"} {
		if _, err := os.Stat(filepath.Join(locksDir, name+".json")); !os.IsNotExist(err) {
			t.Errorf("lock %q should have been removed", name)
		}
	}

	// Healthy locks remain
	for _, name := range []string{"healthy", "remote"} {
		if _, err := os.Stat(filepath.Join(locksDir, name+".json")); err != nil {
			t.Errorf("lock %q should still exist", name)
		}
	}
}

func TestSweep_NonJSONFiles_Skipped(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	// Write non-json files
	if err := os.WriteFile(filepath.Join(locksDir, "readme.txt"), []byte("ignore me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locksDir, ".lock-tmp.tmp"), []byte("temp"), 0600); err != nil {
		t.Fatal(err)
	}

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_EmptyFile_Skipped(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	// Empty JSON file (race condition: file created but not yet written)
	if err := os.WriteFile(filepath.Join(locksDir, "empty.json"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (empty file should be skipped)", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}

	// File should still exist
	if _, err := os.Stat(filepath.Join(locksDir, "empty.json")); err != nil {
		t.Error("empty file should not be removed (may be in-progress write)")
	}
}

func TestSweep_AuditEvents_IncludeHolderInfo(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	expired := time.Now().Add(-10 * time.Minute)
	writeLock(t, locksDir, "audited", &lockfile.Lock{
		Version:    1,
		Name:       "audited",
		Owner:      "agent-007",
		Host:       "mi6.example.com",
		PID:        7,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	auditor := audit.NewWriter(rootDir)
	pruned, _ := PruneAllExpired(rootDir, auditor)
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}

	events := readSweepAuditEvents(t, rootDir)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}

	e := events[0]
	if e.Event != audit.EventAutoPrune {
		t.Errorf("event = %q, want %q", e.Event, audit.EventAutoPrune)
	}
	if e.Extra["pruned_owner"] != "agent-007" {
		t.Errorf("pruned_owner = %v, want 'agent-007'", e.Extra["pruned_owner"])
	}
	if e.Extra["pruned_host"] != "mi6.example.com" {
		t.Errorf("pruned_host = %v, want 'mi6.example.com'", e.Extra["pruned_host"])
	}
	// PID is stored as float64 in JSON round-trip
	if e.Extra["pruned_pid"] != float64(7) {
		t.Errorf("pruned_pid = %v, want 7", e.Extra["pruned_pid"])
	}
	if e.Extra["sweep_reason"] != "expired" {
		t.Errorf("sweep_reason = %v, want 'expired'", e.Extra["sweep_reason"])
	}
}
