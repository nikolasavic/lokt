package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lock"
)

// TestGuardHeartbeatPreventsStaleBreak validates that heartbeat renewal
// prevents stale-break from incorrectly breaking active locks.
//
// This test simulates the critical race condition: a guard process with TTL
// actively renewing its lock via heartbeat, while a concurrent contender
// attempts to break the lock with --break-stale.
//
// The test verifies:
// 1. No stale-break succeeds while heartbeat is running
// 2. Lock remains held by original owner throughout heartbeat lifetime
// 3. Contender eventually succeeds after heartbeat stops
func TestGuardHeartbeatPreventsStaleBreak(t *testing.T) {
	root := t.TempDir()
	const lockName = "renewal-race-test"
	const ttl = 2 * time.Second // TTL=2s enables 1s renewal interval

	// Acquire lock with TTL
	err := lock.Acquire(root, lockName, lock.AcquireOptions{TTL: ttl})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Track stale-break attempts and outcomes
	type attempt struct {
		ts      time.Time
		success bool
	}
	var mu sync.Mutex
	var attempts []attempt

	// Start heartbeat goroutine (mirrors runHeartbeat from main.go:771-794)
	ctx, cancelHeartbeat := context.WithCancel(context.Background())
	heartbeatStopped := make(chan struct{})
	go func() {
		defer close(heartbeatStopped)
		// Calculate interval: TTL/2, minimum 500ms
		interval := ttl / 2
		const minInterval = 500 * time.Millisecond
		if interval < minInterval {
			interval = minInterval
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Renew lock (mirrors main.go:787)
				err := lock.Renew(root, lockName, lock.RenewOptions{})
				if err != nil {
					t.Logf("warning: lock renewal failed: %v", err)
				}
			}
		}
	}()

	// Launch contender goroutine attempting stale-break
	contenderDone := make(chan struct{})
	go func() {
		defer close(contenderDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// After heartbeat stops, attempt one final break
				// (should succeed since lock is now truly stale)
				time.Sleep(ttl + 100*time.Millisecond) // Wait for TTL expiry
				err := lock.Release(root, lockName, lock.ReleaseOptions{BreakStale: true})
				mu.Lock()
				attempts = append(attempts, attempt{
					ts:      time.Now(),
					success: err == nil,
				})
				mu.Unlock()
				return
			case <-ticker.C:
				// Attempt stale-break (should fail while heartbeat is active)
				err := lock.Release(root, lockName, lock.ReleaseOptions{BreakStale: true})
				mu.Lock()
				attempts = append(attempts, attempt{
					ts:      time.Now(),
					success: err == nil,
				})
				mu.Unlock()
			}
		}
	}()

	// Let test run for 3 seconds (allows 2-3 renewal cycles at 1s interval)
	testStart := time.Now()
	time.Sleep(3 * time.Second)

	// Stop heartbeat and wait for goroutines to finish
	heartbeatStopTime := time.Now()
	cancelHeartbeat()
	<-heartbeatStopped
	<-contenderDone

	// Analyze results
	mu.Lock()
	defer mu.Unlock()

	if len(attempts) == 0 {
		t.Fatal("No stale-break attempts recorded")
	}

	// Count successful breaks during heartbeat lifetime
	breaksDuringHeartbeat := 0
	breaksAfterHeartbeat := 0

	for _, att := range attempts {
		if att.success {
			if att.ts.Before(heartbeatStopTime) {
				breaksDuringHeartbeat++
				t.Errorf("Stale-break succeeded at %v (heartbeat stopped at %v) — VIOLATION",
					att.ts.Sub(testStart), heartbeatStopTime.Sub(testStart))
			} else {
				breaksAfterHeartbeat++
			}
		}
	}

	// Verify: zero successful breaks during heartbeat lifetime
	if breaksDuringHeartbeat > 0 {
		t.Errorf("Expected 0 successful stale-breaks during heartbeat, got %d", breaksDuringHeartbeat)
	}

	// Verify: at least one successful break after heartbeat stopped
	if breaksAfterHeartbeat == 0 {
		t.Error("Expected at least 1 successful stale-break after heartbeat stopped, got 0")
	}

	t.Logf("Results: %d total attempts, %d breaks during heartbeat (expected 0), %d breaks after stop (expected ≥1)",
		len(attempts), breaksDuringHeartbeat, breaksAfterHeartbeat)
}
