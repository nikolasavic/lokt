# L-201: Auto-Prune Dead Locks Before Acquire

## Problem Statement

When a process crashes while holding a lock, the lock file remains on disk. Currently, subsequent attempts to acquire the same lock fail with `HeldError`, even though the original holder is dead. Users must manually run `unlock --break-stale` or use `lock --wait` (which has auto-prune logic) to recover.

This creates friction for workflows like CI/CD or multi-agent systems where crashes can leave orphaned locks. The system should be self-healing: if we can definitively prove a lock holder is dead (same host + dead PID), we should silently clean up and proceed.

## Requirements

1. **Auto-prune on immediate acquire**: When `Acquire()` encounters an existing lock, check if it's stale (dead PID on same host). If so, remove it silently and retry acquisition.

2. **Same-host constraint**: Only auto-prune locks where we can verify PID liveness (i.e., lock.Host == current hostname). Cross-host locks remain untouched since we cannot verify remote PIDs.

3. **Audit logging**: Emit a distinct audit event (`auto-prune`) when a lock is automatically removed, including the previous holder's identity for debugging.

4. **Silent operation**: No user-visible output for auto-prune. The acquire should appear to succeed normally.

5. **Single retry**: After removing a stale lock, retry acquisition once. If another process grabs it first (race), return HeldError as normal.

## Acceptance Criteria

- [ ] `lokt lock <name>` succeeds when existing lock has dead PID on same host
- [ ] `lokt guard <name> -- cmd` auto-prunes before acquiring
- [ ] Cross-host locks are never auto-pruned (even if TTL expired)
- [ ] TTL-expired locks on same host with live PID are NOT auto-pruned (TTL alone insufficient)
- [ ] Audit log contains `auto-prune` event with previous holder info
- [ ] No user-visible output during auto-prune (silent)
- [ ] Race condition: if another process acquires between prune and retry, return HeldError

## Edge Cases

1. **Lock holder still alive**: Must not prune. Only dead PIDs qualify.
2. **Different host**: Cannot verify PID liveness remotely. Do not prune.
3. **Corrupted lock file**: Let L-203 handle this separately. For now, parsing errors should not trigger prune.
4. **Race during prune**: Process A prunes, Process B acquires, Process A retries and gets HeldError. This is correct behavior.
5. **File removal fails**: Log warning, return original HeldError.

## Constraints

- No new CLI flags (behavior is automatic)
- Must not change exit codes or error messages for non-stale locks
- Audit event must be distinguishable from manual `stale-break`

## Implementation Notes

### Key Files
- `internal/lock/acquire.go:73-83` - Add stale check before returning HeldError
- `internal/audit/audit.go:12-20` - Add `EventAutoPrune` constant
- `internal/lock/acquire_test.go` - Add test cases

### Pattern to Follow
Mirror the existing `tryBreakStale()` function in `acquire.go:169-188`, but integrate into the immediate `Acquire()` path rather than the wait loop.

### Reusable Components
- `stale.Check()` - Determine staleness
- `stale.IsProcessAlive()` - PID liveness (already used)
- Existing audit emission pattern from `release.go:190-203`

## Verification

- Level: agent
- Environments: local

### Agent-Verifiable
- Unit tests pass for new auto-prune behavior
- Audit log contains correct event type and holder info
- Existing tests continue to pass (no regression)

### Manual Verification (optional)
- Create lock, kill process, verify new acquire succeeds
