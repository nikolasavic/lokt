//go:build unix

package stale

import "syscall"

// IsProcessAlive checks if a process with the given PID exists.
// On Unix, uses kill(pid, 0) which checks for process existence
// without actually sending a signal.
//
// Returns true if the process exists (including if we lack permission
// to signal it - EPERM means it exists but we can't signal it).
func IsProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	// No error means process exists and we can signal it
	// EPERM means process exists but we lack permission
	// ESRCH means process does not exist
	return err == nil || err == syscall.EPERM
}
