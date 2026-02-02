# lokt-skc: Idempotent Reentrant Acquire (Owner-Matched Lock Refresh)

## Problem

When `lokt lock` is called and the lock is already held by the **same owner**, it fails with exit 2 (`HeldError`). This forces callers to implement a TOCTOU-prone check-then-acquire pattern:

```bash
# Current: ~33 lines of fragile hook script
if ! lokt status build --json 2>/dev/null | jq -e '.owner == "agent-1"' >/dev/null 2>&1; then
  lokt lock --ttl 5m build || { echo "held"; exit 1; }
fi
```

With reentrant acquire, this collapses to:

```bash
# After: ~8 lines, race-free
lokt lock --ttl 5m build || { echo "held"; exit 1; }
```

## Requirements

1. **Owner match = refresh**: When `lokt lock` is called and the existing lock has the same `LOKT_OWNER`, overwrite the lock file with fresh timestamp + new TTL and exit 0.
2. **Owner mismatch + active = deny**: When the existing lock has a different owner and is not expired/stale, return exit 2 (unchanged behavior).
3. **Owner mismatch + stale = evict**: When the existing lock has a different owner but is expired or dead-PID, break and acquire (unchanged behavior via auto-prune).
4. **Audit trail**: Emit a `"renew"` audit event (reuse existing `EventRenew`) when an owner-matched refresh occurs, distinct from `"acquire"`.
5. **PID/host update**: The refreshed lock file updates PID, host, and pid_start_ns to the current process identity (the owner may be the same LOKT_OWNER value but a different process).
6. **No new CLI flags**: This is the default behavior — no `--reentrant` flag needed.

## Acceptance Criteria

- [ ] AC1: `lokt lock foo` succeeds (exit 0) when `foo` is already held by the same `LOKT_OWNER`
- [ ] AC2: The refreshed lock file has updated `acquired_ts`, `pid`, `host`, `pid_start_ns`, and optionally new `ttl_sec`
- [ ] AC3: `lokt lock foo` still returns exit 2 when `foo` is held by a different owner
- [ ] AC4: A `"renew"` audit event is emitted for owner-matched refresh
- [ ] AC5: Existing tests continue to pass (no regressions in contention, race, auto-prune tests)
- [ ] AC6: `lokt lock --wait foo` also benefits from reentrant acquire (immediate success on owner match)

## Edge Cases

- **Same owner, different PID**: The lock was acquired by `LOKT_OWNER=agent-1` PID 100, now PID 200 calls `lokt lock` with `LOKT_OWNER=agent-1`. Should refresh (owner is the identity that matters, not PID).
- **Same owner, different host**: Lock held by `agent-1@host-A`, now `agent-1@host-B` calls. This is still same-owner — refresh.
- **Expired lock, same owner**: Should refresh (not evict-and-reacquire). Simpler path.
- **Corrupted lock file**: Existing corrupted-lock handling takes priority (remove + acquire fresh).
- **Concurrent refresh race**: Two processes with same owner try to refresh simultaneously. Both use `lockfile.Write()` which is atomic (temp+rename). Last writer wins, both get exit 0. This is safe because both are the same owner.

## Constraints

- Owner matching is by `LOKT_OWNER` string only (not PID or host). This matches the semantics: "the same agent identity can re-acquire its own lock."
- Must not break the `O_CREATE|O_EXCL` fast path for uncontested locks.
- The reentrant path must use `lockfile.Write()` for atomic overwrite, not raw file writes.

## Verification

- Level: required
- Environments: local
- Agent-verifiable: unit tests, integration test via CLI
