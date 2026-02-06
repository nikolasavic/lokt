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

// EnvLoktNoSweep is the environment variable that disables opportunistic sweep.
const EnvLoktNoSweep = "LOKT_NO_SWEEP"

// PruneAllExpired scans the locks/ and freezes/ directories and removes any
// lock that is definitively stale: expired TTL with dead PID on the same host,
// expired TTL on a cross-host lock (PID cannot be verified), or corrupted.
// Dead PID alone does NOT trigger pruning — the lock/unlock scripting pattern
// intentionally outlives the acquiring process.
// This is a best-effort operation — individual errors are collected but never
// block the caller.
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
//
// The sweep is conservative: it requires BOTH expired TTL AND dead PID before
// removing a same-host lock. Dead PID alone is not sufficient because the
// lock/unlock scripting pattern intentionally outlives the acquiring process.
// Cross-host expired locks are pruned since PID cannot be verified remotely.
func checkStale(path string) (string, *lockfile.Lock) {
	lf, err := lockfile.Read(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrCorrupted) {
			return "corrupted", nil
		}
		// Unsupported version, empty file, permission error — don't touch.
		return "", nil
	}

	// Require expired TTL as minimum condition for sweep.
	if !lf.IsExpired() {
		return "", nil
	}

	// Expired. On same host, also require dead PID.
	hostname, _ := os.Hostname()
	if hostname != "" && hostname == lf.Host {
		if stale.IsProcessAlive(lf.PID) {
			// PID exists — check for recycling via start time.
			if lf.PIDStartNS != 0 {
				if start, err := stale.GetProcessStartTime(lf.PID); err == nil && start == lf.PIDStartNS {
					return "", nil // Same process, still alive
				}
				// Different start time → PID recycled, original holder dead
			} else {
				return "", nil // Can't verify recycling, conservatively skip
			}
		}
		// Dead or recycled PID + expired TTL → prune
		return "expired+dead_pid", lf
	}

	// Cross-host: can't verify PID; expired TTL alone justifies prune.
	return string(stale.ReasonExpired), lf
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
