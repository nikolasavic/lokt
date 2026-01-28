//go:build windows

package doctor

// CheckNetworkFS checks if the given path is on a network filesystem.
// On Windows, we would need to use GetDriveType or similar APIs.
// For now, return OK with a note that detection is limited.
func CheckNetworkFS(path string) CheckResult {
	result := CheckResult{Name: "network_fs"}

	// TODO: Implement Windows network drive detection using GetDriveType
	// For now, we cannot reliably detect network filesystems on Windows
	result.Status = StatusOK
	result.Message = "network filesystem detection not available on Windows"
	return result
}

// GetFSTypeName returns a human-readable filesystem type name.
// On Windows, detection is limited.
func GetFSTypeName(path string) string {
	return "unknown"
}
