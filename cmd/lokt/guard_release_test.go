package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestGuardRelease_ChildFailure verifies that guard releases the lock when
// the child process exits with a non-zero code.
func TestGuardRelease_ChildFailure(t *testing.T) {
	binary := buildBinary(t)
	rootDir := t.TempDir()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	const lockName = "guard-fail-test"

	cmd := exec.Command(binary, "guard", lockName, "--", "false")
	cmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=test-guard",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	err := cmd.Run()

	// Guard should propagate child's exit code (false = 1)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}

	// Lock file should be removed
	lockPath := filepath.Join(locksDir, lockName+".json")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after child failure")
	}
}

// TestGuardRelease_ExitCodePropagation verifies guard propagates arbitrary
// non-zero exit codes from the child.
func TestGuardRelease_ExitCodePropagation(t *testing.T) {
	binary := buildBinary(t)
	rootDir := t.TempDir()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	const lockName = "guard-exit-test"

	cmd := exec.Command(binary, "guard", lockName, "--", "sh", "-c", "exit 42")
	cmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=test-guard",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.ExitCode() != 42 {
		t.Errorf("exit code = %d, want 42", exitErr.ExitCode())
	}

	// Lock file should be removed
	lockPath := filepath.Join(locksDir, lockName+".json")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after child exit 42")
	}
}

// TestGuardRelease_Signal verifies that sending SIGTERM to guard releases the
// lock and exits with 128+15=143.
func TestGuardRelease_Signal(t *testing.T) {
	binary := buildBinary(t)
	rootDir := t.TempDir()
	locksDir := filepath.Join(rootDir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}

	const lockName = "guard-signal-test"

	cmd := exec.Command(binary, "guard", lockName, "--", "sleep", "60")
	cmd.Env = []string{
		"LOKT_ROOT=" + rootDir,
		"LOKT_OWNER=test-guard",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start guard: %v", err)
	}

	// Wait for lock file to appear (guard acquired the lock)
	lockPath := filepath.Join(locksDir, lockName+".json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lockPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file never appeared — guard may not have acquired")
	}

	// Brief pause to ensure signal handler is registered after lock acquisition.
	// There's a small window between lock.Acquire returning and signal.Notify
	// being called in cmdGuard.
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM to guard
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	// Exit code 143 = 128 + 15 (SIGTERM)
	if exitErr.ExitCode() != 143 {
		t.Errorf("exit code = %d, want 143 (128+SIGTERM)", exitErr.ExitCode())
	}

	// Lock file should be removed
	// Give a brief moment for cleanup (release is synchronous before exit,
	// but filesystem visibility can lag slightly)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after SIGTERM")
	}
}
