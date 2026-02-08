package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/stale"
)

// --- Error type .Error() methods ---

func TestHeldError_Error_WithAgentID(t *testing.T) {
	err := &HeldError{
		Lock: &lockfile.Lock{
			Name:       "build",
			Owner:      "alice",
			AgentID:    "agent-1234",
			Host:       "ci-host",
			PID:        42,
			AcquiredAt: time.Now().Add(-5 * time.Minute),
		},
	}
	msg := err.Error()
	for _, want := range []string{"build", "alice", "agent-1234", "ci-host", "42"} {
		if !strings.Contains(msg, want) {
			t.Errorf("HeldError.Error() = %q, missing %q", msg, want)
		}
	}
}

func TestHeldError_Error_WithoutAgentID(t *testing.T) {
	err := &HeldError{
		Lock: &lockfile.Lock{
			Name:       "deploy",
			Owner:      "bob",
			Host:       "prod-host",
			PID:        99,
			AcquiredAt: time.Now().Add(-10 * time.Second),
		},
	}
	msg := err.Error()
	if strings.Contains(msg, "agent:") {
		t.Errorf("HeldError.Error() should not contain 'agent:' without AgentID, got %q", msg)
	}
	for _, want := range []string{"deploy", "bob", "prod-host", "99"} {
		if !strings.Contains(msg, want) {
			t.Errorf("HeldError.Error() = %q, missing %q", msg, want)
		}
	}
}

func TestHeldError_Unwrap(t *testing.T) {
	err := &HeldError{Lock: &lockfile.Lock{Name: "test"}}
	if !errors.Is(err, ErrLockHeld) {
		t.Error("HeldError should unwrap to ErrLockHeld")
	}
}

func TestNotOwnerError_Error(t *testing.T) {
	err := &NotOwnerError{
		Lock: &lockfile.Lock{
			Name:  "deploy",
			Owner: "alice",
			Host:  "host-a",
		},
		Current: identity.Identity{
			Owner: "bob",
			Host:  "host-b",
		},
	}
	msg := err.Error()
	for _, want := range []string{"deploy", "alice", "host-a", "bob", "host-b"} {
		if !strings.Contains(msg, want) {
			t.Errorf("NotOwnerError.Error() = %q, missing %q", msg, want)
		}
	}
}

func TestNotStaleError_Error_ReasonUnknown(t *testing.T) {
	err := &NotStaleError{
		Lock: &lockfile.Lock{
			Name:  "remote-lock",
			Owner: "alice",
			Host:  "remote-host",
			PID:   42,
		},
		Reason: "unknown",
	}
	msg := err.Error()
	if !strings.Contains(msg, "cannot verify PID on remote host") {
		t.Errorf("NotStaleError.Error() with ReasonUnknown should mention remote host, got %q", msg)
	}
}

func TestNotStaleError_Error_NotStale(t *testing.T) {
	err := &NotStaleError{
		Lock: &lockfile.Lock{
			Name:  "local-lock",
			Owner: "alice",
			Host:  "my-host",
			PID:   1234,
		},
		Reason: "", // ReasonNotStale
	}
	msg := err.Error()
	if !strings.Contains(msg, "not stale") {
		t.Errorf("NotStaleError.Error() with empty reason should say 'not stale', got %q", msg)
	}
	if !strings.Contains(msg, "1234") {
		t.Errorf("NotStaleError.Error() should include PID, got %q", msg)
	}
}

func TestNotStaleError_Unwrap(t *testing.T) {
	err := &NotStaleError{Lock: &lockfile.Lock{Name: "test"}}
	if !errors.Is(err, ErrNotStale) {
		t.Error("NotStaleError should unwrap to ErrNotStale")
	}
}

func TestFrozenError_WithAgentID(t *testing.T) {
	exp := time.Now().Add(10 * time.Minute)
	err := &FrozenError{
		Lock: &lockfile.Lock{
			Name:       "deploy",
			Owner:      "admin",
			AgentID:    "deploy-bot",
			Host:       "ci-host",
			PID:        99,
			AcquiredAt: time.Now().Add(-5 * time.Minute),
			TTLSec:     900,
			ExpiresAt:  &exp,
		},
	}
	msg := err.Error()
	if !strings.Contains(msg, "agent: deploy-bot") {
		t.Errorf("FrozenError.Error() with AgentID should mention agent, got %q", msg)
	}
	if !strings.Contains(msg, "remaining") {
		t.Errorf("FrozenError.Error() with remaining time should mention 'remaining', got %q", msg)
	}
}

func TestFrozenError_Unwrap(t *testing.T) {
	err := &FrozenError{Lock: &lockfile.Lock{Name: "test"}}
	if !errors.Is(err, ErrFrozen) {
		t.Error("FrozenError should unwrap to ErrFrozen")
	}
}

// --- Release: unsupported version paths ---

func TestRelease_UnsupportedVersion_Force(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Write lock with future version
	path := filepath.Join(locksDir, "future.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"future","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Force should remove it
	err := Release(root, "future", ReleaseOptions{Force: true})
	if err != nil {
		t.Fatalf("Release(force) on unsupported version = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Lock file should be removed after force release")
	}
}

func TestRelease_UnsupportedVersion_Normal(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(locksDir, "future.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"future","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Normal release should return error wrapping ErrUnsupportedVersion
	err := Release(root, "future", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() on unsupported version should fail")
	}
	if !errors.Is(err, lockfile.ErrUnsupportedVersion) {
		t.Errorf("error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

// --- Unfreeze: unsupported version + corrupted paths ---

func TestUnfreeze_UnsupportedVersion_Force(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err != nil {
		t.Fatalf("Unfreeze(force) on unsupported version = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Freeze file should be removed after force unfreeze")
	}
}

func TestUnfreeze_UnsupportedVersion_Normal(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	err := Unfreeze(root, "deploy", UnfreezeOptions{})
	if err == nil {
		t.Fatal("Unfreeze() on unsupported version should fail")
	}
	if !errors.Is(err, lockfile.ErrUnsupportedVersion) {
		t.Errorf("error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

func TestUnfreeze_Corrupted_Force(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte("garbage data{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err != nil {
		t.Fatalf("Unfreeze(force) on corrupted = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Corrupted freeze file should be removed after force unfreeze")
	}
}

func TestUnfreeze_Corrupted_Normal(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte("garbage data{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	err := Unfreeze(root, "deploy", UnfreezeOptions{})
	if err == nil {
		t.Fatal("Unfreeze() on corrupted should fail")
	}
	if !errors.Is(err, lockfile.ErrCorrupted) {
		t.Errorf("error should wrap ErrCorrupted, got %v", err)
	}
}

// --- CheckFreeze: unsupported version + corrupted paths ---

func TestCheckFreeze_UnsupportedVersion(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Unsupported version should be treated as active (fail safe)
	err := CheckFreeze(root, "deploy", nil)
	if err == nil {
		t.Fatal("CheckFreeze() on unsupported version should return error (fail safe)")
	}
	if !errors.Is(err, lockfile.ErrUnsupportedVersion) {
		t.Errorf("error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

func TestCheckFreeze_Corrupted(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte("not json{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Corrupted freeze should be auto-removed, return nil
	err := CheckFreeze(root, "deploy", nil)
	if err != nil {
		t.Errorf("CheckFreeze() on corrupted should auto-prune and return nil, got %v", err)
	}
	// File should be removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Corrupted freeze file should be auto-removed")
	}
}

// --- Freeze: corrupted existing freeze replacement ---

func TestFreeze_CorruptedExisting_Replaced(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	// Create corrupted freeze
	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte("corrupted{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Freeze should remove corrupted and create new
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() should replace corrupted, got error = %v", err)
	}

	// Verify new freeze is valid
	lf, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new freeze error = %v", err)
	}
	if lf.Name != "deploy" {
		t.Errorf("Name = %q, want %q", lf.Name, "deploy")
	}
}

// --- Freeze: unsupported version existing ---

func TestFreeze_UnsupportedVersionExisting(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Freeze should fail with unsupported version error
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err == nil {
		t.Fatal("Freeze() should fail when existing has unsupported version")
	}
	if !errors.Is(err, lockfile.ErrUnsupportedVersion) {
		t.Errorf("error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

func TestUnfreeze_ValidatesName(t *testing.T) {
	root := t.TempDir()
	err := Unfreeze(root, "..", UnfreezeOptions{})
	if err == nil {
		t.Fatal("Unfreeze() with invalid name should fail")
	}
	if !errors.Is(err, lockfile.ErrInvalidName) {
		t.Errorf("error should wrap ErrInvalidName, got %v", err)
	}
}

func TestRelease_ValidatesName(t *testing.T) {
	root := t.TempDir()
	err := Release(root, "..", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() with invalid name should fail")
	}
	if !errors.Is(err, lockfile.ErrInvalidName) {
		t.Errorf("error should wrap ErrInvalidName, got %v", err)
	}
}

// --- Unfreeze: permission error (generic read error) ---

func TestUnfreeze_PermissionDenied(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	// Create freeze file and make it unreadable
	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })

	err := Unfreeze(root, "deploy", UnfreezeOptions{})
	if err == nil {
		t.Fatal("Unfreeze() on unreadable file should fail")
	}
}

// --- CheckFreeze: generic read error (permission denied) ---

func TestCheckFreeze_PermissionDenied(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	// Create freeze file and make it unreadable
	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })

	// Generic read error should be treated as "no freeze" (safe fallback)
	err := CheckFreeze(root, "deploy", nil)
	if err != nil {
		t.Errorf("CheckFreeze() with permission denied should return nil (safe fallback), got %v", err)
	}
}

// --- Legacy freeze name stripping in audit events ---

func TestUnfreeze_LegacyPrefix_AuditEvent(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatal(err)
	}
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Create legacy freeze (in locks/ with freeze- prefix)
	legacyFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	legacyPath := filepath.Join(locksDir, "freeze-deploy.json")
	if err := lockfile.Write(legacyPath, legacyFreeze); err != nil {
		t.Fatal(err)
	}

	auditor := audit.NewWriter(root)
	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true, Auditor: auditor})
	if err != nil {
		t.Fatalf("Unfreeze(force) on legacy freeze = %v", err)
	}

	// Audit event should have clean name (prefix stripped)
	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}
	if events[0].Name != "deploy" {
		t.Errorf("Audit event Name = %q, want %q (prefix should be stripped)", events[0].Name, "deploy")
	}
}

// --- Release: empty lock file ---

func TestRelease_EmptyFile(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create empty lock file (race condition: file created, not yet written)
	path := filepath.Join(locksDir, "empty.json")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// Normal release should return generic read error
	err := Release(root, "empty", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() on empty file should fail")
	}
	// Empty file is NOT ErrCorrupted, it's a generic error
	if errors.Is(err, lockfile.ErrCorrupted) {
		t.Error("Empty file should not be classified as corrupted")
	}
}

// --- Sweep: directory entry skipping ---

func TestSweep_SkipsSubdirectories(t *testing.T) {
	root := setupSweepRoot(t)
	locksDir := filepath.Join(root, "locks")

	// Create a subdirectory in locks/
	if err := os.MkdirAll(filepath.Join(locksDir, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}

	pruned, errs := PruneAllExpired(root, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

// --- Freeze: create file error (non-directory parent) ---

// --- Acquire: error paths ---

func TestAcquire_InvalidName(t *testing.T) {
	root := t.TempDir()
	err := Acquire(root, "..", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() with invalid name should fail")
	}
	if !errors.Is(err, lockfile.ErrInvalidName) {
		t.Errorf("error should wrap ErrInvalidName, got %v", err)
	}
}

func TestAcquire_UnsupportedVersionContention(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create a lock file with future version
	path := filepath.Join(locksDir, "future.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"future","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Acquire should fail with UnsupportedVersion
	err := Acquire(root, "future", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() on unsupported version lock should fail")
	}
	if !errors.Is(err, lockfile.ErrUnsupportedVersion) {
		t.Errorf("error should wrap ErrUnsupportedVersion, got %v", err)
	}
}

func TestAcquire_CorruptedLock_Replaced(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Also create freezes dir (EnsureDirs creates both)
	if err := os.MkdirAll(filepath.Join(root, "freezes"), 0700); err != nil {
		t.Fatal(err)
	}

	// Create a corrupted lock file
	path := filepath.Join(locksDir, "corrupt.json")
	if err := os.WriteFile(path, []byte("garbage{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Acquire should remove corrupted and create new lock
	err := Acquire(root, "corrupt", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() should replace corrupted lock, got error = %v", err)
	}

	// Verify new lock is valid
	lf, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new lock error = %v", err)
	}
	if lf.Name != "corrupt" {
		t.Errorf("Name = %q, want %q", lf.Name, "corrupt")
	}
}

func TestAcquire_CreateFileFails(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "freezes"), 0700); err != nil {
		t.Fatal(err)
	}

	// Make locks dir read-only so OpenFile with O_CREATE fails
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	err := Acquire(root, "deploy", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() with read-only locks dir should fail")
	}
}

func TestAcquire_EnsureDirsFail(t *testing.T) {
	// Use a file as root so MkdirAll fails
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	err := Acquire(filePath, "deploy", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() with file-as-root should fail")
	}
}

// --- Sweep: PID start time recycling detection ---

func TestSweep_ExpiredTTL_RecycledPID_SameHost(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	hostname, _ := os.Hostname()
	expired := time.Now().Add(-2 * time.Minute)
	writeLock(t, locksDir, "recycled", &lockfile.Lock{
		Version:    1,
		Name:       "recycled",
		Owner:      "departed",
		Host:       hostname,
		PID:        os.Getpid(), // PID is alive (our process)
		PIDStartNS: 1,           // but start time is wrong (recycled)
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1 (expired + recycled PID)", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestSweep_ExpiredTTL_SameHost_LivePID_MatchingStartTime(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	hostname, _ := os.Hostname()
	expired := time.Now().Add(-2 * time.Minute)

	// Get our actual start time
	startNS, err := getProcessStartTime()
	if err != nil {
		t.Skip("Cannot get process start time")
	}

	writeLock(t, locksDir, "alive-match", &lockfile.Lock{
		Version:    1,
		Name:       "alive-match",
		Owner:      "me",
		Host:       hostname,
		PID:        os.Getpid(),
		PIDStartNS: startNS, // matching start time = same process, not recycled
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (same process still alive)", pruned)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

// --- Release/Unfreeze: os.Remove failure paths ---

func TestRelease_RemoveFails(t *testing.T) {
	root := t.TempDir()

	// Acquire a lock
	err := Acquire(root, "perm-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Make locks directory read-only so os.Remove fails
	locksDir := filepath.Join(root, "locks")
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	// Release should fail with remove error
	err = Release(root, "perm-test", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() should fail when directory is read-only")
	}
}

func TestRelease_UnsupportedVersion_Force_RemoveFails(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(locksDir, "future.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"future","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	err := Release(root, "future", ReleaseOptions{Force: true})
	if err == nil {
		t.Fatal("Release(force) on unsupported version with read-only dir should fail")
	}
}

func TestRelease_Corrupted_Force_RemoveFails(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(locksDir, "corrupt.json")
	if err := os.WriteFile(path, []byte("garbage{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	err := Release(root, "corrupt", ReleaseOptions{Force: true})
	if err == nil {
		t.Fatal("Release(force) on corrupted with read-only dir should fail")
	}
}

func TestUnfreeze_RemoveFails(t *testing.T) {
	root := t.TempDir()

	// Create freeze
	err := Freeze(root, "perm-test", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// Make freezes directory read-only
	freezesDir := filepath.Join(root, "freezes")
	if err := os.Chmod(freezesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(freezesDir, 0700) })

	// Unfreeze should fail with remove error
	err = Unfreeze(root, "perm-test", UnfreezeOptions{})
	if err == nil {
		t.Fatal("Unfreeze() should fail when directory is read-only")
	}
}

func TestUnfreeze_UnsupportedVersion_Force_RemoveFails(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"deploy","owner":"x","host":"y","pid":1,"acquired_ts":"2025-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only
	if err := os.Chmod(freezesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(freezesDir, 0700) })

	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err == nil {
		t.Fatal("Unfreeze(force) on unsupported version with read-only dir should fail")
	}
}

func TestUnfreeze_Corrupted_Force_RemoveFails(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte("garbage{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only
	if err := os.Chmod(freezesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(freezesDir, 0700) })

	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err == nil {
		t.Fatal("Unfreeze(force) on corrupted with read-only dir should fail")
	}
}

// --- Sweep: read-only dir error path ---

func TestSweep_ReadDirError(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	// Make dir unreadable
	if err := os.Chmod(locksDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) == 0 {
		t.Error("expected ReadDir error")
	}
}

// --- Sweep: remove fails ---

func TestSweep_RemoveFails(t *testing.T) {
	rootDir := setupSweepRoot(t)
	locksDir := filepath.Join(rootDir, "locks")

	expired := time.Now().Add(-2 * time.Minute)
	writeLock(t, locksDir, "no-remove", &lockfile.Lock{
		Version:    1,
		Name:       "no-remove",
		Owner:      "other",
		Host:       "other-host",
		PID:        12345,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	})

	// Make dir read-only (can read entries but can't remove files)
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	pruned, errs := PruneAllExpired(rootDir, nil)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (remove should fail)", pruned)
	}
	if len(errs) == 0 {
		t.Error("expected remove error")
	}
}

// --- ReleaseByOwner: lockfile.Read returns IsNotExist (simulated race) ---

func TestReleaseByOwner_ReadNotExist_Race(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create a dangling symlink — ReadDir sees it but Read gets NotExist
	symPath := filepath.Join(locksDir, "phantom.json")
	if err := os.Symlink("/nonexistent/target/file", symPath); err != nil {
		t.Skip("Cannot create symlinks")
	}

	released, err := ReleaseByOwner(root, "anyone", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v, want nil", err)
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want empty", released)
	}
}

func getProcessStartTime() (int64, error) {
	return stale.GetProcessStartTime(os.Getpid())
}

// --- Freeze: create file error ---

func TestFreeze_WriteFail_EnsureDirsError(t *testing.T) {
	// Use a file as the root so MkdirAll fails (can't create dir inside a file)
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	err := Freeze(filePath, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err == nil {
		t.Fatal("Freeze() with file-as-root should fail")
	}
}

// --- ReleaseByOwner: ReadDir error (not NotExist) ---

func TestReleaseByOwner_ReadDirError(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Make locks dir unreadable
	if err := os.Chmod(locksDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	_, err := ReleaseByOwner(root, "anyone", ReleaseOptions{})
	if err == nil {
		t.Fatal("ReleaseByOwner() with unreadable locks dir should fail")
	}
}

// --- ReleaseByOwner: skips subdirectories ---

func TestReleaseByOwner_SkipsSubdirectories(t *testing.T) {
	root := t.TempDir()

	// Create a lock with known owner
	t.Setenv("LOKT_OWNER", "agent-sub")
	err := Acquire(root, "real-lock", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Create a subdirectory in locks/
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(filepath.Join(locksDir, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}

	released, err := ReleaseByOwner(root, "agent-sub", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v", err)
	}
	if len(released) != 1 || released[0] != "real-lock" {
		t.Errorf("released = %v, want [real-lock]", released)
	}
}

// --- ReleaseByOwner: os.Remove fails ---

func TestReleaseByOwner_RemoveFails(t *testing.T) {
	root := t.TempDir()

	// Create a lock with known owner
	t.Setenv("LOKT_OWNER", "agent-rm")
	err := Acquire(root, "perm-lock", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Make locks dir read-only (can read entries but can't remove files)
	locksDir := filepath.Join(root, "locks")
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	released, err := ReleaseByOwner(root, "agent-rm", ReleaseOptions{})
	if err != nil {
		t.Fatalf("ReleaseByOwner() error = %v, want nil (errors logged to stderr)", err)
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want empty (remove should fail)", released)
	}
}

// --- AcquireWithWait: non-held error returns immediately ---

func TestAcquireWithWait_NonHeldError(t *testing.T) {
	root := t.TempDir()

	// Invalid name causes a non-HeldError
	err := AcquireWithWait(context.Background(), root, "..", AcquireOptions{})
	if err == nil {
		t.Fatal("AcquireWithWait() with invalid name should fail")
	}
	if !errors.Is(err, lockfile.ErrInvalidName) {
		t.Errorf("error should wrap ErrInvalidName, got %v", err)
	}
}

// --- tryBreakStale: corrupted lock, remove fails ---

func TestTryBreakStale_CorruptedRemoveFails(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create corrupted lock file
	path := filepath.Join(locksDir, "broken.json")
	if err := os.WriteFile(path, []byte("garbage{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only so Remove fails
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	result := tryBreakStale(root, "broken")
	if result {
		t.Error("tryBreakStale() should return false when remove fails")
	}
}

// --- tryBreakStale: stale lock, remove fails ---

func TestTryBreakStale_StaleRemoveFails(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create an expired lock on a different host (cross-host stale)
	expired := time.Now().Add(-2 * time.Minute)
	lock := &lockfile.Lock{
		Version:    1,
		Name:       "stale-lock",
		Owner:      "other",
		Host:       "remote-host-that-does-not-exist",
		PID:        12345,
		AcquiredAt: expired,
		TTLSec:     60,
		ExpiresAt:  &expired,
	}
	if err := lockfile.Write(filepath.Join(locksDir, "stale-lock.json"), lock); err != nil {
		t.Fatal(err)
	}

	// Make dir read-only so Remove fails
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	result := tryBreakStale(root, "stale-lock")
	if result {
		t.Error("tryBreakStale() should return false when remove fails")
	}
}

// --- Freeze: unreadable existing freeze (not corrupted, not unsupported) ---

func TestFreeze_UnreadableExisting(t *testing.T) {
	root := t.TempDir()
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatal(err)
	}

	// Create empty freeze file (Read returns generic error, not Corrupted/Unsupported)
	path := filepath.Join(freezesDir, "deploy.json")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// Freeze should return HeldError (treats unreadable as held by unknown)
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err == nil {
		t.Fatal("Freeze() with unreadable existing should fail")
	}
	var held *HeldError
	if !errors.As(err, &held) {
		t.Errorf("error should be HeldError, got %T: %v", err, err)
	}
}

// --- Renew: lockfile.Write error ---

func TestRenew_WriteError(t *testing.T) {
	root := t.TempDir()

	// Acquire a lock
	err := Acquire(root, "renew-fail", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Make locks dir read-only so lockfile.Write (CreateTemp) fails
	locksDir := filepath.Join(root, "locks")
	if err := os.Chmod(locksDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locksDir, 0700) })

	err = Renew(root, "renew-fail", RenewOptions{})
	if err == nil {
		t.Fatal("Renew() should fail when dir is read-only")
	}
}
