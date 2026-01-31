package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestUnlockOwner_ReleasesMatchingLocks(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "lock-a.json", &lockfile.Lock{
		Name: "lock-a", Owner: "agent-1", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})
	writeLockJSON(t, locksDir, "lock-b.json", &lockfile.Lock{
		Name: "lock-b", Owner: "agent-1", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})
	writeLockJSON(t, locksDir, "lock-c.json", &lockfile.Lock{
		Name: "lock-c", Owner: "agent-2", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--owner", "agent-1"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "released 2 lock(s)") {
		t.Errorf("stdout = %q, want 'released 2 lock(s)'", stdout)
	}

	// agent-2's lock should still exist
	if _, err := os.Stat(locksDir + "/lock-c.json"); os.IsNotExist(err) {
		t.Error("agent-2 lock should still exist")
	}
}

func TestUnlockOwner_NoMatches(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "lock-a.json", &lockfile.Lock{
		Name: "lock-a", Owner: "agent-1", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--owner", "nobody"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "no locks matched") {
		t.Errorf("stdout = %q, want 'no locks matched'", stdout)
	}
}

func TestUnlockAll_UsesCurrentIdentity(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "my-session")

	writeLockJSON(t, locksDir, "mine.json", &lockfile.Lock{
		Name: "mine", Owner: "my-session", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})
	writeLockJSON(t, locksDir, "theirs.json", &lockfile.Lock{
		Name: "theirs", Owner: "other", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--all"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "released 1 lock(s)") {
		t.Errorf("stdout = %q, want 'released 1 lock(s)'", stdout)
	}

	// other's lock should still exist
	if _, err := os.Stat(locksDir + "/theirs.json"); os.IsNotExist(err) {
		t.Error("other's lock should still exist")
	}
}

func TestUnlockOwner_JSON(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "lock-a.json", &lockfile.Lock{
		Name: "lock-a", Owner: "agent-1", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})
	writeLockJSON(t, locksDir, "lock-b.json", &lockfile.Lock{
		Name: "lock-b", Owner: "agent-1", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--owner", "agent-1", "--json"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}

	var out unlockByOwnerOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
	if len(out.Released) != 2 {
		t.Errorf("released = %v, want 2 entries", out.Released)
	}
}

func TestUnlockOwner_JSONNoMatches(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdUnlock, []string{"--owner", "nobody", "--json"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}

	var out unlockByOwnerOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Count != 0 {
		t.Errorf("count = %d, want 0", out.Count)
	}
	if len(out.Released) != 0 {
		t.Errorf("released = %v, want empty", out.Released)
	}
}

func TestUnlock_MutualExclusion_OwnerWithName(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, []string{"--owner", "agent-1", "lockname"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "cannot be combined with a lock name") {
		t.Errorf("stderr = %q, want mutual exclusion error", stderr)
	}
}

func TestUnlock_MutualExclusion_AllWithName(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, []string{"--all", "lockname"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "cannot be combined with a lock name") {
		t.Errorf("stderr = %q, want mutual exclusion error", stderr)
	}
}

func TestUnlock_MutualExclusion_OwnerAndAll(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, []string{"--owner", "agent-1", "--all"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("stderr = %q, want mutually exclusive error", stderr)
	}
}

func TestUnlock_MutualExclusion_ForceWithOwner(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, []string{"--force", "--owner", "agent-1"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--force/--break-stale cannot be combined") {
		t.Errorf("stderr = %q, want flag conflict error", stderr)
	}
}

func TestUnlock_MutualExclusion_BreakStaleWithAll(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, []string{"--break-stale", "--all"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--force/--break-stale cannot be combined") {
		t.Errorf("stderr = %q, want flag conflict error", stderr)
	}
}

func TestUnlock_SingleLockStillWorks(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "me")

	writeLockJSON(t, locksDir, "mylock.json", &lockfile.Lock{
		Name: "mylock", Owner: "me", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"mylock"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, `released lock "mylock"`) {
		t.Errorf("stdout = %q, want released message", stdout)
	}
}
