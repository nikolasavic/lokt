package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestStatus_ArgReorder(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "zone-api.json", &lockfile.Lock{
		Name:       "zone-api",
		Owner:      "alice",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     300,
	})

	tests := []struct {
		name string
		args []string
	}{
		{"name then json", []string{"zone-api", "--json"}},
		{"json then name", []string{"--json", "zone-api"}},
		{"name then json then prune", []string{"zone-api", "--json", "--prune-expired"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := captureCmd(cmdStatus, tc.args)
			if code != ExitOK {
				t.Errorf("expected exit %d, got %d", ExitOK, code)
			}
			var out statusOutput
			if err := json.Unmarshal([]byte(stdout), &out); err != nil {
				t.Fatalf("invalid JSON for args %v: %v\noutput: %s", tc.args, err, stdout)
			}
			if out.Name != "zone-api" {
				t.Errorf("expected name 'zone-api', got %q", out.Name)
			}
		})
	}
}

func TestStatus_NoLocksDir(t *testing.T) {
	// Set LOKT_ROOT to a dir without locks/ subdir
	dir := t.TempDir()
	t.Setenv("LOKT_ROOT", dir)

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{"text", nil, "no locks"},
		{"json", []string{"--json"}, "[]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := captureCmd(cmdStatus, tc.args)
			if code != ExitOK {
				t.Errorf("expected exit %d, got %d", ExitOK, code)
			}
			if !strings.Contains(stdout, tc.wantText) {
				t.Errorf("expected %q in output, got: %s", tc.wantText, stdout)
			}
		})
	}
}

func TestStatus_EmptyLocksDir(t *testing.T) {
	setupTestRoot(t) // creates locks/ but no files

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{"text", nil, "no locks"},
		{"json", []string{"--json"}, "[]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := captureCmd(cmdStatus, tc.args)
			if code != ExitOK {
				t.Errorf("expected exit %d, got %d", ExitOK, code)
			}
			if !strings.Contains(stdout, tc.wantText) {
				t.Errorf("expected %q in output, got: %s", tc.wantText, stdout)
			}
		})
	}
}

func TestStatus_SpecificLock_Text(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "build.json", &lockfile.Lock{
		Name:       "build",
		Owner:      "alice",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-30 * time.Second),
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"build"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	for _, want := range []string{"name:", "owner:", "host:", "pid:", "age:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got: %s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "alice") {
		t.Errorf("expected owner 'alice' in output, got: %s", stdout)
	}
}

func TestStatus_SpecificLock_JSON(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	acqTime := time.Now().Add(-45 * time.Second)
	writeLockJSON(t, locksDir, "api.json", &lockfile.Lock{
		Name:       "api",
		Owner:      "bob",
		Host:       hostname,
		PID:        1,
		AcquiredAt: acqTime,
		TTLSec:     300,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"--json", "api"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out statusOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}

	if out.Name != "api" {
		t.Errorf("expected name 'api', got %q", out.Name)
	}
	if out.Owner != "bob" {
		t.Errorf("expected owner 'bob', got %q", out.Owner)
	}
	if out.Host != hostname {
		t.Errorf("expected host %q, got %q", hostname, out.Host)
	}
	if out.PID != 1 {
		t.Errorf("expected pid 1, got %d", out.PID)
	}
	if out.TTLSec != 300 {
		t.Errorf("expected ttl_sec 300, got %d", out.TTLSec)
	}
	if out.Expired {
		t.Error("expected expired=false")
	}

	// Validate RFC3339 timestamp
	if _, err := time.Parse(time.RFC3339, out.AcquiredAt); err != nil {
		t.Errorf("acquired_ts not valid RFC3339: %q", out.AcquiredAt)
	}

	// age_sec should be ~45 (with ±2 tolerance)
	if math.Abs(float64(out.AgeSec)-45) > 2 {
		t.Errorf("expected age_sec ~45, got %d", out.AgeSec)
	}

	// pid_status should be one of the known values
	switch out.PIDStatus {
	case "alive", "dead", "unknown":
		// ok
	default:
		t.Errorf("unexpected pid_status %q", out.PIDStatus)
	}
}

func TestStatus_SpecificLock_NotFound(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdStatus, []string{"nonexistent"})
	if code != ExitNotFound {
		t.Errorf("expected exit %d, got %d", ExitNotFound, code)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("expected 'not found' in stderr, got: %s", stderr)
	}
}

func TestStatus_SpecificLock_WithTTL(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "deploy.json", &lockfile.Lock{
		Name:       "deploy",
		Owner:      "ci",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"deploy"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "ttl:") {
		t.Errorf("expected 'ttl:' in output, got: %s", stdout)
	}
	if strings.Contains(stdout, "EXPIRED") {
		t.Errorf("active lock should not show EXPIRED, got: %s", stdout)
	}
}

func TestStatus_SpecificLock_Expired_Text(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "old.json", &lockfile.Lock{
		Name:       "old",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"old"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "EXPIRED") {
		t.Errorf("expected 'EXPIRED' in output, got: %s", stdout)
	}
}

func TestStatus_SpecificLock_Expired_JSON(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "stale.json", &lockfile.Lock{
		Name:       "stale",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"--json", "stale"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out statusOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if !out.Expired {
		t.Error("expected expired=true for expired lock")
	}
}

func TestStatus_SpecificLock_PruneExpired(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "prunable.json", &lockfile.Lock{
		Name:       "prunable",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"--prune-expired", "prunable"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "pruned") {
		t.Errorf("expected 'pruned' message, got: %s", stdout)
	}

	// File should be removed
	if _, err := os.Stat(filepath.Join(locksDir, "prunable.json")); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after prune")
	}
}

func TestStatus_SpecificLock_PruneNonExpired(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "active.json", &lockfile.Lock{
		Name:       "active",
		Owner:      "worker",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"--prune-expired", "active"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	// Should show the lock normally (not pruned)
	if !strings.Contains(stdout, "active") {
		t.Errorf("expected lock info in output, got: %s", stdout)
	}

	// File should still exist
	if _, err := os.Stat(filepath.Join(locksDir, "active.json")); os.IsNotExist(err) {
		t.Error("expected lock file to still exist for non-expired lock")
	}
}

func TestStatus_ListAll_Text(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeLockJSON(t, locksDir, name+".json", &lockfile.Lock{
			Name:       name,
			Owner:      "user",
			Host:       hostname,
			PID:        1,
			AcquiredAt: time.Now().Add(-5 * time.Second),
		})
	}

	stdout, _, code := captureCmd(cmdStatus, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("expected %q in output, got: %s", name, stdout)
		}
	}
}

func TestStatus_ListAll_JSON(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	for _, name := range []string{"one", "two", "three"} {
		writeLockJSON(t, locksDir, name+".json", &lockfile.Lock{
			Name:       name,
			Owner:      "user",
			Host:       hostname,
			PID:        1,
			AcquiredAt: time.Now().Add(-5 * time.Second),
		})
	}

	stdout, _, code := captureCmd(cmdStatus, []string{"--json"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out []statusOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if len(out) != 3 {
		t.Errorf("expected 3 locks, got %d", len(out))
	}
}

func TestStatus_ListAll_PruneExpired(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	// Active lock
	writeLockJSON(t, locksDir, "active.json", &lockfile.Lock{
		Name:       "active",
		Owner:      "worker",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-10 * time.Second),
		TTLSec:     600,
	})
	// Expired lock
	writeLockJSON(t, locksDir, "expired.json", &lockfile.Lock{
		Name:       "expired",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	tests := []struct {
		name string
		args []string
	}{
		{"text", []string{"--prune-expired"}},
		{"json", []string{"--json", "--prune-expired"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Re-create expired lock for each sub-test
			writeLockJSON(t, locksDir, "expired.json", &lockfile.Lock{
				Name:       "expired",
				Owner:      "cron",
				Host:       "server",
				PID:        1234,
				AcquiredAt: time.Now().Add(-10 * time.Minute),
				TTLSec:     60,
			})

			stdout, _, code := captureCmd(cmdStatus, tc.args)
			if code != ExitOK {
				t.Errorf("expected exit %d, got %d", ExitOK, code)
			}

			if tc.name == "json" {
				// pruneLockIfExpired prints text before the JSON array,
				// so extract the JSON portion starting at '['.
				jsonStart := strings.Index(stdout, "[")
				if jsonStart < 0 {
					t.Fatalf("no JSON array found in output: %s", stdout)
				}
				var out []statusOutput
				if err := json.Unmarshal([]byte(stdout[jsonStart:]), &out); err != nil {
					t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
				}
				// Only active lock should remain
				if len(out) != 1 {
					t.Errorf("expected 1 lock after prune, got %d", len(out))
				}
				if len(out) > 0 && out[0].Name != "active" {
					t.Errorf("expected remaining lock 'active', got %q", out[0].Name)
				}
			} else {
				if !strings.Contains(stdout, "active") {
					t.Errorf("expected 'active' in output, got: %s", stdout)
				}
				if !strings.Contains(stdout, "pruned") {
					t.Errorf("expected 'pruned' message, got: %s", stdout)
				}
			}

			// Expired lock file should be removed
			if _, err := os.Stat(filepath.Join(locksDir, "expired.json")); !os.IsNotExist(err) {
				t.Error("expected expired lock file to be removed")
			}
		})
	}
}

func TestStatus_ListAll_FreezeLock(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	// Create freezes/ directory and write freeze there
	freezesDir := filepath.Join(rootDir, "freezes")
	if err := os.MkdirAll(freezesDir, 0700); err != nil {
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

	stdout, _, code := captureCmd(cmdStatus, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "[FROZEN]") {
		t.Errorf("expected '[FROZEN]' in output, got: %s", stdout)
	}
}

func TestStatus_ListAll_ExpiredLock(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	writeLockJSON(t, locksDir, "old.json", &lockfile.Lock{
		Name:       "old",
		Owner:      "cron",
		Host:       "server",
		PID:        1234,
		AcquiredAt: time.Now().Add(-10 * time.Minute),
		TTLSec:     60,
	})

	stdout, _, code := captureCmd(cmdStatus, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "[EXPIRED]") {
		t.Errorf("expected '[EXPIRED]' in output, got: %s", stdout)
	}
}

func TestStatus_JSON_FieldsComplete(t *testing.T) {
	_, locksDir := setupTestRoot(t)

	hostname, _ := os.Hostname()
	writeLockJSON(t, locksDir, "full.json", &lockfile.Lock{
		Name:       "full",
		Owner:      "tester",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-60 * time.Second),
		TTLSec:     300,
	})

	stdout, _, _ := captureCmd(cmdStatus, []string{"--json", "full"})

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	// Check all required fields exist
	for _, field := range []string{"name", "owner", "host", "pid", "acquired_ts", "ttl_sec", "age_sec", "expired", "pid_status"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required field %q", field)
		}
	}

	// Validate types
	if _, ok := raw["name"].(string); !ok {
		t.Error("name should be a string")
	}
	if _, ok := raw["owner"].(string); !ok {
		t.Error("owner should be a string")
	}
	if _, ok := raw["host"].(string); !ok {
		t.Error("host should be a string")
	}
	if _, ok := raw["pid"].(float64); !ok {
		t.Error("pid should be a number")
	}
	if _, ok := raw["age_sec"].(float64); !ok {
		t.Error("age_sec should be a number")
	}
	if _, ok := raw["expired"].(bool); !ok {
		t.Error("expired should be a boolean")
	}
	if _, ok := raw["pid_status"].(string); !ok {
		t.Error("pid_status should be a string")
	}

	// Validate RFC3339 timestamp
	ts, _ := raw["acquired_ts"].(string)
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("acquired_ts not valid RFC3339: %q", ts)
	}
}

func TestStatus_JSON_FreezeLock(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	// Create freezes/ directory and write freeze there
	freezesDir := filepath.Join(rootDir, "freezes")
	if err := os.MkdirAll(freezesDir, 0700); err != nil {
		t.Fatalf("mkdir freezes: %v", err)
	}

	hostname, _ := os.Hostname()
	writeLockJSON(t, freezesDir, "db.json", &lockfile.Lock{
		Name:       "db",
		Owner:      "ops",
		Host:       hostname,
		PID:        1,
		AcquiredAt: time.Now().Add(-30 * time.Second),
		TTLSec:     600,
	})

	stdout, _, code := captureCmd(cmdStatus, []string{"--json"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out []statusOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if !out[0].Freeze {
		t.Error("expected freeze=true for freeze in freezes/ directory")
	}
	if out[0].Name != "db" {
		t.Errorf("expected name 'db', got %q", out[0].Name)
	}
}
