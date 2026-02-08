package lock

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

// TestStress_ConcurrentHammer verifies mutual exclusion under heavy
// goroutine contention. 100 goroutines race to acquire the same lock
// using the same O_EXCL atomic-create mechanism as Acquire.
//
// Each goroutine spins until it acquires the lock, enters a critical
// section tracked by an atomic counter, holds briefly, then releases.
// Every goroutine completes multiple acquire-hold-release cycles.
//
// Invariants checked:
//   - At any instant, exactly 0 or 1 goroutine holds the lock
//   - No deadlocks (all goroutines complete within timeout)
//   - No panics
//   - No file corruption (lockfile is valid JSON when held)
//   - All 100 goroutines eventually complete
//
// Run with: go test -race -run TestStress_ConcurrentHammer ./internal/lock/
func TestStress_ConcurrentHammer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}

	rootDir := t.TempDir()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const numGoroutines = 100
	const rounds = 10
	lockPath := filepath.Join(locksDir, "hammer.json")

	var holders int64    // current goroutines in critical section
	var violations int64 // times holders > 1 observed
	var totalAcquires int64
	var completed int64 // goroutines that finished all rounds

	var wg sync.WaitGroup
	start := make(chan struct{}) // barrier for simultaneous start

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start

			owner := fmt.Sprintf("g-%d", id)

			for r := 0; r < rounds; r++ {
				// Spin until we acquire the lock
				for {
					f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
					if err != nil {
						runtime.Gosched()
						continue
					}
					_ = f.Close()
					break
				}

				// Write lock data atomically (temp + rename)
				lk := &lockfile.Lock{
					Version:    1,
					Name:       "hammer",
					Owner:      owner,
					Host:       "stress-test",
					PID:        os.Getpid(),
					AcquiredAt: time.Now(),
					TTLSec:     5,
				}
				if err := lockfile.Write(lockPath, lk); err != nil {
					_ = os.Remove(lockPath)
					t.Errorf("goroutine %d round %d: lockfile.Write error: %v", id, r, err)
					continue
				}

				// === CRITICAL SECTION ===
				n := atomic.AddInt64(&holders, 1)
				if n != 1 {
					atomic.AddInt64(&violations, 1)
				}
				atomic.AddInt64(&totalAcquires, 1)

				// Verify lockfile integrity while holding
				readLk, readErr := lockfile.Read(lockPath)
				if readErr != nil {
					t.Errorf("goroutine %d round %d: lockfile corrupt while holding: %v", id, r, readErr)
				} else if readLk.Owner != owner {
					t.Errorf("goroutine %d round %d: owner = %q, want %q", id, r, readLk.Owner, owner)
				}

				// Hold briefly — random duration to vary scheduling
				time.Sleep(time.Duration(rand.Intn(50)) * time.Microsecond) //nolint:gosec // test jitter
				runtime.Gosched()

				atomic.AddInt64(&holders, -1)
				// === END CRITICAL SECTION ===

				_ = os.Remove(lockPath)
			}

			atomic.AddInt64(&completed, 1)
		}(i)
	}

	close(start)

	// Deadlock detection
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("deadlock: only %d/%d goroutines completed within 60s",
			atomic.LoadInt64(&completed), numGoroutines)
	}

	// Assertions
	v := atomic.LoadInt64(&violations)
	total := atomic.LoadInt64(&totalAcquires)
	comp := atomic.LoadInt64(&completed)

	if v > 0 {
		t.Errorf("mutual exclusion violated %d times out of %d acquires", v, total)
	}
	if total != numGoroutines*rounds {
		t.Errorf("total acquires = %d, want %d", total, numGoroutines*rounds)
	}
	if comp != numGoroutines {
		t.Errorf("only %d/%d goroutines completed", comp, numGoroutines)
	}

	t.Logf("completed: %d acquires across %d goroutines × %d rounds", total, numGoroutines, rounds)
}
