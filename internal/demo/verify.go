package demo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// VerifyResult holds the outcome of ledger verification.
type VerifyResult struct {
	OK            bool
	EntryCount    int
	ExpectedCount int
	Failures      []string
}

// VerifyLedger checks all invariants of a mosaic ledger file:
// - Entry count matches expected total
// - Indices are contiguous 0..total-1
// - No duplicate indices
// - Hash chain is valid (each entry's prev matches previous entry's h)
// - All JSON lines parse successfully
func VerifyLedger(path string, expectedTotal int) (*VerifyResult, error) {
	f, err := os.Open(path) //nolint:gosec // path is controlled
	if err != nil {
		return nil, fmt.Errorf("verify open: %w", err)
	}
	defer func() { _ = f.Close() }()

	result := &VerifyResult{
		OK:            true,
		ExpectedCount: expectedTotal,
	}

	seen := make(map[int]bool)
	prevHash := genesisHash
	lineNum := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry LedgerEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			result.OK = false
			result.Failures = append(result.Failures,
				fmt.Sprintf("line %d: JSON parse error: %v", lineNum, err))
			continue
		}

		// Check hash chain
		if entry.Prev != prevHash {
			result.OK = false
			result.Failures = append(result.Failures,
				fmt.Sprintf("line %d (i=%d): chain break: prev=%q expected=%q",
					lineNum, entry.Index, entry.Prev, prevHash))
		}

		computed := ComputeHash(&entry)
		if entry.Hash != computed {
			result.OK = false
			result.Failures = append(result.Failures,
				fmt.Sprintf("line %d (i=%d): hash mismatch: got=%q computed=%q",
					lineNum, entry.Index, entry.Hash, computed))
		}

		// Check duplicate index
		if seen[entry.Index] {
			result.OK = false
			result.Failures = append(result.Failures,
				fmt.Sprintf("line %d: duplicate index %d", lineNum, entry.Index))
		}
		seen[entry.Index] = true

		prevHash = entry.Hash
		result.EntryCount++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("verify scan: %w", err)
	}

	// Check count
	if result.EntryCount != expectedTotal {
		result.OK = false
		result.Failures = append(result.Failures,
			fmt.Sprintf("entry count: got %d, expected %d", result.EntryCount, expectedTotal))
	}

	// Check contiguity: every index from 0..total-1 must be present
	for i := 0; i < expectedTotal; i++ {
		if !seen[i] {
			result.OK = false
			result.Failures = append(result.Failures,
				fmt.Sprintf("missing index %d", i))
			// Only report first few missing
			if len(result.Failures) > 20 {
				result.Failures = append(result.Failures, "... (truncated)")
				break
			}
		}
	}

	return result, nil
}
