package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestLock_JSONDeny(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other-agent")

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "zone-api.json", &lockfile.Lock{
		Name:       "zone-api",
		Owner:      "alice",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now().Add(-60 * time.Second),
		TTLSec:     300,
	})

	stdout, stderr, code := captureCmd(cmdLock, []string{"--json", "zone-api"})
	if code != ExitLockHeld {
		t.Fatalf("expected exit %d, got %d; stderr: %s", ExitLockHeld, code, stderr)
	}
	if stderr != "" {
		t.Errorf("expected no stderr with --json, got: %s", stderr)
	}

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", out.Status)
	}
	if out.Name != "zone-api" {
		t.Errorf("expected name 'zone-api', got %q", out.Name)
	}
	if out.HolderOwner != "alice" {
		t.Errorf("expected holder_owner 'alice', got %q", out.HolderOwner)
	}
	if out.HolderHost != hostname {
		t.Errorf("expected holder_host %q, got %q", hostname, out.HolderHost)
	}
	if out.HolderPID != os.Getpid() {
		t.Errorf("expected holder_pid %d, got %d", os.Getpid(), out.HolderPID)
	}
	if out.HolderAgeSec < 59 {
		t.Errorf("expected holder_age_sec >= 59, got %d", out.HolderAgeSec)
	}
	if out.HolderTTLSec != 300 {
		t.Errorf("expected holder_ttl_sec 300, got %d", out.HolderTTLSec)
	}
	if out.HolderRemainSec <= 0 {
		t.Errorf("expected holder_remaining_sec > 0, got %d", out.HolderRemainSec)
	}
	if out.HolderExpired {
		t.Error("expected holder_expired false")
	}
	if out.HolderPIDStatus != "alive" {
		t.Errorf("expected holder_pid_status 'alive', got %q", out.HolderPIDStatus)
	}
	if out.HolderAcquiredTS == "" {
		t.Error("expected holder_acquired_ts to be set")
	}
}

func TestLock_JSONDenyExpired(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other-agent")

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "expired-lock.json", &lockfile.Lock{
		Name:       "expired-lock",
		Owner:      "bob",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now().Add(-600 * time.Second),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdLock, []string{"--json", "expired-lock"})
	if code != ExitLockHeld {
		t.Fatalf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if !out.HolderExpired {
		t.Error("expected holder_expired true for expired TTL")
	}
	if out.HolderRemainSec != 0 {
		t.Errorf("expected holder_remaining_sec 0 for expired lock, got %d", out.HolderRemainSec)
	}
}

func TestLock_JSONDenyNoTTL(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other-agent")

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "no-ttl.json", &lockfile.Lock{
		Name:       "no-ttl",
		Owner:      "charlie",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now().Add(-30 * time.Second),
	})

	stdout, _, code := captureCmd(cmdLock, []string{"--json", "no-ttl"})
	if code != ExitLockHeld {
		t.Fatalf("expected exit %d, got %d", ExitLockHeld, code)
	}

	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.HolderTTLSec != 0 {
		t.Errorf("expected holder_ttl_sec 0 for no-TTL lock, got %d", out.HolderTTLSec)
	}
	if out.HolderRemainSec != 0 {
		t.Errorf("expected holder_remaining_sec 0 for no-TTL lock, got %d", out.HolderRemainSec)
	}
	if out.HolderExpired {
		t.Error("expected holder_expired false for no-TTL lock")
	}
}

func TestLock_JSONAcquireSuccess(t *testing.T) {
	setupTestRoot(t)

	stdout, stderr, code := captureCmd(cmdLock, []string{"--json", "fresh-lock"})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d; stderr: %s", ExitOK, code, stderr)
	}

	var out lockAcquireOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.Status != "acquired" {
		t.Errorf("expected status 'acquired', got %q", out.Status)
	}
	if out.Name != "fresh-lock" {
		t.Errorf("expected name 'fresh-lock', got %q", out.Name)
	}
}

func TestLock_JSONExitCode(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other-agent")

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "held.json", &lockfile.Lock{
		Name:       "held",
		Owner:      "alice",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		TTLSec:     300,
	})

	_, _, code := captureCmd(cmdLock, []string{"--json", "held"})
	if code != ExitLockHeld {
		t.Errorf("expected exit %d, got %d", ExitLockHeld, code)
	}
}

func TestLock_WithoutJSON_Unchanged(t *testing.T) {
	_, locksDir := setupTestRoot(t)
	t.Setenv("LOKT_OWNER", "other-agent")

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "test-lock.json", &lockfile.Lock{
		Name:       "test-lock",
		Owner:      "alice",
		Host:       hostname,
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		TTLSec:     300,
	})

	stdout, stderr, code := captureCmd(cmdLock, []string{"test-lock"})
	if code != ExitLockHeld {
		t.Fatalf("expected exit %d, got %d", ExitLockHeld, code)
	}
	// Without --json: error goes to stderr, nothing on stdout
	if stdout != "" {
		t.Errorf("expected no stdout without --json, got: %s", stdout)
	}
	if stderr == "" {
		t.Error("expected stderr output without --json")
	}
	// Verify it's not JSON
	var out lockDenyOutput
	if err := json.Unmarshal([]byte(stderr), &out); err == nil {
		t.Error("stderr should not be valid JSON without --json flag")
	}
}

func TestLock_WithMetadata(t *testing.T) {
	rootDir, locksDir := setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, []string{
		"--meta", "path=/tmp/out.txt",
		"--meta", "commit=abc123",
		"meta-test",
	})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d; stderr: %s", ExitOK, code, stderr)
	}

	// Read the lockfile and verify metadata
	lf, err := lockfile.Read(locksDir + "/meta-test.json")
	if err != nil {
		t.Fatalf("failed to read lockfile: %v", err)
	}
	if lf.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if lf.Metadata["path"] != "/tmp/out.txt" {
		t.Errorf("expected path=/tmp/out.txt, got %q", lf.Metadata["path"])
	}
	if lf.Metadata["commit"] != "abc123" {
		t.Errorf("expected commit=abc123, got %q", lf.Metadata["commit"])
	}
	_ = rootDir // used by setupTestRoot
}

func TestLock_MetadataLastWins(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	_, stderr, code := captureCmd(cmdLock, []string{
		"--meta", "key=first",
		"--meta", "key=second",
		"dupe-test",
	})
	if code != ExitOK {
		t.Fatalf("expected exit %d, got %d; stderr: %s", ExitOK, code, stderr)
	}

	lf, err := lockfile.Read(locksDir + "/dupe-test.json")
	if err != nil {
		t.Fatalf("failed to read lockfile: %v", err)
	}
	if lf.Metadata["key"] != "second" {
		t.Errorf("expected last value 'second', got %q", lf.Metadata["key"])
	}
}

func TestLock_MetadataInvalidFormat(t *testing.T) {
	setupTestRoot(t)

	// Flag parsing will fail, but we need to check the error
	// Since flag.ExitOnError is used, we can't directly test this via captureCmd
	// Instead, test the metaFlag type directly
	var m metaFlag

	if err := m.Set("noequals"); err == nil {
		t.Error("expected error for missing '='")
	}
	if err := m.Set("=value"); err == nil {
		t.Error("expected error for empty key")
	}
	if err := m.Set("key="); err != nil {
		t.Errorf("empty value should be allowed, got error: %v", err)
	}
	if m.data["key"] != "" {
		t.Errorf("expected empty string for key=, got %q", m.data["key"])
	}
}
