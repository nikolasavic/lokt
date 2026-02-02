# L-130: TTL & Stale Lock Handling

## Problem Statement

Locks can become stale when processes crash without releasing them. Without proper TTL handling, these orphaned locks block other processes indefinitely, requiring manual intervention (`--force`) which bypasses safety checks.

The current implementation has basic TTL support:
- `Lock.TTLSec` field exists
- `Lock.IsExpired()` method works
- `--ttl` flag accepted in `lock` and `guard` commands
- Status shows `[EXPIRED]` marker

What's missing:
1. A safe way to break expired locks without `--force` (break-glass)
2. PID liveness detection to identify truly stale locks
3. Automatic cleanup of expired locks during status

## Requirements

### R1: Break-Stale Flag for Unlock
- Add `unlock --break-stale` that removes expired locks only
- Refuse to break non-expired locks (unlike `--force`)
- Error message explains why if lock not expired

### R2: PID Liveness Check (Unix)
- On Unix, use `kill(pid, 0)` to check if PID is alive
- If PID dead AND same host, lock is considered stale
- Stale locks can be broken with `--break-stale` even if not TTL-expired
- Display liveness status in `status` output

### R3: Auto-Prune Expired in Status
- Add `status --prune-expired` flag
- Removes expired locks while listing
- Reports which locks were pruned

### R4: Duration Validation
- Validate TTL durations are positive
- Reject zero or negative durations with clear error

## Acceptance Criteria

- [ ] `unlock --break-stale` removes expired locks
- [ ] `unlock --break-stale` refuses to break non-expired locks (exit code 1)
- [ ] `unlock --break-stale` removes locks where PID is dead (same host only)
- [ ] `status` shows PID liveness (alive/dead/unknown) on same host
- [ ] `status --prune-expired` removes expired locks and reports them
- [ ] `--ttl 0` or negative durations produce clear error
- [ ] All new functionality has unit tests

## Edge Cases

1. **Cross-host locks**: Cannot verify PID on remote host - treat as "unknown"
2. **PID reuse**: Rare but possible - TTL provides backup protection
3. **Race conditions**: Lock disappears between read and remove - handle gracefully
4. **Permission errors**: Cannot signal PID due to permissions - treat as "alive" (safe default)

## Constraints

- No external dependencies for PID liveness (use syscall)
- Must work on macOS and Linux
- Windows fallback: skip PID check, rely on TTL only
- Backward compatible with existing lock files

## Verification

- Level: agent
- Environments: local
- Type: unit tests + CLI integration tests

## Technical Notes

Existing code to build on:
- `internal/lockfile/lockfile.go:23` - `IsExpired()` method
- `internal/lock/release.go:43` - `Release()` function
- `cmd/lokt/main.go:117` - `cmdUnlock()` with `--force` flag

PID liveness on Unix:
```go
import "syscall"

func isProcessAlive(pid int) bool {
    err := syscall.Kill(pid, 0)
    return err == nil || err == syscall.EPERM
}
```
