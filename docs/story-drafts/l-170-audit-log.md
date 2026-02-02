# L-170: Audit JSONL Schema v1

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: local

---

## Problem

Lokt has no observability. When coordination issues occur (contention, force breaks, stale locks), operators have no forensic trail to understand what happened. Before we can emit or query audit events, we need a stable schema and write infrastructure.

## Users

- **Operators**: Need to debug lock contention and understand who held what when
- **Developers**: Need to verify lock behavior during development

## Requirements

1. Define audit event schema with all necessary fields for forensics
2. Create `internal/audit` package with Event type and Writer
3. Writer appends JSONL to `<root>/audit.log` with atomic append + fsync
4. Writer is non-blocking (failures logged to stderr, don't break caller)

## Non-Goals

- Emitting events from lock operations (L-171)
- CLI commands for reading audit log (L-172, L-173)
- Log rotation or size management
- Network-based audit sinks

## Acceptance Criteria

- [ ] **Schema defined**: Event struct with ts, event, name, owner, host, pid, ttl_sec, extra fields
- [ ] **Event types**: Constants for acquire, deny, release, force-break, stale-break
- [ ] **Writer created**: audit.Writer with Emit(event) method
- [ ] **Atomic append**: Uses O_APPEND + fsync for concurrent safety
- [ ] **Non-blocking**: Writer.Emit never returns error to caller; logs failures to stderr
- [ ] **Unit tests**: Cover schema serialization and file append behavior

## Edge Cases

- Audit file doesn't exist — create on first write
- Audit directory doesn't exist — use root.EnsureDirs pattern
- Concurrent writers — O_APPEND is atomic on POSIX for small writes
- Disk full — log warning, don't block lock operations

## Constraints

- Mirror existing package patterns (see `internal/lockfile`)
- JSON field names match backlog spec: `ts`, `event`, `name`, `owner`, `host`, `pid`, `ttl_sec`, `extra`
- Event timestamp uses RFC3339 format (consistent with lockfile)

---

## Notes

Event types to support (from backlog L-170-173):
- `acquire` — lock successfully acquired
- `deny` — lock acquisition denied (held by another)
- `release` — lock released normally
- `force-break` — lock removed via --force
- `stale-break` — lock removed via --break-stale

The `extra` field is a map for event-specific data (e.g., holder info on deny).

---

**Next:** Run `/kickoff L-170` to promote to Beads execution layer.
