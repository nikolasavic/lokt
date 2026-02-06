// Package lock implements lock acquisition and release.
// This file provides opportunistic sweep of stale locks and freezes.
package lock

import (
	"errors"
	"os"
	"strings"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
	"github.com/nikolasavic/lokt/internal/stale"
)

// PruneAllExpired scans the locks/ and freezes/ directories and removes any
// lock that is definitively stale: expired TTL, dead PID on the same host,
// or corrupted. This is a best-effort operation — individual errors are
// collected but never block the caller.
func PruneAllExpired(rootDir string, auditor *audit.Writer) (int, []error) {
	var total int
	var errs []error

	n, e := sweepDir(root.LocksPath(rootDir), rootDir, auditor)
	total += n
	errs = append(errs, e...)

	n, e = sweepDir(root.FreezesPath(rootDir), rootDir, auditor)
	total += n
	errs = append(errs, e...)

	return total, errs
}

// sweepDir scans a single directory and removes stale .json lock files.
func sweepDir(dir, rootDir string, auditor *audit.Writer) (int, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, []error{err}
	}

	id := identity.Current()
	var pruned int
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		lockName := name[:len(name)-5]

		path := dir + "/" + name
		reason, lf := checkStale(path)
		if reason == "" {
			continue
		}

		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			continue
		}
		_ = lockfile.SyncDir(path)
		pruned++

		emitSweepEvent(auditor, id, lockName, reason, lf)
	}

	return pruned, errs
}

// checkStale reads a lock file and returns the stale reason (or "" if not stale).
// Returns the lock for audit event emission; nil if the file was corrupted.
func checkStale(path string) (string, *lockfile.Lock) {
	lf, err := lockfile.Read(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrCorrupted) {
			return "corrupted", nil
		}
		// Unsupported version, empty file, permission error — don't touch.
		return "", nil
	}

	result := stale.Check(lf)
	if result.Stale {
		return string(result.Reason), lf
	}
	return "", nil
}

// emitSweepEvent emits an auto-prune audit event for a swept lock.
func emitSweepEvent(w *audit.Writer, id identity.Identity, name, reason string, lf *lockfile.Lock) {
	if w == nil {
		return
	}
	extra := map[string]any{
		"sweep_reason": reason,
	}
	if lf != nil {
		extra["pruned_owner"] = lf.Owner
		extra["pruned_host"] = lf.Host
		extra["pruned_pid"] = lf.PID
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
