//go:build unix

// Package netfs detects network filesystems where atomic O_EXCL is unreliable.
package netfs

import (
	"syscall"
)

// Filesystem magic numbers from statfs(2).
const (
	nfsMagic   = 0x6969     // NFS_SUPER_MAGIC (also NFS4)
	cifsMagic  = 0xff534d42 // CIFS_MAGIC_NUMBER
	smbfsMagic = 0x517B     // SMB_SUPER_MAGIC
	ncpfsMagic = 0x564c     // NCP_SUPER_MAGIC
	afsMagic   = 0x5346414F // AFS_SUPER_MAGIC
	fuseMagic  = 0x65735546 // FUSE_SUPER_MAGIC (could be SSHFS, GlusterFS, etc.)
)

// Check reports whether the given path resides on a network filesystem.
// Returns true and the filesystem name if a network FS is detected.
// Returns false on local filesystems or if detection fails.
func Check(path string) (network bool, fsName string) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false, ""
	}

	switch stat.Type {
	case nfsMagic:
		return true, "NFS"
	case cifsMagic, smbfsMagic:
		return true, "CIFS/SMB"
	case ncpfsMagic:
		return true, "NCP"
	case afsMagic:
		return true, "AFS"
	case fuseMagic:
		return true, "FUSE"
	default:
		return false, ""
	}
}
