// Package root handles discovery of the Lokt root directory.
package root

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	EnvLoktRoot = "LOKT_ROOT"
	DirName     = ".lokt"
	LocksDir    = "locks"
	FreezesDir  = "freezes"
)

// DiscoveryMethod indicates how the Lokt root was discovered.
type DiscoveryMethod int

const (
	// MethodEnvVar indicates root was set via LOKT_ROOT environment variable.
	MethodEnvVar DiscoveryMethod = iota
	// MethodGit indicates root was discovered via git common dir (.git/lokt/).
	MethodGit
	// MethodLocalDir indicates root was set to .lokt/ in current directory.
	MethodLocalDir
)

// String returns a human-readable name for the discovery method.
func (m DiscoveryMethod) String() string {
	switch m {
	case MethodEnvVar:
		return "env"
	case MethodGit:
		return "git"
	case MethodLocalDir:
		return "local"
	default:
		return "unknown"
	}
}

// Find locates the Lokt root directory using the following precedence:
// 1. LOKT_ROOT environment variable
// 2. Git common dir (for worktree support): .git/lokt/
// 3. .lokt/ in current working directory
func Find() (string, error) {
	path, _, err := FindWithMethod()
	return path, err
}

// FindWithMethod locates the Lokt root directory and reports which method was used.
// Returns the path, discovery method, and any error.
func FindWithMethod() (string, DiscoveryMethod, error) {
	// 1. Check environment variable
	if envRoot := os.Getenv(EnvLoktRoot); envRoot != "" {
		return envRoot, MethodEnvVar, nil
	}

	// 2. Try git common dir
	if gitRoot, err := findGitRoot(); err == nil {
		return filepath.Join(gitRoot, "lokt"), MethodGit, nil
	}

	// 3. Fall back to .lokt/ in cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", MethodLocalDir, err
	}
	return filepath.Join(cwd, DirName), MethodLocalDir, nil
}

// findGitRoot returns the git common directory (handles worktrees).
func findGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(out))

	// Handle relative paths from git
	if !filepath.IsAbs(gitDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		gitDir = filepath.Join(cwd, gitDir)
	}

	return gitDir, nil
}

// EnsureDirs creates the root, locks, and freezes directories if they don't exist.
func EnsureDirs(root string) error {
	if err := os.MkdirAll(filepath.Join(root, LocksDir), 0700); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(root, FreezesDir), 0700)
}

// LocksPath returns the path to the locks directory.
func LocksPath(root string) string {
	return filepath.Join(root, LocksDir)
}

// LockFilePath returns the path to a specific lock file.
func LockFilePath(root, name string) string {
	return filepath.Join(root, LocksDir, name+".json")
}

// FreezesPath returns the path to the freezes directory.
func FreezesPath(root string) string {
	return filepath.Join(root, FreezesDir)
}

// FreezeFilePath returns the path to a specific freeze file.
func FreezeFilePath(root, name string) string {
	return filepath.Join(root, FreezesDir, name+".json")
}
