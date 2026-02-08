// Package audit provides append-only audit logging for lock operations.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Event types for audit log entries.
const (
	EventAcquire       = "acquire"        // Lock successfully acquired
	EventDeny          = "deny"           // Lock acquisition denied (held by another)
	EventRelease       = "release"        // Lock released normally
	EventForceBreak    = "force-break"    // Lock removed via --force
	EventStaleBreak    = "stale-break"    // Lock removed via --break-stale
	EventAutoPrune     = "auto-prune"     // Lock auto-removed (dead PID on same host)
	EventCorruptBreak  = "corrupt-break"  // Lock removed (corrupted/malformed file)
	EventRenew         = "renew"          // Lock TTL renewed (heartbeat)
	EventFreeze        = "freeze"         // Freeze switch activated
	EventUnfreeze      = "unfreeze"       // Freeze switch deactivated
	EventForceUnfreeze = "force-unfreeze" // Freeze removed via --force
	EventFreezeDeny    = "freeze-deny"    // Guard blocked by active freeze
)

// Event represents a single audit log entry.
// Each event is serialized as one JSON line in the audit log.
type Event struct {
	Timestamp time.Time      `json:"ts"`
	Event     string         `json:"event"`
	Name      string         `json:"name"`
	LockID    string         `json:"lock_id,omitempty"`
	Owner     string         `json:"owner"`
	Host      string         `json:"host"`
	PID       int            `json:"pid"`
	AgentID   string         `json:"agent_id,omitempty"`
	TTLSec    int            `json:"ttl_sec,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

const auditFileName = "audit.log"

// Injectable function for testability.
var openFileFn = os.OpenFile

// Writer appends audit events to a JSONL file.
// All writes are non-blocking: errors are logged to stderr, never returned.
type Writer struct {
	rootDir string
}

// NewWriter creates a Writer that will append to <rootDir>/audit.log.
func NewWriter(rootDir string) *Writer {
	return &Writer{rootDir: rootDir}
}

// Emit appends an event to the audit log.
// This method never returns an error. If writing fails, the error is logged to stderr.
// This ensures lock operations are never blocked by audit failures.
func (w *Writer) Emit(e *Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	data, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lokt: audit marshal error: %v\n", err)
		return
	}
	data = append(data, '\n')

	path := filepath.Join(w.rootDir, auditFileName)

	// O_APPEND is atomic on POSIX for writes smaller than PIPE_BUF (typically 4096 bytes).
	// Our events are well under this limit.
	f, err := openFileFn(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // G304: path is controlled
	if err != nil {
		fmt.Fprintf(os.Stderr, "lokt: audit open error: %v\n", err)
		return
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "lokt: audit write error: %v\n", err)
		return
	}

	if err := f.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "lokt: audit sync error: %v\n", err)
	}
}
