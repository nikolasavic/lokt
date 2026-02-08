package lockfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAge(t *testing.T) {
	lock := Lock{
		AcquiredAt: time.Now().Add(-10 * time.Minute),
	}

	age := lock.Age()
	if age < 9*time.Minute || age > 11*time.Minute {
		t.Errorf("Age() = %v, want ~10m", age)
	}
}

func TestAge_Recent(t *testing.T) {
	lock := Lock{
		AcquiredAt: time.Now(),
	}

	age := lock.Age()
	if age > 1*time.Second {
		t.Errorf("Age() = %v, want <1s", age)
	}
}

func TestGenerateLockID_Length(t *testing.T) {
	id := GenerateLockID()
	if len(id) != 32 {
		t.Errorf("GenerateLockID() length = %d, want 32", len(id))
	}
}

func TestGenerateLockID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := GenerateLockID()
		if ids[id] {
			t.Fatalf("GenerateLockID() produced duplicate: %q", id)
		}
		ids[id] = true
	}
}

func TestSyncDir_NonexistentPath(t *testing.T) {
	err := SyncDir("/nonexistent/path/file.json")
	if err == nil {
		t.Fatal("SyncDir() on nonexistent path should fail")
	}
}

func TestSyncDir_ValidPath(t *testing.T) {
	dir := t.TempDir()
	err := SyncDir(dir + "/dummy.json")
	if err != nil {
		t.Fatalf("SyncDir() on valid dir = %v", err)
	}
}

func TestWrite_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	readonlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0700) })

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "alice",
		Host:       "h1",
		PID:        1,
		AcquiredAt: time.Now(),
	}

	// Write should fail because CreateTemp can't create in read-only dir
	err := Write(filepath.Join(readonlyDir, "test.json"), lock)
	if err == nil {
		t.Fatal("Write() to read-only dir should fail")
	}
}

func TestWrite_MarshalIndentError(t *testing.T) {
	// json.MarshalIndent won't fail for Lock struct (all fields are serializable),
	// so this path is effectively dead code. We just verify normal Write works.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "alice",
		Host:       "h1",
		PID:        1,
		AcquiredAt: time.Now(),
	}
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// Verify it can be read back
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}
