package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestMultiProcessContention spawns N real OS processes all racing to guard
// the same lock with a long-running child. Asserts exactly 1 acquires (exit 0)
// and the rest are denied (exit 2). Uses guard so the winner's PID stays alive
// during the test, preventing auto-prune from creating multiple winners.
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
		cmd      *exec.Cmd
	}

	results := make([]result, n)
	var wg sync.WaitGroup

	// Use a barrier: create all processes, then start them near-simultaneously.
	// The winner runs "sleep 60" (killed after test), losers run "true" (never reached).
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			owner := fmt.Sprintf("agent-%d", idx)

			// Use guard with a long-running child. Losers exit immediately with code 2.
			// Winner holds the lock while sleep runs.
			cmd := exec.Command(binary, "guard", "--ttl", "30s", lockName, "--", "sleep", "60")
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
				cmd:      cmd,
			}
		}(i)
	}

	// Wait for all losers to finish (they exit immediately with code 2).
	// The winner is still running sleep 60, but we need to give processes time to race.
	// Poll until we see exactly n-1 completed processes.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Check if lock file exists (winner acquired)
		lockPath := filepath.Join(locksDir, lockName+".json")
		if _, err := os.Stat(lockPath); err == nil {
			// Lock exists — give losers time to finish
			time.Sleep(500 * time.Millisecond)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Kill the winner's sleep child so the guard exits and wg completes.
	// Retry reads to handle transient mid-write states under heavy load.
	lockPath := filepath.Join(locksDir, lockName+".json")
	var lockData struct {
		Owner  string `json:"owner"`
		PID    int    `json:"pid"`
		LockID string `json:"lock_id"`
	}
	for i := range 10 {
		data, err := os.ReadFile(lockPath)
		if err == nil {
			if json.Unmarshal(data, &lockData) == nil && lockData.PID > 0 {
				break
			}
		}
		if i == 9 {
			t.Fatalf("read lockfile after 10 retries: last err=%v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Kill the guard process (which holds the lock)
	if lockData.PID > 0 {
		_ = syscall.Kill(lockData.PID, syscall.SIGTERM)
	}

	wg.Wait()

	// Count winners and losers.
	// Exit code -1 means signal kill with indeterminate status (heavy load).
	var winners, denied []int
	for i, r := range results {
		switch r.exitCode {
		case ExitOK, 143, -1: // 143 = 128+SIGTERM, -1 = signal kill under load
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

	if lockData.Owner != results[winners[0]].owner {
		t.Errorf("lockfile owner = %q, want %q (winner)", lockData.Owner, results[winners[0]].owner)
	}
	if lockData.LockID == "" {
		t.Error("lockfile lock_id is empty, expected non-empty")
	}

	t.Logf("winner: %s, lock_id: %s", results[winners[0]].owner, lockData.LockID)
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

			type procResult struct {
				exitCode int
				cmd      *exec.Cmd
			}
			results := make([]procResult, n)
			var wg sync.WaitGroup

			for i := range n {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					cmd := exec.Command(binary, "guard", "--ttl", "30s", lockName, "--", "sleep", "60")
					cmd.Env = []string{
						"LOKT_ROOT=" + rootDir,
						"LOKT_OWNER=" + fmt.Sprintf("agent-%d", idx),
						"HOME=" + os.Getenv("HOME"),
						"PATH=" + os.Getenv("PATH"),
					}
					err := cmd.Run()
					code := 0
					if err != nil {
						var exitErr *exec.ExitError
						if errors.As(err, &exitErr) {
							code = exitErr.ExitCode()
						}
					}
					results[idx] = procResult{exitCode: code, cmd: cmd}
				}(i)
			}

			// Wait for lock to appear, then kill winner
			lockPath := filepath.Join(locksDir, lockName+".json")
			deadline := time.Now().Add(10 * time.Second)
			lockFound := false
			for time.Now().Before(deadline) {
				if _, err := os.Stat(lockPath); err == nil {
					lockFound = true
					time.Sleep(300 * time.Millisecond)
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

			// Read lockfile to find winner PID and kill it
			// Retry a few times in case of transient read errors
			var winnerPID int
			for i := 0; i < 5 && lockFound; i++ {
				if data, err := os.ReadFile(lockPath); err == nil {
					var lk struct {
						PID int `json:"pid"`
					}
					if json.Unmarshal(data, &lk) == nil && lk.PID > 0 {
						winnerPID = lk.PID
						break
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
			if winnerPID > 0 {
				_ = syscall.Kill(winnerPID, syscall.SIGTERM)
			}

			wg.Wait()

			// Count winners and collect exit codes for diagnostics.
			// Exit code -1 means the process was killed by a signal but Go
			// couldn't determine the code (happens under heavy load with -race).
			// Treat it the same as 143 (SIGTERM) since we sent SIGTERM to the winner.
			winners := 0
			var exitCodes []int
			for _, r := range results {
				exitCodes = append(exitCodes, r.exitCode)
				if r.exitCode == ExitOK || r.exitCode == 143 || r.exitCode == -1 {
					winners++
				}
			}

			if winners != 1 {
				t.Errorf("run %d: expected 1 winner, got %d (lockFound=%v, winnerPID=%d, exitCodes=%v)",
					run, winners, lockFound, winnerPID, exitCodes)
			}
		})
	}
}
