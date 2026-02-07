// Package identity provides agent identity for lock ownership.
package identity

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/user"
	"sync"

	"github.com/nikolasavic/lokt/internal/stale"
)

const EnvLoktOwner = "LOKT_OWNER"

// EnvLoktAgentID overrides the auto-generated agent identifier.
// When set, its value is used as-is. When empty or unset, an ID is
// auto-generated from the process PID and start time.
const EnvLoktAgentID = "LOKT_AGENT_ID"

// Identity represents the identity of a lock holder.
type Identity struct {
	Owner   string
	Host    string
	PID     int
	AgentID string
}

// Current returns the identity of the current process.
func Current() Identity {
	return Identity{
		Owner:   getOwner(),
		Host:    getHost(),
		PID:     os.Getpid(),
		AgentID: getAgentID(),
	}
}

func getOwner() string {
	if owner := os.Getenv(EnvLoktOwner); owner != "" {
		return owner
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

func getHost() string {
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return "unknown"
}

var (
	autoAgentID     string
	autoAgentIDOnce sync.Once
)

func getAgentID() string {
	if id := os.Getenv(EnvLoktAgentID); id != "" {
		return id
	}
	autoAgentIDOnce.Do(func() {
		autoAgentID = generateAgentID()
	})
	return autoAgentID
}

// generateAgentID produces a short, deterministic ID from the current
// process's PID and start time. Format: "agent-XXXX" (4 hex digits).
func generateAgentID() string {
	pid := os.Getpid()
	startNS, err := stale.GetProcessStartTime(pid)
	// If start time unavailable (Windows, etc.), use PID alone.
	// Less collision-resistant but still functional.
	input := fmt.Sprintf("%d-%d", pid, startNS)
	if err != nil {
		input = fmt.Sprintf("%d", pid)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(input))
	return fmt.Sprintf("agent-%04x", h.Sum32()&0xFFFF)
}
