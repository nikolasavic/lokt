package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

// --- extractLockName unit tests ---

func TestExtractLockName(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"simple name", "build", "build"},
		{"with ttl flag after", "build --ttl 5m", "build"},
		{"with ttl flag before", "--ttl 5m build", "build"},
		{"with timeout flag before", "--timeout 30 deploy", "deploy"},
		{"with multiple flags", "--ttl 5m --timeout 30 test", "test"},
		{"with boolean flag", "--break-stale build", "build"},
		{"variable dollar", "$NAME", ""},
		{"variable braces", "${LOCK_NAME}", ""},
		{"placeholder angle brackets", "<name>", ""},
		{"empty string", "", ""},
		{"only flags", "--ttl 5m --timeout 10", ""},
		{"name between flags", "--ttl 5m build --break-stale", "build"},
		{"quoted string", `"build"`, ""},
		{"single-quoted", "'build'", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractLockName(tc.args)
			if got != tc.want {
				t.Errorf("extractLockName(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// --- parseGuardedScript unit tests ---

func TestParseGuardedScript(t *testing.T) {
	projectRoot := t.TempDir()
	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	t.Run("simple guard line", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "build.sh")
		content := "#!/bin/bash\nlokt guard build -- make build\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 1 {
			t.Fatalf("expected 1 script, got %d", len(scripts))
		}
		if scripts[0].Lock != "build" {
			t.Errorf("lock = %q, want %q", scripts[0].Lock, "build")
		}
		if scripts[0].Command != "make build" {
			t.Errorf("command = %q, want %q", scripts[0].Command, "make build")
		}
		if !strings.HasPrefix(scripts[0].Path, "./") {
			t.Errorf("path should start with './', got %q", scripts[0].Path)
		}
	})

	t.Run("guard with flags", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "deploy.sh")
		content := "#!/bin/bash\nlokt guard --ttl 5m deploy -- kubectl apply -f deploy.yaml\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 1 {
			t.Fatalf("expected 1 script, got %d", len(scripts))
		}
		if scripts[0].Lock != "deploy" {
			t.Errorf("lock = %q, want %q", scripts[0].Lock, "deploy")
		}
		if scripts[0].Command != "kubectl apply -f deploy.yaml" {
			t.Errorf("command = %q, want %q", scripts[0].Command, "kubectl apply -f deploy.yaml")
		}
	})

	t.Run("multiple guards in one script", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "ci.sh")
		content := "#!/bin/bash\n" +
			"lokt guard lint -- golangci-lint run\n" +
			"lokt guard test -- go test ./...\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 2 {
			t.Fatalf("expected 2 scripts, got %d", len(scripts))
		}
		if scripts[0].Lock != "lint" {
			t.Errorf("first lock = %q, want %q", scripts[0].Lock, "lint")
		}
		if scripts[1].Lock != "test" {
			t.Errorf("second lock = %q, want %q", scripts[1].Lock, "test")
		}
	})

	t.Run("no guard lines", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "plain.sh")
		content := "#!/bin/bash\necho hello\nmake build\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 0 {
			t.Errorf("expected 0 scripts, got %d", len(scripts))
		}
	})

	t.Run("guard with variable lock name is skipped", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "var.sh")
		content := "#!/bin/bash\nlokt guard $LOCK_NAME -- make build\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 0 {
			t.Errorf("expected 0 scripts (variable lock name), got %d", len(scripts))
		}
	})

	t.Run("long command is truncated", func(t *testing.T) {
		path := filepath.Join(scriptsDir, "long.sh")
		longCmd := strings.Repeat("x", 80)
		content := "#!/bin/bash\nlokt guard build -- " + longCmd + "\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := parseGuardedScript(path, projectRoot)
		if len(scripts) != 1 {
			t.Fatalf("expected 1 script, got %d", len(scripts))
		}
		if len(scripts[0].Command) > 60 {
			t.Errorf("command should be truncated to 60 chars, got %d", len(scripts[0].Command))
		}
		if !strings.HasSuffix(scripts[0].Command, "...") {
			t.Error("truncated command should end with '...'")
		}
	})

	t.Run("nonexistent file returns nil", func(t *testing.T) {
		scripts := parseGuardedScript("/nonexistent/path.sh", projectRoot)
		if scripts != nil {
			t.Errorf("expected nil for nonexistent file, got %v", scripts)
		}
	})
}

// --- discoverGuardedScripts unit tests ---

func TestDiscoverGuardedScripts(t *testing.T) {
	t.Run("discovers scripts in scripts dir", func(t *testing.T) {
		// Set up a project root with a .lokt directory (so findProjectRoot works)
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		scriptsDir := filepath.Join(projectRoot, "scripts")
		if err := os.MkdirAll(scriptsDir, 0750); err != nil {
			t.Fatalf("mkdir scripts: %v", err)
		}

		content := "#!/bin/bash\nlokt guard build -- make build\n"
		if err := os.WriteFile(filepath.Join(scriptsDir, "build.sh"), []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 1 {
			t.Fatalf("expected 1 script, got %d", len(scripts))
		}
		if scripts[0].Lock != "build" {
			t.Errorf("lock = %q, want %q", scripts[0].Lock, "build")
		}
	})

	t.Run("discovers scripts in bin dir", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		binDir := filepath.Join(projectRoot, "bin")
		if err := os.MkdirAll(binDir, 0750); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}

		content := "#!/bin/bash\nlokt guard deploy -- helm upgrade app\n"
		if err := os.WriteFile(filepath.Join(binDir, "deploy.sh"), []byte(content), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 1 {
			t.Fatalf("expected 1 script, got %d", len(scripts))
		}
		if scripts[0].Lock != "deploy" {
			t.Errorf("lock = %q, want %q", scripts[0].Lock, "deploy")
		}
	})

	t.Run("discovers scripts in multiple directories", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		scriptsDir := filepath.Join(projectRoot, "scripts")
		binDir := filepath.Join(projectRoot, "bin")
		for _, d := range []string{scriptsDir, binDir} {
			if err := os.MkdirAll(d, 0750); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}

		if err := os.WriteFile(filepath.Join(scriptsDir, "build.sh"),
			[]byte("#!/bin/bash\nlokt guard build -- make build\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}
		if err := os.WriteFile(filepath.Join(binDir, "test.sh"),
			[]byte("#!/bin/bash\nlokt guard test -- go test ./...\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 2 {
			t.Fatalf("expected 2 scripts, got %d", len(scripts))
		}

		foundLocks := map[string]bool{}
		for _, s := range scripts {
			foundLocks[s.Lock] = true
		}
		if !foundLocks["build"] {
			t.Error("expected to find 'build' lock")
		}
		if !foundLocks["test"] {
			t.Error("expected to find 'test' lock")
		}
	})

	t.Run("ignores non-sh files", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		scriptsDir := filepath.Join(projectRoot, "scripts")
		if err := os.MkdirAll(scriptsDir, 0750); err != nil {
			t.Fatalf("mkdir scripts: %v", err)
		}

		// Write a .py file with lokt guard - should be ignored
		if err := os.WriteFile(filepath.Join(scriptsDir, "build.py"),
			[]byte("#!/usr/bin/env python\n# lokt guard build -- make build\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 0 {
			t.Errorf("expected 0 scripts (non-.sh files ignored), got %d", len(scripts))
		}
	})

	t.Run("deduplicates by lock name", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		// Create same lock name in two directories (scripts/ is scanned first)
		scriptsDir := filepath.Join(projectRoot, "scripts")
		binDir := filepath.Join(projectRoot, "bin")
		for _, d := range []string{scriptsDir, binDir} {
			if err := os.MkdirAll(d, 0750); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}

		if err := os.WriteFile(filepath.Join(scriptsDir, "build.sh"),
			[]byte("#!/bin/bash\nlokt guard build -- make build\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}
		if err := os.WriteFile(filepath.Join(binDir, "build.sh"),
			[]byte("#!/bin/bash\nlokt guard build -- make all\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 1 {
			t.Errorf("expected 1 script (deduplicated), got %d", len(scripts))
		}
	})

	t.Run("ignores non-guard scripts", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		scriptsDir := filepath.Join(projectRoot, "scripts")
		if err := os.MkdirAll(scriptsDir, 0750); err != nil {
			t.Fatalf("mkdir scripts: %v", err)
		}

		if err := os.WriteFile(filepath.Join(scriptsDir, "plain.sh"),
			[]byte("#!/bin/bash\necho hello world\nmake build\n"), 0600); err != nil {
			t.Fatalf("write script: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 0 {
			t.Errorf("expected 0 scripts for non-guard script, got %d", len(scripts))
		}
	})

	t.Run("no script directories exist", func(t *testing.T) {
		projectRoot := t.TempDir()
		loktRoot := filepath.Join(projectRoot, ".lokt")
		if err := os.MkdirAll(loktRoot, 0750); err != nil {
			t.Fatalf("mkdir .lokt: %v", err)
		}

		scripts := discoverGuardedScripts(loktRoot)
		if len(scripts) != 0 {
			t.Errorf("expected 0 scripts when no dirs exist, got %d", len(scripts))
		}
	})
}

// --- findProjectRoot unit tests ---

func TestFindProjectRoot(t *testing.T) {
	t.Run("git lokt path", func(t *testing.T) {
		base := t.TempDir()
		gitLokt := filepath.Join(base, "project", ".git", "lokt")
		if err := os.MkdirAll(gitLokt, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got := findProjectRoot(gitLokt)
		want := filepath.Join(base, "project")
		if got != want {
			t.Errorf("findProjectRoot(%q) = %q, want %q", gitLokt, got, want)
		}
	})

	t.Run("dot-lokt path", func(t *testing.T) {
		base := t.TempDir()
		dotLokt := filepath.Join(base, "project", ".lokt")
		if err := os.MkdirAll(dotLokt, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got := findProjectRoot(dotLokt)
		want := filepath.Join(base, "project")
		if got != want {
			t.Errorf("findProjectRoot(%q) = %q, want %q", dotLokt, got, want)
		}
	})

	t.Run("lokt basename path", func(t *testing.T) {
		base := t.TempDir()
		loktDir := filepath.Join(base, "project", "lokt")
		if err := os.MkdirAll(loktDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got := findProjectRoot(loktDir)
		want := filepath.Join(base, "project")
		if got != want {
			t.Errorf("findProjectRoot(%q) = %q, want %q", loktDir, got, want)
		}
	})
}

// --- scanCurrentLocks unit tests ---

func TestScanCurrentLocks(t *testing.T) {
	t.Run("no locks", func(t *testing.T) {
		rootDir, _ := setupTestRoot(t)
		locks := scanCurrentLocks(rootDir)
		if len(locks) != 0 {
			t.Errorf("expected 0 locks, got %d", len(locks))
		}
	})

	t.Run("single lock", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		hostname, _ := os.Hostname()
		writeLockJSON(t, locksDir, "build.json", &lockfile.Lock{
			Name:       "build",
			Owner:      "alice",
			Host:       hostname,
			PID:        1234,
			AcquiredAt: time.Now().Add(-30 * time.Second),
		})

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 1 {
			t.Fatalf("expected 1 lock, got %d", len(locks))
		}
		if locks[0].Name != "build" {
			t.Errorf("name = %q, want %q", locks[0].Name, "build")
		}
		if locks[0].Owner != "alice" {
			t.Errorf("owner = %q, want %q", locks[0].Owner, "alice")
		}
		if locks[0].Host != hostname {
			t.Errorf("host = %q, want %q", locks[0].Host, hostname)
		}
		if locks[0].Freeze {
			t.Error("expected freeze=false for regular lock")
		}
	})

	t.Run("expired lock", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		writeLockJSON(t, locksDir, "old.json", &lockfile.Lock{
			Name:       "old",
			Owner:      "cron",
			Host:       "server",
			PID:        1234,
			AcquiredAt: time.Now().Add(-10 * time.Minute),
			TTLSec:     60,
		})

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 1 {
			t.Fatalf("expected 1 lock, got %d", len(locks))
		}
		if !locks[0].Expired {
			t.Error("expected expired=true for expired lock")
		}
	})

	t.Run("freeze lock", func(t *testing.T) {
		rootDir, _ := setupTestRoot(t)
		freezesDir := filepath.Join(rootDir, "freezes")
		if err := os.MkdirAll(freezesDir, 0750); err != nil {
			t.Fatalf("mkdir freezes: %v", err)
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

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 1 {
			t.Fatalf("expected 1 lock, got %d", len(locks))
		}
		if !locks[0].Freeze {
			t.Error("expected freeze=true for freeze lock")
		}
		if locks[0].Name != "deploy" {
			t.Errorf("name = %q, want %q", locks[0].Name, "deploy")
		}
	})

	t.Run("corrupted lock file is skipped", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		// Write invalid JSON
		if err := os.WriteFile(filepath.Join(locksDir, "broken.json"), []byte("{invalid"), 0600); err != nil {
			t.Fatalf("write broken lock: %v", err)
		}

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 0 {
			t.Errorf("expected 0 locks (corrupted skipped), got %d", len(locks))
		}
	})

	t.Run("mixed locks and freezes", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		hostname, _ := os.Hostname()

		writeLockJSON(t, locksDir, "build.json", &lockfile.Lock{
			Name:       "build",
			Owner:      "alice",
			Host:       hostname,
			PID:        1,
			AcquiredAt: time.Now().Add(-10 * time.Second),
		})

		freezesDir := filepath.Join(rootDir, "freezes")
		if err := os.MkdirAll(freezesDir, 0750); err != nil {
			t.Fatalf("mkdir freezes: %v", err)
		}
		writeLockJSON(t, freezesDir, "deploy.json", &lockfile.Lock{
			Name:       "deploy",
			Owner:      "admin",
			Host:       hostname,
			PID:        2,
			AcquiredAt: time.Now().Add(-5 * time.Second),
		})

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 2 {
			t.Fatalf("expected 2 locks, got %d", len(locks))
		}

		foundLock := false
		foundFreeze := false
		for _, l := range locks {
			if l.Name == "build" && !l.Freeze {
				foundLock = true
			}
			if l.Name == "deploy" && l.Freeze {
				foundFreeze = true
			}
		}
		if !foundLock {
			t.Error("expected to find regular lock 'build'")
		}
		if !foundFreeze {
			t.Error("expected to find freeze 'deploy'")
		}
	})

	t.Run("directories in locks dir are skipped", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		// Create a subdirectory inside locks/
		if err := os.MkdirAll(filepath.Join(locksDir, "subdir.json"), 0750); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 0 {
			t.Errorf("expected 0 locks (directories skipped), got %d", len(locks))
		}
	})

	t.Run("non-json files are skipped", func(t *testing.T) {
		rootDir, locksDir := setupTestRoot(t)
		if err := os.WriteFile(filepath.Join(locksDir, "readme.txt"), []byte("not a lock"), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		locks := scanCurrentLocks(rootDir)
		if len(locks) != 0 {
			t.Errorf("expected 0 locks (non-json skipped), got %d", len(locks))
		}
	})
}

// --- cmdPrime integration tests ---

// setupPrimeTestRoot creates a .lokt-named root inside a temp directory so that
// findProjectRoot resolves to the temp dir rather than falling back to
// git rev-parse (which would return the real project root and discover
// real scripts). Returns (loktRoot, locksDir).
func setupPrimeTestRoot(t *testing.T) (string, string) {
	t.Helper()
	projectRoot := t.TempDir()
	loktRoot := filepath.Join(projectRoot, ".lokt")
	locksDir := filepath.Join(loktRoot, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	t.Setenv("LOKT_ROOT", loktRoot)
	return loktRoot, locksDir
}

func TestCmdPrime_DefaultOutput_NoScripts(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	// Should contain fallback guidance since no wrapper scripts exist
	if !strings.Contains(stdout, "Lokt Coordination Active") {
		t.Errorf("expected header in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "No wrapper scripts detected") {
		t.Errorf("expected fallback guidance, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lokt guard build --ttl 5m -- make build") {
		t.Errorf("expected example guard command, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_WithScripts(t *testing.T) {
	// Create a project root with .lokt and scripts/
	projectRoot := t.TempDir()
	loktRoot := filepath.Join(projectRoot, ".lokt")
	locksDir := filepath.Join(loktRoot, "locks")
	for _, d := range []string{loktRoot, locksDir} {
		if err := os.MkdirAll(d, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	t.Setenv("LOKT_ROOT", loktRoot)

	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "build.sh"),
		[]byte("#!/bin/bash\nlokt guard build -- make build\n"), 0600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	// Should contain the table header and the discovered script
	if !strings.Contains(stdout, "Guarded Operations") {
		t.Errorf("expected 'Guarded Operations' section, got: %s", stdout)
	}
	if !strings.Contains(stdout, "| Operation | Use this | NOT this |") {
		t.Errorf("expected table header, got: %s", stdout)
	}
	if !strings.Contains(stdout, "build") {
		t.Errorf("expected 'build' lock in table, got: %s", stdout)
	}
	if !strings.Contains(stdout, "make build") {
		t.Errorf("expected 'make build' command in table, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_WithActiveLocks(t *testing.T) {
	_, locksDir := setupPrimeTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "build.json", &lockfile.Lock{
		Name:       "build",
		Owner:      "alice",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-30 * time.Second),
	})

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "Current Status") {
		t.Errorf("expected 'Current Status' section, got: %s", stdout)
	}
	if !strings.Contains(stdout, "**build**") {
		t.Errorf("expected lock name in bold, got: %s", stdout)
	}
	if !strings.Contains(stdout, "alice") {
		t.Errorf("expected owner 'alice' in output, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_NoLocks(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "No locks held.") {
		t.Errorf("expected 'No locks held.' in output, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_ExpiredLock(t *testing.T) {
	_, locksDir := setupPrimeTestRoot(t)

	writeLockJSON(t, locksDir, "old.json", &lockfile.Lock{
		Name:       "old",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "[EXPIRED]") {
		t.Errorf("expected '[EXPIRED]' in output, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_FrozenLock(t *testing.T) {
	rootDir, _ := setupPrimeTestRoot(t)

	freezesDir := filepath.Join(rootDir, "freezes")
	if err := os.MkdirAll(freezesDir, 0750); err != nil {
		t.Fatalf("mkdir freezes: %v", err)
	}
	hostname, _ := os.Hostname()
	writeLockJSON(t, freezesDir, "deploy.json", &lockfile.Lock{
		Name:       "deploy",
		Owner:      "admin",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "[FROZEN]") {
		t.Errorf("expected '[FROZEN]' in output, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_IdentityWithLoktOwner(t *testing.T) {
	setupPrimeTestRoot(t)
	t.Setenv("LOKT_OWNER", "test-agent-42")

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "Your Identity") {
		t.Errorf("expected 'Your Identity' section, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test-agent-42") {
		t.Errorf("expected LOKT_OWNER value in identity, got: %s", stdout)
	}
}

func TestCmdPrime_DefaultOutput_IdentityFallbackToOSUser(t *testing.T) {
	setupPrimeTestRoot(t)
	t.Setenv("LOKT_OWNER", "") // Clear LOKT_OWNER to force OS fallback

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	// Should fall back to the OS username
	u, err := user.Current()
	if err != nil {
		t.Skipf("cannot get OS user: %v", err)
	}
	if !strings.Contains(stdout, u.Username) {
		t.Errorf("expected OS username %q in identity, got: %s", u.Username, stdout)
	}
}

func TestCmdPrime_DefaultOutput_Sections(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	// Verify all expected sections are present
	sections := []string{
		"# Lokt Coordination Active",
		"## Guarded Operations",
		"## If a command fails with \"lock held by another\"",
		"## Lock diagnostics",
		"## Your Identity",
		"## Current Status",
	}
	for _, section := range sections {
		if !strings.Contains(stdout, section) {
			t.Errorf("expected section %q in output, got: %s", section, stdout)
		}
	}
}

// --- Format flag tests ---

func TestCmdPrime_FormatFlag(t *testing.T) {
	validFormats := []string{
		"claude-md",
		"cursorrules",
		"windsurfrules",
		"copilot",
		"clinerules",
		"aider",
	}

	for _, format := range validFormats {
		t.Run(format, func(t *testing.T) {
			setupPrimeTestRoot(t)

			stdout, _, code := captureCmd(cmdPrime, []string{"--format", format})
			if code != ExitOK {
				t.Errorf("expected exit %d for format %q, got %d", ExitOK, format, code)
			}
			if stdout == "" {
				t.Errorf("expected non-empty output for format %q", format)
			}
			// Format output should NOT contain live status section
			if strings.Contains(stdout, "## Current Status") {
				t.Errorf("format %q should not contain live 'Current Status' section", format)
			}
		})
	}
}

func TestCmdPrime_FormatFlag_InvalidFormat(t *testing.T) {
	setupPrimeTestRoot(t)

	_, stderr, code := captureCmd(cmdPrime, []string{"--format", "invalid-format"})
	if code != ExitUsage {
		t.Errorf("expected exit %d for invalid format, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "unknown format") {
		t.Errorf("expected 'unknown format' in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "supported formats") {
		t.Errorf("expected 'supported formats' in stderr, got: %s", stderr)
	}
}

func TestCmdPrime_FormatFlag_WindsurfUnder2000Chars(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "windsurfrules"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if len(stdout) >= 2000 {
		t.Errorf("windsurfrules output should be under 2000 chars, got %d", len(stdout))
	}
}

func TestCmdPrime_FormatFlag_WindsurfWithScripts(t *testing.T) {
	// Windsurfrules must stay compact even with scripts
	projectRoot := t.TempDir()
	loktRoot := filepath.Join(projectRoot, ".lokt")
	locksDir := filepath.Join(loktRoot, "locks")
	for _, d := range []string{loktRoot, locksDir} {
		if err := os.MkdirAll(d, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	t.Setenv("LOKT_ROOT", loktRoot)

	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	// Write several scripts
	for i, name := range []string{"build", "test", "lint", "deploy", "fmt"} {
		content := "#!/bin/bash\nlokt guard " + name + " -- make " + name + "\n"
		if err := os.WriteFile(filepath.Join(scriptsDir, name+".sh"),
			[]byte(content), 0600); err != nil {
			t.Fatalf("write script %d: %v", i, err)
		}
	}

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "windsurfrules"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if len(stdout) >= 2000 {
		t.Errorf("windsurfrules output should be under 2000 chars even with scripts, got %d", len(stdout))
	}
}

func TestCmdPrime_FormatFlag_ClaudeMD(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "claude-md"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "Concurrent Operations (Lokt)") {
		t.Errorf("expected claude-md header, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lokt status") {
		t.Errorf("expected diagnostic commands in output, got: %s", stdout)
	}
}

func TestCmdPrime_FormatFlag_CursorRules(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "cursorrules"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "Lokt Lock Coordination") {
		t.Errorf("expected cursorrules header, got: %s", stdout)
	}
	if !strings.Contains(stdout, "MANDATORY") {
		t.Errorf("expected 'MANDATORY' in cursorrules output, got: %s", stdout)
	}
}

func TestCmdPrime_FormatFlag_ClineRules(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "clinerules"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	// Cline rules has YAML frontmatter
	if !strings.Contains(stdout, "---") {
		t.Errorf("expected YAML frontmatter, got: %s", stdout)
	}
	if !strings.Contains(stdout, "description: Lokt lock coordination rules") {
		t.Errorf("expected frontmatter description, got: %s", stdout)
	}
}

func TestCmdPrime_FormatFlag_Aider_NoScripts(t *testing.T) {
	setupPrimeTestRoot(t)

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "aider"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "Lokt lock coordination") {
		t.Errorf("expected aider header, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lokt guard") {
		t.Errorf("expected guard hint in aider output, got: %s", stdout)
	}
}

func TestCmdPrime_FormatFlag_Aider_WithScripts(t *testing.T) {
	projectRoot := t.TempDir()
	loktRoot := filepath.Join(projectRoot, ".lokt")
	locksDir := filepath.Join(loktRoot, "locks")
	for _, d := range []string{loktRoot, locksDir} {
		if err := os.MkdirAll(d, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	t.Setenv("LOKT_ROOT", loktRoot)

	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	// lint and test scripts get mapped to lint-cmd and test-cmd
	if err := os.WriteFile(filepath.Join(scriptsDir, "lint.sh"),
		[]byte("#!/bin/bash\nlokt guard lint -- golangci-lint run\n"), 0600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "test.sh"),
		[]byte("#!/bin/bash\nlokt guard test -- go test ./...\n"), 0600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	stdout, _, code := captureCmd(cmdPrime, []string{"--format", "aider"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	if !strings.Contains(stdout, "lint-cmd:") {
		t.Errorf("expected 'lint-cmd:' in aider output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test-cmd:") {
		t.Errorf("expected 'test-cmd:' in aider output, got: %s", stdout)
	}
}

func TestCmdPrime_FormatFlag_Copilot(t *testing.T) {
	setupPrimeTestRoot(t)

	// Copilot uses same renderer as claude-md
	stdoutCopilot, _, codeCopilot := captureCmd(cmdPrime, []string{"--format", "copilot"})
	if codeCopilot != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, codeCopilot)
	}

	stdoutClaude, _, codeClaude := captureCmd(cmdPrime, []string{"--format", "claude-md"})
	if codeClaude != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, codeClaude)
	}

	if stdoutCopilot != stdoutClaude {
		t.Error("copilot and claude-md formats should produce identical output")
	}
}

func TestCmdPrime_FormatFlag_WithScriptsTable(t *testing.T) {
	projectRoot := t.TempDir()
	loktRoot := filepath.Join(projectRoot, ".lokt")
	locksDir := filepath.Join(loktRoot, "locks")
	for _, d := range []string{loktRoot, locksDir} {
		if err := os.MkdirAll(d, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	t.Setenv("LOKT_ROOT", loktRoot)

	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "build.sh"),
		[]byte("#!/bin/bash\nlokt guard build -- make build\n"), 0600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	// Test formats that use the table (claude-md, clinerules)
	for _, format := range []string{"claude-md", "clinerules"} {
		t.Run(format, func(t *testing.T) {
			stdout, _, code := captureCmd(cmdPrime, []string{"--format", format})
			if code != ExitOK {
				t.Errorf("expected exit %d, got %d", ExitOK, code)
			}
			if !strings.Contains(stdout, "| Operation | Use this | NOT this |") {
				t.Errorf("expected script table in %s output, got: %s", format, stdout)
			}
			if !strings.Contains(stdout, "build") {
				t.Errorf("expected 'build' in %s table, got: %s", format, stdout)
			}
		})
	}
}

// --- Error handling tests ---

func TestCmdPrime_NoLoktRoot(t *testing.T) {
	// Unset LOKT_ROOT so root.Find() doesn't short-circuit with the env value.
	t.Setenv("LOKT_ROOT", "")

	// Chdir to a temp directory outside any git repo so git-based discovery fails
	// and cwd-based .lokt/ also doesn't exist.
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// root.Find() will fall back to <cwd>/.lokt which doesn't exist.
	// However, root.Find() always returns a path (never an error for cwd fallback).
	// The command itself succeeds but with empty locks/ since the dir doesn't exist.
	// So instead, test that when LOKT_ROOT points to a valid but truly empty dir
	// (no locks subdir), cmdPrime still succeeds gracefully with "No locks held."
	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "No locks held.") {
		t.Errorf("expected 'No locks held.' in output, got: %s", stdout)
	}
}

func TestCmdPrime_EmptyLocksDirectory(t *testing.T) {
	setupPrimeTestRoot(t) // creates locks/ but no files

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "No locks held.") {
		t.Errorf("expected 'No locks held.' in output, got: %s", stdout)
	}
}

func TestCmdPrime_CorruptedLockFileSkipped(t *testing.T) {
	_, locksDir := setupPrimeTestRoot(t)

	// Write valid lock
	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "good.json", &lockfile.Lock{
		Name:       "good",
		Owner:      "alice",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
	})

	// Write corrupted lock
	if err := os.WriteFile(filepath.Join(locksDir, "bad.json"),
		[]byte("not valid json{{{"), 0600); err != nil {
		t.Fatalf("write bad lock: %v", err)
	}

	stdout, _, code := captureCmd(cmdPrime, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d (corrupted files skipped), got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "**good**") {
		t.Errorf("expected valid lock 'good' in output, got: %s", stdout)
	}
}

// --- guardLineRegexp tests ---

func TestGuardLineRegexp(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantArgs  string
		wantCmd   string
	}{
		{
			name:      "simple guard",
			line:      "lokt guard build -- make build",
			wantMatch: true,
			wantArgs:  "build",
			wantCmd:   "make build",
		},
		{
			name:      "guard with flags",
			line:      "lokt guard --ttl 5m build -- make build",
			wantMatch: true,
			wantArgs:  "--ttl 5m build",
			wantCmd:   "make build",
		},
		{
			name:      "guard with extra spaces",
			line:      "lokt  guard  build  --  make build",
			wantMatch: true,
			wantArgs:  "build",
			wantCmd:   "make build",
		},
		{
			name:      "no guard keyword",
			line:      "lokt lock build",
			wantMatch: false,
		},
		{
			name:      "no double dash separator",
			line:      "lokt guard build make build",
			wantMatch: false,
		},
		{
			name:      "guard in comment",
			line:      "# lokt guard build -- make build",
			wantMatch: true, // regex matches anywhere in line
			wantArgs:  "build",
			wantCmd:   "make build",
		},
		{
			name:      "complex command after --",
			line:      "lokt guard git-push -- git pull --rebase && git push",
			wantMatch: true,
			wantArgs:  "git-push",
			wantCmd:   "git pull --rebase && git push",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matches := guardLineRegexp.FindStringSubmatch(tc.line)
			if tc.wantMatch {
				if matches == nil {
					t.Fatalf("expected match for %q, got none", tc.line)
				}
				if matches[1] != tc.wantArgs {
					t.Errorf("args = %q, want %q", matches[1], tc.wantArgs)
				}
				if matches[2] != tc.wantCmd {
					t.Errorf("cmd = %q, want %q", matches[2], tc.wantCmd)
				}
			} else if matches != nil {
				t.Errorf("expected no match for %q, got %v", tc.line, matches)
			}
		})
	}
}
