package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nikolasavic/lokt/internal/lock"
	"github.com/nikolasavic/lokt/internal/root"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Exit codes
const (
	ExitOK        = 0
	ExitError     = 1
	ExitLockHeld  = 2
	ExitNotFound  = 3
	ExitNotOwner  = 4
	ExitUsage     = 64
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
	fmt.Println("  status [name]     Show lock status")
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
	force := fs.Bool("force", false, "Remove lock without ownership check")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lokt unlock [--force] <name>")
		return ExitUsage
	}
	name := fs.Arg(0)

	rootDir, err := root.Find()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	err = lock.Release(rootDir, name, lock.ReleaseOptions{Force: *force})
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
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("released lock %q\n", name)
	return ExitOK
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
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
		return showLock(rootDir, fs.Arg(0))
	}

	// List all locks
	if len(entries) == 0 {
		fmt.Println("no locks")
		return ExitOK
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			showLockBrief(rootDir, name[:len(name)-5])
		}
	}
	return ExitOK
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
	fmt.Printf("pid:      %d\n", lock.PID)
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
	expired := ""
	if lock.IsExpired() {
		expired = " [EXPIRED]"
	}
	fmt.Printf("%-20s  %s@%s  %s%s\n", name, lock.Owner, lock.Host, age, expired)
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
