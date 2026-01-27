package lock

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nikolasavic/lokt/internal/lockfile"
)

func TestAcquire(t *testing.T) {
	root := t.TempDir()

	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Verify lock file exists
	path := filepath.Join(root, "locks", "test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock file error = %v", err)
	}

	if lock.Name != "test" {
		t.Errorf("Name = %q, want %q", lock.Name, "test")
	}
	if lock.Owner == "" {
		t.Error("Owner should not be empty")
	}
	if lock.PID == 0 {
		t.Error("PID should not be 0")
	}
}

func TestAcquireWithTTL(t *testing.T) {
	root := t.TempDir()

	err := Acquire(root, "ttl-test", AcquireOptions{TTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	path := filepath.Join(root, "locks", "ttl-test.json")
	lock, err := lockfile.Read(path)
	if err != nil {
		t.Fatalf("Read lock file error = %v", err)
	}

	if lock.TTLSec != 300 {
		t.Errorf("TTLSec = %d, want 300", lock.TTLSec)
	}
}

func TestAcquireContention(t *testing.T) {
	root := t.TempDir()

	// First acquire succeeds
	err := Acquire(root, "contested", AcquireOptions{})
	if err != nil {
		t.Fatalf("First Acquire() error = %v", err)
	}

	// Second acquire fails with HeldError
	err = Acquire(root, "contested", AcquireOptions{})
	if err == nil {
		t.Fatal("Second Acquire() should fail")
	}

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("error should be *HeldError, got %T", err)
	}

	if held.Lock.Name != "contested" {
		t.Errorf("HeldError.Lock.Name = %q, want %q", held.Lock.Name, "contested")
	}

	if !errors.Is(err, ErrLockHeld) {
		t.Error("error should wrap ErrLockHeld")
	}
}

func TestAcquireRace(t *testing.T) {
	root := t.TempDir()

	// Race multiple goroutines - exactly one should win
	const n = 10
	var wg sync.WaitGroup
	wins := make(chan int, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := Acquire(root, "race", AcquireOptions{})
			if err == nil {
				wins <- id
			}
		}(i)
	}

	wg.Wait()
	close(wins)

	winCount := 0
	for range wins {
		winCount++
	}

	if winCount != 1 {
		t.Errorf("Expected exactly 1 winner, got %d", winCount)
	}
}

func TestAcquireCreatesDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "path")

	err := Acquire(root, "test", AcquireOptions{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// Verify directories were created
	locksDir := filepath.Join(root, "locks")
	info, err := os.Stat(locksDir)
	if err != nil {
		t.Fatalf("Stat locks dir error = %v", err)
	}
	if !info.IsDir() {
		t.Error("locks should be a directory")
	}
}
