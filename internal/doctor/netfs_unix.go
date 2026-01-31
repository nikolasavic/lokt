//go:build unix

package doctor

import (
	"fmt"
	"syscall"

	"github.com/nikolasavic/lokt/internal/netfs"
)

// CheckNetworkFS checks if the given path is on a network filesystem.
// On Unix systems, uses statfs to check the filesystem type.
func CheckNetworkFS(path string) CheckResult {
	result := CheckResult{Name: "network_fs"}

	if network, fsName := netfs.Check(path); network {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%s filesystem detected; atomic O_EXCL may not be reliable", fsName)
		return result
	}

	// Check if path is accessible at all
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		result.Status = StatusOK
		result.Message = "filesystem type unknown (path may not exist)"
		return result
	}

	result.Status = StatusOK
	return result
}

// GetFSTypeName returns a human-readable filesystem type name if known.
func GetFSTypeName(path string) string {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return "unknown"
	}

	fsType := stat.Type
	switch fsType {
	case 0x6969:
		return "nfs"
	case 0xff534d42, 0x517B:
		return "cifs"
	case 0x564c:
		return "ncp"
	case 0x5346414F:
		return "afs"
	case 0x65735546:
		return "fuse"
	case 0x9123683E:
		return "btrfs"
	case 0xEF53:
		return "ext4"
	case 0x01021994:
		return "tmpfs"
	case 0x4244:
		return "hfs"
	case 0x482b:
		return "hfs+"
	case 0x1badface:
		return "apfs"
	default:
		return fmt.Sprintf("0x%x", fsType)
	}
}
