//go:build unix

package doctor

import (
	"fmt"
	"syscall"
)

// Network filesystem magic numbers (from Linux statfs)
// These are the f_type values returned by statfs(2)
const (
	nfsMagic     = 0x6969     // NFS_SUPER_MAGIC
	cifsMagic    = 0xff534d42 // CIFS_MAGIC_NUMBER
	smbfsMagic   = 0x517B     // SMB_SUPER_MAGIC
	ncpfsMagic   = 0x564c     // NCP_SUPER_MAGIC
	afs          = 0x5346414F // AFS_SUPER_MAGIC
	fuseMagic    = 0x65735546 // FUSE_SUPER_MAGIC (could be network, could be local)
	sshfsMagic   = fuseMagic  // SSHFS uses FUSE
	nfs4Magic    = 0x6969     // Same as NFS
	glusterMagic = 0x0534f4c47
)

// CheckNetworkFS checks if the given path is on a network filesystem.
// On Unix systems, uses statfs to check the filesystem type.
func CheckNetworkFS(path string) CheckResult {
	result := CheckResult{Name: "network_fs"}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		// Can't determine - not necessarily an error, the path might not exist yet
		result.Status = StatusOK
		result.Message = "filesystem type unknown (path may not exist)"
		return result
	}

	fsType := stat.Type

	// Check for known network filesystem types
	switch fsType {
	case nfsMagic: // Also covers NFS4 (same magic number)
		result.Status = StatusWarn
		result.Message = "NFS filesystem detected; atomic O_EXCL may not be reliable"
		return result
	case cifsMagic, smbfsMagic:
		result.Status = StatusWarn
		result.Message = "CIFS/SMB filesystem detected; atomic O_EXCL may not be reliable"
		return result
	case ncpfsMagic:
		result.Status = StatusWarn
		result.Message = "NCP filesystem detected; atomic operations may not be reliable"
		return result
	case afs:
		result.Status = StatusWarn
		result.Message = "AFS filesystem detected; atomic operations may not be reliable"
		return result
	case fuseMagic:
		// FUSE could be anything - SSHFS, local FUSE, etc.
		result.Status = StatusWarn
		result.Message = "FUSE filesystem detected; may be network-mounted (e.g., SSHFS)"
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
	case nfsMagic:
		return "nfs"
	case cifsMagic, smbfsMagic:
		return "cifs"
	case ncpfsMagic:
		return "ncp"
	case afs:
		return "afs"
	case fuseMagic:
		return "fuse"
	case 0x9123683E: // BTRFS
		return "btrfs"
	case 0xEF53: // EXT4
		return "ext4"
	case 0x01021994: // TMPFS
		return "tmpfs"
	case 0x4244: // HFS
		return "hfs"
	case 0x482b: // HFS+
		return "hfs+"
	case 0x1badface: // APFS (Apple)
		return "apfs"
	default:
		return fmt.Sprintf("0x%x", fsType)
	}
}
