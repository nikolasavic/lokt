// Package stale provides detection of stale/orphaned locks.
package stale

import (
	"os"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

// Reason describes why a lock is considered stale.
type Reason string

const (
	ReasonExpired  Reason = "expired"  // TTL has elapsed
	ReasonDeadPID  Reason = "dead_pid" // Process no longer running
	ReasonNotStale Reason = ""         // Lock is not stale
	ReasonUnknown  Reason = "unknown"  // Cannot determine (cross-host)
)

// Result contains the staleness check result.
type Result struct {
	Stale  bool
	Reason Reason
}

// Check determines if a lock is stale.
// A lock is stale if:
// - TTL has expired, OR
// - The owning process is dead (same host only)
//
// For cross-host locks, PID cannot be verified so only TTL is checked.
func Check(lock *lockfile.Lock) Result {
	// Check TTL expiry first (works for any host)
	if lock.IsExpired() {
		return Result{Stale: true, Reason: ReasonExpired}
	}

	// Check PID liveness (only meaningful on same host)
	hostname, err := os.Hostname()
	if err != nil || hostname != lock.Host {
		// Cannot verify cross-host locks
		return Result{Stale: false, Reason: ReasonUnknown}
	}

	// Same host - check if process is alive
	if !IsProcessAlive(lock.PID) {
		return Result{Stale: true, Reason: ReasonDeadPID}
	}

	return Result{Stale: false, Reason: ReasonNotStale}
}
