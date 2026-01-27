// Package lock implements lock acquisition and release.
package lock

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
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
	f.Close()

	// Write lock data atomically (replaces the empty file)
	if err := lockfile.Write(path, lock); err != nil {
		// Clean up on failure
		os.Remove(path)
		return fmt.Errorf("write lock file: %w", err)
	}

	return nil
}
