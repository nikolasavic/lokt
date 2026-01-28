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

	// Verify freeze lock file exists with correct naming
	path := filepath.Join(root, "locks", "freeze-deploy.json")
	lf, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read freeze lock error = %v", err)
	}

	if lf.Name != "freeze-deploy" {
		t.Errorf("Name = %q, want %q", lf.Name, "freeze-deploy")
	}
	if lf.TTLSec != 900 {
		t.Errorf("TTLSec = %d, want 900", lf.TTLSec)
	}
	if lf.Owner == "" {
		t.Error("Owner should not be empty")
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

	// Create an expired freeze manually
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSec:     60, // 1 minute TTL = expired
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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

	// Verify freeze lock file is gone
	path := filepath.Join(root, "locks", "freeze-deploy.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Freeze lock file should be deleted")
	}
}

func TestUnfreezeNotFound(t *testing.T) {
	root := t.TempDir()

	// Ensure locks dir exists
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	err := Unfreeze(root, "nonexistent", UnfreezeOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Unfreeze() error = %v, want ErrNotFound", err)
	}
}

func TestUnfreezeNotOwner(t *testing.T) {
	root := t.TempDir()

	// Create freeze from different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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

	// Create freeze from different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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

	// Ensure locks dir exists
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
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

	// Create an expired freeze manually
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSec:     60, // 1 minute TTL = expired
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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

	// Create a freeze with a dead PID on same host.
	// Unlike regular locks, freeze locks are NOT auto-pruned by dead PID
	// because the freeze command exits immediately after creating the lock.
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	deadFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "dead-process",
		Host:       hostname,
		PID:        999999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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

	// Create freeze from different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	otherFreeze := &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     900,
	}
	path := filepath.Join(locksDir, "freeze-deploy.json")
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
	err := &FrozenError{
		Lock: &lockfile.Lock{
			Name:       "freeze-deploy",
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
	// Should mention the operation name (without freeze- prefix)
	if !containsStr(msg, "deploy") {
		t.Errorf("Error message should mention 'deploy', got %q", msg)
	}
	if !containsStr(msg, "alice") {
		t.Errorf("Error message should mention owner 'alice', got %q", msg)
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
