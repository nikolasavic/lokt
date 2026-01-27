package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikolasavic/lokt/internal/lock"
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
	fmt.Println("  unlock <name>     Release a lock")
	fmt.Println("    --force         Remove without ownership check (break-glass)")
	fmt.Println("    --break-stale   Remove only if stale (expired TTL or dead PID)")
	fmt.Println("  status [name]     Show lock status")
	fmt.Println("    --prune-expired Remove expired locks while listing")
	fmt.Println("  guard <name> -- <cmd...>  Run command while holding lock")
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
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt lock [--ttl duration] <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	if *ttl < 0 {
		fmt.Fprintln(os.Stderr, "error: TTL must be positive (e.g., 5m, 1h)")
		return ExitUsage
	}

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	err = lock.Acquire(rootDir, name, lock.AcquireOptions{TTL: *ttl})
	if err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			fmt.Fprintf(os.Stderr, "error: %v\n", held)
			return ExitLockHeld
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("acquired lock %q\n", name)
	return ExitOK
}

func cmdUnlock(args []string) int {
	fs := flag.NewFlagSet("unlock", flag.ExitOnError)
	force := fs.Bool("force", false, "Remove lock without ownership check (break-glass)")
	breakStale := fs.Bool("break-stale", false, "Remove lock only if stale (expired TTL or dead PID)")
	fs.Parse(args)

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

	err = lock.Release(rootDir, name, lock.ReleaseOptions{
		Force:      *force,
		BreakStale: *breakStale,
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
	fs.Parse(args)

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	locksDir := root.LocksPath(rootDir)
	entries, err := os.ReadDir(locksDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no locks")
			return ExitOK
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	// If a specific lock name given, show just that one
	if fs.NArg() > 0 {
		name := fs.Arg(0)
		if *pruneExpired {
			return showLockWithPrune(rootDir, name)
		}
		return showLock(rootDir, name)
	}

	// List all locks
	if len(entries) == 0 {
		fmt.Println("no locks")
		return ExitOK
	}

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
			showLockBrief(rootDir, lockName)
		}
	}

	if pruned > 0 {
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
		fmt.Fprintln(os.Stderr, "usage: lokt guard [--ttl duration] <name> -- <command...>")
		return ExitUsage
	}

	// Parse flags (before --)
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	ttl := fs.Duration("ttl", 0, "Lock TTL (e.g., 5m, 1h)")
	if err := fs.Parse(args[:dashIdx]); err != nil {
		fmt.Fprintln(os.Stderr, "usage: lokt guard [--ttl duration] <name> -- <command...>")
		return ExitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt guard [--ttl duration] <name> -- <command...>")
		return ExitUsage
	}
	name := fs.Arg(0)
	cmdArgs := args[dashIdx+1:]

	if *ttl < 0 {
		fmt.Fprintln(os.Stderr, "error: TTL must be positive (e.g., 5m, 1h)")
		return ExitUsage
	}

	// Resolve root
	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	// Acquire lock
	if err := lock.Acquire(rootDir, name, lock.AcquireOptions{TTL: *ttl}); err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			fmt.Fprintf(os.Stderr, "error: %v\n", held)
			return ExitLockHeld
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	// Ensure release on all paths
	released := false
	releaseLock := func() {
		if !released {
			lock.Release(rootDir, name, lock.ReleaseOptions{})
			released = true
		}
	}
	defer releaseLock()

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
		child.Process.Signal(sig)
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

func showLock(rootDir, name string) int {
	path := root.LockFilePath(rootDir, name)
	lock, err := readLockFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "lock %q not found\n", name)
			return ExitNotFound
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	age := time.Since(lock.AcquiredAt).Truncate(time.Second)
	fmt.Printf("name:     %s\n", lock.Name)
	fmt.Printf("owner:    %s\n", lock.Owner)
	fmt.Printf("host:     %s\n", lock.Host)
	fmt.Printf("pid:      %d (%s)\n", lock.PID, pidLiveness(lock))
	fmt.Printf("age:      %s\n", age)
	if lock.TTLSec > 0 {
		fmt.Printf("ttl:      %ds\n", lock.TTLSec)
		if lock.IsExpired() {
			fmt.Println("status:   EXPIRED")
		}
	}
	return ExitOK
}

func showLockBrief(rootDir, name string) {
	path := root.LockFilePath(rootDir, name)
	lock, err := readLockFile(path)
	if err != nil {
		return
	}

	age := time.Since(lock.AcquiredAt).Truncate(time.Second)
	status := ""
	if lock.IsExpired() {
		status = " [EXPIRED]"
	} else if liveness := pidLiveness(lock); liveness == "dead" {
		status = " [DEAD]"
	}
	fmt.Printf("%-20s  %s@%s  %s%s\n", name, lock.Owner, lock.Host, age, status)
}

// showLockWithPrune shows a lock and removes it if expired.
func showLockWithPrune(rootDir, name string) int {
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
		fmt.Printf("pruned expired lock %q\n", name)
		return ExitOK
	}

	// Not expired, show normally
	return showLock(rootDir, name)
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
