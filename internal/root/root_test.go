package root

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryMethodString(t *testing.T) {
	tests := []struct {
		method DiscoveryMethod
		want   string
	}{
		{MethodEnvVar, "env"},
		{MethodGit, "git"},
		{MethodLocalDir, "local"},
		{DiscoveryMethod(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.method.String(); got != tt.want {
			t.Errorf("DiscoveryMethod(%d).String() = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestFindWithMethod_EnvVar(t *testing.T) {
	// Set LOKT_ROOT and verify it's used
	testPath := "/tmp/test-lokt-root"
	if err := os.Setenv(EnvLoktRoot, testPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv(EnvLoktRoot) }()

	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}
	if path != testPath {
		t.Errorf("FindWithMethod() path = %q, want %q", path, testPath)
	}
	if method != MethodEnvVar {
		t.Errorf("FindWithMethod() method = %v, want MethodEnvVar", method)
	}
}

func TestFind_Unchanged(t *testing.T) {
	// Verify Find() still works (backwards compatibility)
	testPath := "/tmp/test-lokt-root"
	if err := os.Setenv(EnvLoktRoot, testPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv(EnvLoktRoot) }()

	path, err := Find()
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if path != testPath {
		t.Errorf("Find() = %q, want %q", path, testPath)
	}
}

func TestEnsureDirs_CreatesBothDirectories(t *testing.T) {
	rootDir := t.TempDir()

	if err := EnsureDirs(rootDir); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	locksPath := filepath.Join(rootDir, LocksDir)
	freezesPath := filepath.Join(rootDir, FreezesDir)

	if info, err := os.Stat(locksPath); err != nil {
		t.Errorf("locks directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("locks path is not a directory")
	}

	if info, err := os.Stat(freezesPath); err != nil {
		t.Errorf("freezes directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("freezes path is not a directory")
	}
}

func TestFreezesPath(t *testing.T) {
	root := t.TempDir()
	got := FreezesPath(root)
	want := root + string(filepath.Separator) + FreezesDir
	if got != want {
		t.Errorf("FreezesPath() = %q, want %q", got, want)
	}
}

func TestFreezeFilePath(t *testing.T) {
	root := t.TempDir()
	got := FreezeFilePath(root, "deploy")
	want := root + string(filepath.Separator) + FreezesDir + string(filepath.Separator) + "deploy.json"
	if got != want {
		t.Errorf("FreezeFilePath() = %q, want %q", got, want)
	}
}

func TestLocksPath(t *testing.T) {
	root := t.TempDir()
	got := LocksPath(root)
	want := root + string(filepath.Separator) + LocksDir
	if got != want {
		t.Errorf("LocksPath() = %q, want %q", got, want)
	}
}

func TestLockFilePath(t *testing.T) {
	root := t.TempDir()
	got := LockFilePath(root, "build")
	want := root + string(filepath.Separator) + LocksDir + string(filepath.Separator) + "build.json"
	if got != want {
		t.Errorf("LockFilePath() = %q, want %q", got, want)
	}
}
