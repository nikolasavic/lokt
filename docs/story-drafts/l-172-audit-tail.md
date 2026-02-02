# L-172: audit --tail

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: local

---

## Problem

Operators debugging live lock contention need real-time visibility into audit events. Currently, they must repeatedly run `lokt audit --since 1m` to check for new events. A tail-follow mode would allow continuous monitoring without polling.

## Users

- **Operators**: Need to watch lock activity in real-time during debugging or incident response
- **Developers**: Need to observe lock behavior while testing concurrent processes

## Requirements

1. Add `--tail` flag to `lokt audit` command
2. When `--tail` is specified, continuously follow the audit log for new events
3. Support optional `--name` filter in tail mode (same as batch mode)
4. Handle graceful shutdown on SIGINT/SIGTERM
5. Handle file truncation/rotation gracefully (re-open on EOF if file shrinks)

## Non-Goals

- Log rotation management (out of scope for audit command)
- Pretty-printed output (keep JSONL for machine consumption)
- Multiple simultaneous log files
- `--since` combined with `--tail` (tail always starts from current position)

## Acceptance Criteria

- [ ] **Tail mode**: Given `--tail` flag, when new events are appended to audit.log, then they are printed to stdout immediately
- [ ] **Name filter in tail**: Given `--tail --name build`, when events are appended, then only "build" lock events are printed
- [ ] **Graceful shutdown**: Given tail mode running, when SIGINT received, then process exits cleanly with exit 0
- [ ] **Missing file**: Given `--tail` when audit.log doesn't exist, then wait for file creation and start tailing
- [ ] **File rotation**: Given tail mode, when audit.log is truncated, then continue reading from beginning of new content
- [ ] **Usage exclusive**: Given `--since` and `--tail` together, then show error (mutually exclusive)

## Edge Cases

- Audit log doesn't exist yet — wait for creation, then start tailing
- Audit log is truncated (rotated) — detect via file size decrease, seek to beginning
- Audit log is deleted — handle gracefully, wait for recreation
- Very fast writes — buffer appropriately, don't miss events
- Malformed JSON line — skip and continue (same as batch mode)

## Constraints

- Mirror signal handling patterns in guard command (`signal.NotifyContext`)
- Keep CLI consistent with existing flags
- Poll interval should be reasonable (100-500ms) to balance responsiveness and CPU
- Use existing `auditEvent` struct for parsing

---

## Notes

Implementation approach:
1. Add `--tail` flag to `cmdAudit` flag set
2. If `--tail` and `--since` both specified, error and exit
3. In tail mode:
   - Open or wait for audit.log
   - Seek to end (start from current position)
   - Use polling loop with file stat to detect new content
   - On new content, read and print matching lines
   - On SIGINT/SIGTERM, exit cleanly
4. Handle file rotation by comparing inode or detecting size decrease

Tail implementation options:
- Simple: Poll with `os.Stat` + `Seek` (portable, simple)
- Advanced: `fsnotify` for filesystem events (more responsive, adds dependency)

Recommend simple polling approach to avoid new dependencies.

---

**Next:** Run `/kickoff L-172` to promote to Beads execution layer.
