# L-171: Emit Audit Events

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: local

---

## Problem

The audit package (L-170) provides schema and writer infrastructure, but no events are actually emitted. Lock operations happen silently with no trail. Operators need visibility into acquire/deny/release events for debugging and forensics.

## Users

- **Operators**: Need audit trail to debug contention and understand lock history
- **Developers**: Need to verify audit events are emitted correctly during testing

## Requirements

1. Instrument `lock.Acquire()` to emit `acquire` event on success, `deny` event on HeldError
2. Instrument `lock.Release()` to emit `release`, `force-break`, or `stale-break` based on options
3. Pass audit.Writer through options or create package-level writer
4. Include holder info in `extra` field for deny events

## Non-Goals

- CLI commands for reading audit log (L-172, L-173)
- Emitting events from CLI layer (keep it in internal/lock)
- Audit for AcquireWithWait intermediate retries (only final outcome)

## Acceptance Criteria

- [ ] **Acquire success**: Emits `acquire` event with lock details
- [ ] **Acquire denied**: Emits `deny` event with holder info in extra
- [ ] **Release normal**: Emits `release` event
- [ ] **Release force**: Emits `force-break` event
- [ ] **Release stale**: Emits `stale-break` event
- [ ] **Non-blocking**: Audit failures don't affect lock operation success/failure
- [ ] **Tests updated**: Verify events are emitted in expected scenarios

## Edge Cases

- Audit writer is nil — skip emit, don't panic
- Lock validation fails before acquire — no event (not a lock state change)
- AcquireWithWait succeeds after retries — single acquire event at end

## Constraints

- Don't change function signatures in breaking way (add to Options structs)
- Events should include all identity info (owner, host, pid)
- Reuse existing identity.Current() pattern

---

## Notes

Design choice: Add `Auditor *audit.Writer` field to AcquireOptions and ReleaseOptions. This keeps audit optional and doesn't require global state.

Event emission points:
- `Acquire()`: after successful write (acquire), or when returning HeldError (deny)
- `Release()`: after successful remove, with event type based on opts.Force/opts.BreakStale

---

**Next:** Run `/kickoff L-171` to promote to Beads execution layer.
