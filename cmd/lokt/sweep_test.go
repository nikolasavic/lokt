package main

import (
	"testing"
)

func TestSweepEnabled(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"lock", true},
		{"unlock", true},
		{"status", true},
		{"guard", true},
		{"freeze", true},
		{"unfreeze", true},
		{"why", true},
		{"exists", true},
		{"version", false},
		{"help", false},
		{"audit", false},
		{"doctor", false},
		{"demo", false},
		{"prime", false},
	}
	for _, tc := range tests {
		t.Run(tc.cmd, func(t *testing.T) {
			got := sweepEnabled(tc.cmd)
			if got != tc.want {
				t.Errorf("sweepEnabled(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestSweepEnabled_DisabledByEnv(t *testing.T) {
	t.Setenv("LOKT_NO_SWEEP", "1")

	if sweepEnabled("lock") {
		t.Error("expected sweep disabled when LOKT_NO_SWEEP is set")
	}
}

func TestRunSweep_NoRoot(t *testing.T) {
	// Ensure no LOKT_ROOT is set and we're not in a git repo with .lokt
	t.Setenv("LOKT_ROOT", "")
	// runSweep should not panic or fail — it silently ignores errors
	runSweep()
}

func TestRunSweep_WithRoot(t *testing.T) {
	setupTestRoot(t)
	// Should run without error even with empty locks dir
	runSweep()
}
