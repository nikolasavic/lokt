package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

// TestExitCodeContract is a table-driven test that systematically verifies
// every exit code path in the CLI. It serves as living documentation of the
// exit code contract: which scenarios produce which codes.
func TestExitCodeContract(t *testing.T) {
	tests := []struct {
		name     string
		cmd      func([]string) int
		args     []string
		setup    func(t *testing.T, rootDir, locksDir string)
		wantCode int
	}{
		// ── lock command ────────────────────────────────────────────
		{
			name:     "lock/success",
			cmd:      cmdLock,
			args:     []string{"fresh-lock"},
			wantCode: ExitOK,
		},
		{
			name: "lock/held-by-other",
			cmd:  cmdLock,
			args: []string{"taken"},
			setup: func(t *testing.T, _, locksDir string) {
				t.Setenv("LOKT_OWNER", "other-agent")
				writeLockJSON(t, locksDir, "taken.json", &lockfile.Lock{
					Name: "taken", Owner: "alice", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(), TTLSec: 300,
				})
			},
			wantCode: ExitLockHeld,
		},
		{
			name: "lock/corrupt-file-auto-cleaned",
			cmd:  cmdLock,
			args: []string{"corrupt"},
			setup: func(t *testing.T, _, locksDir string) {
				if err := os.WriteFile(
					filepath.Join(locksDir, "corrupt.json"),
					[]byte("{{{not json"),
					0600,
				); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: ExitOK, // corrupt file is auto-removed, lock acquired
		},
		{
			name:     "lock/invalid-name",
			cmd:      cmdLock,
			args:     []string{"bad/name"},
			wantCode: ExitError,
		},

		// ── unlock command ──────────────────────────────────────────
		{
			name: "unlock/success",
			cmd:  cmdUnlock,
			args: []string{"mylock"},
			setup: func(t *testing.T, _, locksDir string) {
				t.Setenv("LOKT_OWNER", "me")
				writeLockJSON(t, locksDir, "mylock.json", &lockfile.Lock{
					Name: "mylock", Owner: "me", Host: "h",
					PID: 1, AcquiredAt: time.Now(),
				})
			},
			wantCode: ExitOK,
		},
		{
			name:     "unlock/not-found",
			cmd:      cmdUnlock,
			args:     []string{"nonexistent"},
			wantCode: ExitNotFound,
		},
		{
			name: "unlock/not-owner",
			cmd:  cmdUnlock,
			args: []string{"theirs"},
			setup: func(t *testing.T, _, locksDir string) {
				t.Setenv("LOKT_OWNER", "me")
				writeLockJSON(t, locksDir, "theirs.json", &lockfile.Lock{
					Name: "theirs", Owner: "someone-else", Host: "h",
					PID: 1, AcquiredAt: time.Now(),
				})
			},
			wantCode: ExitNotOwner,
		},
		{
			name:     "unlock/usage-owner-with-name",
			cmd:      cmdUnlock,
			args:     []string{"--owner", "agent-1", "lockname"},
			wantCode: ExitUsage,
		},

		// ── status command ──────────────────────────────────────────
		{
			name:     "status/success-list",
			cmd:      cmdStatus,
			args:     []string{},
			wantCode: ExitOK,
		},
		{
			name:     "status/not-found",
			cmd:      cmdStatus,
			args:     []string{"nonexistent"},
			wantCode: ExitNotFound,
		},

		// ── guard command ───────────────────────────────────────────
		{
			name:     "guard/success",
			cmd:      cmdGuard,
			args:     []string{"--ttl", "1m", "glock", "--", "true"},
			wantCode: ExitOK,
		},
		{
			name: "guard/held-by-other",
			cmd:  cmdGuard,
			args: []string{"taken", "--", "true"},
			setup: func(t *testing.T, _, locksDir string) {
				t.Setenv("LOKT_OWNER", "other-agent")
				writeLockJSON(t, locksDir, "taken.json", &lockfile.Lock{
					Name: "taken", Owner: "alice", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(), TTLSec: 300,
				})
			},
			wantCode: ExitLockHeld,
		},
		{
			name: "guard/frozen",
			cmd:  cmdGuard,
			args: []string{"frozen-name", "--", "true"},
			setup: func(t *testing.T, rootDir, _ string) {
				freezesDir := filepath.Join(rootDir, "freezes")
				if err := os.MkdirAll(freezesDir, 0700); err != nil {
					t.Fatal(err)
				}
				exp := time.Now().Add(10 * time.Minute)
				writeLockJSON(t, freezesDir, "frozen-name.json", &lockfile.Lock{
					Version: 1, Name: "frozen-name", Owner: "ops", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(),
					TTLSec: 600, ExpiresAt: &exp,
				})
			},
			wantCode: ExitLockHeld,
		},
		{
			name:     "guard/child-exit-code",
			cmd:      cmdGuard,
			args:     []string{"glock2", "--", "false"},
			wantCode: 1, // child 'false' exits 1
		},
		{
			name:     "guard/usage-no-separator",
			cmd:      cmdGuard,
			args:     []string{"lockname", "true"},
			wantCode: ExitUsage,
		},

		// ── freeze command ──────────────────────────────────────────
		{
			name:     "freeze/success",
			cmd:      cmdFreeze,
			args:     []string{"--ttl", "10m", "flock"},
			wantCode: ExitOK,
		},
		{
			name: "freeze/already-frozen",
			cmd:  cmdFreeze,
			args: []string{"--ttl", "10m", "existing"},
			setup: func(t *testing.T, rootDir, _ string) {
				freezesDir := filepath.Join(rootDir, "freezes")
				if err := os.MkdirAll(freezesDir, 0700); err != nil {
					t.Fatal(err)
				}
				exp := time.Now().Add(10 * time.Minute)
				writeLockJSON(t, freezesDir, "existing.json", &lockfile.Lock{
					Version: 1, Name: "existing", Owner: "ops", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(),
					TTLSec: 600, ExpiresAt: &exp,
				})
			},
			wantCode: ExitLockHeld,
		},
		{
			name:     "freeze/usage-no-ttl",
			cmd:      cmdFreeze,
			args:     []string{"flock-no-ttl"},
			wantCode: ExitUsage,
		},

		// ── unfreeze command ────────────────────────────────────────
		{
			name: "unfreeze/success",
			cmd:  cmdUnfreeze,
			args: []string{"my-freeze"},
			setup: func(t *testing.T, rootDir, _ string) {
				t.Setenv("LOKT_OWNER", "me")
				freezesDir := filepath.Join(rootDir, "freezes")
				if err := os.MkdirAll(freezesDir, 0700); err != nil {
					t.Fatal(err)
				}
				exp := time.Now().Add(10 * time.Minute)
				writeLockJSON(t, freezesDir, "my-freeze.json", &lockfile.Lock{
					Version: 1, Name: "my-freeze", Owner: "me", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(),
					TTLSec: 600, ExpiresAt: &exp,
				})
			},
			wantCode: ExitOK,
		},
		{
			name:     "unfreeze/not-found",
			cmd:      cmdUnfreeze,
			args:     []string{"no-such-freeze"},
			wantCode: ExitNotFound,
		},
		{
			name: "unfreeze/not-owner",
			cmd:  cmdUnfreeze,
			args: []string{"their-freeze"},
			setup: func(t *testing.T, rootDir, _ string) {
				t.Setenv("LOKT_OWNER", "me")
				freezesDir := filepath.Join(rootDir, "freezes")
				if err := os.MkdirAll(freezesDir, 0700); err != nil {
					t.Fatal(err)
				}
				exp := time.Now().Add(10 * time.Minute)
				writeLockJSON(t, freezesDir, "their-freeze.json", &lockfile.Lock{
					Version: 1, Name: "their-freeze", Owner: "someone-else", Host: "h",
					PID: os.Getpid(), AcquiredAt: time.Now(),
					TTLSec: 600, ExpiresAt: &exp,
				})
			},
			wantCode: ExitNotOwner,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rootDir, locksDir := setupTestRoot(t)
			if tc.setup != nil {
				tc.setup(t, rootDir, locksDir)
			}

			_, _, code := captureCmd(tc.cmd, tc.args)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
		})
	}
}
