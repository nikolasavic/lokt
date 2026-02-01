package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestFreeze(t *testing.T) {
	root := t.TempDir()

	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// Verify freeze file exists in freezes/ directory with clean name
	path := filepath.Join(root, "freezes", "deploy.json")
	lf, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read freeze lock error = %v", err)
	}

	if lf.Name != "deploy" {
		t.Errorf("Name = %q, want %q", lf.Name, "deploy")
	}
	if lf.TTLSec != 900 {
		t.Errorf("TTLSec = %d, want 900", lf.TTLSec)
	}
	if lf.Owner == "" {
		t.Error("Owner should not be empty")
	}

	// Verify nothing was written to the legacy location
	legacyPath := filepath.Join(root, "locks", "freeze-deploy.json")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("Should not write to legacy locks/freeze-deploy.json path")
	}
}

func TestFreezeRequiresTTL(t *testing.T) {
	root := t.TempDir()

	err := Freeze(root, "deploy", FreezeOptions{})
	if err == nil {
		t.Fatal("Freeze() without TTL should fail")
	}
}

func TestFreezeContention(t *testing.T) {
	root := t.TempDir()

	// First freeze succeeds
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("First Freeze() error = %v", err)
	}

	// Second freeze fails (already frozen)
	err = Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err == nil {
		t.Fatal("Second Freeze() should fail")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T: %v", err, err)
	}
}

func TestFreezeReplacesExpired(t *testing.T) {
	root := t.TempDir()

	// Create an expired freeze manually at new location
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	// Also create locks dir (EnsureDirs creates both)
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSec:     60, // 1 minute TTL = expired
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, expiredFreeze); err != nil {
		t.Fatalf("Write expired freeze error = %v", err)
	}

	// New freeze should replace the expired one
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() should replace expired freeze, got error = %v", err)
	}

	// Verify new freeze
	lf, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new freeze error = %v", err)
	}
	if lf.Owner == "other-owner" {
		t.Error("Freeze should be owned by us, not 'other-owner'")
	}
}

func TestUnfreeze(t *testing.T) {
	root := t.TempDir()

	// Create freeze first
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// Unfreeze
	err = Unfreeze(root, "deploy", UnfreezeOptions{})
	if err != nil {
		t.Fatalf("Unfreeze() error = %v", err)
	}

	// Verify freeze file is gone from new location
	path := filepath.Join(root, "freezes", "deploy.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Freeze lock file should be deleted")
	}
}

func TestUnfreezeNotFound(t *testing.T) {
	root := t.TempDir()

	// Ensure both dirs exist
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "freezes"), 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	err := Unfreeze(root, "nonexistent", UnfreezeOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Unfreeze() error = %v, want ErrNotFound", err)
	}
}

func TestUnfreezeNotOwner(t *testing.T) {
	root := t.TempDir()

	// Create freeze from different owner in new location
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, otherFreeze); err != nil {
		t.Fatalf("Write other freeze error = %v", err)
	}

	// Unfreeze should fail with NotOwnerError
	err := Unfreeze(root, "deploy", UnfreezeOptions{})

	var notOwner *NotOwnerError
	if !errors.As(err, &notOwner) {
		t.Fatalf("error should be *NotOwnerError, got %T: %v", err, err)
	}
}

func TestUnfreezeForce(t *testing.T) {
	root := t.TempDir()

	// Create freeze from different owner in new location
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, otherFreeze); err != nil {
		t.Fatalf("Write other freeze error = %v", err)
	}

	// Force unfreeze should succeed
	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err != nil {
		t.Fatalf("Unfreeze(force) error = %v", err)
	}

	// Verify freeze is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Freeze lock file should be deleted after force unfreeze")
	}
}

func TestCheckFreeze_NoFreeze(t *testing.T) {
	root := t.TempDir()

	// Ensure both dirs exist
	if err := os.MkdirAll(filepath.Join(root, "freezes"), 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	err := CheckFreeze(root, "deploy", nil)
	if err != nil {
		t.Errorf("CheckFreeze() with no freeze should return nil, got %v", err)
	}
}

func TestCheckFreeze_ActiveFreeze(t *testing.T) {
	root := t.TempDir()

	// Create freeze
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// CheckFreeze should return FrozenError
	err = CheckFreeze(root, "deploy", nil)
	if err == nil {
		t.Fatal("CheckFreeze() should return error when frozen")
	}

	var frozen *FrozenError
	if !errors.As(err, &frozen) {
		t.Fatalf("error should be *FrozenError, got %T: %v", err, err)
	}

	if !errors.Is(err, ErrFrozen) {
		t.Error("error should wrap ErrFrozen")
	}
}

func TestCheckFreeze_ExpiredFreeze(t *testing.T) {
	root := t.TempDir()

	// Create an expired freeze manually in new location
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSec:     60, // 1 minute TTL = expired
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, expiredFreeze); err != nil {
		t.Fatalf("Write expired freeze error = %v", err)
	}

	// CheckFreeze should auto-prune and return nil
	err := CheckFreeze(root, "deploy", nil)
	if err != nil {
		t.Errorf("CheckFreeze() should auto-prune expired freeze, got %v", err)
	}

	// Verify freeze file was removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Expired freeze lock file should be deleted")
	}
}

func TestCheckFreeze_DeadPIDFreezeStillBlocks(t *testing.T) {
	root := t.TempDir()

	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create a freeze with a dead PID on same host in new location.
	// Unlike regular locks, freeze locks are NOT auto-pruned by dead PID
	// because the freeze command exits immediately after creating the lock.
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	deadFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "dead-process",
		Host:       hostname,
		PID:        999999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, deadFreeze); err != nil {
		t.Fatalf("Write dead PID freeze error = %v", err)
	}

	// CheckFreeze should still block (dead PID doesn't invalidate freeze)
	err = CheckFreeze(root, "deploy", nil)
	if err == nil {
		t.Fatal("CheckFreeze() should still block for dead PID freeze")
	}

	var frozen *FrozenError
	if !errors.As(err, &frozen) {
		t.Fatalf("error should be *FrozenError, got %T: %v", err, err)
	}

	// Freeze file should still exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Dead PID freeze lock file should NOT be deleted")
	}
}

func TestIsFreezeLock(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"freeze-deploy", true},
		{"freeze-build", true},
		{"deploy", false},
		{"freeze", false},  // Just "freeze" without dash+name is not a freeze lock
		{"freeze-", false}, // Empty name after prefix
		{"my-freeze-lock", false},
	}

	for _, tt := range tests {
		got := IsFreezeLock(tt.name)
		if got != tt.want {
			t.Errorf("IsFreezeLock(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFreezeEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute, Auditor: auditor})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventFreeze {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventFreeze)
	}
	if e.Name != "deploy" {
		t.Errorf("Name = %q, want %q", e.Name, "deploy")
	}
	if e.TTLSec != 900 {
		t.Errorf("TTLSec = %d, want 900", e.TTLSec)
	}
}

func TestUnfreezeEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()

	// Freeze without auditor
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// Unfreeze with auditor
	auditor := audit.NewWriter(root)
	err = Unfreeze(root, "deploy", UnfreezeOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Unfreeze() error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventUnfreeze {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventUnfreeze)
	}
	if e.Name != "deploy" {
		t.Errorf("Name = %q, want %q", e.Name, "deploy")
	}
}

func TestUnfreezeForceEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()

	// Create freeze from different owner in new location
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(freezesDir, "deploy.json")
	if err := lockfile.Write(path, otherFreeze); err != nil {
		t.Fatalf("Write other freeze error = %v", err)
	}

	auditor := audit.NewWriter(root)
	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true, Auditor: auditor})
	if err != nil {
		t.Fatalf("Unfreeze(force) error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventForceUnfreeze {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventForceUnfreeze)
	}
}

func TestCheckFreezeEmitsFreezeDenyEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create active freeze
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// CheckFreeze should emit freeze-deny event
	err = CheckFreeze(root, "deploy", auditor)
	if err == nil {
		t.Fatal("CheckFreeze() should return error when frozen")
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventFreezeDeny {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventFreezeDeny)
	}
	if e.Name != "deploy" {
		t.Errorf("Name = %q, want %q", e.Name, "deploy")
	}
	if e.Extra == nil {
		t.Fatal("Extra should contain freeze holder info")
	}
	if _, ok := e.Extra["freeze_owner"]; !ok {
		t.Error("Extra should contain freeze_owner")
	}
}

func TestFreezeValidatesName(t *testing.T) {
	root := t.TempDir()

	err := Freeze(root, "..", FreezeOptions{TTL: 15 * time.Minute})
	if err == nil {
		t.Fatal("Freeze() with invalid name should fail")
	}
}

func TestFrozenErrorMessage(t *testing.T) {
	// Test with new-style clean name
	err := &FrozenError{
		Lock: &lockfile.Lock{
			Name:       "deploy",
			Owner:      "alice",
			Host:       "dev-host",
			PID:        1234,
			AcquiredAt: time.Now().Add(-5 * time.Minute),
			TTLSec:     900, // 15 minutes
		},
	}

	msg := err.Error()
	if msg == "" {
		t.Error("Error message should not be empty")
	}
	if !containsStr(msg, "deploy") {
		t.Errorf("Error message should mention 'deploy', got %q", msg)
	}
	if !containsStr(msg, "alice") {
		t.Errorf("Error message should mention owner 'alice', got %q", msg)
	}

	// Test with legacy prefixed name (backward compat)
	legacyErr := &FrozenError{
		Lock: &lockfile.Lock{
			Name:       "freeze-deploy",
			Owner:      "bob",
			Host:       "ci-host",
			PID:        5678,
			AcquiredAt: time.Now().Add(-2 * time.Minute),
			TTLSec:     600,
		},
	}
	legacyMsg := legacyErr.Error()
	if !containsStr(legacyMsg, "deploy") {
		t.Errorf("Legacy error message should mention 'deploy', got %q", legacyMsg)
	}
	if containsStr(legacyMsg, "freeze-deploy") {
		t.Errorf("Legacy error message should strip prefix, got %q", legacyMsg)
	}
}

func TestCheckFreeze_LegacyFallback(t *testing.T) {
	root := t.TempDir()

	// Create freeze in legacy location only (no freezes/ dir file)
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	legacyFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "alice",
		Host:       "ci-host",
		PID:        12345,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	legacyPath := filepath.Join(locksDir, "freeze-deploy.json")
	if err := lockfile.Write(legacyPath, legacyFreeze); err != nil {
		t.Fatalf("Write legacy freeze error = %v", err)
	}

	// CheckFreeze should find the legacy freeze
	err := CheckFreeze(root, "deploy", nil)
	if err == nil {
		t.Fatal("CheckFreeze() should detect legacy freeze")
	}
	var frozen *FrozenError
	if !errors.As(err, &frozen) {
		t.Fatalf("error should be *FrozenError, got %T: %v", err, err)
	}
}

func TestUnfreeze_LegacyFallback(t *testing.T) {
	root := t.TempDir()

	// Create freeze in legacy location
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

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
		t.Fatalf("Write legacy freeze error = %v", err)
	}

	// Force unfreeze should find and remove legacy file
	err := Unfreeze(root, "deploy", UnfreezeOptions{Force: true})
	if err != nil {
		t.Fatalf("Unfreeze(force) on legacy freeze error = %v", err)
	}

	// Legacy file should be gone
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("Legacy freeze file should be deleted")
	}
}

func TestCheckFreeze_ExpiredLegacyFreeze(t *testing.T) {
	root := t.TempDir()

	// Create an expired freeze in legacy location
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other",
		Host:       "other",
		PID:        99999,
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSec:     60,
	}
	legacyPath := filepath.Join(locksDir, "freeze-deploy.json")
	if err := lockfile.Write(legacyPath, expiredFreeze); err != nil {
		t.Fatalf("Write expired legacy freeze error = %v", err)
	}

	// CheckFreeze should auto-prune the legacy expired freeze
	err := CheckFreeze(root, "deploy", nil)
	if err != nil {
		t.Errorf("CheckFreeze() should auto-prune expired legacy freeze, got %v", err)
	}

	// Legacy file should be removed
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("Expired legacy freeze file should be deleted")
	}
}

func TestFreezeNoNamespaceCollision(t *testing.T) {
	root := t.TempDir()

	// Create a regular lock named "freeze-deploy" in locks/
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	freezesDir := filepath.Join(root, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	regularLock := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "user",
		Host:       "host",
		PID:        1234,
		AcquiredAt: time.Now(),
		TTLSec:     300,
	}
	regularPath := filepath.Join(locksDir, "freeze-deploy.json")
	if err := lockfile.Write(regularPath, regularLock); err != nil {
		t.Fatalf("Write regular lock error = %v", err)
	}

	// Create an actual freeze for "deploy" — should go to freezes/deploy.json
	err := Freeze(root, "deploy", FreezeOptions{TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	// Both files should exist independently
	freezePath := filepath.Join(freezesDir, "deploy.json")
	if _, err := os.Stat(regularPath); os.IsNotExist(err) {
		t.Error("Regular lock at locks/freeze-deploy.json should still exist")
	}
	if _, err := os.Stat(freezePath); os.IsNotExist(err) {
		t.Error("Freeze at freezes/deploy.json should exist")
	}

	// CheckFreeze should find the actual freeze, not be confused by the regular lock
	err = CheckFreeze(root, "deploy", nil)
	if err == nil {
		t.Fatal("CheckFreeze() should detect the freeze")
	}
	var frozen *FrozenError
	if !errors.As(err, &frozen) {
		t.Fatalf("error should be *FrozenError, got %T: %v", err, err)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
