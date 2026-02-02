# L-173: audit --since

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: local

---

## Problem

Operators need to query audit history to debug lock issues. The audit.log file exists but there's no CLI to read it. Need basic filtering by time to find relevant events.

## Users

- **Operators**: Need to see recent lock activity (e.g., "what happened in the last hour?")
- **Developers**: Need to verify audit events during testing

## Requirements

1. Add `lokt audit` command with `--since` flag
2. Support duration format: `--since 1h`, `--since 30m`, `--since 24h`
3. Support ISO 8601 timestamp: `--since 2026-01-27T10:00:00Z`
4. Optional `--name` filter for specific lock
5. Output events as JSONL (one per line) to stdout

## Non-Goals

- `--tail` follow mode (L-172)
- Pretty-printed output (keep it machine-readable)
- Log rotation or cleanup

## Acceptance Criteria

- [ ] `lokt audit --since 1h` shows events from last hour
- [ ] `lokt audit --since 2026-01-27T10:00:00Z` shows events after timestamp
- [ ] `--name build` filters to only "build" lock events
- [ ] Output is valid JSONL
- [ ] Missing audit.log handled gracefully (empty output, exit 0)
- [ ] Invalid --since format shows helpful error

## Edge Cases

- Audit log doesn't exist — empty output, exit 0
- Empty audit log — empty output, exit 0
- Malformed lines in audit log — skip and continue
- No events match filter — empty output, exit 0
- --since in future — empty output (no events)

## Constraints

- Keep CLI consistent with existing commands (same flag patterns)
- Parse duration using Go's time.ParseDuration
- Parse timestamp using time.Parse with RFC3339

---

## Notes

Implementation approach:
1. Add `cmdAudit` to main.go
2. Parse --since as duration first, fallback to RFC3339 timestamp
3. Read audit.log line by line, parse JSON, filter by timestamp and optional name
4. Output matching events to stdout

---

**Next:** Run `/kickoff L-173` to promote to Beads execution layer.
