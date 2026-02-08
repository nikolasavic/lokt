package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runLokt executes the lokt binary with the given args and env overrides.
// Returns (stdout, stderr, exitCode).
func runLokt(t *testing.T, binary, rootDir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=integration-test",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error (not ExitError): %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// setupIntegrationRoot creates a fresh lokt root with locks/ dir.
func setupIntegrationRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "locks"), 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	return dir
}

// TestIntegration_FullLifecycle exercises: lock → status → unlock → status.
func TestIntegration_FullLifecycle(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "lifecycle-test"

	// 1. Acquire lock with TTL
	_, _, code := runLokt(t, binary, rootDir, "lock", "--ttl", "5m", name)
	if code != ExitOK {
		t.Fatalf("lock: exit %d, want 0", code)
	}

	// 2. Verify lock exists via status --json
	stdout, _, code := runLokt(t, binary, rootDir, "status", "--json", name)
	if code != ExitOK {
		t.Fatalf("status: exit %d, want 0", code)
	}
	var statusResp struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal([]byte(stdout), &statusResp); err != nil {
		t.Fatalf("parse status JSON: %v\nraw: %s", err, stdout)
	}
	if statusResp.Name != name {
		t.Errorf("status name = %q, want %q", statusResp.Name, name)
	}
	if statusResp.Owner != "integration-test" {
		t.Errorf("status owner = %q, want %q", statusResp.Owner, "integration-test")
	}

	// 3. Re-acquire (renew) — same owner should succeed
	_, _, code = runLokt(t, binary, rootDir, "lock", "--ttl", "10m", name)
	if code != ExitOK {
		t.Fatalf("renew: exit %d, want 0 (reentrant acquire)", code)
	}

	// 4. Unlock
	_, _, code = runLokt(t, binary, rootDir, "unlock", name)
	if code != ExitOK {
		t.Fatalf("unlock: exit %d, want 0", code)
	}

	// 5. Status should show not found
	_, _, code = runLokt(t, binary, rootDir, "status", name)
	if code != ExitNotFound {
		t.Errorf("status after unlock: exit %d, want %d (not found)", code, ExitNotFound)
	}
}

// TestIntegration_FreezeBlocksGuard exercises: freeze → guard denied → unfreeze → guard succeeds.
func TestIntegration_FreezeBlocksGuard(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "freeze-test"

	// 1. Freeze the lock
	_, _, code := runLokt(t, binary, rootDir, "freeze", "--ttl", "5m", name)
	if code != ExitOK {
		t.Fatalf("freeze: exit %d, want 0", code)
	}

	// 2. Guard should be denied (frozen)
	_, stderr, code := runLokt(t, binary, rootDir, "guard", name, "--", "true")
	if code != ExitLockHeld {
		t.Fatalf("guard during freeze: exit %d, want %d\nstderr: %s", code, ExitLockHeld, stderr)
	}

	// 3. Unfreeze
	_, _, code = runLokt(t, binary, rootDir, "unfreeze", name)
	if code != ExitOK {
		t.Fatalf("unfreeze: exit %d, want 0", code)
	}

	// 4. Guard should now succeed
	stdout, _, code := runLokt(t, binary, rootDir, "guard", name, "--", "echo", "guarded")
	if code != ExitOK {
		t.Fatalf("guard after unfreeze: exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "guarded") {
		t.Errorf("guard stdout = %q, want to contain 'guarded'", stdout)
	}
}

// TestIntegration_StaleRecovery exercises: acquire → simulate dead PID → break-stale → reacquire.
func TestIntegration_StaleRecovery(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "stale-test"

	// 1. Use guard to acquire a lock with a short-lived child
	//    The child exits immediately, and guard releases the lock.
	//    Then we manually create a stale lockfile simulating a crashed holder.
	locksDir := filepath.Join(rootDir, "locks")
	lockPath := filepath.Join(locksDir, name+".json")

	// Create a lockfile with PID 1 (always alive) and expired TTL.
	// This won't be auto-pruned by `lock` (PID is alive), but IS stale
	// due to expired TTL, so `--break-stale` can remove it.
	expiredAt := time.Now().Add(-30 * time.Minute)
	staleLock := map[string]any{
		"version":     1,
		"name":        name,
		"owner":       "dead-agent",
		"host":        mustHostname(t),
		"pid":         1, // PID 1 always alive — prevents auto-prune
		"acquired_ts": time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
		"ttl_sec":     300,
		"expires_at":  expiredAt.Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(staleLock, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale lock: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	// 2. Try to acquire — should be denied (lock exists)
	_, _, code := runLokt(t, binary, rootDir, "lock", name)
	if code != ExitLockHeld {
		t.Fatalf("lock on stale: exit %d, want %d (held)", code, ExitLockHeld)
	}

	// 3. Break stale lock
	_, _, code = runLokt(t, binary, rootDir, "unlock", "--break-stale", name)
	if code != ExitOK {
		t.Fatalf("break-stale: exit %d, want 0", code)
	}

	// 4. Now acquire should succeed
	_, _, code = runLokt(t, binary, rootDir, "lock", "--ttl", "5m", name)
	if code != ExitOK {
		t.Fatalf("lock after break-stale: exit %d, want 0", code)
	}

	// Cleanup
	_, _, _ = runLokt(t, binary, rootDir, "unlock", name)
}

// TestIntegration_WaitWorkflow exercises: guard holds lock → waiter blocked → guard exits → waiter acquires.
func TestIntegration_WaitWorkflow(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "wait-test"

	// 1. Start a guard holding the lock with a long child
	holderCmd := exec.Command(binary, "guard", "--ttl", "30s", name, "--", "sleep", "60")
	holderCmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=holder-agent",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	if err := holderCmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}

	// Wait for lock to appear
	lockPath := filepath.Join(rootDir, "locks", name+".json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lockPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("holder lock never appeared")
	}

	// 2. Start a waiter in the background
	waiterCmd := exec.Command(binary, "lock", "--wait", "--timeout", "10s", name)
	waiterCmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=waiter-agent",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	var waiterStdout, waiterStderr strings.Builder
	waiterCmd.Stdout = &waiterStdout
	waiterCmd.Stderr = &waiterStderr
	if err := waiterCmd.Start(); err != nil {
		t.Fatalf("start waiter: %v", err)
	}

	// 3. Brief pause, then kill the holder to release the lock
	time.Sleep(500 * time.Millisecond)
	if err := holderCmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = holderCmd.Wait()

	// 4. Wait for the waiter to finish (should acquire after holder releases)
	waiterErr := waiterCmd.Wait()
	waiterCode := 0
	if waiterErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waiterErr, &exitErr) {
			waiterCode = exitErr.ExitCode()
		} else {
			t.Fatalf("waiter error: %v", waiterErr)
		}
	}

	if waiterCode != ExitOK {
		t.Errorf("waiter: exit %d, want 0\nstderr: %s", waiterCode, waiterStderr.String())
	}

	// 5. Verify the waiter now holds the lock
	stdout, _, code := runLokt(t, binary, rootDir, "status", "--json", name)
	if code != ExitOK {
		t.Fatalf("status: exit %d, want 0", code)
	}
	var statusResp struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal([]byte(stdout), &statusResp); err != nil {
		t.Fatalf("parse status: %v", err)
	}
	if statusResp.Owner != "waiter-agent" {
		t.Errorf("lock owner = %q, want %q", statusResp.Owner, "waiter-agent")
	}

	// Cleanup: unlock as waiter
	cleanupCmd := exec.Command(binary, "unlock", name)
	cleanupCmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=waiter-agent",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	_ = cleanupCmd.Run()
}

// TestIntegration_GuardLifecycle exercises: guard spawn → child runs → child exits → lock released.
func TestIntegration_GuardLifecycle(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "guard-lifecycle"

	// 1. Run guard with a child that writes a marker file
	markerPath := filepath.Join(t.TempDir(), "marker")
	stdout, _, code := runLokt(t, binary, rootDir, "guard", "--ttl", "30s", name, "--",
		"sh", "-c", "echo done > "+markerPath)
	if code != ExitOK {
		t.Fatalf("guard: exit %d, want 0\nstdout: %s", code, stdout)
	}

	// 2. Verify child executed (marker file exists)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(data), "done") {
		t.Errorf("marker content = %q, want 'done'", string(data))
	}

	// 3. Verify lock was released
	lockPath := filepath.Join(rootDir, "locks", name+".json")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after guard completed")
	}

	// 4. Another process should be able to acquire the same lock
	_, _, code = runLokt(t, binary, rootDir, "lock", name)
	if code != ExitOK {
		t.Fatalf("lock after guard: exit %d, want 0", code)
	}
	_, _, _ = runLokt(t, binary, rootDir, "unlock", name)
}

// TestIntegration_GuardSignalCleanup verifies guard cleans up on SIGTERM.
func TestIntegration_GuardSignalCleanup(t *testing.T) {
	binary := buildBinary(t)
	rootDir := setupIntegrationRoot(t)
	const name = "guard-signal"

	cmd := exec.Command(binary, "guard", "--ttl", "30s", name, "--", "sleep", "60")
	cmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=integration-test",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start guard: %v", err)
	}

	// Wait for lock to appear
	lockPath := filepath.Join(rootDir, "locks", name+".json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lockPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock never appeared")
	}

	// Brief pause for signal handler registration
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 143 {
			t.Errorf("exit = %v, want exit code 143", err)
		}
	}

	// Lock should be cleaned up
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after SIGTERM")
	}
}

func mustHostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Fatalf("hostname: %v", err)
	}
	return h
}
