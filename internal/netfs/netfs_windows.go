//go:build windows

package netfs

// Check reports whether the given path resides on a network filesystem.
// Windows detection is not yet implemented; always returns false.
func Check(_ string) (network bool, fsName string) {
	return false, ""
}
