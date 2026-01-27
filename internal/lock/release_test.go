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
