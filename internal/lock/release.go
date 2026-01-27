package lock

import (
	"errors"
	"fmt"
	"os"

	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
	"github.com/nikolasavic/lokt/internal/stale"
)

var (
	// ErrNotFound is returned when the lock doesn't exist.
	ErrNotFound = errors.New("lock not found")
	// ErrNotOwner is returned when trying to release a lock owned by someone else.
	ErrNotOwner = errors.New("not lock owner")
	// ErrNotStale is returned when trying to break a lock that is not stale.
	ErrNotStale = errors.New("lock not stale")
)

// NotOwnerError provides details about ownership mismatch.
type NotOwnerError struct {
	Lock    *lockfile.Lock
	Current identity.Identity
}

func (e *NotOwnerError) Error() string {
	return fmt.Sprintf("lock %q owned by %s@%s, not %s@%s",
		e.Lock.Name, e.Lock.Owner, e.Lock.Host, e.Current.Owner, e.Current.Host)
}

func (e *NotOwnerError) Unwrap() error {
	return ErrNotOwner
}

// NotStaleError provides details about why a lock is not stale.
type NotStaleError struct {
	Lock   *lockfile.Lock
	Reason stale.Reason
}

func (e *NotStaleError) Error() string {
	if e.Reason == stale.ReasonUnknown {
		return fmt.Sprintf("lock %q held by %s@%s: cannot verify PID on remote host",
			e.Lock.Name, e.Lock.Owner, e.Lock.Host)
	}
	return fmt.Sprintf("lock %q held by %s@%s is not stale (owner PID %d is alive)",
		e.Lock.Name, e.Lock.Owner, e.Lock.Host, e.Lock.PID)
}

func (e *NotStaleError) Unwrap() error {
	return ErrNotStale
}

// ReleaseOptions configures lock release.
type ReleaseOptions struct {
	Force      bool // Skip ownership check (break-glass)
	BreakStale bool // Remove only if lock is stale (expired TTL or dead PID)
}

// Release removes a lock file.
// Returns ErrNotFound if lock doesn't exist.
// Returns NotOwnerError if caller doesn't own the lock (unless Force or BreakStale is set).
// Returns NotStaleError if BreakStale is set but the lock is not stale.
func Release(rootDir, name string, opts ReleaseOptions) error {
	if err := lockfile.ValidateName(name); err != nil {
		return err
	}

	path := root.LockFilePath(rootDir, name)

	// Check if lock exists
	existing, err := lockfile.Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("read lock: %w", err)
	}

	// Handle different release modes
	switch {
	case opts.Force:
		// Force: skip all checks
	case opts.BreakStale:
		// BreakStale: only remove if lock is stale
		result := stale.Check(existing)
		if !result.Stale {
			return &NotStaleError{Lock: existing, Reason: result.Reason}
		}
	default:
		// Normal: check ownership
		id := identity.Current()
		if existing.Owner != id.Owner {
			return &NotOwnerError{Lock: existing, Current: id}
		}
	}

	// Remove the lock file
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("remove lock: %w", err)
	}

	return nil
}
