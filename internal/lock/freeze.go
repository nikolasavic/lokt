package lock

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
	"github.com/nikolasavic/lokt/internal/stale"
)

// FreezePrefix is prepended to the lock name for freeze locks.
const FreezePrefix = "freeze-"

// ErrFrozen is returned when a guard operation is blocked by an active freeze.
var ErrFrozen = errors.New("operation frozen")

// FrozenError provides details about the active freeze.
type FrozenError struct {
	Lock *lockfile.Lock
}

func (e *FrozenError) Error() string {
	age := time.Since(e.Lock.AcquiredAt).Truncate(time.Second)
	remaining := ""
	if rem := e.Lock.Remaining(); rem > 0 {
		remaining = fmt.Sprintf(", %s remaining", rem.Truncate(time.Second))
	}
	// Handle both new-style (clean name) and legacy (freeze-prefixed) names
	displayName := e.Lock.Name
	if IsFreezeLock(displayName) {
		displayName = displayName[len(FreezePrefix):]
	}
	if e.Lock.AgentID != "" {
		return fmt.Sprintf("operation %q frozen by %s (agent: %s)@%s for %s%s",
			displayName, e.Lock.Owner, e.Lock.AgentID, e.Lock.Host, age, remaining)
	}
	return fmt.Sprintf("operation %q frozen by %s@%s for %s%s",
		displayName, e.Lock.Owner, e.Lock.Host, age, remaining)
}

func (e *FrozenError) Unwrap() error {
	return ErrFrozen
}

// FreezeOptions configures freeze creation.
type FreezeOptions struct {
	TTL     time.Duration
	Auditor *audit.Writer
}

// Freeze creates a freeze lock for the given name.
// TTL is required (must be > 0). The freeze blocks guard commands until
// unfreeze or TTL expiry.
func Freeze(rootDir, name string, opts FreezeOptions) error {
	if err := lockfile.ValidateName(name); err != nil {
		return err
	}

	if opts.TTL <= 0 {
		return fmt.Errorf("freeze requires a TTL (e.g., --ttl 15m)")
	}

	if err := root.EnsureDirs(rootDir); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	path := root.FreezeFilePath(rootDir, name)
	id := identity.Current()

	now := time.Now()
	ttlSec := int(opts.TTL.Seconds())
	exp := now.Add(time.Duration(ttlSec) * time.Second)
	lock := &lockfile.Lock{
		Version:    lockfile.CurrentLockfileVersion,
		Name:       name,
		LockID:     lockfile.GenerateLockID(),
		Owner:      id.Owner,
		Host:       id.Host,
		PID:        id.PID,
		AgentID:    id.AgentID,
		AcquiredAt: now,
		TTLSec:     ttlSec,
		ExpiresAt:  &exp,
	}
	if startNS, err := stale.GetProcessStartTime(id.PID); err == nil {
		lock.PIDStartNS = startNS
	}

	// Atomic create
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			existing, readErr := lockfile.Read(path)
			if readErr != nil {
				if errors.Is(readErr, lockfile.ErrUnsupportedVersion) {
					return readErr
				}
				if errors.Is(readErr, lockfile.ErrCorrupted) {
					if removeErr := os.Remove(path); removeErr == nil {
						_ = lockfile.SyncDir(path)
						f2, retryErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
						if retryErr == nil {
							_ = f2.Close()
							goto writeLock
						}
					}
				}
				return &HeldError{Lock: &lockfile.Lock{Name: name}}
			}

			// If existing freeze is expired, remove and retry
			if existing.IsExpired() {
				if removeErr := os.Remove(path); removeErr == nil {
					_ = lockfile.SyncDir(path)
					f2, retryErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
					if retryErr == nil {
						_ = f2.Close()
						goto writeLock
					}
				}
			}

			return &HeldError{Lock: existing}
		}
		return fmt.Errorf("create freeze file: %w", err)
	}
	_ = f.Close()

writeLock:
	if err := lockfile.Write(path, lock); err != nil {
		_ = os.Remove(path)
		_ = lockfile.SyncDir(path)
		return fmt.Errorf("write freeze file: %w", err)
	}

	emitFreezeEvent(opts.Auditor, id, name, lock.TTLSec, lock.LockID)
	return nil
}

// UnfreezeOptions configures freeze removal.
type UnfreezeOptions struct {
	Force   bool
	Auditor *audit.Writer
}

// Unfreeze removes a freeze lock.
// Returns ErrNotFound if no freeze exists.
// Returns NotOwnerError if caller doesn't own the freeze (unless Force is set).
// Checks the new freezes/ directory first, then falls back to the legacy
// locks/freeze-<name>.json location for backward compatibility.
func Unfreeze(rootDir, name string, opts UnfreezeOptions) error {
	if err := lockfile.ValidateName(name); err != nil {
		return err
	}

	existing, path, err := readFreezeFile(rootDir, name)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		if errors.Is(err, lockfile.ErrUnsupportedVersion) {
			if opts.Force {
				if removeErr := os.Remove(path); removeErr != nil {
					if os.IsNotExist(removeErr) {
						return ErrNotFound
					}
					return fmt.Errorf("remove freeze: %w", removeErr)
				}
				_ = lockfile.SyncDir(path)
				return nil
			}
			return fmt.Errorf("read freeze: %w", err)
		}
		if errors.Is(err, lockfile.ErrCorrupted) {
			if opts.Force {
				if removeErr := os.Remove(path); removeErr != nil {
					if os.IsNotExist(removeErr) {
						return ErrNotFound
					}
					return fmt.Errorf("remove corrupted freeze: %w", removeErr)
				}
				_ = lockfile.SyncDir(path)
				return nil
			}
			return fmt.Errorf("freeze %q has corrupted data: %w", name, err)
		}
		return fmt.Errorf("read freeze: %w", err)
	}

	if !opts.Force {
		id := identity.Current()
		if existing.Owner != id.Owner {
			return &NotOwnerError{Lock: existing, Current: id}
		}
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("remove freeze: %w", err)
	}
	if err := lockfile.SyncDir(path); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}

	emitUnfreezeEvent(opts.Auditor, existing, opts.Force, existing.LockID)
	return nil
}

// CheckFreeze checks if a freeze is active for the given name.
// Returns nil if no freeze is active (safe to proceed).
// Returns FrozenError if an active, non-expired freeze exists.
// Auto-prunes expired freezes.
// Checks the new freezes/ directory first, then falls back to the legacy
// locks/freeze-<name>.json location for backward compatibility.
func CheckFreeze(rootDir, name string, auditor *audit.Writer) error {
	existing, path, err := readFreezeFile(rootDir, name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No freeze
		}
		if errors.Is(err, lockfile.ErrUnsupportedVersion) {
			// Freeze from newer lokt version — treat as active (fail safe)
			return err
		}
		if errors.Is(err, lockfile.ErrCorrupted) {
			// Corrupted freeze file — remove it
			_ = os.Remove(path)
			_ = lockfile.SyncDir(path)
			return nil
		}
		return nil // Can't read, assume no freeze
	}

	// Auto-prune expired freeze (TTL-based only).
	// Unlike regular locks, freeze locks are NOT auto-pruned by dead PID
	// because the freeze command exits immediately after creating the lock.
	if existing.IsExpired() {
		_ = os.Remove(path)
		_ = lockfile.SyncDir(path)
		return nil
	}

	// Active freeze — emit deny event and return error
	emitFreezeDenyEvent(auditor, name, existing, existing.LockID)
	return &FrozenError{Lock: existing}
}

// readFreezeFile reads a freeze file, checking the new freezes/ directory first
// and falling back to the legacy locks/freeze-<name>.json location.
// Returns the lock data, the path it was found at, and any error.
func readFreezeFile(rootDir, name string) (*lockfile.Lock, string, error) {
	// Try new location first: freezes/<name>.json
	path := root.FreezeFilePath(rootDir, name)
	lk, err := lockfile.Read(path)
	if err == nil {
		return lk, path, nil
	}
	if !os.IsNotExist(err) {
		// File exists at new path but is corrupted/unsupported
		return nil, path, err
	}

	// Fall back to legacy location: locks/freeze-<name>.json
	legacyPath := root.LockFilePath(rootDir, FreezePrefix+name)
	lk, err = lockfile.Read(legacyPath)
	return lk, legacyPath, err
}

// IsFreezeLock returns true if the lock name has the freeze prefix.
//
// Deprecated: With freeze namespace separation, freeze detection should use
// directory membership (freezes/ vs locks/) rather than name prefix.
// Kept for backward compatibility with legacy freeze files in locks/.
func IsFreezeLock(name string) bool {
	return len(name) > len(FreezePrefix) && name[:len(FreezePrefix)] == FreezePrefix
}

func emitFreezeEvent(w *audit.Writer, id identity.Identity, name string, ttlSec int, lockID string) {
	if w == nil {
		return
	}
	w.Emit(&audit.Event{
		Event:   audit.EventFreeze,
		Name:    name,
		LockID:  lockID,
		Owner:   id.Owner,
		Host:    id.Host,
		PID:     id.PID,
		AgentID: id.AgentID,
		TTLSec:  ttlSec,
	})
}

func emitUnfreezeEvent(w *audit.Writer, lock *lockfile.Lock, force bool, lockID string) {
	if w == nil {
		return
	}
	eventType := audit.EventUnfreeze
	if force {
		eventType = audit.EventForceUnfreeze
	}
	id := identity.Current()
	// Strip freeze- prefix for the event name
	name := lock.Name
	if IsFreezeLock(name) {
		name = name[len(FreezePrefix):]
	}
	w.Emit(&audit.Event{
		Event:   eventType,
		Name:    name,
		LockID:  lockID,
		Owner:   id.Owner,
		Host:    id.Host,
		PID:     id.PID,
		AgentID: id.AgentID,
		TTLSec:  lock.TTLSec,
	})
}

func emitFreezeDenyEvent(w *audit.Writer, name string, freeze *lockfile.Lock, lockID string) {
	if w == nil {
		return
	}
	id := identity.Current()
	w.Emit(&audit.Event{
		Event:   audit.EventFreezeDeny,
		Name:    name,
		LockID:  lockID,
		Owner:   id.Owner,
		Host:    id.Host,
		PID:     id.PID,
		AgentID: id.AgentID,
		Extra: map[string]any{
			"freeze_owner": freeze.Owner,
			"freeze_host":  freeze.Host,
			"freeze_pid":   freeze.PID,
		},
	})
}
