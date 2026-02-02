# L-141: Add --timeout flag for wait

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

With `--wait` added in L-140, users can wait indefinitely for a lock. However, there's no way to specify a maximum wait time, which can lead to processes hanging forever if the lock holder never releases.

Users need a way to specify a timeout after which the wait aborts with a clear exit code.

## Users

- **CI Pipelines**: Need bounded wait times to prevent job timeouts
- **AI Agents**: Need predictable failure after reasonable wait
- **Shell Scripts**: Need to handle "gave up waiting" differently from "lock acquired"

## Requirements

1. Add `--timeout` duration flag to `lokt lock` command
2. When used with `--wait`, abort after timeout with clear exit code
3. Use exit code 2 (ExitLockHeld) on timeout - consistent with immediate deny
4. Print holder info on timeout (same as immediate deny)
5. `--timeout` without `--wait` is an error (or implies `--wait`)

## Non-Goals

- Backoff/jitter: Covered by L-142
- Progress output: Covered by L-143

## Acceptance Criteria

- [ ] **Timeout flag**: `lokt lock --wait --timeout 5s foo` waits up to 5 seconds
- [ ] **Exit code on timeout**: When timeout expires, exit code is 2 (ExitLockHeld)
- [ ] **Holder info on timeout**: Print who holds the lock when timing out
- [ ] **Success before timeout**: If lock acquired before timeout, exit code is 0
- [ ] **Usage error**: `--timeout` without `--wait` prints usage error (or implies --wait)

## Edge Cases

- Timeout of 0 — should be immediate (equivalent to no --wait)
- Negative timeout — reject with usage error
- Lock released just as timeout expires — accept if acquired

## Constraints

- Reuse existing AcquireWithWait with context.WithTimeout
- Preserve all L-140 behavior when --timeout not specified

---

## Implementation Notes

### Key Files
- `cmd/lokt/main.go` - Add --timeout flag, use context.WithTimeout
- No changes needed to internal/lock (context already supported)

### Approach
Use `context.WithTimeout` wrapping the signal context when --timeout is specified. The existing `AcquireWithWait` already handles context cancellation correctly.

---

**Next:** Run `/kickoff L-141` to promote to Beads execution layer.
