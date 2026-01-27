//go:build windows

package stale

// IsProcessAlive checks if a process with the given PID exists.
// On Windows, we cannot easily check PID liveness without additional
// dependencies, so we conservatively return true (assume alive).
// Stale lock detection will rely on TTL expiry instead.
func IsProcessAlive(pid int) bool {
	// Conservative: assume process is alive
	// TTL expiry provides the safety net on Windows
	return true
}
