//go:build windows

package doctor

// CheckNetworkFS checks if the given path is on a network filesystem.
// On Windows, detection is not yet implemented.
func CheckNetworkFS(_ string) CheckResult {
	return CheckResult{
		Name:    "network_fs",
		Status:  StatusOK,
		Message: "network filesystem detection not available on Windows",
	}
}

// GetFSTypeName returns a human-readable filesystem type name.
// On Windows, detection is limited.
func GetFSTypeName(_ string) string {
	return "unknown"
}
