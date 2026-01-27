// Package lock implements lock acquisition and release.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

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
	TTL time.Duration
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
			// Lock exists - read it and return holder info
			existing, readErr := lockfile.Read(path)
			if readErr != nil {
				return fmt.Errorf("lock exists but unreadable: %w", readErr)
			}
			return &HeldError{Lock: existing}
		}
		return fmt.Errorf("create lock file: %w", err)
	}
	_ = f.Close()

	// Write lock data atomically (replaces the empty file)
	if err := lockfile.Write(path, lock); err != nil {
		_ = os.Remove(path) // Clean up on failure
		return fmt.Errorf("write lock file: %w", err)
	}

	return nil
}

// DefaultPollInterval is the default polling interval for AcquireWithWait.
const DefaultPollInterval = 100 * time.Millisecond

// AcquireWithWait attempts to acquire a lock, polling until successful or context is cancelled.
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

	ticker := time.NewTicker(DefaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Try to break stale locks before acquiring
			_ = tryBreakStale(rootDir, name)

			err := Acquire(rootDir, name, opts)
			if err == nil {
				return nil
			}
			if !errors.As(err, &held) {
				return err // Non-held error, don't retry
			}
			// Lock still held, continue polling
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
