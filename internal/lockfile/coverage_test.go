package lockfile

import (
	"errors"
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

func TestGenerateLockID_RandFailFallback(t *testing.T) {
	old := randReadFn
	defer func() { randReadFn = old }()
	randReadFn = func(b []byte) (int, error) { return 0, errors.New("entropy exhausted") }

	id := GenerateLockID()
	if len(id) != 32 {
		t.Errorf("GenerateLockID() fallback length = %d, want 32", len(id))
	}
	// Should be a hex-encoded timestamp
	if id == "" {
		t.Error("GenerateLockID() fallback should not be empty")
	}
}

func TestWrite_WriteError(t *testing.T) {
	old := createTempFn
	defer func() { createTempFn = old }()

	createTempFn = func(_ string, _ string) (*os.File, error) {
		// Return a pipe with closed read end — Write will fail with EPIPE
		r, pw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		_ = r.Close()
		return pw, nil
	}

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "alice",
		Host:       "h1",
		PID:        1,
		AcquiredAt: time.Now(),
	}

	err := Write(filepath.Join(t.TempDir(), "test.json"), lock)
	if err == nil {
		t.Fatal("Write() should fail when tmp.Write fails")
	}
}

func TestWrite_SyncError(t *testing.T) {
	old := createTempFn
	defer func() { createTempFn = old }()

	var readers []*os.File
	createTempFn = func(_ string, _ string) (*os.File, error) {
		// Pipe with open read end: Write succeeds, Sync fails (EINVAL on pipe)
		r, pw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		readers = append(readers, r)
		return pw, nil
	}
	defer func() {
		for _, r := range readers {
			_ = r.Close()
		}
	}()

	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "alice",
		Host:       "h1",
		PID:        1,
		AcquiredAt: time.Now(),
	}

	err := Write(filepath.Join(t.TempDir(), "test.json"), lock)
	if err == nil {
		t.Fatal("Write() should fail when tmp.Sync fails")
	}
}

func TestWrite_RenameError(t *testing.T) {
	// Rename fails when source and destination are on different filesystems (EXDEV),
	// or when destination dir doesn't exist. We test the latter.
	dir := t.TempDir()
	lock := &Lock{
		Version:    CurrentLockfileVersion,
		Name:       "test",
		Owner:      "alice",
		Host:       "h1",
		PID:        1,
		AcquiredAt: time.Now(),
	}

	// Write to a path whose parent doesn't exist — Rename will fail
	// But CreateTemp uses the parent dir, which must exist. So we need
	// CreateTemp to write to a real dir, but Rename to target a missing dir.
	old := createTempFn
	defer func() { createTempFn = old }()
	createTempFn = func(_ string, pattern string) (*os.File, error) {
		// Create temp in a DIFFERENT directory than the target
		return os.CreateTemp(dir, pattern)
	}

	// Target is in /nonexistent/ — Rename will fail with EXDEV or ENOENT
	err := Write("/nonexistent/dir/test.json", lock)
	if err == nil {
		t.Fatal("Write() should fail when Rename fails")
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
