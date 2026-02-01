package lock

import (
	"fmt"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
)

// RenewOptions configures lock renewal.
type RenewOptions struct {
	Auditor *audit.Writer // Optional audit writer for event logging
}

// ErrLockStolen is returned when the lock is now owned by someone else.
var ErrLockStolen = fmt.Errorf("lock stolen")

// Renew updates the lock's acquired timestamp to extend its TTL.
// Returns an error if the lock doesn't exist or is owned by someone else.
func Renew(rootDir, name string, opts RenewOptions) error {
	path := root.LockFilePath(rootDir, name)

	// Read current lock
	existing, err := lockfile.Read(path)
	if err != nil {
		return fmt.Errorf("read lock: %w", err)
	}

	// Verify we still own it
	id := identity.Current()
	if existing.Owner != id.Owner || existing.Host != id.Host || existing.PID != id.PID {
		return fmt.Errorf("%w: now owned by %s@%s (pid %d)",
			ErrLockStolen, existing.Owner, existing.Host, existing.PID)
	}

	// Update timestamp and version, then rewrite atomically
	existing.Version = lockfile.CurrentLockfileVersion
	existing.AcquiredAt = time.Now()
	if existing.TTLSec > 0 {
		exp := existing.AcquiredAt.Add(time.Duration(existing.TTLSec) * time.Second)
		existing.ExpiresAt = &exp
	}
	if err := lockfile.Write(path, existing); err != nil {
		return fmt.Errorf("write lock: %w", err)
	}

	// Emit audit event on success
	emitRenewEvent(opts.Auditor, id, name, existing.TTLSec, existing.LockID)

	return nil
}

// emitRenewEvent emits a renew audit event. Safe to call with nil auditor.
func emitRenewEvent(w *audit.Writer, id identity.Identity, name string, ttlSec int, lockID string) {
	if w == nil {
		return
	}
	w.Emit(&audit.Event{
		Event:  audit.EventRenew,
		Name:   name,
		LockID: lockID,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: ttlSec,
	})
}
