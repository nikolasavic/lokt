// Package lock implements lock acquisition and release.
package lock

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
	"github.com/nikolasavic/lokt/internal/stale"
)

var (
	// ErrLockHeld is returned when a lock is already held by another owner.
	ErrLockHeld = errors.New("lock held")
)

// HeldError provides details about who holds a contested lock.
type HeldError struct {
	Lock *lockfile.Lock
}

func (e *HeldError) Error() string {
	age := time.Since(e.Lock.AcquiredAt).Truncate(time.Second)
	return fmt.Sprintf("lock %q held by %s@%s (pid %d) for %s",
		e.Lock.Name, e.Lock.Owner, e.Lock.Host, e.Lock.PID, age)
}

func (e *HeldError) Unwrap() error {
	return ErrLockHeld
}

// AcquireOptions configures lock acquisition.
type AcquireOptions struct {
	TTL     time.Duration
	Auditor *audit.Writer // Optional audit writer for event logging
}

// Acquire attempts to atomically acquire a lock.
// Returns HeldError if the lock is already held.
func Acquire(rootDir, name string, opts AcquireOptions) error {
	if err := lockfile.ValidateName(name); err != nil {
		return err
	}

	if err := root.EnsureDirs(rootDir); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	path := root.LockFilePath(rootDir, name)
	id := identity.Current()

	lock := &lockfile.Lock{
		Name:       name,
		Owner:      id.Owner,
		Host:       id.Host,
		PID:        id.PID,
		AcquiredAt: time.Now(),
	}
	if opts.TTL > 0 {
		lock.TTLSec = int(opts.TTL.Seconds())
	}

	// Try atomic create - fails if file exists
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			// Lock exists - read it and check if stale
			existing, readErr := lockfile.Read(path)
			if readErr != nil {
				// File exists but unreadable (likely being written by another process)
				// Return a synthetic HeldError so AcquireWithWait will retry
				return &HeldError{Lock: &lockfile.Lock{Name: name}}
			}

			// Auto-prune: if lock holder is dead (same host only), remove and retry once
			result := stale.Check(existing)
			if result.Stale && result.Reason == stale.ReasonDeadPID {
				if removeErr := os.Remove(path); removeErr == nil {
					// Emit auto-prune event with previous holder info
					emitAutoPruneEvent(opts.Auditor, id, name, existing)

					// Retry acquisition once
					f2, retryErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
					if retryErr == nil {
						_ = f2.Close()
						// Continue to write lock data below
						goto writeLock
					}
					// Retry failed (race condition - another process got it)
					// Fall through to emit deny and return HeldError
					existing2, _ := lockfile.Read(path)
					if existing2 != nil {
						existing = existing2
					}
				}
				// Remove failed, fall through to HeldError
			}

			// Emit deny event
			emitDenyEvent(opts.Auditor, id, name, lock.TTLSec, existing)
			return &HeldError{Lock: existing}
		}
		return fmt.Errorf("create lock file: %w", err)
	}
	_ = f.Close()

writeLock:
	// Write lock data atomically (replaces the empty file)
	if err := lockfile.Write(path, lock); err != nil {
		_ = os.Remove(path) // Clean up on failure
		return fmt.Errorf("write lock file: %w", err)
	}

	// Emit acquire event
	emitAcquireEvent(opts.Auditor, id, name, lock.TTLSec)

	return nil
}

// Backoff parameters for AcquireWithWait polling.
const (
	baseInterval = 50 * time.Millisecond
	maxInterval  = 2 * time.Second
)

// backoffInterval calculates the next poll interval with exponential backoff and jitter.
// Jitter is ±25% to desynchronize competing waiters.
func backoffInterval(attempt int) time.Duration {
	// Exponential backoff: base * 2^attempt, capped at maxInterval
	// Use lookup table to avoid overflow concerns
	multipliers := []time.Duration{1, 2, 4, 8, 16, 32, 64}
	var multiplier time.Duration = 64 // default for attempt >= 6
	if attempt < len(multipliers) {
		multiplier = multipliers[attempt]
	}

	interval := baseInterval * multiplier
	if interval > maxInterval {
		interval = maxInterval
	}

	// Add jitter: multiply by 0.75 to 1.25
	// Using math/rand is fine here - this is timing jitter, not security
	jitter := 0.75 + rand.Float64()*0.5 //nolint:gosec // G404: jitter doesn't need crypto rand
	return time.Duration(float64(interval) * jitter)
}

// AcquireWithWait attempts to acquire a lock, polling until successful or context is cancelled.
// Uses exponential backoff with jitter to avoid thundering herd.
// If the lock is held by a stale process (expired TTL or dead PID), it will be broken automatically.
// Returns nil on successful acquisition, ctx.Err() on cancellation, or another error on failure.
func AcquireWithWait(ctx context.Context, rootDir, name string, opts AcquireOptions) error {
	// First attempt without waiting
	err := Acquire(rootDir, name, opts)
	if err == nil {
		return nil
	}

	var held *HeldError
	if !errors.As(err, &held) {
		return err // Non-held error (validation, permission, etc.), don't retry
	}

	attempt := 0
	for {
		interval := backoffInterval(attempt)
		attempt++

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			// Try to break stale locks before acquiring
			_ = tryBreakStale(rootDir, name)

			err := Acquire(rootDir, name, opts)
			if err == nil {
				return nil
			}
			if !errors.As(err, &held) {
				return err // Non-held error, don't retry
			}
			// Lock still held, continue polling with increased backoff
		}
	}
}

// tryBreakStale attempts to remove a lock if it's stale.
// Returns true if the lock was removed, false otherwise.
func tryBreakStale(rootDir, name string) bool {
	path := root.LockFilePath(rootDir, name)
	existing, err := lockfile.Read(path)
	if err != nil {
		return false
	}

	result := stale.Check(existing)
	if !result.Stale {
		return false
	}

	// Lock is stale, try to remove it
	if err := os.Remove(path); err != nil {
		return false
	}
	return true
}

// emitAcquireEvent emits an acquire audit event. Safe to call with nil auditor.
func emitAcquireEvent(w *audit.Writer, id identity.Identity, name string, ttlSec int) {
	if w == nil {
		return
	}
	w.Emit(&audit.Event{
		Event:  audit.EventAcquire,
		Name:   name,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: ttlSec,
	})
}

// emitDenyEvent emits a deny audit event with holder info. Safe to call with nil auditor.
func emitDenyEvent(w *audit.Writer, id identity.Identity, name string, ttlSec int, holder *lockfile.Lock) {
	if w == nil {
		return
	}
	extra := map[string]any{
		"holder_owner": holder.Owner,
		"holder_host":  holder.Host,
		"holder_pid":   holder.PID,
	}
	w.Emit(&audit.Event{
		Event:  audit.EventDeny,
		Name:   name,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: ttlSec,
		Extra:  extra,
	})
}

// emitAutoPruneEvent emits an auto-prune audit event. Safe to call with nil auditor.
// Records that a stale lock (dead PID on same host) was automatically removed.
func emitAutoPruneEvent(w *audit.Writer, id identity.Identity, name string, pruned *lockfile.Lock) {
	if w == nil {
		return
	}
	extra := map[string]any{
		"pruned_owner": pruned.Owner,
		"pruned_host":  pruned.Host,
		"pruned_pid":   pruned.PID,
	}
	w.Emit(&audit.Event{
		Event: audit.EventAutoPrune,
		Name:  name,
		Owner: id.Owner,
		Host:  id.Host,
		PID:   id.PID,
		Extra: extra,
	})
}
