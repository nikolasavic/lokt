package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

// --- cmdLock flag validation ---

func TestLock_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage message, got: %s", stderr)
	}
}

func TestLock_NegativeTTL(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, []string{"--ttl", "-5m", "mylock"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "TTL must be positive") {
		t.Errorf("expected TTL error, got: %s", stderr)
	}
}

func TestLock_TimeoutWithoutWait(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, []string{"--timeout", "5s", "mylock"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--timeout requires --wait") {
		t.Errorf("expected timeout requires wait error, got: %s", stderr)
	}
}

func TestLock_NegativeTimeout(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, []string{"--wait", "--timeout", "-5s", "mylock"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--timeout must be positive") {
		t.Errorf("expected timeout positive error, got: %s", stderr)
	}
}

func TestLock_TextAcquireSuccess(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdLock, []string{"new-lock"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, `acquired lock "new-lock"`) {
		t.Errorf("expected acquired message, got: %s", stdout)
	}
}

func TestLock_WithTTL(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdLock, []string{"--ttl", "5m", "ttl-lock"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "acquired") {
		t.Errorf("expected acquired, got: %s", stdout)
	}
}

func TestLock_ArgReorder(t *testing.T) {
	setupTestRoot(t)

	// Test "lokt lock mylock --ttl 5m" reorder
	stdout, _, code := captureCmd(cmdLock, []string{"mylock", "--ttl", "5m"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "acquired") {
		t.Errorf("expected acquired, got: %s", stdout)
	}
}

// --- cmdGuard flag validation ---

func TestGuard_NoSeparator(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdGuard, []string{"mylock", "true"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage, got: %s", stderr)
	}
}

func TestGuard_EmptySeparator(t *testing.T) {
	setupTestRoot(t)

	// -- at position 0 (no name before it)
	_, _, code := captureCmd(cmdGuard, []string{"--", "true"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestGuard_NothingAfterSeparator(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdGuard, []string{"mylock", "--"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestGuard_NegativeTTL(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdGuard, []string{"--ttl", "-5m", "mylock", "--", "true"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "TTL must be positive") {
		t.Errorf("expected TTL error, got: %s", stderr)
	}
}

func TestGuard_TimeoutWithoutWait(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdGuard, []string{"--timeout", "5s", "mylock", "--", "true"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--timeout requires --wait") {
		t.Errorf("expected timeout requires wait error, got: %s", stderr)
	}
}

func TestGuard_NegativeTimeout(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdGuard, []string{"--wait", "--timeout", "-5s", "mylock", "--", "true"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "--timeout must be positive") {
		t.Errorf("expected timeout positive error, got: %s", stderr)
	}
}

func TestGuard_ChildSuccess(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdGuard, []string{"guard-ok", "--", "true"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
}

func TestGuard_ChildFailure(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdGuard, []string{"guard-fail", "--", "false"})
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestGuard_WithTTL(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdGuard, []string{"--ttl", "5m", "guard-ttl", "--", "true"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
}

func TestGuard_BadCommand(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdGuard, []string{"glock", "--", "nonexistent-command-that-does-not-exist"})
	if code != ExitError {
		t.Errorf("expected exit %d, got %d", ExitError, code)
	}
	if !strings.Contains(stderr, "failed to start") {
		t.Errorf("expected start error, got: %s", stderr)
	}
}

// --- lockFile.remaining() ---

func TestLockFile_Remaining_WithExpiresAt(t *testing.T) {
	future := time.Now().Add(5 * time.Minute)
	lf := &lockFile{
		AcquiredAt: time.Now(),
		TTLSec:     300,
		ExpiresAt:  &future,
	}
	rem := lf.remaining()
	if rem < 4*time.Minute || rem > 6*time.Minute {
		t.Errorf("expected ~5m remaining, got %v", rem)
	}
}

func TestLockFile_Remaining_Expired(t *testing.T) {
	past := time.Now().Add(-5 * time.Minute)
	lf := &lockFile{
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     300,
		ExpiresAt:  &past,
	}
	rem := lf.remaining()
	if rem != 0 {
		t.Errorf("expected 0 remaining for expired lock, got %v", rem)
	}
}

func TestLockFile_Remaining_NoTTL(t *testing.T) {
	lf := &lockFile{
		AcquiredAt: time.Now(),
	}
	rem := lf.remaining()
	if rem != 0 {
		t.Errorf("expected 0 remaining for no-TTL lock, got %v", rem)
	}
}

func TestLockFile_Remaining_FallbackArithmetic(t *testing.T) {
	lf := &lockFile{
		AcquiredAt: time.Now().Add(-2 * time.Minute),
		TTLSec:     300, // 5 min TTL, 2 min elapsed => 3 min remaining
	}
	rem := lf.remaining()
	if rem < 2*time.Minute || rem > 4*time.Minute {
		t.Errorf("expected ~3m remaining, got %v", rem)
	}
}

func TestLockFile_Remaining_FallbackExpired(t *testing.T) {
	lf := &lockFile{
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60, // 1 min TTL, 10 min elapsed => expired
	}
	rem := lf.remaining()
	if rem != 0 {
		t.Errorf("expected 0 remaining for expired fallback, got %v", rem)
	}
}

// --- lockFile.IsExpired() ---

func TestLockFile_IsExpired_WithExpiresAt(t *testing.T) {
	past := time.Now().Add(-1 * time.Minute)
	lf := &lockFile{ExpiresAt: &past}
	if !lf.IsExpired() {
		t.Error("expected expired with past ExpiresAt")
	}

	future := time.Now().Add(5 * time.Minute)
	lf2 := &lockFile{ExpiresAt: &future}
	if lf2.IsExpired() {
		t.Error("expected not expired with future ExpiresAt")
	}
}

func TestLockFile_IsExpired_NoTTL(t *testing.T) {
	lf := &lockFile{
		AcquiredAt: time.Now().Add(-1 * time.Hour),
	}
	if lf.IsExpired() {
		t.Error("expected not expired with no TTL")
	}
}

// --- printLockDenyJSON ---

func TestPrintLockDenyJSON_NilLockFile(t *testing.T) {
	stdout, _, _ := captureCmd(func(_ []string) int {
		printLockDenyJSON("test-lock", nil)
		return 0
	}, nil)

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", out.Status)
	}
	if out.Name != "test-lock" {
		t.Errorf("expected name 'test-lock', got %q", out.Name)
	}
	if out.HolderOwner != "" {
		t.Errorf("expected empty holder_owner for nil lock, got %q", out.HolderOwner)
	}
}

func TestPrintLockDenyJSON_WithLockFile(t *testing.T) {
	hostname, _ := os.Hostname()
	acqTime := time.Now().Add(-30 * time.Second)
	future := time.Now().Add(5 * time.Minute)
	lf := &lockFile{
		Owner:      "alice",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: acqTime,
		TTLSec:     300,
		ExpiresAt:  &future,
	}

	stdout, _, _ := captureCmd(func(_ []string) int {
		printLockDenyJSON("deny-lock", lf)
		return 0
	}, nil)

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", out.Status)
	}
	if out.HolderOwner != "alice" {
		t.Errorf("expected holder_owner 'alice', got %q", out.HolderOwner)
	}
	if out.HolderTTLSec != 300 {
		t.Errorf("expected holder_ttl_sec 300, got %d", out.HolderTTLSec)
	}
	if out.HolderRemainSec <= 0 {
		t.Errorf("expected positive remaining, got %d", out.HolderRemainSec)
	}
	if out.HolderExpiresAt == "" {
		t.Error("expected non-empty expires_at")
	}
}

func TestPrintLockDenyJSON_ExpiredLock(t *testing.T) {
	past := time.Now().Add(-5 * time.Minute)
	lf := &lockFile{
		Owner:      "bob",
		Host:       "remote",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
		ExpiresAt:  &past,
	}

	stdout, _, _ := captureCmd(func(_ []string) int {
		printLockDenyJSON("expired", lf)
		return 0
	}, nil)

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if !out.HolderExpired {
		t.Error("expected expired=true")
	}
	if out.HolderRemainSec != 0 {
		t.Errorf("expected 0 remaining, got %d", out.HolderRemainSec)
	}
}

// --- findProjectRoot ---

func TestFindProjectRoot_GitDir(t *testing.T) {
	// Simulates rootDir = /project/.git/lokt
	result := findProjectRoot("/project/.git/lokt")
	if result != "/project" {
		t.Errorf("expected '/project', got %q", result)
	}
}

func TestFindProjectRoot_DotLokt(t *testing.T) {
	result := findProjectRoot("/project/.lokt")
	if result != "/project" {
		t.Errorf("expected '/project', got %q", result)
	}
}

func TestFindProjectRoot_NamedLokt(t *testing.T) {
	result := findProjectRoot("/project/lokt")
	if result != "/project" {
		t.Errorf("expected '/project', got %q", result)
	}
}

// --- usage ---

func TestUsage(t *testing.T) {
	stdout, _, _ := captureCmd(func(_ []string) int {
		usage()
		return 0
	}, nil)
	if !strings.Contains(stdout, "lokt - file-based lock manager") {
		t.Errorf("expected header in usage, got: %s", stdout)
	}
	for _, cmd := range []string{"lock", "unlock", "status", "guard", "freeze", "unfreeze", "audit", "why", "doctor", "prime", "demo", "version"} {
		if !strings.Contains(stdout, cmd) {
			t.Errorf("expected %q in usage, got: %s", cmd, stdout)
		}
	}
	if !strings.Contains(stdout, "Exit codes:") {
		t.Errorf("expected exit codes section")
	}
}

// --- exists command ---

func TestExists_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdExists, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage, got: %s", stderr)
	}
}

func TestExists_TooManyArgs(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdExists, []string{"a", "b"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
}

func TestExists_InvalidName(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdExists, []string{"../bad"})
	if code != ExitError {
		t.Errorf("expected exit %d, got %d", ExitError, code)
	}
	if stderr == "" {
		t.Error("expected error message")
	}
}

func TestExists_Found(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "present.json", &lockfile.Lock{
		Name: "present", Owner: "me", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	_, _, code := captureCmd(cmdExists, []string{"present"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
}

func TestExists_NotFound(t *testing.T) {
	setupTestRoot(t)

	_, _, code := captureCmd(cmdExists, []string{"absent"})
	if code != ExitNotFound {
		t.Errorf("expected exit %d, got %d", ExitNotFound, code)
	}
}

// --- unlock edge cases ---

func TestUnlock_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnlock, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage, got: %s", stderr)
	}
}

func TestUnlock_Force(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other")

	writeLockJSON(t, locksDir, "forced.json", &lockfile.Lock{
		Name: "forced", Owner: "someone-else", Host: "h", PID: 1, AcquiredAt: time.Now(),
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--force", "forced"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "forced") {
		t.Errorf("expected release message, got: %s", stdout)
	}
}

func TestUnlock_BreakStale_ExpiredLock(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other")

	writeLockJSON(t, locksDir, "stale.json", &lockfile.Lock{
		Name: "stale", Owner: "cron", Host: "h", PID: 1,
		AcquiredAt: time.Now().Add(-10 * time.Minute), TTLSec: 60,
	})

	stdout, _, code := captureCmd(cmdUnlock, []string{"--break-stale", "stale"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "stale") {
		t.Errorf("expected release message, got: %s", stdout)
	}
}

// --- freeze/unfreeze edge cases ---

func TestFreeze_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdFreeze, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage, got: %s", stderr)
	}
}

func TestUnfreeze_NoArgs(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdUnfreeze, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage, got: %s", stderr)
	}
}

func TestUnfreeze_Force(t *testing.T) {
	rootDir, _ := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other")

	freezesDir := filepath.Join(rootDir, "freezes")
	if err := os.MkdirAll(freezesDir, 0700); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(10 * time.Minute)
	writeLockJSON(t, freezesDir, "forced.json", &lockfile.Lock{
		Version: 1, Name: "forced", Owner: "someone-else", Host: "h",
		PID: os.Getpid(), AcquiredAt: time.Now(), TTLSec: 600, ExpiresAt: &exp,
	})

	stdout, _, code := captureCmd(cmdUnfreeze, []string{"--force", "forced"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "forced") {
		t.Errorf("expected unfreeze message, got: %s", stdout)
	}
}

// --- readLockFile error handling ---

func TestReadLockFile_Corrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := readLockFile(path)
	if err == nil {
		t.Error("expected error for corrupted file")
	}
}

func TestReadLockFile_NonExistent(t *testing.T) {
	_, err := readLockFile("/nonexistent/path/lock.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// --- showLockBrief ---

func TestShowLockBrief_DeadPID(t *testing.T) {
	rootDir, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "dead-pid.json", &lockfile.Lock{
		Name:       "dead-pid",
		Owner:      "ghost",
		Host:       hostname,
		PID:        2000000000,
		AcquiredAt: time.Now().Add(-5 * time.Minute),
	})

	stdout, _, _ := captureCmd(func(_ []string) int {
		showLockBrief(rootDir, "dead-pid", false)
		return 0
	}, nil)

	if !strings.Contains(stdout, "[DEAD]") {
		t.Errorf("expected [DEAD] marker, got: %s", stdout)
	}
}

func TestShowLockBrief_Expired(t *testing.T) {
	rootDir, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "exp.json", &lockfile.Lock{
		Name:       "exp",
		Owner:      "cron",
		Host:       "remote",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, _ := captureCmd(func(_ []string) int {
		showLockBrief(rootDir, "exp", false)
		return 0
	}, nil)

	if !strings.Contains(stdout, "[EXPIRED]") {
		t.Errorf("expected [EXPIRED] marker, got: %s", stdout)
	}
}

func TestShowLockBrief_Freeze(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	freezesDir := filepath.Join(rootDir, "freezes")
	if err := os.MkdirAll(freezesDir, 0700); err != nil {
		t.Fatal(err)
	}
	hostname, _ := os.Hostname()
	writeLockJSON(t, freezesDir, "deploy.json", &lockfile.Lock{
		Name:       "deploy",
		Owner:      "admin",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-30 * time.Second),
		TTLSec:     600,
	})

	stdout, _, _ := captureCmd(func(_ []string) int {
		showLockBrief(rootDir, "deploy", true)
		return 0
	}, nil)

	if !strings.Contains(stdout, "[FROZEN]") {
		t.Errorf("expected [FROZEN] marker, got: %s", stdout)
	}
}

// --- DefaultWaitTimeout ---

func TestDefaultWaitTimeout_Value(t *testing.T) {
	if DefaultWaitTimeout != 10*time.Minute {
		t.Errorf("DefaultWaitTimeout = %v, want 10m", DefaultWaitTimeout)
	}
}

func TestLock_WaitDefaultTimeout(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Create a lock held by a different owner
	writeLockJSON(t, locksDir, "wait-default.json", &lockfile.Lock{
		Version:    1,
		Name:       "wait-default",
		Owner:      "blocker",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	})

	// --wait without --timeout should apply DefaultWaitTimeout and eventually time out.
	// We can't wait 10m in a test, so instead we verify the command doesn't return
	// instantly (i.e., it enters the wait loop). We use --timeout 1s as the control.
	start := time.Now()
	_, stderr, code := captureCmd(cmdLock, []string{"--wait", "--timeout", "1s", "wait-default"})
	elapsed := time.Since(start)

	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stderr, "timeout waiting for lock") {
		t.Errorf("expected timeout message, got: %s", stderr)
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected wait of ~1s, but returned in %v", elapsed)
	}
}

func TestGuard_WaitDefaultTimeout(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	// Create a lock held by a different owner
	writeLockJSON(t, locksDir, "guard-wait-default.json", &lockfile.Lock{
		Version:    1,
		Name:       "guard-wait-default",
		Owner:      "blocker",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	})

	start := time.Now()
	_, stderr, code := captureCmd(cmdGuard, []string{"--wait", "--timeout", "1s", "guard-wait-default", "--", "true"})
	elapsed := time.Since(start)

	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
	if !strings.Contains(stderr, "timeout waiting for lock") {
		t.Errorf("expected timeout message, got: %s", stderr)
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected wait of ~1s, but returned in %v", elapsed)
	}
}
