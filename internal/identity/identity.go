// Package identity provides agent identity for lock ownership.
package identity

import (
	"os"
	"os/user"
)

const EnvLoktOwner = "LOKT_OWNER"

// Identity represents the identity of a lock holder.
type Identity struct {
	Owner string
	Host  string
	PID   int
}

// Current returns the identity of the current process.
func Current() Identity {
	return Identity{
		Owner: getOwner(),
		Host:  getHost(),
		PID:   os.Getpid(),
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
