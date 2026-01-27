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
)

// Find locates the Lokt root directory using the following precedence:
// 1. LOKT_ROOT environment variable
// 2. Git common dir (for worktree support): .git/lokt/
// 3. .lokt/ in current working directory
func Find() (string, error) {
	// 1. Check environment variable
	if envRoot := os.Getenv(EnvLoktRoot); envRoot != "" {
		return envRoot, nil
	}

	// 2. Try git common dir
	if gitRoot, err := findGitRoot(); err == nil {
		return filepath.Join(gitRoot, "lokt"), nil
	}

	// 3. Fall back to .lokt/ in cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, DirName), nil
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

// EnsureDirs creates the root and locks directories if they don't exist.
func EnsureDirs(root string) error {
	locksPath := filepath.Join(root, LocksDir)
	return os.MkdirAll(locksPath, 0700)
}

// LocksPath returns the path to the locks directory.
func LocksPath(root string) string {
	return filepath.Join(root, LocksDir)
}

// LockFilePath returns the path to a specific lock file.
func LockFilePath(root, name string) string {
	return filepath.Join(root, LocksDir, name+".json")
}
