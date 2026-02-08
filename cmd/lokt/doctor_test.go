package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nikolasavic/lokt/internal/doctor"
	"github.com/nikolasavic/lokt/internal/root"
)

func TestCmdDoctor_TextOutput(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdDoctor, nil)
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "lokt doctor") {
		t.Errorf("expected 'lokt doctor' header, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Root:") {
		t.Errorf("expected 'Root:' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "LOKT_ROOT env") {
		t.Errorf("expected 'LOKT_ROOT env' method, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Checks:") {
		t.Errorf("expected 'Checks:' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Result:") {
		t.Errorf("expected 'Result:' in output, got: %s", stdout)
	}
}

func TestCmdDoctor_JSONOutput(t *testing.T) {
	setupTestRoot(t)

	stdout, _, code := captureCmd(cmdDoctor, []string{"--json"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}

	var out doctorOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, stdout)
	}
	if out.ProtocolVersion != 1 {
		t.Errorf("expected protocol version 1, got %d", out.ProtocolVersion)
	}
	if out.RootMethod != "env" {
		t.Errorf("expected root_method 'env', got %q", out.RootMethod)
	}
	if out.RootPath == "" {
		t.Error("expected non-empty root_path")
	}
	if len(out.Checks) == 0 {
		t.Error("expected at least one check result")
	}
}

func TestMethodDescription(t *testing.T) {
	tests := []struct {
		method root.DiscoveryMethod
		want   string
	}{
		{root.MethodEnvVar, "LOKT_ROOT env"},
		{root.MethodGit, "git common dir"},
		{root.MethodLocalDir, ".lokt/ fallback"},
		{root.DiscoveryMethod(99), "unknown"},
	}
	for _, tc := range tests {
		got := methodDescription(tc.method)
		if got != tc.want {
			t.Errorf("methodDescription(%d) = %q, want %q", tc.method, got, tc.want)
		}
	}
}

func TestPrintCheckResult(t *testing.T) {
	tests := []struct {
		name   string
		result doctor.CheckResult
		want   string
	}{
		{
			name:   "ok",
			result: doctor.CheckResult{Name: "writable", Status: doctor.StatusOK},
			want:   "[OK]",
		},
		{
			name:   "warn",
			result: doctor.CheckResult{Name: "clock", Status: doctor.StatusWarn, Message: "slight drift"},
			want:   "[WARN]",
		},
		{
			name:   "fail",
			result: doctor.CheckResult{Name: "writable", Status: doctor.StatusFail, Message: "permission denied"},
			want:   "[FAIL]",
		},
		{
			name:   "display-name-mapping",
			result: doctor.CheckResult{Name: "writable", Status: doctor.StatusOK},
			want:   "Directory writable",
		},
		{
			name:   "clock-display-name",
			result: doctor.CheckResult{Name: "clock", Status: doctor.StatusOK},
			want:   "Clock sanity",
		},
		{
			name:   "unknown-name-passthrough",
			result: doctor.CheckResult{Name: "custom_check", Status: doctor.StatusOK},
			want:   "custom_check",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, _ := captureCmd(func(_ []string) int {
				printCheckResult(tc.result)
				return 0
			}, nil)
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("expected %q in output, got: %s", tc.want, stdout)
			}
		})
	}
}

func TestPrintCheckResult_WithMessage(t *testing.T) {
	stdout, _, _ := captureCmd(func(_ []string) int {
		printCheckResult(doctor.CheckResult{
			Name:    "writable",
			Status:  doctor.StatusWarn,
			Message: "check details here",
		})
		return 0
	}, nil)
	if !strings.Contains(stdout, "check details here") {
		t.Errorf("expected message in output, got: %s", stdout)
	}
}

func TestOverallDescription(t *testing.T) {
	tests := []struct {
		status doctor.Status
		want   string
	}{
		{doctor.StatusOK, "PASS"},
		{doctor.StatusWarn, "PASS with warnings"},
		{doctor.StatusFail, "FAIL"},
		{doctor.Status("other"), "UNKNOWN"},
	}
	for _, tc := range tests {
		got := overallDescription(tc.status)
		if got != tc.want {
			t.Errorf("overallDescription(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
