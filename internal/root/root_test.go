package root

import (
	"os"
	"testing"
)

func TestDiscoveryMethodString(t *testing.T) {
	tests := []struct {
		method DiscoveryMethod
		want   string
	}{
		{MethodEnvVar, "env"},
		{MethodGit, "git"},
		{MethodLocalDir, "local"},
		{DiscoveryMethod(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.method.String(); got != tt.want {
			t.Errorf("DiscoveryMethod(%d).String() = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestFindWithMethod_EnvVar(t *testing.T) {
	// Set LOKT_ROOT and verify it's used
	testPath := "/tmp/test-lokt-root"
	if err := os.Setenv(EnvLoktRoot, testPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv(EnvLoktRoot) }()

	path, method, err := FindWithMethod()
	if err != nil {
		t.Fatalf("FindWithMethod() error = %v", err)
	}
	if path != testPath {
		t.Errorf("FindWithMethod() path = %q, want %q", path, testPath)
	}
	if method != MethodEnvVar {
		t.Errorf("FindWithMethod() method = %v, want MethodEnvVar", method)
	}
}

func TestFind_Unchanged(t *testing.T) {
	// Verify Find() still works (backwards compatibility)
	testPath := "/tmp/test-lokt-root"
	if err := os.Setenv(EnvLoktRoot, testPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv(EnvLoktRoot) }()

	path, err := Find()
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if path != testPath {
		t.Errorf("Find() = %q, want %q", path, testPath)
	}
}
