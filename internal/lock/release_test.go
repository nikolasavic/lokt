package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestRelease(t *testing.T) {
	root := t.TempDir()

	// Acquire first
	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Release should succeed
	err = Release(root, "test", ReleaseOptions{})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Lock file should be gone
	path := filepath.Join(root, "locks", "test.json")
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted")
	}
}

func TestReleaseNotFound(t *testing.T) {
	root := t.TempDir()

	err := Release(root, "nonexistent", ReleaseOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Release() error = %v, want ErrNotFound", err)
	}
}

func TestReleaseNotOwner(t *testing.T) {
	root := t.TempDir()

	// Create a lock with different owner
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "other-owner",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	path := filepath.Join(locksDir, "other-owner.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Release without force should fail
	err := Release(root, "other-owner", ReleaseOptions{})
	if err == nil {
		t.Fatal("Release() should fail for non-owner")
	}

	var notOwner *NotOwnerError
	if !errors.As(err, &notOwner) {
		t.Fatalf("error should be *NotOwnerError, got %T", err)
	}

	if !errors.Is(err, ErrNotOwner) {
		t.Error("error should wrap ErrNotOwner")
	}
}

func TestReleaseForce(t *testing.T) {
	root := t.TempDir()

	// Create a lock with different owner
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "force-test",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now(),
	}
	path := filepath.Join(locksDir, "force-test.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// Force release should succeed
	err := Release(root, "force-test", ReleaseOptions{Force: true})
	if err != nil {
		t.Fatalf("Release(force=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after force release")
	}
}

func TestReleaseBreakStale_ExpiredTTL(t *testing.T) {
	root := t.TempDir()

	// Create a lock with expired TTL
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "stale-ttl",
		Owner:      "someone-else",
		Host:       "other-host",
		PID:        99999,
		AcquiredAt: time.Now().Add(-2 * time.Hour), // 2 hours ago
		TTLSec:     60,                             // 1 minute TTL (expired)
	}
	path := filepath.Join(locksDir, "stale-ttl.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should succeed for expired TTL
	err := Release(root, "stale-ttl", ReleaseOptions{BreakStale: true})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after break-stale")
	}
}

func TestReleaseBreakStale_DeadPID(t *testing.T) {
	root := t.TempDir()

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Create a lock with dead PID on same host
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "stale-pid",
		Owner:      "someone-else",
		Host:       hostname,
		PID:        99999999, // Very unlikely to exist
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "stale-pid.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should succeed for dead PID
	err = Release(root, "stale-pid", ReleaseOptions{BreakStale: true})
	if err != nil {
		t.Fatalf("Release(BreakStale=true) error = %v", err)
	}

	// Lock file should be gone
	_, err = os.Stat(path)
	if !os.IsNotExist(err) {
		t.Error("Lock file should be deleted after break-stale")
	}
}

func TestReleaseBreakStale_NotStale(t *testing.T) {
	root := t.TempDir()

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("Cannot get hostname")
	}

	// Create a lock with alive PID on same host
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "not-stale",
		Owner:      "someone-else",
		Host:       hostname,
		PID:        os.Getpid(), // Current process - definitely alive
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "not-stale.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should fail for non-stale lock
	err = Release(root, "not-stale", ReleaseOptions{BreakStale: true})
	if err == nil {
		t.Fatal("Release(BreakStale=true) should fail for non-stale lock")
	}

	var notStale *NotStaleError
	if !errors.As(err, &notStale) {
		t.Fatalf("error should be *NotStaleError, got %T", err)
	}

	if !errors.Is(err, ErrNotStale) {
		t.Error("error should wrap ErrNotStale")
	}

	// Lock file should still exist
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		t.Error("Lock file should NOT be deleted for non-stale lock")
	}
}

func TestReleaseBreakStale_CrossHost(t *testing.T) {
	root := t.TempDir()

	// Create a lock on different host (cannot verify PID)
	locksDir := filepath.Join(root, "locks")
	os.MkdirAll(locksDir, 0700)

	lock := &lockfile.Lock{
		Name:       "cross-host",
		Owner:      "someone-else",
		Host:       "definitely-not-this-host.example.com",
		PID:        99999,
		AcquiredAt: time.Now(),
		TTLSec:     0, // No TTL
	}
	path := filepath.Join(locksDir, "cross-host.json")
	if err := lockfile.Write(path, lock); err != nil {
		t.Fatalf("Write lock error = %v", err)
	}

	// BreakStale should fail for cross-host lock without TTL
	err := Release(root, "cross-host", ReleaseOptions{BreakStale: true})
	if err == nil {
		t.Fatal("Release(BreakStale=true) should fail for cross-host lock without TTL")
	}

	var notStale *NotStaleError
	if !errors.As(err, &notStale) {
		t.Fatalf("error should be *NotStaleError, got %T", err)
	}

	// Lock file should still exist
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		t.Error("Lock file should NOT be deleted for cross-host lock")
	}
}
