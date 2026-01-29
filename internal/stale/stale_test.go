package stale

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	// Current process should always be alive
	if !IsProcessAlive(os.Getpid()) {
		t.Error("IsProcessAlive returned false for current process")
	}
}

func TestIsProcessAlive_NonExistent(t *testing.T) {
	// PID 1 is init, but a very high PID is unlikely to exist
	// Use a PID that's almost certainly invalid
	if IsProcessAlive(99999999) {
		t.Error("IsProcessAlive returned true for non-existent PID 99999999")
	}
}

func TestCheck_ExpiredTTL(t *testing.T) {
	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "otherhost", // Different host so PID check is skipped
		PID:        12345,
		AcquiredAt: time.Now().Add(-2 * time.Hour), // 2 hours ago
		TTLSec:     60,                             // 1 minute TTL (expired)
	}

	result := Check(lock)
	if !result.Stale {
		t.Error("Check should return stale for expired lock")
	}
	if result.Reason != ReasonExpired {
		t.Errorf("Check should return ReasonExpired, got %v", result.Reason)
	}
}

func TestCheck_DeadPID_SameHost(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       hostname,
		PID:        99999999, // Very unlikely to exist
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}

	result := Check(lock)
	if !result.Stale {
		t.Error("Check should return stale for dead PID on same host")
	}
	if result.Reason != ReasonDeadPID {
		t.Errorf("Check should return ReasonDeadPID, got %v", result.Reason)
	}
}

func TestCheck_AlivePID_SameHost(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       hostname,
		PID:        os.Getpid(), // Current process - definitely alive
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}

	result := Check(lock)
	if result.Stale {
		t.Error("Check should not return stale for alive PID on same host")
	}
	if result.Reason != ReasonNotStale {
		t.Errorf("Check should return ReasonNotStale, got %v", result.Reason)
	}
}

func TestCheck_CrossHost_NoTTL(t *testing.T) {
	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "definitely-not-this-host.example.com",
		PID:        12345,
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}

	result := Check(lock)
	if result.Stale {
		t.Error("Check should not return stale for cross-host lock without TTL")
	}
	if result.Reason != ReasonUnknown {
		t.Errorf("Check should return ReasonUnknown for cross-host, got %v", result.Reason)
	}
}

func TestCheck_CrossHost_ExpiredTTL(t *testing.T) {
	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       "definitely-not-this-host.example.com",
		PID:        12345,
		AcquiredAt: time.Now().Add(-2 * time.Hour),
		TTLSec:     60, // Expired
	}

	result := Check(lock)
	if !result.Stale {
		t.Error("Check should return stale for cross-host lock with expired TTL")
	}
	if result.Reason != ReasonExpired {
		t.Errorf("Check should return ReasonExpired, got %v", result.Reason)
	}
}

func TestCheck_RecycledPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("start time not supported on Windows")
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Use our own PID (alive) but with a bogus start time — simulates PID recycling.
	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       hostname,
		PID:        os.Getpid(),
		PIDStartNS: 1, // wrong start time — original process is dead, PID recycled
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}

	result := Check(lock)
	if !result.Stale {
		t.Error("Check should return stale for recycled PID (different start time)")
	}
	if result.Reason != ReasonDeadPID {
		t.Errorf("Check should return ReasonDeadPID, got %v", result.Reason)
	}
}

func TestCheck_SamePID_SameStartTime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("start time not supported on Windows")
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Get our real start time.
	startNS, err := GetProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessStartTime: %v", err)
	}

	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       hostname,
		PID:        os.Getpid(),
		PIDStartNS: startNS,
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}

	result := Check(lock)
	if result.Stale {
		t.Error("Check should not return stale when PID and start time match")
	}
	if result.Reason != ReasonNotStale {
		t.Errorf("Check should return ReasonNotStale, got %v", result.Reason)
	}
}

func TestCheck_NoPIDStartNS_Degradation(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Old lock without PIDStartNS (zero value) — should degrade gracefully.
	// PID is alive, so lock should not be stale.
	lock := &lockfile.Lock{
		Name:       "test",
		Owner:      "testuser",
		Host:       hostname,
		PID:        os.Getpid(),
		PIDStartNS: 0, // no start time recorded (old lock format)
		AcquiredAt: time.Now(),
		TTLSec:     0,
	}

	result := Check(lock)
	if result.Stale {
		t.Error("Check should not return stale for old lock (PIDStartNS=0) with alive PID")
	}
	if result.Reason != ReasonNotStale {
		t.Errorf("Check should return ReasonNotStale, got %v", result.Reason)
	}
}
