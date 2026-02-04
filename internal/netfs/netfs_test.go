package netfs

import (
	"os"
	"path/filepath"
	"testing"
)

// Note: The network filesystem detection branches (NFS, CIFS, FUSE, etc.)
// cannot be unit tested without actual network mounts. The tests below cover:
// - Local filesystem detection (returns false)
// - Error handling (nonexistent path returns false)
// - Various edge cases for path handling
//
// Coverage is ~50% because we can't mock syscall.Statfs without refactoring.
// This is acceptable for this package - the code is simple and the untested
// branches are straightforward switch cases.

func TestCheck_LocalDir(t *testing.T) {
	dir := t.TempDir()

	network, fsName := Check(dir)
	if network {
		t.Errorf("Check(%q) = true (%s), want false for local temp dir", dir, fsName)
	}
	if fsName != "" {
		t.Errorf("Check(%q) returned fsName=%q, want empty for local fs", dir, fsName)
	}
}

func TestCheck_NonexistentPath(t *testing.T) {
	network, fsName := Check("/nonexistent/path/that/does/not/exist")
	if network {
		t.Error("Check on nonexistent path should return false")
	}
	if fsName != "" {
		t.Errorf("Check on nonexistent path returned fsName=%q, want empty", fsName)
	}
}

func TestCheck_RootDir(t *testing.T) {
	// Root directory should be local on most test systems
	network, _ := Check("/")
	// Don't assert false - some CI systems might have / on NFS
	// Just verify the function doesn't panic
	_ = network
}

func TestCheck_CurrentDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get cwd: %v", err)
	}
	network, fsName := Check(cwd)
	// Current working directory is likely local in tests
	if network {
		t.Logf("cwd %q detected as network fs (%s) - unusual but not wrong", cwd, fsName)
	}
}

func TestCheck_Symlink(t *testing.T) {
	// Create a symlink and check it
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")

	if err := os.Mkdir(target, 0750); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	network, _ := Check(link)
	if network {
		t.Errorf("Check(%q) symlink to local dir should return false", link)
	}
}

func TestCheck_File(t *testing.T) {
	// Check on a file (not directory) - should still work
	dir := t.TempDir()
	file := filepath.Join(dir, "testfile")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	network, _ := Check(file)
	if network {
		t.Errorf("Check(%q) on local file should return false", file)
	}
}

func TestCheck_EmptyPath(t *testing.T) {
	// Empty path should fail gracefully
	network, fsName := Check("")
	if network {
		t.Error("Check on empty path should return false")
	}
	if fsName != "" {
		t.Errorf("Check on empty path returned fsName=%q, want empty", fsName)
	}
}
