package main

import (
	"os"
	"strings"
	"testing"
)

func TestCmdDemo_WritesScript(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	stdout, _, code := captureCmd(cmdDemo, nil)
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}

	if !strings.Contains(stdout, "Wrote lokt-hexwall-demo.sh") {
		t.Errorf("expected stdout to mention filename, got: %s", stdout)
	}

	// Verify file exists and is readable.
	data, err := os.ReadFile("lokt-hexwall-demo.sh")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(data)

	// Verify shebang.
	if !strings.HasPrefix(content, "#!/usr/bin/env bash") {
		t.Error("script should start with #!/usr/bin/env bash")
	}

	// Verify executable permissions.
	info, err := os.Stat("lokt-hexwall-demo.sh")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("script should be executable")
	}

	// Verify key sections exist in the script.
	sections := []string{
		"Configuration",
		"Preflight",
		"critical.sh",
		"Cleanup trap",
		"Spawn workers",
		"lokt guard",
		"--no-lock",
		"tail -f",
	}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("script should contain %q", s)
		}
	}
}

func TestCmdDemo_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// Create existing file with different content.
	if err := os.WriteFile("lokt-hexwall-demo.sh", []byte("old content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, code := captureCmd(cmdDemo, nil)
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}

	data, err := os.ReadFile("lokt-hexwall-demo.sh")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) == "old content" {
		t.Error("script should have been overwritten")
	}
	if !strings.HasPrefix(string(data), "#!/usr/bin/env bash") {
		t.Error("script should start with shebang after overwrite")
	}
}

func TestCmdDemo_ScriptStructure(t *testing.T) {
	// Verify the script constant directly without writing to disk.
	if !strings.HasPrefix(hexwallScript, "#!/usr/bin/env bash") {
		t.Error("hexwallScript should start with shebang")
	}
	if !strings.HasSuffix(hexwallScript, "\n") {
		t.Error("hexwallScript should end with newline")
	}

	// Worker function and critical section must be present.
	required := []string{
		"worker()",
		"CRITICAL_EOF",
		"STATE_DIR=",
		"WORKER_PIDS=",
		"trap cleanup EXIT",
		"lokt guard",
		"sleep 0.001",
		"hexwall:",
	}
	for _, s := range required {
		if !strings.Contains(hexwallScript, s) {
			t.Errorf("hexwallScript should contain %q", s)
		}
	}
}
