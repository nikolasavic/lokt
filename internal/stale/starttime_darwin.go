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

// sysctlFn wraps the sysctl syscall for testability.
var sysctlFn = func(mib []int32, old []byte, oldlen *uintptr) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), //nolint:gosec // sysctl requires pointer
		uintptr(len(mib)),                //nolint:gosec // MIB length
		pointerOrZero(old),               //nolint:gosec // sysctl output buffer
		uintptr(unsafe.Pointer(oldlen)),  //nolint:gosec // sysctl requires pointer
		0,                                //nolint:gosec // NULL newp
		0,                                //nolint:gosec // newlen=0
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func pointerOrZero(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0])) //nolint:gosec // sysctl output buffer
}

// GetProcessStartTime returns the process start time as nanoseconds since
// the Unix epoch. On macOS, uses sysctl KERN_PROC to read kinfo_proc and
// extract p_starttime.
//
// Returns (0, error) if the process doesn't exist or the syscall fails.
func GetProcessStartTime(pid int) (int64, error) {
	mib := []int32{
		1,          // CTL_KERN
		14,         // KERN_PROC
		1,          // KERN_PROC_PID
		int32(pid), //nolint:gosec // PID fits in int32
	}

	// Query the required buffer size first.
	n := uintptr(0)
	if err := sysctlFn(mib, nil, &n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, errors.New("process not found")
	}

	buf := make([]byte, n)
	if err := sysctlFn(mib, buf, &n); err != nil {
		return 0, err
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
