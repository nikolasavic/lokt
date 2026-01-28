package lock

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestAcquire(t *testing.T) {
	root := t.TempDir()

	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Verify lock file exists
	path := filepath.Join(root, "locks", "test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock file error = %v", err)
	}

	if lock.Name != "test" {
		t.Errorf("Name = %q, want %q", lock.Name, "test")
	}
	if lock.Owner == "" {
		t.Error("Owner should not be empty")
	}
	if lock.PID == 0 {
		t.Error("PID should not be 0")
	}
}

func TestAcquireWithTTL(t *testing.T) {
	root := t.TempDir()

	err := Acquire(root, "ttl-test", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	path := filepath.Join(root, "locks", "ttl-test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock file error = %v", err)
	}

	if lock.TTLSec != 300 {
		t.Errorf("TTLSec = %d, want 300", lock.TTLSec)
	}
}

func TestAcquireContention(t *testing.T) {
	root := t.TempDir()

	// First acquire succeeds
	err := Acquire(root, "contested", AcquireOptions{})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Second acquire fails with HeldError
	err = Acquire(root, "contested", AcquireOptions{})
	if err == nil {
		t.Fatal("Second Acquire() should fail")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}

	if held.Lock.Name != "contested" {
		t.Errorf("HeldError.Lock.Name = %q, want %q", held.Lock.Name, "contested")
	}

	if !errors.Is(err, ErrLockHeld) {
		t.Error("error should wrap ErrLockHeld")
	}
}

func TestAcquireRace(t *testing.T) {
	root := t.TempDir()

	// Race multiple goroutines - exactly one should win
	const n = 10
	var wg sync.WaitGroup
	wins := make(chan int, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := Acquire(root, "race", AcquireOptions{})
			if err == nil {
				wins <- id
			}
		}(i)
	}

	wg.Wait()
	close(wins)

	winCount := 0
	for range wins {
		winCount++
	}

	if winCount != 1 {
		t.Errorf("Expected exactly 1 winner, got %d", winCount)
	}
}

func TestAcquireCreatesDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "path")

	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Verify directories were created
	locksDir := filepath.Join(root, "locks")
	info, err := os.Stat(locksDir)
	if err != nil {
		t.Fatalf("Stat locks dir error = %v", err)
	}
	if !info.IsDir() {
		t.Error("locks should be a directory")
	}
}

func TestAcquireWithWait_ImmediateSuccess(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// No contention, should succeed immediately
	err := AcquireWithWait(ctx, root, "no-contention", AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireWithWait() error = %v", err)
	}

	// Verify lock exists
	path := filepath.Join(root, "locks", "no-contention.json")
	_, err = lockfile.Read(path)
	if err != nil {
		t.Fatalf("Lock file should exist: %v", err)
	}
}

func TestAcquireWithWait_WaitsForRelease(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// First acquire the lock
	err := Acquire(root, "wait-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Initial Acquire() error = %v", err)
	}

	// Start a goroutine that releases the lock after a short delay
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = Release(root, "wait-test", ReleaseOptions{})
	}()

	// AcquireWithWait should succeed after the lock is released
	start := time.Now()
	err = AcquireWithWait(ctx, root, "wait-test", AcquireOptions{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AcquireWithWait() error = %v", err)
	}

	// Should have waited at least 100ms (one poll cycle)
	if elapsed < 100*time.Millisecond {
		t.Errorf("Expected to wait, but elapsed = %v", elapsed)
	}
}

func TestAcquireWithWait_ContextCancellation(t *testing.T) {
	root := t.TempDir()

	// Acquire lock to create contention
	err := Acquire(root, "cancel-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Initial Acquire() error = %v", err)
	}

	// Create a context that cancels after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// AcquireWithWait should return context error
	err = AcquireWithWait(ctx, root, "cancel-test", AcquireOptions{})
	if err == nil {
		t.Fatal("Expected error from context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestAcquireWithWait_BreaksExpiredLock(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Create an expired lock manually
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredLock := &lockfile.Lock{
		Name:       "expired-test",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-10 * time.Minute), // 10 minutes ago
		TTLSec:     60,                                // 1 minute TTL = expired
	}
	path := filepath.Join(locksDir, "expired-test.json")
	if err := lockfile.Write(path, expiredLock); err != nil {
		t.Fatalf("Write expired lock error = %v", err)
	}

	// AcquireWithWait should break the expired lock and succeed
	err := AcquireWithWait(ctx, root, "expired-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireWithWait() error = %v", err)
	}

	// Verify we now own the lock
	newLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new lock error = %v", err)
	}
	if newLock.Owner == "other-owner" {
		t.Error("Lock should have been acquired by us, not 'other-owner'")
	}
}

func TestAcquireWithWait_BreaksDeadPIDLock(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Get current hostname for same-host lock
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create a lock with a dead PID (PID 1 is init, use unlikely PID)
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	deadPIDLock := &lockfile.Lock{
		Name:       "dead-pid-test",
		Owner:      "dead-process",
		Host:       hostname, // Same host so PID check applies
		PID:        999999,   // Very unlikely to be a real PID
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL, relies on PID check
	}
	path := filepath.Join(locksDir, "dead-pid-test.json")
	if err := lockfile.Write(path, deadPIDLock); err != nil {
		t.Fatalf("Write dead PID lock error = %v", err)
	}

	// AcquireWithWait should break the dead PID lock and succeed
	err = AcquireWithWait(ctx, root, "dead-pid-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireWithWait() error = %v", err)
	}

	// Verify we now own the lock
	newLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new lock error = %v", err)
	}
	if newLock.Owner == "dead-process" {
		t.Error("Lock should have been acquired by us, not 'dead-process'")
	}
}

func TestAcquireWithWait_PreservesTTL(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Acquire with TTL
	err := AcquireWithWait(ctx, root, "ttl-wait-test", AcquireOptions{TTL: 10 * time.Minute})
	if err != nil {
		t.Fatalf("AcquireWithWait() error = %v", err)
	}

	// Verify TTL was set
	path := filepath.Join(root, "locks", "ttl-wait-test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	if lock.TTLSec != 600 {
		t.Errorf("TTLSec = %d, want 600", lock.TTLSec)
	}
}

func TestBackoffInterval(t *testing.T) {
	// Expected multipliers for each attempt
	multipliers := []time.Duration{1, 2, 4, 8, 16, 32, 64}

	// Test exponential growth
	for attempt := 0; attempt < 10; attempt++ {
		interval := backoffInterval(attempt)

		// Calculate expected base interval
		var multiplier time.Duration = 64 // capped at attempt >= 6
		if attempt < len(multipliers) {
			multiplier = multipliers[attempt]
		}
		expectedBase := baseInterval * multiplier
		if expectedBase > maxInterval {
			expectedBase = maxInterval
		}

		minExpected := time.Duration(float64(expectedBase) * 0.75)
		maxExpected := time.Duration(float64(expectedBase) * 1.25)

		if interval < minExpected || interval > maxExpected {
			t.Errorf("attempt %d: interval %v outside expected range [%v, %v]",
				attempt, interval, minExpected, maxExpected)
		}
	}

	// After many attempts, should be capped at maxInterval (±jitter)
	interval := backoffInterval(100)
	if interval < time.Duration(float64(maxInterval)*0.75) {
		t.Errorf("high attempt interval %v should be near maxInterval", interval)
	}
	if interval > time.Duration(float64(maxInterval)*1.25) {
		t.Errorf("high attempt interval %v exceeds maxInterval with jitter", interval)
	}
}

// readAuditEvents reads all events from the audit log file.
func readAuditEvents(t *testing.T, rootDir string) []audit.Event {
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

func TestAcquireEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	err := Acquire(root, "audit-test", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventAcquire {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventAcquire)
	}
	if e.Name != "audit-test" {
		t.Errorf("Name = %q, want %q", e.Name, "audit-test")
	}
	if e.Owner == "" {
		t.Error("Owner should not be empty")
	}
	if e.PID == 0 {
		t.Error("PID should not be 0")
	}
}

func TestAcquireDeniedEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// First acquire succeeds
	err := Acquire(root, "deny-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Second acquire fails and should emit deny event
	err = Acquire(root, "deny-test", AcquireOptions{Auditor: auditor})
	if err == nil {
		t.Fatal("Second Acquire() should fail")
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	e := events[0]
	if e.Event != audit.EventDeny {
		t.Errorf("Event = %q, want %q", e.Event, audit.EventDeny)
	}
	if e.Name != "deny-test" {
		t.Errorf("Name = %q, want %q", e.Name, "deny-test")
	}
	// Check holder info in extra
	if e.Extra == nil {
		t.Fatal("Extra should contain holder info")
	}
	if _, ok := e.Extra["holder_owner"]; !ok {
		t.Error("Extra should contain holder_owner")
	}
	if _, ok := e.Extra["holder_host"]; !ok {
		t.Error("Extra should contain holder_host")
	}
	if _, ok := e.Extra["holder_pid"]; !ok {
		t.Error("Extra should contain holder_pid")
	}
}

func TestAcquireNilAuditorDoesNotPanic(t *testing.T) {
	root := t.TempDir()

	// Should not panic with nil auditor
	err := Acquire(root, "nil-auditor-test", AcquireOptions{Auditor: nil})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
}
