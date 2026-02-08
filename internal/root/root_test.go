package root

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// initGitRepo creates a git repository in the given directory.
// Returns an error if git init fails.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\noutput: %s", err, out)
	}
}

// withWorkingDir temporarily changes the working directory for a test.
// Returns a cleanup function that restores the original directory.
func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to dir %q: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("failed to restore dir: %v", err)
		}
	}
}

func TestFindWithMethod_GitRepo(t *testing.T) {
	// Ensure LOKT_ROOT is not set (so git path is tried)
	if err := os.Unsetenv(EnvLoktRoot); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}

	// Create a temp directory with a git repo
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	// Change to the git repo directory
	cleanup := withWorkingDir(t, repoDir)
	defer cleanup()

	// FindWithMethod should discover the git root
	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}

	if method != MethodGit {
		t.Errorf("FindWithMethod() method = %v, want MethodGit", method)
	}

	// Path should end with /lokt (inside .git directory)
	if !strings.HasSuffix(path, string(filepath.Separator)+"lokt") {
		t.Errorf("FindWithMethod() path = %q, want suffix '/lokt'", path)
	}

	// Path should be inside the .git directory
	// Note: On macOS, /var is symlinked to /private/var, so we resolve both paths
	gitDir := filepath.Join(repoDir, ".git")
	expectedPath := filepath.Join(gitDir, "lokt")
	resolvedExpected, _ := filepath.EvalSymlinks(expectedPath)
	resolvedActual, _ := filepath.EvalSymlinks(path)
	if resolvedActual != resolvedExpected {
		t.Errorf("FindWithMethod() path = %q (resolved: %q), want %q (resolved: %q)",
			path, resolvedActual, expectedPath, resolvedExpected)
	}
}

func TestFindWithMethod_GitRepo_Subdirectory(t *testing.T) {
	// Ensure LOKT_ROOT is not set
	if err := os.Unsetenv(EnvLoktRoot); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}

	// Create a temp directory with a git repo and a subdirectory
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	subDir := filepath.Join(repoDir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0750); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Change to the subdirectory (still within the git repo)
	cleanup := withWorkingDir(t, subDir)
	defer cleanup()

	// FindWithMethod should still discover the git root
	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}

	if method != MethodGit {
		t.Errorf("FindWithMethod() method = %v, want MethodGit", method)
	}

	// Path should point to <repo>/.git/lokt regardless of subdirectory
	// Note: On macOS, /var is symlinked to /private/var, so we resolve both paths
	gitDir := filepath.Join(repoDir, ".git")
	expectedPath := filepath.Join(gitDir, "lokt")
	resolvedExpected, _ := filepath.EvalSymlinks(expectedPath)
	resolvedActual, _ := filepath.EvalSymlinks(path)
	if resolvedActual != resolvedExpected {
		t.Errorf("FindWithMethod() path = %q (resolved: %q), want %q (resolved: %q)",
			path, resolvedActual, expectedPath, resolvedExpected)
	}
}

func TestFindWithMethod_EnvVarOverridesGit(t *testing.T) {
	// Create a git repo
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	cleanup := withWorkingDir(t, repoDir)
	defer cleanup()

	// Set LOKT_ROOT - should override git discovery
	customPath := "/custom/lokt/root"
	if err := os.Setenv(EnvLoktRoot, customPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv(EnvLoktRoot) }()

	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}

	if method != MethodEnvVar {
		t.Errorf("FindWithMethod() method = %v, want MethodEnvVar (env should override git)", method)
	}

	if path != customPath {
		t.Errorf("FindWithMethod() path = %q, want %q", path, customPath)
	}
}

func TestFindWithMethod_LocalFallback(t *testing.T) {
	// Ensure LOKT_ROOT is not set
	if err := os.Unsetenv(EnvLoktRoot); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}

	// Create a temp directory that is NOT a git repo
	nonGitDir := t.TempDir()

	// Change to the non-git directory
	cleanup := withWorkingDir(t, nonGitDir)
	defer cleanup()

	// FindWithMethod should fall back to local .lokt
	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}

	if method != MethodLocalDir {
		t.Errorf("FindWithMethod() method = %v, want MethodLocalDir", method)
	}

	// Path should be {cwd}/.lokt
	// Note: On macOS, /var is symlinked to /private/var, so we resolve both paths
	expectedPath := filepath.Join(nonGitDir, DirName)
	resolvedExpected, _ := filepath.EvalSymlinks(expectedPath)
	resolvedActual, _ := filepath.EvalSymlinks(path)
	if resolvedActual != resolvedExpected {
		t.Errorf("FindWithMethod() path = %q (resolved: %q), want %q (resolved: %q)",
			path, resolvedActual, expectedPath, resolvedExpected)
	}

	// Path should end with .lokt
	if !strings.HasSuffix(path, string(filepath.Separator)+DirName) {
		t.Errorf("FindWithMethod() path = %q, want suffix '/%s'", path, DirName)
	}
}

func TestFindWithMethod_GetwdError(t *testing.T) {
	t.Setenv(EnvLoktRoot, "")

	// Use a non-git directory so findGitRoot fails and we reach the getwd path
	nonGitDir := t.TempDir()
	cleanup := withWorkingDir(t, nonGitDir)
	defer cleanup()

	old := getwdFn
	defer func() { getwdFn = old }()
	getwdFn = func() (string, error) {
		return "", errors.New("getwd: no such file or directory")
	}

	_, method, err := FindWithMethod()
	if err == nil {
		t.Error("expected error when getwd fails")
	}
	if method != MethodLocalDir {
		t.Errorf("method = %v, want MethodLocalDir", method)
	}
}

func TestEnsureDirs_PermissionDenied(t *testing.T) {
	// Skip on root user (permission checks don't apply)
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	// Create a temp directory
	parentDir := t.TempDir()

	// Create a read-only directory
	readOnlyDir := filepath.Join(parentDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0500); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	// Ensure we can clean up
	defer func() { _ = os.Chmod(readOnlyDir, 0700) }()

	// Try to create directories inside the read-only directory
	// This should fail with permission denied
	rootPath := filepath.Join(readOnlyDir, "lokt")
	err := EnsureDirs(rootPath)

	if err == nil {
		t.Error("EnsureDirs() expected error for read-only parent, got nil")
	}
}
