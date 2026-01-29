package stale

import (
	"os"
	"runtime"
	"testing"
)

func TestGetProcessStartTime_CurrentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("start time not supported on Windows")
	}

	pid := os.Getpid()
	ns1, err := GetProcessStartTime(pid)
	if err != nil {
		t.Fatalf("GetProcessStartTime(%d) error: %v", pid, err)
	}
	if ns1 == 0 {
		t.Fatal("GetProcessStartTime returned 0 for current process")
	}

	// Calling again should return the same value (process hasn't restarted).
	ns2, err := GetProcessStartTime(pid)
	if err != nil {
		t.Fatalf("GetProcessStartTime(%d) second call error: %v", pid, err)
	}
	if ns1 != ns2 {
		t.Errorf("GetProcessStartTime returned different values: %d vs %d", ns1, ns2)
	}
}

func TestGetProcessStartTime_NonExistent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("start time not supported on Windows")
	}

	_, err := GetProcessStartTime(99999999)
	if err == nil {
		t.Error("GetProcessStartTime should return error for non-existent PID")
	}
}
