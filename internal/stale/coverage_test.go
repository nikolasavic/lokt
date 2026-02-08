package stale

import (
	"errors"
	"os"
	"testing"
)

func TestIsProcessAlive_PID1(t *testing.T) {
	// PID 1 (init/launchd) should always be alive
	if !IsProcessAlive(1) {
		t.Error("IsProcessAlive(1) should return true")
	}
}

func TestIsProcessAlive_LargeInvalidPID(t *testing.T) {
	// Very large PID that cannot exist
	if IsProcessAlive(4194304) {
		t.Error("IsProcessAlive(4194304) should return false")
	}
}

func TestIsProcessAlive_ZeroPID(t *testing.T) {
	// PID 0 is the kernel on Unix — kill(0, 0) sends signal to all processes
	// in the process group, so we just verify it doesn't panic
	_ = IsProcessAlive(0)
}

func TestGetProcessStartTime_NegativePID(t *testing.T) {
	// Negative PIDs may trigger sysctl errno on some platforms
	_, err := GetProcessStartTime(-1)
	if err == nil {
		t.Error("GetProcessStartTime(-1) should return error")
	}
}

func TestGetProcessStartTime_SysctlSizeError(t *testing.T) {
	old := sysctlFn
	defer func() { sysctlFn = old }()

	sysctlFn = func(_ []int32, _ []byte, _ *uintptr) error {
		return errors.New("sysctl failed")
	}

	_, err := GetProcessStartTime(1)
	if err == nil {
		t.Fatal("expected error from sysctl size query")
	}
}

func TestGetProcessStartTime_SysctlZeroSize(t *testing.T) {
	old := sysctlFn
	defer func() { sysctlFn = old }()

	sysctlFn = func(_ []int32, _ []byte, oldlen *uintptr) error {
		*oldlen = 0
		return nil
	}

	_, err := GetProcessStartTime(1)
	if err == nil {
		t.Fatal("expected 'process not found' error")
	}
}

func TestGetProcessStartTime_SysctlDataError(t *testing.T) {
	old := sysctlFn
	defer func() { sysctlFn = old }()

	call := 0
	sysctlFn = func(_ []int32, _ []byte, oldlen *uintptr) error {
		call++
		if call == 1 {
			// Size query succeeds with some size
			*oldlen = 648
			return nil
		}
		// Data query fails
		return errors.New("sysctl data failed")
	}

	_, err := GetProcessStartTime(1)
	if err == nil {
		t.Fatal("expected error from sysctl data query")
	}
}

func TestGetProcessStartTime_ZeroPID(t *testing.T) {
	// PID 0 is the kernel scheduler — behavior varies by platform
	_, err := GetProcessStartTime(0)
	// We just verify it doesn't panic; error is acceptable
	_ = err
}

func TestGetProcessStartTime_PID1(t *testing.T) {
	// PID 1 should have a valid start time
	ns, err := GetProcessStartTime(1)
	if err != nil {
		t.Skipf("Cannot get start time for PID 1: %v", err)
	}
	if ns <= 0 {
		t.Errorf("PID 1 start time = %d, want positive", ns)
	}
}

func TestGetProcessStartTime_DifferentForDifferentProcesses(t *testing.T) {
	// Our process vs PID 1 should have different start times
	myNS, err := GetProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessStartTime(self) error = %v", err)
	}

	pid1NS, err := GetProcessStartTime(1)
	if err != nil {
		t.Skipf("Cannot get start time for PID 1: %v", err)
	}

	if myNS == pid1NS {
		t.Error("Our process and PID 1 should have different start times")
	}
}
