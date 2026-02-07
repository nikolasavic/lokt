package identity

import (
	"os"
	"os/user"
	"regexp"
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
	if id.AgentID == "" {
		t.Error("AgentID should not be empty")
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

func TestGetAgentID_EnvOverride(t *testing.T) {
	t.Setenv(EnvLoktAgentID, "builder-1")

	id := getAgentID()
	if id != "builder-1" {
		t.Errorf("AgentID = %q, want %q", id, "builder-1")
	}
}

func TestGetAgentID_EmptyEnvFallsToAutoGen(t *testing.T) {
	t.Setenv(EnvLoktAgentID, "")

	id := getAgentID()
	matched, err := regexp.MatchString(`^agent-[0-9a-f]{4}$`, id)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("AgentID = %q, want pattern agent-XXXX", id)
	}
}

func TestGenerateAgentID_Format(t *testing.T) {
	id := generateAgentID()
	matched, err := regexp.MatchString(`^agent-[0-9a-f]{4}$`, id)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("generateAgentID() = %q, want pattern agent-XXXX", id)
	}
}

func TestGenerateAgentID_Deterministic(t *testing.T) {
	// Within the same process, generateAgentID should return the same value.
	a := generateAgentID()
	b := generateAgentID()
	if a != b {
		t.Errorf("generateAgentID() not deterministic: %q != %q", a, b)
	}
}

func TestCurrent_AgentIDFromEnv(t *testing.T) {
	t.Setenv(EnvLoktAgentID, "deploy-agent")

	id := Current()
	if id.AgentID != "deploy-agent" {
		t.Errorf("AgentID = %q, want %q", id.AgentID, "deploy-agent")
	}
}

func TestCurrent_AgentIDAutoGen(t *testing.T) {
	t.Setenv(EnvLoktAgentID, "")

	id := Current()
	matched, err := regexp.MatchString(`^agent-[0-9a-f]{4}$`, id.AgentID)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("AgentID = %q, want pattern agent-XXXX", id.AgentID)
	}
}
