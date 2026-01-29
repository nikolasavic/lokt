package lock

import (
	"errors"
	"fmt"
	"os"

	"github.com/nikolasavic/lokt/internal/audit"
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
	Force      bool          // Skip ownership check (break-glass)
	BreakStale bool          // Remove only if lock is stale (expired TTL or dead PID)
	Auditor    *audit.Writer // Optional audit writer for event logging
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
		if errors.Is(err, lockfile.ErrCorrupted) {
			// Corrupted lock file — handle based on release mode
			if opts.Force || opts.BreakStale {
				if removeErr := os.Remove(path); removeErr != nil {
					if os.IsNotExist(removeErr) {
						return ErrNotFound
					}
					return fmt.Errorf("remove corrupted lock: %w", removeErr)
				}
				_ = lockfile.SyncDir(path)
				emitCorruptBreakReleaseEvent(opts.Auditor, name)
				return nil
			}
			return fmt.Errorf("lock %q has corrupted data: %w", name, err)
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
	if err := lockfile.SyncDir(path); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}

	// Emit release event
	emitReleaseEvent(opts.Auditor, existing, opts)

	return nil
}

// emitCorruptBreakReleaseEvent emits a corrupt-break audit event during release. Safe to call with nil auditor.
func emitCorruptBreakReleaseEvent(w *audit.Writer, name string) {
	if w == nil {
		return
	}
	id := identity.Current()
	w.Emit(&audit.Event{
		Event: audit.EventCorruptBreak,
		Name:  name,
		Owner: id.Owner,
		Host:  id.Host,
		PID:   id.PID,
	})
}

// emitReleaseEvent emits the appropriate release audit event. Safe to call with nil auditor.
func emitReleaseEvent(w *audit.Writer, lock *lockfile.Lock, opts ReleaseOptions) {
	if w == nil {
		return
	}

	eventType := audit.EventRelease
	if opts.Force {
		eventType = audit.EventForceBreak
	} else if opts.BreakStale {
		eventType = audit.EventStaleBreak
	}

	id := identity.Current()
	w.Emit(&audit.Event{
		Event:  eventType,
		Name:   lock.Name,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: lock.TTLSec,
	})
}
