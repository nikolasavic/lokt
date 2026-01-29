package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/doctor"
	"github.com/nikolasavic/lokt/internal/lock"
	"github.com/nikolasavic/lokt/internal/lockfile"
	"github.com/nikolasavic/lokt/internal/root"
	"github.com/nikolasavic/lokt/internal/stale"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Exit codes
const (
	ExitOK       = 0
	ExitError    = 1
	ExitLockHeld = 2
	ExitNotFound = 3
	ExitNotOwner = 4
	ExitUsage    = 64
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(ExitUsage)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var code int
	switch cmd {
	case "version":
		fmt.Printf("lokt %s (commit: %s, built: %s)\n", version, commit, date)
	case "lock":
		code = cmdLock(args)
	case "unlock":
		code = cmdUnlock(args)
	case "status":
		code = cmdStatus(args)
	case "guard":
		code = cmdGuard(args)
	case "freeze":
		code = cmdFreeze(args)
	case "unfreeze":
		code = cmdUnfreeze(args)
	case "audit":
		code = cmdAudit(args)
	case "doctor":
		code = cmdDoctor(args)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		code = ExitUsage
	}
	os.Exit(code)
}

func usage() {
	fmt.Println("lokt - file-based lock manager")
	fmt.Println()
	fmt.Println("Usage: lokt <command> [options] [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  lock <name>       Acquire a lock")
	fmt.Println("    --ttl duration      Lock TTL (e.g., 5m, 1h)")
	fmt.Println("    --wait              Wait for lock to be free")
	fmt.Println("    --timeout duration  Maximum wait time (requires --wait)")
	fmt.Println("  unlock <name>     Release a lock")
	fmt.Println("    --force         Remove without ownership check (break-glass)")
	fmt.Println("    --break-stale   Remove only if stale (expired TTL or dead PID)")
	fmt.Println("  status [name]     Show lock status")
	fmt.Println("    --json          Output in JSON format")
	fmt.Println("    --prune-expired Remove expired locks while listing")
	fmt.Println("  guard <name> -- <cmd...>")
	fmt.Println("                    Run command while holding lock")
	fmt.Println("    --ttl duration      Lock TTL (e.g., 5m, 1h)")
	fmt.Println("    --wait              Wait for lock to be free")
	fmt.Println("    --timeout duration  Maximum wait time (requires --wait)")
	fmt.Println("  freeze <name>     Temporarily block guard commands")
	fmt.Println("    --ttl duration      Freeze duration (required, e.g., 15m, 1h)")
	fmt.Println("  unfreeze <name>   Remove a freeze early")
	fmt.Println("    --force         Remove without ownership check (break-glass)")
	fmt.Println("  audit             Query audit log")
	fmt.Println("    --since duration|ts Show events since (e.g., 1h, 2026-01-27T10:00:00Z)")
	fmt.Println("    --name lock         Filter by lock name")
	fmt.Println("  doctor            Validate lokt setup")
	fmt.Println("    --json          Output in JSON format")
	fmt.Println("  version           Show version info")
	fmt.Println()
	fmt.Println("Exit codes:")
	fmt.Println("  0  Success")
	fmt.Println("  1  General error")
	fmt.Println("  2  Lock held by another owner")
	fmt.Println("  3  Lock not found")
	fmt.Println("  4  Not lock owner")
}

func cmdLock(args []string) int {
	fs := flag.NewFlagSet("lock", flag.ExitOnError)
	ttl := fs.Duration("ttl", 0, "Lock TTL (e.g., 5m, 1h)")
	wait := fs.Bool("wait", false, "Wait for lock to be free")
	timeout := fs.Duration("timeout", 0, "Maximum time to wait (requires --wait)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt lock [--ttl duration] [--wait] [--timeout duration] <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	if *ttl < 0 {
		fmt.Fprintln(os.Stderr, "error: TTL must be positive (e.g., 5m, 1h)")
		return ExitUsage
	}

	if *timeout > 0 && !*wait {
		fmt.Fprintln(os.Stderr, "error: --timeout requires --wait")
		return ExitUsage
	}

	if *timeout < 0 {
		fmt.Fprintln(os.Stderr, "error: --timeout must be positive (e.g., 5s, 1m)")
		return ExitUsage
	}

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditor := audit.NewWriter(rootDir)
	opts := lock.AcquireOptions{TTL: *ttl, Auditor: auditor}

	if *wait {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		if *timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, *timeout)
			defer cancel()
		}

		err = lock.AcquireWithWait(ctx, rootDir, name, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "interrupted")
				return ExitError
			}
			if errors.Is(err, context.DeadlineExceeded) {
				// Timeout - try to get current holder info
				path := root.LockFilePath(rootDir, name)
				if lf, readErr := readLockFile(path); readErr == nil {
					age := time.Since(lf.AcquiredAt).Truncate(time.Second)
					fmt.Fprintf(os.Stderr, "error: timeout waiting for lock %q held by %s@%s (pid %d) for %s\n",
						name, lf.Owner, lf.Host, lf.PID, age)
				} else {
					fmt.Fprintf(os.Stderr, "error: timeout waiting for lock %q\n", name)
				}
				return ExitLockHeld
			}
			var held *lock.HeldError
			if errors.As(err, &held) {
				fmt.Fprintf(os.Stderr, "error: %v\n", held)
				return ExitLockHeld
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
	} else {
		err = lock.Acquire(rootDir, name, opts)
		if err != nil {
			var held *lock.HeldError
			if errors.As(err, &held) {
				fmt.Fprintf(os.Stderr, "error: %v\n", held)
				return ExitLockHeld
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
	}

	fmt.Printf("acquired lock %q\n", name)
	return ExitOK
}

func cmdUnlock(args []string) int {
	fs := flag.NewFlagSet("unlock", flag.ExitOnError)
	force := fs.Bool("force", false, "Remove lock without ownership check (break-glass)")
	breakStale := fs.Bool("break-stale", false, "Remove lock only if stale (expired TTL or dead PID)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt unlock [--force | --break-stale] <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditor := audit.NewWriter(rootDir)
	err = lock.Release(rootDir, name, lock.ReleaseOptions{
		Force:      *force,
		BreakStale: *breakStale,
		Auditor:    auditor,
	})
	if err != nil {
		if errors.Is(err, lock.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "error: lock %q not found\n", name)
			return ExitNotFound
		}
		var notOwner *lock.NotOwnerError
		if errors.As(err, &notOwner) {
			fmt.Fprintf(os.Stderr, "error: %v\n", notOwner)
			return ExitNotOwner
		}
		var notStale *lock.NotStaleError
		if errors.As(err, &notStale) {
			fmt.Fprintf(os.Stderr, "error: %v\n", notStale)
			return ExitError
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("released lock %q\n", name)
	return ExitOK
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	pruneExpired := fs.Bool("prune-expired", false, "Remove expired locks while listing")
	jsonOutput := fs.Bool("json", false, "Output in JSON format")
	_ = fs.Parse(args)

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	locksDir := root.LocksPath(rootDir)
	entries, err := os.ReadDir(locksDir)
	if err != nil {
		if os.IsNotExist(err) {
			if *jsonOutput {
				fmt.Println("[]")
			} else {
				fmt.Println("no locks")
			}
			return ExitOK
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	// If a specific lock name given, show just that one
	if fs.NArg() > 0 {
		name := fs.Arg(0)
		if *pruneExpired {
			return showLockWithPrune(rootDir, name, *jsonOutput)
		}
		return showLock(rootDir, name, *jsonOutput)
	}

	// List all locks
	if len(entries) == 0 {
		if *jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("no locks")
		}
		return ExitOK
	}

	var outputs []statusOutput
	pruned := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			lockName := name[:len(name)-5]
			if *pruneExpired {
				if pruneLockIfExpired(rootDir, lockName) {
					pruned++
					continue
				}
			}
			if *jsonOutput {
				path := root.LockFilePath(rootDir, lockName)
				lf, err := readLockFile(path)
				if err == nil {
					outputs = append(outputs, lockToStatusOutput(lf))
				}
			} else {
				showLockBrief(rootDir, lockName)
			}
		}
	}

	if *jsonOutput {
		if outputs == nil {
			outputs = []statusOutput{}
		}
		data, _ := json.MarshalIndent(outputs, "", "  ")
		fmt.Println(string(data))
	}

	if pruned > 0 && !*jsonOutput {
		fmt.Printf("\npruned %d expired lock(s)\n", pruned)
	}
	return ExitOK
}

func cmdGuard(args []string) int {
	// Find "--" separator
	dashIdx := -1
	for i, arg := range args {
		if arg == "--" {
			dashIdx = i
			break
		}
	}
	if dashIdx == -1 || dashIdx == 0 || dashIdx == len(args)-1 {
		fmt.Fprintln(os.Stderr, "usage: lokt guard [flags] <name> -- <command...>")
		return ExitUsage
	}

	// Parse flags (before --)
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	ttl := fs.Duration("ttl", 0, "Lock TTL (e.g., 5m, 1h)")
	wait := fs.Bool("wait", false, "Wait for lock to be free")
	timeout := fs.Duration("timeout", 0, "Maximum time to wait (requires --wait)")
	if err := fs.Parse(args[:dashIdx]); err != nil {
		fmt.Fprintln(os.Stderr, "usage: lokt guard [flags] <name> -- <command...>")
		return ExitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt guard [flags] <name> -- <command...>")
		return ExitUsage
	}
	name := fs.Arg(0)
	cmdArgs := args[dashIdx+1:]

	if *ttl < 0 {
		fmt.Fprintln(os.Stderr, "error: TTL must be positive (e.g., 5m, 1h)")
		return ExitUsage
	}

	if *timeout > 0 && !*wait {
		fmt.Fprintln(os.Stderr, "error: --timeout requires --wait")
		return ExitUsage
	}

	if *timeout < 0 {
		fmt.Fprintln(os.Stderr, "error: --timeout must be positive (e.g., 5s, 1m)")
		return ExitUsage
	}

	// Resolve root
	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditor := audit.NewWriter(rootDir)

	// Check for active freeze before acquiring
	if err := lock.CheckFreeze(rootDir, name, auditor); err != nil {
		var frozen *lock.FrozenError
		if errors.As(err, &frozen) {
			fmt.Fprintf(os.Stderr, "error: %v\n", frozen)
			return ExitLockHeld
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	opts := lock.AcquireOptions{TTL: *ttl, Auditor: auditor}

	// Acquire lock (with optional wait)
	if *wait {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		if *timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, *timeout)
			defer cancel()
		}

		err = lock.AcquireWithWait(ctx, rootDir, name, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "interrupted")
				return ExitError
			}
			if errors.Is(err, context.DeadlineExceeded) {
				path := root.LockFilePath(rootDir, name)
				if lf, readErr := readLockFile(path); readErr == nil {
					age := time.Since(lf.AcquiredAt).Truncate(time.Second)
					fmt.Fprintf(os.Stderr, "error: timeout waiting for lock %q held by %s@%s (pid %d) for %s\n",
						name, lf.Owner, lf.Host, lf.PID, age)
				} else {
					fmt.Fprintf(os.Stderr, "error: timeout waiting for lock %q\n", name)
				}
				return ExitLockHeld
			}
			var held *lock.HeldError
			if errors.As(err, &held) {
				fmt.Fprintf(os.Stderr, "error: %v\n", held)
				return ExitLockHeld
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
	} else {
		if err := lock.Acquire(rootDir, name, opts); err != nil {
			var held *lock.HeldError
			if errors.As(err, &held) {
				fmt.Fprintf(os.Stderr, "error: %v\n", held)
				return ExitLockHeld
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
	}

	// Ensure release on all paths
	released := false
	releaseLock := func() {
		if !released {
			_ = lock.Release(rootDir, name, lock.ReleaseOptions{Auditor: auditor})
			released = true
		}
	}
	defer releaseLock()

	// Start heartbeat goroutine if TTL is set
	var cancelHeartbeat context.CancelFunc
	if *ttl > 0 {
		var heartbeatCtx context.Context
		heartbeatCtx, cancelHeartbeat = context.WithCancel(context.Background())
		go runHeartbeat(heartbeatCtx, rootDir, name, *ttl, auditor)
	}
	defer func() {
		if cancelHeartbeat != nil {
			cancelHeartbeat()
		}
	}()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Run child command
	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to start command: %v\n", err)
		return ExitError
	}

	// Wait for child or signal
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()

	select {
	case sig := <-sigCh:
		// Forward signal to child
		_ = child.Process.Signal(sig)
		<-done // wait for child to exit
		releaseLock()
		// Exit with 128 + signal number (standard Unix convention)
		if s, ok := sig.(syscall.Signal); ok {
			return 128 + int(s)
		}
		return ExitError
	case err := <-done:
		if err == nil {
			return ExitOK
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
}

// runHeartbeat periodically renews the lock's TTL while the context is active.
// It runs at TTL/2 intervals to ensure the lock is renewed before expiration.
// Renewal failures are logged as warnings but don't stop the heartbeat.
func runHeartbeat(ctx context.Context, rootDir, name string, ttl time.Duration, auditor *audit.Writer) {
	// Calculate interval: TTL/2, with a minimum of 500ms
	interval := ttl / 2
	const minInterval = 500 * time.Millisecond
	if interval < minInterval {
		interval = minInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := lock.Renew(rootDir, name, lock.RenewOptions{Auditor: auditor})
			if err != nil {
				// Log warning but continue - child may still complete successfully
				fmt.Fprintf(os.Stderr, "warning: lock renewal failed: %v\n", err)
			}
		}
	}
}

func showLock(rootDir, name string, jsonOutput bool) int {
	path := root.LockFilePath(rootDir, name)
	lf, err := readLockFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "lock %q not found\n", name)
			return ExitNotFound
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	if jsonOutput {
		output := lockToStatusOutput(lf)
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
		return ExitOK
	}

	age := time.Since(lf.AcquiredAt).Truncate(time.Second)
	fmt.Printf("name:     %s\n", lf.Name)
	fmt.Printf("owner:    %s\n", lf.Owner)
	fmt.Printf("host:     %s\n", lf.Host)
	fmt.Printf("pid:      %d (%s)\n", lf.PID, pidLiveness(lf))
	fmt.Printf("age:      %s\n", age)
	if lf.TTLSec > 0 {
		fmt.Printf("ttl:      %ds\n", lf.TTLSec)
		if lf.IsExpired() {
			fmt.Println("status:   EXPIRED")
		}
	}
	return ExitOK
}

func showLockBrief(rootDir, name string) {
	path := root.LockFilePath(rootDir, name)
	lf, err := readLockFile(path)
	if err != nil {
		return
	}

	age := time.Since(lf.AcquiredAt).Truncate(time.Second)
	status := ""
	if lock.IsFreezeLock(name) {
		status = " [FROZEN]"
	}
	if lf.IsExpired() {
		status += " [EXPIRED]"
	} else if liveness := pidLiveness(lf); liveness == "dead" {
		status += " [DEAD]"
	}
	fmt.Printf("%-20s  %s@%s  %s%s\n", name, lf.Owner, lf.Host, age, status)
}

// showLockWithPrune shows a lock and removes it if expired.
func showLockWithPrune(rootDir, name string, jsonOutput bool) int {
	path := root.LockFilePath(rootDir, name)
	lf, err := readLockFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "lock %q not found\n", name)
			return ExitNotFound
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	if lf.IsExpired() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error removing lock: %v\n", err)
			return ExitError
		}
		_ = lockfile.SyncDir(path)
		if !jsonOutput {
			fmt.Printf("pruned expired lock %q\n", name)
		}
		return ExitOK
	}

	// Not expired, show normally
	return showLock(rootDir, name, jsonOutput)
}

// pruneLockIfExpired removes a lock if expired, returns true if pruned.
func pruneLockIfExpired(rootDir, name string) bool {
	path := root.LockFilePath(rootDir, name)
	lf, err := readLockFile(path)
	if err != nil {
		return false
	}

	if !lf.IsExpired() {
		return false
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false
	}
	_ = lockfile.SyncDir(path)

	fmt.Printf("pruned: %s (expired)\n", name)
	return true
}

func readLockFile(path string) (*lockFile, error) {
	// Import lockfile package inline to avoid import cycle
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lf lockFile
	if err := parseJSON(data, &lf); err != nil {
		return nil, err
	}
	return &lf, nil
}

type lockFile struct {
	Name       string    `json:"name"`
	Owner      string    `json:"owner"`
	Host       string    `json:"host"`
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_ts"`
	TTLSec     int       `json:"ttl_sec,omitempty"`
}

// statusOutput is the JSON structure for status --json output.
type statusOutput struct {
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	Host       string `json:"host"`
	PID        int    `json:"pid"`
	AcquiredAt string `json:"acquired_ts"`
	TTLSec     int    `json:"ttl_sec,omitempty"`
	AgeSec     int    `json:"age_sec"`
	Expired    bool   `json:"expired"`
	PIDStatus  string `json:"pid_status"`
	Freeze     bool   `json:"freeze,omitempty"`
}

func lockToStatusOutput(lf *lockFile) statusOutput {
	out := statusOutput{
		Name:       lf.Name,
		Owner:      lf.Owner,
		Host:       lf.Host,
		PID:        lf.PID,
		AcquiredAt: lf.AcquiredAt.Format(time.RFC3339),
		TTLSec:     lf.TTLSec,
		AgeSec:     int(time.Since(lf.AcquiredAt).Seconds()),
		Expired:    lf.IsExpired(),
		PIDStatus:  pidLiveness(lf),
	}
	if lock.IsFreezeLock(lf.Name) {
		out.Freeze = true
	}
	return out
}

func (l *lockFile) IsExpired() bool {
	if l.TTLSec <= 0 {
		return false
	}
	return time.Since(l.AcquiredAt) > time.Duration(l.TTLSec)*time.Second
}

func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// pidLiveness returns "alive", "dead", or "unknown" based on PID status.
func pidLiveness(lock *lockFile) string {
	hostname, err := os.Hostname()
	if err != nil || hostname != lock.Host {
		return "unknown"
	}
	if stale.IsProcessAlive(lock.PID) {
		return "alive"
	}
	return "dead"
}

func cmdFreeze(args []string) int {
	fs := flag.NewFlagSet("freeze", flag.ExitOnError)
	ttl := fs.Duration("ttl", 0, "Freeze duration (required, e.g., 15m, 1h)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt freeze --ttl <duration> <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	if *ttl <= 0 {
		fmt.Fprintln(os.Stderr, "error: --ttl is required for freeze (e.g., --ttl 15m)")
		return ExitUsage
	}

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditor := audit.NewWriter(rootDir)
	err = lock.Freeze(rootDir, name, lock.FreezeOptions{TTL: *ttl, Auditor: auditor})
	if err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			fmt.Fprintf(os.Stderr, "error: %v\n", held)
			return ExitLockHeld
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("frozen %q for %s\n", name, *ttl)
	return ExitOK
}

func cmdUnfreeze(args []string) int {
	fs := flag.NewFlagSet("unfreeze", flag.ExitOnError)
	force := fs.Bool("force", false, "Remove freeze without ownership check (break-glass)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt unfreeze [--force] <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditor := audit.NewWriter(rootDir)
	err = lock.Unfreeze(rootDir, name, lock.UnfreezeOptions{Force: *force, Auditor: auditor})
	if err != nil {
		if errors.Is(err, lock.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "error: freeze %q not found\n", name)
			return ExitNotFound
		}
		var notOwner *lock.NotOwnerError
		if errors.As(err, &notOwner) {
			fmt.Fprintf(os.Stderr, "error: %v\n", notOwner)
			return ExitNotOwner
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("unfrozen %q\n", name)
	return ExitOK
}

func cmdAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	since := fs.String("since", "", "Show events since duration (1h, 30m) or timestamp (RFC3339)")
	tail := fs.Bool("tail", false, "Follow audit log for new events (like tail -f)")
	name := fs.String("name", "", "Filter by lock name")
	_ = fs.Parse(args)

	// Validate: --since and --tail are mutually exclusive
	if *since != "" && *tail {
		fmt.Fprintln(os.Stderr, "error: --since and --tail are mutually exclusive")
		return ExitUsage
	}

	// Require at least one mode
	if *since == "" && !*tail {
		fmt.Fprintln(os.Stderr, "usage: lokt audit --since <duration|timestamp> [--name <lock>]")
		fmt.Fprintln(os.Stderr, "       lokt audit --tail [--name <lock>]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  --since: query historical events")
		fmt.Fprintln(os.Stderr, "    duration: 1h, 30m, 24h")
		fmt.Fprintln(os.Stderr, "    timestamp: 2026-01-27T10:00:00Z (RFC3339)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  --tail: follow log for new events (Ctrl+C to stop)")
		return ExitUsage
	}

	// Handle tail mode
	if *tail {
		return cmdAuditTail(*name)
	}

	// Parse --since: try duration first, then RFC3339
	sinceTime, err := parseSince(*since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --since value %q: %v\n", *since, err)
		fmt.Fprintln(os.Stderr, "  expected duration (1h, 30m) or RFC3339 timestamp")
		return ExitUsage
	}

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditPath := filepath.Join(rootDir, "audit.log")
	f, err := os.Open(auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No audit log yet - empty output is fine
			return ExitOK
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event auditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// Skip malformed lines
			continue
		}

		// Filter by time
		if event.Timestamp.Before(sinceTime) {
			continue
		}

		// Filter by name if specified
		if *name != "" && event.Name != *name {
			continue
		}

		// Output matching event
		fmt.Println(string(line))
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading audit log: %v\n", err)
		return ExitError
	}

	return ExitOK
}

// auditEvent is the JSON structure for audit log entries.
type auditEvent struct {
	Timestamp time.Time      `json:"ts"`
	Event     string         `json:"event"`
	Name      string         `json:"name"`
	Owner     string         `json:"owner"`
	Host      string         `json:"host"`
	PID       int            `json:"pid"`
	TTLSec    int            `json:"ttl_sec,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// parseSince parses a duration string (e.g., "1h", "30m") or RFC3339 timestamp.
// Returns the time after which events should be shown.
func parseSince(s string) (time.Time, error) {
	// Try duration first
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}

	// Try RFC3339 timestamp
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("not a valid duration or RFC3339 timestamp")
}

// cmdAuditTail follows the audit log for new events (like tail -f).
// It polls the file for new content and prints matching events.
// Exits cleanly on SIGINT/SIGTERM.
func cmdAuditTail(nameFilter string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	auditPath := filepath.Join(rootDir, "audit.log")
	return tailAuditLog(ctx, auditPath, nameFilter)
}

// tailAuditLog implements the polling loop for following the audit log.
// It handles file creation, truncation, and graceful shutdown.
func tailAuditLog(ctx context.Context, path string, nameFilter string) int {
	const pollInterval = 200 * time.Millisecond

	var (
		f      *os.File
		offset int64
		err    error
	)

	// Wait for file to exist
	for {
		f, err = os.Open(path)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}

		// File doesn't exist yet - wait for creation
		select {
		case <-ctx.Done():
			return ExitOK
		case <-time.After(pollInterval):
			continue
		}
	}
	defer func() { _ = f.Close() }()

	// Seek to end to start tailing from current position
	offset, err = f.Seek(0, 2) // SEEK_END
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	reader := bufio.NewReader(f)

	// Main polling loop
	for {
		select {
		case <-ctx.Done():
			return ExitOK
		default:
		}

		// Check for file changes (truncation, deletion)
		stat, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// File was deleted - wait for recreation
				_ = f.Close()
				f = nil

				for {
					select {
					case <-ctx.Done():
						return ExitOK
					case <-time.After(pollInterval):
					}

					f, err = os.Open(path)
					if err == nil {
						offset = 0
						reader = bufio.NewReader(f)
						break
					}
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}

		// Detect truncation (file size decreased)
		if stat.Size() < offset {
			_, err = f.Seek(0, 0) // SEEK_SET
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return ExitError
			}
			offset = 0
			reader.Reset(f)
		}

		// Read available lines
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				// No more data available
				break
			}

			offset += int64(len(line))

			// Trim newline for processing
			line = line[:len(line)-1]
			if len(line) == 0 {
				continue
			}

			var event auditEvent
			if err := json.Unmarshal(line, &event); err != nil {
				// Skip malformed lines
				continue
			}

			// Apply name filter if specified
			if nameFilter != "" && event.Name != nameFilter {
				continue
			}

			// Output matching event
			fmt.Println(string(line))
		}

		// Wait before next poll
		select {
		case <-ctx.Done():
			return ExitOK
		case <-time.After(pollInterval):
		}
	}
}

// doctorOutput is the JSON structure for doctor command output.
type doctorOutput struct {
	RootMethod string               `json:"root_method"`
	RootPath   string               `json:"root_path"`
	Checks     []doctor.CheckResult `json:"checks"`
	Overall    doctor.Status        `json:"overall"`
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output in JSON format")
	_ = fs.Parse(args)

	// Discover root with method
	rootPath, method, err := root.FindWithMethod()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	// Run all health checks
	results := []doctor.CheckResult{
		doctor.CheckWritable(rootPath),
		doctor.CheckNetworkFS(rootPath),
		doctor.CheckClock(),
	}

	overall := doctor.Overall(results)

	if *jsonOutput {
		output := doctorOutput{
			RootMethod: method.String(),
			RootPath:   rootPath,
			Checks:     results,
			Overall:    overall,
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
	} else {
		// Text output
		fmt.Println("lokt doctor")
		fmt.Println()
		fmt.Printf("Root:        %s (via %s)\n", filepath.Base(rootPath), methodDescription(method))
		fmt.Printf("Path:        %s\n", rootPath)
		fmt.Println()
		fmt.Println("Checks:")
		for _, r := range results {
			printCheckResult(r)
		}
		fmt.Println()
		fmt.Printf("Result: %s\n", overallDescription(overall))
	}

	// Exit code: 1 if any check failed, 0 otherwise (warnings don't fail)
	if overall == doctor.StatusFail {
		return ExitError
	}
	return ExitOK
}

// methodDescription returns a human-readable description of the discovery method.
func methodDescription(m root.DiscoveryMethod) string {
	switch m {
	case root.MethodEnvVar:
		return "LOKT_ROOT env"
	case root.MethodGit:
		return "git common dir"
	case root.MethodLocalDir:
		return ".lokt/ fallback"
	default:
		return "unknown"
	}
}

// printCheckResult prints a single check result in text format.
func printCheckResult(r doctor.CheckResult) {
	var marker string
	switch r.Status {
	case doctor.StatusOK:
		marker = "[OK]"
	case doctor.StatusWarn:
		marker = "[WARN]"
	case doctor.StatusFail:
		marker = "[FAIL]"
	}

	// Map check names to display names
	displayNames := map[string]string{
		"writable":   "Directory writable",
		"network_fs": "Network filesystem",
		"clock":      "Clock sanity",
	}
	displayName := displayNames[r.Name]
	if displayName == "" {
		displayName = r.Name
	}

	fmt.Printf("  %-6s %s\n", marker, displayName)
	if r.Message != "" {
		fmt.Printf("         %s\n", r.Message)
	}
}

// overallDescription returns a human-readable overall result.
func overallDescription(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "PASS"
	case doctor.StatusWarn:
		return "PASS with warnings"
	case doctor.StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}
