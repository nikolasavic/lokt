// Package doctor provides health check utilities for validating lokt setup.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status represents the result of a health check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// CheckResult contains the result of a single health check.
type CheckResult struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Overall computes the overall status from multiple check results.
// Returns "fail" if any check failed, "warn" if any warned, "ok" otherwise.
func Overall(results []CheckResult) Status {
	for _, r := range results {
		if r.Status == StatusFail {
			return StatusFail
		}
	}
	for _, r := range results {
		if r.Status == StatusWarn {
			return StatusWarn
		}
	}
	return StatusOK
}

// CheckWritable verifies the directory is writable by creating a test file.
// If the directory doesn't exist, it attempts to create it first.
func CheckWritable(dir string) CheckResult {
	result := CheckResult{Name: "writable"}

	// Ensure directory exists
	locksDir := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("cannot create directory: %v", err)
		return result
	}

	// Create test file
	testFile := filepath.Join(locksDir, ".lokt-doctor-test")
	f, err := os.OpenFile(testFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		// Check if file already exists (shouldn't happen, but handle it)
		if os.IsExist(err) {
			_ = os.Remove(testFile)
			f, err = os.OpenFile(testFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		}
		if err != nil {
			result.Status = StatusFail
			result.Message = fmt.Sprintf("cannot create test file: %v", err)
			return result
		}
	}

	// Write test data
	_, err = f.WriteString("lokt doctor test")
	if err != nil {
		_ = f.Close()
		_ = os.Remove(testFile)
		result.Status = StatusFail
		result.Message = fmt.Sprintf("cannot write to test file: %v", err)
		return result
	}

	// Sync to disk
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(testFile)
		result.Status = StatusFail
		result.Message = fmt.Sprintf("cannot sync test file: %v", err)
		return result
	}

	_ = f.Close()

	// Remove test file
	if err := os.Remove(testFile); err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("cannot remove test file: %v", err)
		return result
	}

	result.Status = StatusOK
	return result
}

// CheckClock verifies the system clock is within a reasonable range.
// Warns if year < 2020 (lokt didn't exist) or > 2100 (likely misconfigured).
func CheckClock() CheckResult {
	result := CheckResult{Name: "clock"}
	year := time.Now().Year()

	if year < 2020 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("system clock appears to be in the past (year %d)", year)
		return result
	}

	if year > 2100 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("system clock appears to be far in the future (year %d)", year)
		return result
	}

	result.Status = StatusOK
	return result
}

// CheckLegacyFreezes warns if the locks/ directory contains freeze-*.json files
// from before the freeze namespace separation. These legacy files will expire
// via TTL; new freezes are written to the freezes/ directory.
func CheckLegacyFreezes(dir string) CheckResult {
	result := CheckResult{Name: "legacy_freezes"}

	locksDir := filepath.Join(dir, "locks")
	entries, err := os.ReadDir(locksDir)
	if err != nil {
		// Directory missing or unreadable — no legacy files
		result.Status = StatusOK
		return result
	}

	var count int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "freeze-") && strings.HasSuffix(name, ".json") {
			count++
		}
	}

	if count == 0 {
		result.Status = StatusOK
		return result
	}

	result.Status = StatusWarn
	result.Message = fmt.Sprintf(
		"%d legacy freeze file(s) in locks/ directory. These will expire via TTL. New freezes use freezes/ directory.",
		count,
	)
	return result
}
