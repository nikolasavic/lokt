package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestWhy_FreeLock(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdWhy, []string{"nonexistent"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "FREE") {
		t.Errorf("expected 'FREE' in output, got: %s", stdout)
	}
}

func TestWhy_FreeLock_JSON(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "nonexistent"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "free" {
		t.Errorf("expected status 'free', got %q", out.Status)
	}
	if len(out.Reasons) != 0 {
		t.Errorf("expected 0 reasons, got %d", len(out.Reasons))
	}
}

func TestWhy_HeldByOther_SameHost_Alive(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Use current PID + 1 million to simulate another alive process on same host.
	// We use PID 1 which is always alive (launchd/init).
	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "build.json", &lockfile.Lock{
		Name:       "build",
		Owner:      "other-user",
		Host:       hostname,
		PID:        1, // PID 1 is always alive
		AcquiredAt: time.Now().Add(-30 * time.Second),
		TTLSec:     300,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"build"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "HELD") {
		t.Errorf("expected 'HELD' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "other-user") {
		t.Errorf("expected holder identity in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "alive") {
		t.Errorf("expected 'alive' PID status, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lokt lock --wait build") {
		t.Errorf("expected --wait suggestion, got: %s", stdout)
	}
}

func TestWhy_HeldByOther_CrossHost(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "deploy.json", &lockfile.Lock{
		Name:       "deploy",
		Owner:      "ci-bot",
		Host:       "remote-server.example.com",
		PID:        99999,
		AcquiredAt: time.Now().Add(-2 * time.Minute),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"deploy"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "remote host") {
		t.Errorf("expected 'remote host' note, got: %s", stdout)
	}
	if !strings.Contains(stdout, "ci-bot@remote-server.example.com") {
		t.Errorf("expected holder identity, got: %s", stdout)
	}
}

func TestWhy_Frozen(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Create freeze lock (freeze-<name>.json)
	writeLockJSON(t, locksDir, "freeze-build.json", &lockfile.Lock{
		Name:       "freeze-build",
		Owner:      "admin",
		Host:       "deploy-host",
		PID:        5000,
		AcquiredAt: time.Now().Add(-1 * time.Minute),
		TTLSec:     600, // 10 min TTL, 1 min elapsed => 9 min remaining
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"build"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "FROZEN") {
		t.Errorf("expected 'FROZEN' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "admin@deploy-host") {
		t.Errorf("expected freeze owner, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Remaining:") {
		t.Errorf("expected remaining TTL, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lokt unfreeze") {
		t.Errorf("expected unfreeze suggestion, got: %s", stdout)
	}
}

func TestWhy_FrozenAndHeld(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()

	// Both freeze and regular lock exist
	writeLockJSON(t, locksDir, "freeze-deploy.json", &lockfile.Lock{
		Name:       "freeze-deploy",
		Owner:      "admin",
		Host:       "other-host",
		PID:        5000,
		AcquiredAt: time.Now().Add(-30 * time.Second),
		TTLSec:     300,
	})
	writeLockJSON(t, locksDir, "deploy.json", &lockfile.Lock{
		Name:       "deploy",
		Owner:      "worker",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     120,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"deploy"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	// Should mention both freeze and held
	if !strings.Contains(stdout, "FROZEN") {
		t.Errorf("expected 'FROZEN', got: %s", stdout)
	}
	if !strings.Contains(stdout, "HELD") {
		t.Errorf("expected 'HELD', got: %s", stdout)
	}
}

func TestWhy_Expired(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "backup.json", &lockfile.Lock{
		Name:       "backup",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60, // 1 min TTL, 10 min ago => expired
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"backup"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "EXPIRED") {
		t.Errorf("expected 'EXPIRED' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "break-stale") {
		t.Errorf("expected --break-stale suggestion, got: %s", stdout)
	}
}

func TestWhy_DeadPID(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()

	// Use a PID that definitely doesn't exist
	writeLockJSON(t, locksDir, "stale.json", &lockfile.Lock{
		Name:       "stale",
		Owner:      "crashed-agent",
		Host:       hostname,
		PID:        2000000000, // Very high PID — won't exist
		AcquiredAt: time.Now().Add(-5 * time.Minute),
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"stale"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "dead") || !strings.Contains(stdout, "STALE") {
		t.Errorf("expected dead PID info, got: %s", stdout)
	}
	if !strings.Contains(stdout, "break-stale") {
		t.Errorf("expected --break-stale suggestion, got: %s", stdout)
	}
}

func TestWhy_Corrupted(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Write invalid JSON
	if err := os.WriteFile(filepath.Join(locksDir, "bad.json"), []byte("not json{{{"), 0600); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}

	stdout, _, code := captureCmd(cmdWhy, []string{"bad"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "CORRUPTED") {
		t.Errorf("expected 'CORRUPTED', got: %s", stdout)
	}
	if !strings.Contains(stdout, "--force") {
		t.Errorf("expected --force suggestion, got: %s", stdout)
	}
}

func TestWhy_SelfHeld(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	me := os.Getpid()

	// Set LOKT_OWNER so identity.Current() returns a known value
	t.Setenv("LOKT_OWNER", "test-self")

	writeLockJSON(t, locksDir, "mine.json", &lockfile.Lock{
		Name:       "mine",
		Owner:      "test-self",
		Host:       hostname,
		PID:        me,
		AcquiredAt: time.Now().Add(-5 * time.Second),
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"mine"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stdout, "SELF-HELD") {
		t.Errorf("expected 'SELF-HELD', got: %s", stdout)
	}
	if !strings.Contains(stdout, "you already hold") {
		t.Errorf("expected 'you already hold' message, got: %s", stdout)
	}
}

func TestWhy_InvalidName(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdWhy, []string{"../bad"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "invalid lock name") {
		t.Errorf("expected invalid name error, got: %s", stderr)
	}
}

func TestWhy_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdWhy, []string{})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage message, got: %s", stderr)
	}
}

func TestWhy_JSON_Held(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "api.json", &lockfile.Lock{
		Name:       "api",
		Owner:      "alice",
		Host:       hostname,
		PID:        1, // alive
		AcquiredAt: time.Now().Add(-45 * time.Second),
		TTLSec:     300,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "api"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", out.Status)
	}
	if out.Name != "api" {
		t.Errorf("expected name 'api', got %q", out.Name)
	}
	if len(out.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(out.Reasons))
	}
	if out.Reasons[0].Type != "held" {
		t.Errorf("expected reason type 'held', got %q", out.Reasons[0].Type)
	}
	if out.Reasons[0].HolderOwner != "alice" {
		t.Errorf("expected holder_owner 'alice', got %q", out.Reasons[0].HolderOwner)
	}
	if out.Reasons[0].HolderTTLSec != 300 {
		t.Errorf("expected holder_ttl_sec 300, got %d", out.Reasons[0].HolderTTLSec)
	}
	if out.Reasons[0].HolderPIDStatus != "alive" {
		t.Errorf("expected holder_pid_status 'alive', got %q", out.Reasons[0].HolderPIDStatus)
	}
	if len(out.Suggestions) == 0 {
		t.Error("expected suggestions, got none")
	}
}

func TestWhy_JSON_Frozen(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "freeze-db.json", &lockfile.Lock{
		Name:       "freeze-db",
		Owner:      "ops",
		Host:       "deploy-box",
		PID:        4000,
		AcquiredAt: time.Now().Add(-2 * time.Minute),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "db"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", out.Status)
	}
	if len(out.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(out.Reasons))
	}
	if out.Reasons[0].Type != "frozen" {
		t.Errorf("expected reason type 'frozen', got %q", out.Reasons[0].Type)
	}
	if out.Reasons[0].FreezeOwner != "ops" {
		t.Errorf("expected freeze_owner 'ops', got %q", out.Reasons[0].FreezeOwner)
	}
	if out.Reasons[0].FreezeRemainingSec <= 0 {
		t.Errorf("expected positive freeze_remaining_sec, got %d", out.Reasons[0].FreezeRemainingSec)
	}
}

func TestWhy_ExpiredFreeze_IsFree(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Freeze that has already expired
	writeLockJSON(t, locksDir, "freeze-old.json", &lockfile.Lock{
		Name:       "freeze-old",
		Owner:      "admin",
		Host:       "server",
		PID:        1000,
		AcquiredAt: time.Now().Add(-20 * time.Minute),
		TTLSec:     60, // expired 19 minutes ago
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"old"})
	if code != ExitOK {
		t.Errorf("expected exit %d (expired freeze = free), got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "FREE") {
		t.Errorf("expected 'FREE' (expired freeze ignored), got: %s", stdout)
	}
}

func TestWhy_JSON_FrozenAndHeld(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()

	writeLockJSON(t, locksDir, "freeze-svc.json", &lockfile.Lock{
		Name:       "freeze-svc",
		Owner:      "admin",
		Host:       "other",
		PID:        5000,
		AcquiredAt: time.Now().Add(-30 * time.Second),
		TTLSec:     300,
	})
	writeLockJSON(t, locksDir, "svc.json", &lockfile.Lock{
		Name:       "svc",
		Owner:      "worker",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     120,
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "svc"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if len(out.Reasons) != 2 {
		t.Fatalf("expected 2 reasons (frozen + held), got %d: %+v", len(out.Reasons), out.Reasons)
	}
	// Freeze should come first
	if out.Reasons[0].Type != "frozen" {
		t.Errorf("expected first reason type 'frozen', got %q", out.Reasons[0].Type)
	}
	if out.Reasons[1].Type != "held" {
		t.Errorf("expected second reason type 'held', got %q", out.Reasons[1].Type)
	}
}

func TestWhy_JSON_Corrupted(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	if err := os.WriteFile(filepath.Join(locksDir, "broken.json"), []byte("{invalid"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "broken"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if len(out.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(out.Reasons))
	}
	if out.Reasons[0].Type != "corrupted" {
		t.Errorf("expected type 'corrupted', got %q", out.Reasons[0].Type)
	}
	found := false
	for _, s := range out.Suggestions {
		if strings.Contains(s, "--force") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --force suggestion in: %v", out.Suggestions)
	}
}

// Ensure all JSON output fields are valid by verifying a round-trip.
func TestWhy_JSON_ValidStructure(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "full.json", &lockfile.Lock{
		Name:       "full",
		Owner:      "tester",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-90 * time.Second),
		TTLSec:     300,
	})

	stdout, _, _ := captureCmd(cmdWhy, []string{"--json", "full"})

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	// Check required top-level fields
	for _, field := range []string{"name", "status", "reasons", "suggestions"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required field %q", field)
		}
	}

	// Verify reasons is an array
	reasons, ok := raw["reasons"].([]interface{})
	if !ok {
		t.Fatal("reasons is not an array")
	}
	if len(reasons) == 0 {
		t.Fatal("expected at least one reason")
	}

	// Verify first reason has required fields
	reason, ok := reasons[0].(map[string]interface{})
	if !ok {
		t.Fatal("reason is not an object")
	}
	for _, field := range []string{"type", "message"} {
		if _, ok := reason[field]; !ok {
			t.Errorf("reason missing required field %q", field)
		}
	}

	// Verify suggestions is an array
	suggestions, ok := raw["suggestions"].([]interface{})
	if !ok {
		t.Fatal("suggestions is not an array")
	}
	if len(suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}

	// Each suggestion should be a string containing "lokt"
	for i, s := range suggestions {
		str, ok := s.(string)
		if !ok {
			t.Errorf("suggestion[%d] is not a string", i)
		}
		if !strings.Contains(str, "lokt") {
			t.Errorf("suggestion[%d] should contain 'lokt': %s", i, str)
		}
	}
}

func TestWhy_DeadPID_JSON(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "dead.json", &lockfile.Lock{
		Name:       "dead",
		Owner:      "ghost",
		Host:       hostname,
		PID:        2000000000,
		AcquiredAt: time.Now().Add(-3 * time.Minute),
	})

	stdout, _, code := captureCmd(cmdWhy, []string{"--json", "dead"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if len(out.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(out.Reasons))
	}
	if out.Reasons[0].Type != "dead_pid" {
		t.Errorf("expected type 'dead_pid', got %q", out.Reasons[0].Type)
	}
	if out.Reasons[0].HolderPIDStatus != "dead" {
		t.Errorf("expected pid_status 'dead', got %q", out.Reasons[0].HolderPIDStatus)
	}
	if out.Reasons[0].HolderStaleReason != "dead_pid" {
		t.Errorf("expected stale_reason 'dead_pid', got %q", out.Reasons[0].HolderStaleReason)
	}

	found := false
	for _, s := range out.Suggestions {
		if strings.Contains(s, "break-stale") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected break-stale suggestion")
	}
}

// Verify the output includes lock name in suggestions.
func TestWhy_SuggestionsIncludeLockName(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "my-lock.json", &lockfile.Lock{
		Name:       "my-lock",
		Owner:      "someone",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
	})

	stdout, _, _ := captureCmd(cmdWhy, []string{"--json", "my-lock"})

	var out whyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, s := range out.Suggestions {
		if !strings.Contains(s, "my-lock") {
			t.Errorf("suggestion should contain lock name 'my-lock': %s", s)
		}
	}
}

// Placeholder test to verify the function signature compiles
// and unused import is not an issue.
var _ = fmt.Sprint // ensure fmt import used
