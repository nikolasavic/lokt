//go:build linux

package stale

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// ErrStartTimeNotSupported is returned on platforms where process start time
// cannot be retrieved.
var ErrStartTimeNotSupported = errors.New("process start time not supported")

// GetProcessStartTime returns the process start time as a raw clock-tick
// value from /proc/<pid>/stat field 22 (starttime). Values are only
// meaningful for same-host comparison.
//
// Returns (0, error) if the process doesn't exist or /proc is unavailable.
func GetProcessStartTime(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	// Field 2 (comm) is enclosed in parentheses and may contain spaces or
	// parentheses. Find the LAST ')' to safely skip past it.
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 || idx+2 >= len(data) {
		return 0, errors.New("malformed /proc/pid/stat")
	}

	// After the closing ')' and a space, fields are space-separated starting
	// at field 3. Field 22 (starttime) is at index 19 after comm (0-based).
	rest := data[idx+2:] // skip ") "
	fields := bytes.Fields(rest)
	const starttimeIdx = 19 // field 22 - field 3 = index 19
	if len(fields) <= starttimeIdx {
		return 0, errors.New("not enough fields in /proc/pid/stat")
	}

	return strconv.ParseInt(string(fields[starttimeIdx]), 10, 64)
}
