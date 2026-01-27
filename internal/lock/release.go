package lock

import (
	"errors"
	"fmt"
	"os"

	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
)

var (
	// ErrNotFound is returned when the lock doesn't exist.
	ErrNotFound = errors.New("lock not found")
	// ErrNotOwner is returned when trying to release a lock owned by someone else.
	ErrNotOwner = errors.New("not lock owner")
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

// ReleaseOptions configures lock release.
type ReleaseOptions struct {
	Force bool // Skip ownership check
}

// Release removes a lock file.
// Returns ErrNotFound if lock doesn't exist.
// Returns NotOwnerError if caller doesn't own the lock (unless Force is set).
func Release(rootDir, name string, opts ReleaseOptions) error {
	path := root.LockFilePath(rootDir, name)

	// Check if lock exists
	existing, err := lockfile.Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("read lock: %w", err)
	}

	// Check ownership unless forcing
	if !opts.Force {
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
