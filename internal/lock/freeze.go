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
	if e.Lock.TTLSec > 0 {
		rem := time.Duration(e.Lock.TTLSec)*time.Second - e.Lock.Age()
		if rem > 0 {
			remaining = fmt.Sprintf(", %s remaining", rem.Truncate(time.Second))
		}
	}
	return fmt.Sprintf("operation %q frozen by %s@%s for %s%s",
		e.Lock.Name[len(FreezePrefix):], e.Lock.Owner, e.Lock.Host, age, remaining)
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

	freezeName := FreezePrefix + name
	path := root.LockFilePath(rootDir, freezeName)
	id := identity.Current()

	lock := &lockfile.Lock{
		Name:       freezeName,
		Owner:      id.Owner,
		Host:       id.Host,
		PID:        id.PID,
		AcquiredAt: time.Now(),
		TTLSec:     int(opts.TTL.Seconds()),
	}

	// Atomic create
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			existing, readErr := lockfile.Read(path)
			if readErr != nil {
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
				return &HeldError{Lock: &lockfile.Lock{Name: freezeName}}
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

	emitFreezeEvent(opts.Auditor, id, name, lock.TTLSec)
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
func Unfreeze(rootDir, name string, opts UnfreezeOptions) error {
	if err := lockfile.ValidateName(name); err != nil {
		return err
	}

	freezeName := FreezePrefix + name
	path := root.LockFilePath(rootDir, freezeName)

	existing, err := lockfile.Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
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

	emitUnfreezeEvent(opts.Auditor, existing, opts.Force)
	return nil
}

// CheckFreeze checks if a freeze is active for the given name.
// Returns nil if no freeze is active (safe to proceed).
// Returns FrozenError if an active, non-expired freeze exists.
// Auto-prunes expired freezes.
func CheckFreeze(rootDir, name string, auditor *audit.Writer) error {
	freezeName := FreezePrefix + name
	path := root.LockFilePath(rootDir, freezeName)

	existing, err := lockfile.Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No freeze
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
	emitFreezeDenyEvent(auditor, name, existing)
	return &FrozenError{Lock: existing}
}

// IsFreezelock returns true if the lock name has the freeze prefix.
func IsFreezeLock(name string) bool {
	return len(name) > len(FreezePrefix) && name[:len(FreezePrefix)] == FreezePrefix
}

func emitFreezeEvent(w *audit.Writer, id identity.Identity, name string, ttlSec int) {
	if w == nil {
		return
	}
	w.Emit(&audit.Event{
		Event:  audit.EventFreeze,
		Name:   name,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: ttlSec,
	})
}

func emitUnfreezeEvent(w *audit.Writer, lock *lockfile.Lock, force bool) {
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
		Event:  eventType,
		Name:   name,
		Owner:  id.Owner,
		Host:   id.Host,
		PID:    id.PID,
		TTLSec: lock.TTLSec,
	})
}

func emitFreezeDenyEvent(w *audit.Writer, name string, freeze *lockfile.Lock) {
	if w == nil {
		return
	}
	id := identity.Current()
	w.Emit(&audit.Event{
		Event: audit.EventFreezeDeny,
		Name:  name,
		Owner: id.Owner,
		Host:  id.Host,
		PID:   id.PID,
		Extra: map[string]any{
			"freeze_owner": freeze.Owner,
			"freeze_host":  freeze.Host,
			"freeze_pid":   freeze.PID,
		},
	})
}
