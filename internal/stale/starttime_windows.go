//go:build windows

package stale

import "errors"

// ErrStartTimeNotSupported is returned on platforms where process start time
// cannot be retrieved.
var ErrStartTimeNotSupported = errors.New("process start time not supported")

// GetProcessStartTime is not supported on Windows.
// Returns (0, ErrStartTimeNotSupported).
func GetProcessStartTime(pid int) (int64, error) {
	return 0, ErrStartTimeNotSupported
}
