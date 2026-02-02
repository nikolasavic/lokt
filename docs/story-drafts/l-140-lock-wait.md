# L-140: Add --wait flag to lock command

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

When multiple processes compete for the same lock, the `lokt lock` command fails immediately if the lock is held. This forces callers to implement their own retry logic, leading to inconsistent polling intervals and no standard timeout handling.

AI agents and CI pipelines need a built-in way to wait for locks to become available rather than failing immediately.

## Users

- **AI Agents**: Need to wait for shared resources (e.g., git worktrees) without complex external retry wrappers
- **CI Pipelines**: Need coordinated access to shared resources with predictable wait behavior
- **Shell Scripts**: Need simple "wait until free" semantics without implementing polling loops

## Requirements

1. Add `--wait` flag to `lokt lock` command that polls until lock is acquired
2. Use sensible default polling interval (e.g., 100ms) for initial implementation
3. Return same exit codes as non-wait mode (0 on success, 2 if still held after wait)
4. Respect signals (SIGINT/SIGTERM) during wait - exit cleanly
5. Print holder info on final failure (same as current behavior)

## Non-Goals

- Timeout duration flag (`--timeout`): Covered by L-141
- Backoff/jitter: Covered by L-142
- Progress output (`--quiet` / periodic updates): Covered by L-143
- Freeze switch integration: Separate epic (L-160+)

## Acceptance Criteria

- [ ] **Basic wait**: Given lock "foo" is held, when `lokt lock --wait foo` runs, then it polls until lock is released and acquires it
- [ ] **Indefinite wait**: Given `--wait` with no timeout, when lock is never released, then command waits indefinitely (until signal)
- [ ] **Signal handling**: Given `lokt lock --wait foo` is waiting, when SIGINT received, then command exits cleanly with non-zero code
- [ ] **Exit code on success**: Given lock becomes free during wait, when acquired, then exit code is 0
- [ ] **TTL preserved**: Given `--wait` combined with `--ttl 5m`, when lock acquired, then TTL is set correctly
- [ ] **Stale lock handling**: Given lock is stale (expired TTL or dead PID), when `--wait` polling, then acquire succeeds by breaking stale lock

## Edge Cases

- Lock released between check and acquire (race) — retry immediately, should succeed on next attempt
- Lock file deleted externally during wait — acquire should succeed on next poll
- Very fast lock churn (held/released rapidly) — polling should eventually succeed
- Lock held by same owner — current behavior returns success (re-entrant), preserve this

## Constraints

- Polling interval hardcoded for L-140 (100ms suggested) — will be configurable in L-142
- No progress output in L-140 — L-143 adds this
- Must not break existing non-wait behavior

---

## Implementation Notes

### Key Files
- `cmd/lokt/main.go:91-126` - cmdLock function, add `--wait` flag
- `internal/lock/acquire.go` - Consider adding `AcquireWithWait` or handling in caller

### Approach Options

**Option A: Loop in cmdLock**
- Add wait logic directly in `cmdLock` function
- Simple, keeps acquire.go unchanged
- Risk: signal handling complexity in main.go

**Option B: New AcquireWithWait function**
- Add `AcquireWithWait(ctx, rootDir, name, opts, pollInterval)` to lock package
- Cleaner separation, reusable for guard command later
- Context-based cancellation for signal handling

**Recommended**: Option B - cleaner architecture, easier to extend for L-141/L-142

### Stale Lock Handling
During wait, if lock is stale (expired TTL or dead PID on same host), should auto-break and acquire. This provides better UX than waiting forever for a dead process.

---

**Next:** Run `/kickoff L-140` to promote to Beads execution layer.
