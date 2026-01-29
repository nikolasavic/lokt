//go:build darwin

package stale

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"
)

// ErrStartTimeNotSupported is returned on platforms where process start time
// cannot be retrieved.
var ErrStartTimeNotSupported = errors.New("process start time not supported")

// GetProcessStartTime returns the process start time as nanoseconds since
// the Unix epoch. On macOS, uses sysctl KERN_PROC to read kinfo_proc and
// extract p_starttime.
//
// Returns (0, error) if the process doesn't exist or the syscall fails.
func GetProcessStartTime(pid int) (int64, error) {
	mib := [4]int32{
		1,          // CTL_KERN
		14,         // KERN_PROC
		1,          // KERN_PROC_PID
		int32(pid), //nolint:gosec // PID fits in int32
	}

	// Query the required buffer size first.
	n := uintptr(0)
	//nolint:gosec // unsafe.Pointer required for sysctl syscall interface
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), //nolint:gosec // sysctl requires pointer
		4,                                //nolint:gosec // MIB length
		0,                                //nolint:gosec // NULL oldp for size query
		uintptr(unsafe.Pointer(&n)),      //nolint:gosec // sysctl requires pointer
		0,                                //nolint:gosec // NULL newp
		0,                                //nolint:gosec // newlen=0
	)
	if errno != 0 {
		return 0, errno
	}
	if n == 0 {
		return 0, errors.New("process not found")
	}

	buf := make([]byte, n)
	_, _, errno = syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), //nolint:gosec // sysctl requires pointer
		4,
		uintptr(unsafe.Pointer(&buf[0])), //nolint:gosec // sysctl output buffer
		uintptr(unsafe.Pointer(&n)),      //nolint:gosec // sysctl requires pointer
		0,
		0,
	)
	if errno != 0 {
		return 0, errno
	}

	// kinfo_proc starts with extern_proc, whose first field is p_starttime.
	// p_starttime is a timeval { int64 tv_sec; int64 tv_usec } at offset 0.
	const pStarttimeOffset = 0
	if int(n) < pStarttimeOffset+16 {
		return 0, errors.New("kinfo_proc too small")
	}

	tvSec := int64(binary.LittleEndian.Uint64(buf[pStarttimeOffset:]))    //nolint:gosec // timeval.tv_sec is signed
	tvUsec := int64(binary.LittleEndian.Uint64(buf[pStarttimeOffset+8:])) //nolint:gosec // timeval.tv_usec is signed

	return tvSec*1e9 + tvUsec*1e3, nil
}
