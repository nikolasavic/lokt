package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

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
