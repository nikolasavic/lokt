package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

var (
	buildOnce    sync.Once
	builtBinary  string
	errBuild     error
	buildCleanup func()
)

// buildBinary compiles the lokt binary once per test run and returns
// the path to the compiled executable. Calls t.Skip if the build fails.
// The binary is cleaned up when the process exits via atexit-style cleanup.
func buildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lokt-test-bin-*")
		if err != nil {
			errBuild = err
			return
		}
		binPath := filepath.Join(dir, "lokt")
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		cmd.Dir = filepath.Join(getModuleRoot(t), "cmd", "lokt")
		out, err := cmd.CombinedOutput()
		if err != nil {
			_ = os.RemoveAll(dir)
			errBuild = fmt.Errorf("go build failed: %w\n%s", err, out)
			return
		}
		builtBinary = binPath
		buildCleanup = func() { _ = os.RemoveAll(dir) }
	})
	if errBuild != nil {
		t.Skipf("cannot build lokt binary: %v", errBuild)
	}
	t.Cleanup(func() {
		// Cleanup is called per-test but the binary persists across tests.
		// Actual removal happens via buildCleanup at process exit (registered in TestMain).
	})
	return builtBinary
}

// getModuleRoot returns the Go module root directory.
func getModuleRoot(t *testing.T) string {
	t.Helper()
	// cmd/lokt is two levels below module root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

// setupTestRoot creates a temp lokt root with locks/ dir and sets LOKT_ROOT.
// Returns (rootDir, locksDir).
func setupTestRoot(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	t.Setenv("LOKT_ROOT", dir)
	return dir, locksDir
}

// captureCmd runs a command function with the given args, capturing stdout and stderr.
// Returns (stdout, stderr, exitCode).
func captureCmd(fn func([]string) int, args []string) (string, string, int) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	code := fn(args)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)

	return outBuf.String(), errBuf.String(), code
}

// TestMain runs the test suite and cleans up the compiled binary afterward.
func TestMain(m *testing.M) {
	code := m.Run()
	if buildCleanup != nil {
		buildCleanup()
	}
	os.Exit(code)
}

// writeLockJSON writes a lockfile.Lock as JSON to the locks dir.
func writeLockJSON(t *testing.T, locksDir, filename string, lk *lockfile.Lock) {
	t.Helper()
	data, err := json.MarshalIndent(lk, "", "  ")
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locksDir, filename), data, 0600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
}
