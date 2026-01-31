package identity

import (
	"os"
	"os/user"
	"testing"
)

func TestCurrent_ReturnsNonEmpty(t *testing.T) {
	id := Current()

	if id.Owner == "" {
		t.Error("Owner should not be empty")
	}
	if id.Host == "" {
		t.Error("Host should not be empty")
	}
	if id.PID == 0 {
		t.Error("PID should not be 0")
	}
	if id.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", id.PID, os.Getpid())
	}
}

func TestGetOwner_EnvOverride(t *testing.T) {
	t.Setenv(EnvLoktOwner, "test-agent-42")

	owner := getOwner()
	if owner != "test-agent-42" {
		t.Errorf("Owner = %q, want %q", owner, "test-agent-42")
	}
}

func TestGetOwner_FallsBackToUsername(t *testing.T) {
	t.Setenv(EnvLoktOwner, "")

	owner := getOwner()

	u, err := user.Current()
	if err != nil {
		t.Skipf("Cannot get current user: %v", err)
	}
	if owner != u.Username {
		t.Errorf("Owner = %q, want OS username %q", owner, u.Username)
	}
}

func TestGetHost_ReturnsHostname(t *testing.T) {
	host := getHost()

	expected, err := os.Hostname()
	if err != nil {
		t.Skipf("Cannot get hostname: %v", err)
	}
	if host != expected {
		t.Errorf("Host = %q, want %q", host, expected)
	}
}

func TestCurrent_UsesEnvOwner(t *testing.T) {
	t.Setenv(EnvLoktOwner, "agent-from-env")

	id := Current()
	if id.Owner != "agent-from-env" {
		t.Errorf("Owner = %q, want %q", id.Owner, "agent-from-env")
	}
}
