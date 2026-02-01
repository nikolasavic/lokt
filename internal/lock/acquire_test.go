package lock

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
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

	// Create a lock held by a different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	otherLock := &lockfile.Lock{
		Name:       "contested",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	if err := lockfile.Write(filepath.Join(locksDir, "contested.json"), otherLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Acquire from current process (different owner) fails with HeldError
	err := Acquire(root, "contested", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() should fail when held by different owner")
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

	// Race multiple goroutines — at least one should win.
	// Since all goroutines share the same process owner, additional
	// goroutines may succeed via reentrant refresh (same-owner path).
	// The key property: no deadlock, at least one acquires, and the
	// final lock state is valid.
	const n = 100
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

	if winCount < 1 {
		t.Errorf("Expected at least 1 winner, got %d", winCount)
	}

	// Verify the final lock is valid and owned by this process
	path := filepath.Join(root, "locks", "race.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read final lock error = %v", err)
	}
	if lock.Name != "race" {
		t.Errorf("Lock name = %q, want %q", lock.Name, "race")
	}
	if lock.PID != os.Getpid() {
		t.Errorf("Lock PID = %d, want %d", lock.PID, os.Getpid())
	}
}

func TestAcquireRace_WaitPollRounds(t *testing.T) {
	root := t.TempDir()

	const goroutines = 100
	const rounds = 5

	for round := 0; round < rounds; round++ {
		lockName := fmt.Sprintf("race-poll-%d", round)

		// Pre-create a lock held by a foreign owner so all goroutines
		// must poll via AcquireWithWait rather than winning immediately.
		locksDir := filepath.Join(root, "locks")
		if err := os.MkdirAll(locksDir, 0750); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}
		blocker := &lockfile.Lock{
			Name:       lockName,
			Owner:      "blocker",
			Host:       "other-host",
			PID:        99999,
			AcquiredAt: time.Now(),
			TTLSec:     30,
		}
		if err := lockfile.Write(filepath.Join(locksDir, lockName+".json"), blocker); err != nil {
			t.Fatalf("Write blocker lock error = %v", err)
		}

		var wg sync.WaitGroup
		wins := make(chan int, goroutines)

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err := AcquireWithWait(ctx, root, lockName, AcquireOptions{TTL: 30 * time.Second})
				if err == nil {
					wins <- id
				}
			}(i)
		}

		// Release the blocker after a short delay so waiters can compete
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = Release(root, lockName, ReleaseOptions{Force: true})
		}()

		wg.Wait()
		close(wins)

		// All goroutines share the same process owner, so once the blocker
		// is released, the first to acquire wins via O_EXCL and the rest
		// succeed via reentrant refresh. This validates that AcquireWithWait
		// polling correctly retries and that no goroutines leak or deadlock
		// under heavy contention.
		winCount := 0
		for range wins {
			winCount++
		}

		if winCount == 0 {
			t.Errorf("Round %d: expected at least 1 winner, got 0", round)
		}

		// Clean up for next round
		_ = Release(root, lockName, ReleaseOptions{Force: true})
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

	// Create a lock held by a different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	otherLock := &lockfile.Lock{
		Name:       "wait-test",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	if err := lockfile.Write(filepath.Join(locksDir, "wait-test.json"), otherLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Start a goroutine that releases the lock after a short delay
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = Release(root, "wait-test", ReleaseOptions{Force: true})
	}()

	// AcquireWithWait should succeed after the lock is released
	start := time.Now()
	err := AcquireWithWait(ctx, root, "wait-test", AcquireOptions{})
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

	// Create a lock held by a different owner to create contention
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	otherLock := &lockfile.Lock{
		Name:       "cancel-test",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	if err := lockfile.Write(filepath.Join(locksDir, "cancel-test.json"), otherLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Create a context that cancels after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// AcquireWithWait should return context error
	err := AcquireWithWait(ctx, root, "cancel-test", AcquireOptions{})
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

	// Create a lock held by a different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	otherLock := &lockfile.Lock{
		Name:       "deny-test",
		Owner:      "other-owner",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	if err := lockfile.Write(filepath.Join(locksDir, "deny-test.json"), otherLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Acquire from current process (different owner) fails and should emit deny event
	err := Acquire(root, "deny-test", AcquireOptions{Auditor: auditor})
	if err == nil {
		t.Fatal("Acquire() should fail when held by different owner")
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

// Auto-prune tests for L-201

func TestAcquire_AutoPrunesDeadPIDLock(t *testing.T) {
	root := t.TempDir()

	// Get current hostname for same-host lock
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create a lock with a dead PID on same host
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	deadPIDLock := &lockfile.Lock{
		Name:       "auto-prune-test",
		Owner:      "dead-process",
		Host:       hostname, // Same host so PID check applies
		PID:        999999,   // Very unlikely to be a real PID
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL, relies on PID check
	}
	path := filepath.Join(locksDir, "auto-prune-test.json")
	if err := lockfile.Write(path, deadPIDLock); err != nil {
		t.Fatalf("Write dead PID lock error = %v", err)
	}

	// Immediate Acquire should auto-prune the dead PID lock and succeed
	err = Acquire(root, "auto-prune-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() should auto-prune dead PID lock, got error = %v", err)
	}

	// Verify we now own the lock
	newLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new lock error = %v", err)
	}
	if newLock.Owner == "dead-process" {
		t.Error("Lock should have been acquired by us, not 'dead-process'")
	}
	if newLock.PID != os.Getpid() {
		t.Errorf("Lock PID = %d, want %d (current process)", newLock.PID, os.Getpid())
	}
}

func TestAcquire_NoAutoPruneCrossHost(t *testing.T) {
	root := t.TempDir()

	// Create a lock from a different host
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	crossHostLock := &lockfile.Lock{
		Name:       "cross-host-test",
		Owner:      "remote-process",
		Host:       "other-host.example.com", // Different host
		PID:        999999,
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}
	path := filepath.Join(locksDir, "cross-host-test.json")
	if err := lockfile.Write(path, crossHostLock); err != nil {
		t.Fatalf("Write cross-host lock error = %v", err)
	}

	// Immediate Acquire should NOT auto-prune cross-host lock
	err := Acquire(root, "cross-host-test", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() should fail for cross-host lock (cannot verify PID)")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}

	// Lock should still belong to remote process
	existingLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	if existingLock.Owner != "remote-process" {
		t.Errorf("Lock owner = %q, should still be 'remote-process'", existingLock.Owner)
	}
}

func TestAcquire_NoAutoPruneLivePID(t *testing.T) {
	root := t.TempDir()

	// Get current hostname
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create a lock with our own PID (definitely alive)
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	livePIDLock := &lockfile.Lock{
		Name:       "live-pid-test",
		Owner:      "other-owner",
		Host:       hostname,
		PID:        os.Getpid(), // Our own PID, definitely alive
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}
	path := filepath.Join(locksDir, "live-pid-test.json")
	if err := lockfile.Write(path, livePIDLock); err != nil {
		t.Fatalf("Write live PID lock error = %v", err)
	}

	// Immediate Acquire should NOT auto-prune live PID lock
	err = Acquire(root, "live-pid-test", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() should fail for live PID lock")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}

	// Lock should still exist with original owner
	existingLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	if existingLock.Owner != "other-owner" {
		t.Errorf("Lock owner = %q, should still be 'other-owner'", existingLock.Owner)
	}
}

func TestAcquire_NoAutoPruneExpiredTTLWithLivePID(t *testing.T) {
	root := t.TempDir()

	// Get current hostname
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create an expired lock with a live PID
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	expiredLiveLock := &lockfile.Lock{
		Name:       "expired-live-test",
		Owner:      "other-owner",
		Host:       hostname,
		PID:        os.Getpid(),                       // Our PID, definitely alive
		AcquiredAt: time.Now().Add(-10 * time.Minute), // 10 minutes ago
		TTLSec:     60,                                // 1 minute TTL = expired
	}
	path := filepath.Join(locksDir, "expired-live-test.json")
	if err := lockfile.Write(path, expiredLiveLock); err != nil {
		t.Fatalf("Write expired live lock error = %v", err)
	}

	// Immediate Acquire should NOT auto-prune (TTL expired but PID alive)
	// Auto-prune only triggers on ReasonDeadPID, not ReasonExpired
	err = Acquire(root, "expired-live-test", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() should fail - auto-prune only works for dead PIDs, not TTL expiry")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}
}

func TestAcquire_AutoPruneEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Get current hostname
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}

	// Create a lock with a dead PID
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	deadPIDLock := &lockfile.Lock{
		Name:       "audit-prune-test",
		Owner:      "dead-process",
		Host:       hostname,
		PID:        999999, // Very unlikely to be a real PID
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}
	path := filepath.Join(locksDir, "audit-prune-test.json")
	if err := lockfile.Write(path, deadPIDLock); err != nil {
		t.Fatalf("Write dead PID lock error = %v", err)
	}

	// Acquire should auto-prune and emit audit event
	err = Acquire(root, "audit-prune-test", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	events := readAuditEvents(t, root)
	// Should have 2 events: auto-prune and acquire
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events (auto-prune + acquire), got %d", len(events))
	}

	// First event should be auto-prune
	pruneEvent := events[0]
	if pruneEvent.Event != audit.EventAutoPrune {
		t.Errorf("First event = %q, want %q", pruneEvent.Event, audit.EventAutoPrune)
	}
	if pruneEvent.Name != "audit-prune-test" {
		t.Errorf("Name = %q, want %q", pruneEvent.Name, "audit-prune-test")
	}
	// Check pruned holder info
	if pruneEvent.Extra == nil {
		t.Fatal("Extra should contain pruned holder info")
	}
	if pruneEvent.Extra["pruned_owner"] != "dead-process" {
		t.Errorf("pruned_owner = %v, want 'dead-process'", pruneEvent.Extra["pruned_owner"])
	}
	if pruneEvent.Extra["pruned_host"] != hostname {
		t.Errorf("pruned_host = %v, want %q", pruneEvent.Extra["pruned_host"], hostname)
	}
	prunedPID, ok := pruneEvent.Extra["pruned_pid"].(float64)
	if !ok || int(prunedPID) != 999999 {
		t.Errorf("pruned_pid = %v, want 999999", pruneEvent.Extra["pruned_pid"])
	}

	// Second event should be acquire
	acquireEvent := events[1]
	if acquireEvent.Event != audit.EventAcquire {
		t.Errorf("Second event = %q, want %q", acquireEvent.Event, audit.EventAcquire)
	}
}

// Corrupted lock file tests for L-203

func TestAcquire_AutoPrunesCorruptedLock(t *testing.T) {
	root := t.TempDir()

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-test.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Acquire should auto-prune the corrupted lock and succeed
	err := Acquire(root, "corrupt-test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() should auto-prune corrupted lock, got error = %v", err)
	}

	// Verify we now own the lock
	newLock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read new lock error = %v", err)
	}
	if newLock.PID != os.Getpid() {
		t.Errorf("Lock PID = %d, want %d (current process)", newLock.PID, os.Getpid())
	}
}

func TestAcquire_CorruptedLockEmitsAuditEvent(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-audit.json")
	if err := os.WriteFile(path, []byte(`{"broken`), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Acquire should auto-prune and emit audit events
	err := Acquire(root, "corrupt-audit", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	events := readAuditEvents(t, root)
	// Should have 2 events: corrupt-break and acquire
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events (corrupt-break + acquire), got %d", len(events))
	}

	// First event should be corrupt-break
	corruptEvent := events[0]
	if corruptEvent.Event != audit.EventCorruptBreak {
		t.Errorf("First event = %q, want %q", corruptEvent.Event, audit.EventCorruptBreak)
	}
	if corruptEvent.Name != "corrupt-audit" {
		t.Errorf("Name = %q, want %q", corruptEvent.Name, "corrupt-audit")
	}

	// Second event should be acquire
	acquireEvt := events[1]
	if acquireEvt.Event != audit.EventAcquire {
		t.Errorf("Second event = %q, want %q", acquireEvt.Event, audit.EventAcquire)
	}
}

func TestTryBreakStale_RemovesCorruptedLock(t *testing.T) {
	root := t.TempDir()

	// Create a corrupted lock file
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	path := filepath.Join(locksDir, "corrupt-stale.json")
	if err := os.WriteFile(path, []byte("garbage data"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// tryBreakStale should remove corrupted lock
	removed := tryBreakStale(root, "corrupt-stale")
	if !removed {
		t.Error("tryBreakStale() should return true for corrupted lock")
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Corrupted lock file should be deleted")
	}
}

// Reentrant acquire tests for lokt-skc

func TestAcquire_ReentrantSameOwner(t *testing.T) {
	root := t.TempDir()

	// First acquire succeeds
	err := Acquire(root, "reentrant", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Read original lock to compare later
	path := filepath.Join(root, "locks", "reentrant.json")
	original, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read original lock error = %v", err)
	}

	// Small delay so timestamps differ
	time.Sleep(10 * time.Millisecond)

	// Second acquire from same owner succeeds (reentrant)
	err = Acquire(root, "reentrant", AcquireOptions{TTL: 10 * time.Minute})
	if err != nil {
		t.Fatalf("Reentrant Acquire() error = %v", err)
	}

	// Verify lock was refreshed
	refreshed, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read refreshed lock error = %v", err)
	}

	if refreshed.Owner != original.Owner {
		t.Errorf("Owner changed: %q -> %q", original.Owner, refreshed.Owner)
	}
	if !refreshed.AcquiredAt.After(original.AcquiredAt) {
		t.Error("AcquiredAt should be refreshed to a newer timestamp")
	}
	if refreshed.TTLSec != 600 {
		t.Errorf("TTLSec = %d, want 600 (new TTL should be applied)", refreshed.TTLSec)
	}
	if refreshed.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d (current process)", refreshed.PID, os.Getpid())
	}
}

func TestAcquire_ReentrantRefreshesTTL(t *testing.T) {
	root := t.TempDir()

	// Acquire with 5-minute TTL
	err := Acquire(root, "ttl-refresh", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Re-acquire with 10-minute TTL
	err = Acquire(root, "ttl-refresh", AcquireOptions{TTL: 10 * time.Minute})
	if err != nil {
		t.Fatalf("Reentrant Acquire() error = %v", err)
	}

	path := filepath.Join(root, "locks", "ttl-refresh.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}

	if lock.TTLSec != 600 {
		t.Errorf("TTLSec = %d, want 600", lock.TTLSec)
	}
}

func TestAcquire_ReentrantRemovesTTL(t *testing.T) {
	root := t.TempDir()

	// Acquire with TTL
	err := Acquire(root, "ttl-remove", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Re-acquire without TTL
	err = Acquire(root, "ttl-remove", AcquireOptions{})
	if err != nil {
		t.Fatalf("Reentrant Acquire() error = %v", err)
	}

	path := filepath.Join(root, "locks", "ttl-remove.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}

	if lock.TTLSec != 0 {
		t.Errorf("TTLSec = %d, want 0 (TTL should be removed)", lock.TTLSec)
	}
}

func TestAcquire_ReentrantDifferentOwnerDenied(t *testing.T) {
	root := t.TempDir()

	// Create a lock held by a different owner
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	otherLock := &lockfile.Lock{
		Name:       "other-owner-test",
		Owner:      "different-agent",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     300,
	}
	if err := lockfile.Write(filepath.Join(locksDir, "other-owner-test.json"), otherLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Acquire from current process (different owner) should fail
	err := Acquire(root, "other-owner-test", AcquireOptions{})
	if err == nil {
		t.Fatal("Acquire() should fail when held by different owner")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}
	if held.Lock.Owner != "different-agent" {
		t.Errorf("HeldError.Lock.Owner = %q, want %q", held.Lock.Owner, "different-agent")
	}
}

func TestAcquire_ReentrantExpiredSameOwner(t *testing.T) {
	root := t.TempDir()

	// Create an expired lock with the same owner as current process
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	// Get current identity to match owner
	id := identity.Current()
	expiredLock := &lockfile.Lock{
		Name:       "expired-reentrant",
		Owner:      id.Owner,
		Host:       "old-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-10 * time.Minute), // Expired
		TTLSec:     60,
	}
	path := filepath.Join(locksDir, "expired-reentrant.json")
	if err := lockfile.Write(path, expiredLock); err != nil {
		t.Fatalf("Write expired lock error = %v", err)
	}

	// Re-acquire should succeed (same owner, refreshes the expired lock)
	err := Acquire(root, "expired-reentrant", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Reentrant Acquire() of expired lock error = %v", err)
	}

	// Verify lock is now fresh with current process identity
	refreshed, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read refreshed lock error = %v", err)
	}
	if refreshed.Owner != id.Owner {
		t.Errorf("Owner = %q, want %q", refreshed.Owner, id.Owner)
	}
	if refreshed.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", refreshed.PID, os.Getpid())
	}
	if refreshed.TTLSec != 300 {
		t.Errorf("TTLSec = %d, want 300", refreshed.TTLSec)
	}
	if refreshed.IsExpired() {
		t.Error("Lock should not be expired after refresh")
	}
}

func TestAcquire_ReentrantEmitsRenewAudit(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// First acquire
	err := Acquire(root, "audit-reentrant", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Reentrant acquire
	err = Acquire(root, "audit-reentrant", AcquireOptions{TTL: 5 * time.Minute, Auditor: auditor})
	if err != nil {
		t.Fatalf("Reentrant Acquire() error = %v", err)
	}

	events := readAuditEvents(t, root)
	// Should have 2 events: acquire + renew
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events (acquire + renew), got %d", len(events))
	}

	// First event should be acquire
	if events[0].Event != audit.EventAcquire {
		t.Errorf("First event = %q, want %q", events[0].Event, audit.EventAcquire)
	}

	// Second event should be renew (not acquire)
	if events[1].Event != audit.EventRenew {
		t.Errorf("Second event = %q, want %q", events[1].Event, audit.EventRenew)
	}
	if events[1].Name != "audit-reentrant" {
		t.Errorf("Renew event Name = %q, want %q", events[1].Name, "audit-reentrant")
	}
}

func TestAcquireWithWait_ReentrantImmediateSuccess(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// First acquire
	err := Acquire(root, "wait-reentrant", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// AcquireWithWait from same owner should succeed immediately
	start := time.Now()
	err = AcquireWithWait(ctx, root, "wait-reentrant", AcquireOptions{TTL: 10 * time.Minute})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AcquireWithWait() reentrant error = %v", err)
	}

	// Should be near-instant (no waiting)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Reentrant AcquireWithWait took %v, expected near-instant", elapsed)
	}

	// Verify TTL was updated
	path := filepath.Join(root, "locks", "wait-reentrant.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	if lock.TTLSec != 600 {
		t.Errorf("TTLSec = %d, want 600", lock.TTLSec)
	}
}

func TestLockID_AcquireRelease(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	err := Acquire(root, "lid-test", AcquireOptions{
		TTL:     5 * time.Minute,
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Read lockfile and verify lock_id
	path := filepath.Join(root, "locks", "lid-test.json")
	lk, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	if len(lk.LockID) != 32 {
		t.Fatalf("LockID length = %d, want 32", len(lk.LockID))
	}

	// Release
	err = Release(root, "lid-test", ReleaseOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Verify acquire and release events share the same lock_id
	events := readAuditEvents(t, root)
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events, got %d", len(events))
	}

	acquireEvent := events[0]
	releaseEvent := events[1]

	if acquireEvent.Event != audit.EventAcquire {
		t.Errorf("First event = %q, want %q", acquireEvent.Event, audit.EventAcquire)
	}
	if releaseEvent.Event != audit.EventRelease {
		t.Errorf("Second event = %q, want %q", releaseEvent.Event, audit.EventRelease)
	}
	if acquireEvent.LockID != lk.LockID {
		t.Errorf("Acquire event LockID = %q, want %q", acquireEvent.LockID, lk.LockID)
	}
	if releaseEvent.LockID != lk.LockID {
		t.Errorf("Release event LockID = %q, want %q (should match acquire)", releaseEvent.LockID, lk.LockID)
	}
}

func TestLockID_RenewPreservesID(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	err := Acquire(root, "renew-lid", AcquireOptions{
		TTL:     5 * time.Minute,
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Read original lock_id
	path := filepath.Join(root, "locks", "renew-lid.json")
	lk, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	origID := lk.LockID

	// Renew
	err = Renew(root, "renew-lid", RenewOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	// Read lock_id again — must be the same
	lk2, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock after renew error = %v", err)
	}
	if lk2.LockID != origID {
		t.Errorf("LockID after renew = %q, want %q (should be preserved)", lk2.LockID, origID)
	}

	// Verify renew audit event has same lock_id
	events := readAuditEvents(t, root)
	var renewEvent *audit.Event
	for i := range events {
		if events[i].Event == audit.EventRenew {
			renewEvent = &events[i]
			break
		}
	}
	if renewEvent == nil {
		t.Fatal("No renew event found in audit log")
	}
	if renewEvent.LockID != origID {
		t.Errorf("Renew event LockID = %q, want %q", renewEvent.LockID, origID)
	}
}

func TestLockID_ReentrantPreservesID(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	err := Acquire(root, "reent-lid", AcquireOptions{
		TTL:     5 * time.Minute,
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Read original lock_id
	path := filepath.Join(root, "locks", "reent-lid.json")
	lk, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	origID := lk.LockID

	// Reentrant acquire (same owner)
	err = Acquire(root, "reent-lid", AcquireOptions{
		TTL:     10 * time.Minute,
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("Reentrant Acquire() error = %v", err)
	}

	// Read lock_id — must be the same
	lk2, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock after reentrant error = %v", err)
	}
	if lk2.LockID != origID {
		t.Errorf("LockID after reentrant = %q, want %q (should be preserved)", lk2.LockID, origID)
	}

	// Verify renew event from reentrant has same lock_id
	events := readAuditEvents(t, root)
	var renewEvent *audit.Event
	for i := range events {
		if events[i].Event == audit.EventRenew {
			renewEvent = &events[i]
			break
		}
	}
	if renewEvent == nil {
		t.Fatal("No renew event found in audit log for reentrant acquire")
	}
	if renewEvent.LockID != origID {
		t.Errorf("Reentrant renew event LockID = %q, want %q", renewEvent.LockID, origID)
	}
}

func TestLockID_BackwardCompat(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Create a pre-ao9 lockfile (no lock_id) manually
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	id := identity.Current()
	oldLock := &lockfile.Lock{
		Version:    1,
		Name:       "old-lock",
		Owner:      id.Owner,
		Host:       id.Host,
		PID:        id.PID,
		AcquiredAt: time.Now(),
		TTLSec:     300,
	}
	path := filepath.Join(locksDir, "old-lock.json")
	if err := lockfile.Write(path, oldLock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Renew old lock — should work, lock_id stays empty
	err := Renew(root, "old-lock", RenewOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	// Lock_id should still be empty after renew of pre-ao9 lock
	lk, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock after renew error = %v", err)
	}
	if lk.LockID != "" {
		t.Errorf("LockID should be empty for renewed pre-ao9 lock, got %q", lk.LockID)
	}

	// Renew event should have empty lock_id (omitted from JSON)
	events := readAuditEvents(t, root)
	if len(events) == 0 {
		t.Fatal("Expected at least 1 audit event")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.LockID != "" {
		t.Errorf("Renew event LockID should be empty for pre-ao9 lock, got %q", lastEvent.LockID)
	}
}

func TestLockID_UniquePerAcquisition(t *testing.T) {
	root := t.TempDir()
	auditor := audit.NewWriter(root)

	// Acquire, release, acquire again — should get different lock_ids
	err := Acquire(root, "unique-lid", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	path := filepath.Join(root, "locks", "unique-lid.json")
	lk1, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	id1 := lk1.LockID

	err = Release(root, "unique-lid", ReleaseOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	err = Acquire(root, "unique-lid", AcquireOptions{Auditor: auditor})
	if err != nil {
		t.Fatalf("Re-Acquire() error = %v", err)
	}
	lk2, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock error = %v", err)
	}
	id2 := lk2.LockID

	if id1 == id2 {
		t.Errorf("Different acquisitions should have different lock_ids, both got %q", id1)
	}
}
