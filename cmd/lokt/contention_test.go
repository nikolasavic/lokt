package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestMultiProcessContention spawns N real OS processes all racing to acquire
// the same lock. Asserts exactly 1 wins (exit 0) and the rest are denied (exit 2).
// This is the first binary-level integration test for mutual exclusion.
func TestMultiProcessContention(t *testing.T) {
	binary := buildBinary(t)

	const n = 10
	const lockName = "contention-test"

	rootDir := t.TempDir()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	type result struct {
		owner    string
		exitCode int
		stdout   string
		pid      int
	}

	results := make([]result, n)
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			owner := fmt.Sprintf("agent-%d", idx)

			cmd := exec.Command(binary, "lock", "--json", lockName)
			cmd.Env = []string{
				"LOKT_ROOT=" + rootDir,
				"LOKT_OWNER=" + owner,
				"HOME=" + os.Getenv("HOME"),
				"PATH=" + os.Getenv("PATH"),
			}
			out, err := cmd.CombinedOutput()

			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					t.Errorf("process %d (%s): unexpected error: %v", idx, owner, err)
					return
				}
			}

			results[idx] = result{
				owner:    owner,
				exitCode: exitCode,
				stdout:   string(out),
				pid:      cmd.ProcessState.Pid(),
			}
		}(i)
	}

	wg.Wait()

	// Count winners and losers
	var winners, denied []int
	for i, r := range results {
		switch r.exitCode {
		case ExitOK:
			winners = append(winners, i)
		case ExitLockHeld:
			denied = append(denied, i)
		default:
			t.Errorf("process %d (%s): unexpected exit code %d\noutput: %s",
				i, r.owner, r.exitCode, r.stdout)
		}
	}

	if len(winners) != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (winners: %v)", len(winners), winners)
	}
	if len(denied) != n-1 {
		t.Errorf("expected %d denied, got %d", n-1, len(denied))
	}

	// Verify lockfile matches the winner
	winner := results[winners[0]]
	lockPath := filepath.Join(locksDir, lockName+".json")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var lockData struct {
		Owner  string `json:"owner"`
		PID    int    `json:"pid"`
		LockID string `json:"lock_id"`
	}
	if err := json.Unmarshal(data, &lockData); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	if lockData.Owner != winner.owner {
		t.Errorf("lockfile owner = %q, want %q (winner)", lockData.Owner, winner.owner)
	}
	if lockData.LockID == "" {
		t.Error("lockfile lock_id is empty, expected non-empty")
	}

	t.Logf("winner: %s (pid %d), lock_id: %s", winner.owner, winner.pid, lockData.LockID)
}

// TestMultiProcessContention_Stability runs the contention test multiple times
// to verify it's not flaky.
func TestMultiProcessContention_Stability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stability test in short mode")
	}

	binary := buildBinary(t)

	const runs = 10
	const n = 10
	const lockName = "stability-test"

	for run := range runs {
		t.Run(fmt.Sprintf("run-%d", run), func(t *testing.T) {
			rootDir := t.TempDir()
			locksDir := filepath.Join(rootDir, "locks")
			if err := os.MkdirAll(locksDir, 0700); err != nil {
				t.Fatalf("mkdir locks: %v", err)
			}

			exitCodes := make([]int, n)
			var wg sync.WaitGroup

			for i := range n {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					cmd := exec.Command(binary, "lock", lockName)
					cmd.Env = []string{
						"LOKT_ROOT=" + rootDir,
						"LOKT_OWNER=" + fmt.Sprintf("agent-%d", idx),
						"HOME=" + os.Getenv("HOME"),
						"PATH=" + os.Getenv("PATH"),
					}
					err := cmd.Run()
					if err != nil {
						var exitErr *exec.ExitError
						if errors.As(err, &exitErr) {
							exitCodes[idx] = exitErr.ExitCode()
						} else {
							t.Errorf("process %d: unexpected error: %v", idx, err)
						}
					}
				}(i)
			}

			wg.Wait()

			winners := 0
			for _, code := range exitCodes {
				if code == ExitOK {
					winners++
				}
			}

			if winners != 1 {
				t.Errorf("run %d: expected 1 winner, got %d", run, winners)
			}
		})
	}
}
