# L-200: Heartbeat/lease renewal for guards

status: draft
created: 2026-01-28
backlog-ref: docs/backlog.md

## Verification
- Level: required
- Environments: sandbox

---

## Problem

When a long-running command is executed under `lokt guard --ttl`, the lock can expire before the command completes. This causes serious issues:

1. **Lock expires mid-execution**: A build taking 10 minutes with a 5-minute TTL loses its protection halfway through
2. **Another process can steal the lock**: Once expired, a second `guard` or `lock` can claim it, leading to concurrent access to the protected resource
3. **No warning to the user**: The original guard command doesn't know its lock expired

Currently, users must guess the maximum runtime and set a very large TTL (or omit TTL entirely), but this defeats the purpose of TTL for detecting dead/crashed processes.

## Users

- **CI/Build operators**: Need long builds (10-30 min) protected by locks without setting unreasonably high TTLs
- **Developers**: Running local builds or migrations under guard with standard TTLs (5m default)
- **Multi-agent systems**: AI agents coordinating work via locks, where tasks may take variable time

## Requirements

1. Guard command must automatically renew its lock's TTL at regular intervals while the child process runs
2. Renewal interval should be a fraction of the TTL (e.g., renew when 50% of TTL elapsed, or at TTL/2 intervals)
3. Renewal must be atomic - if the lock was stolen (e.g., force-unlocked), renewal should fail cleanly
4. If renewal fails, guard should log a warning but continue running the child (the child may still succeed)
5. Heartbeat should emit audit events for observability

## Non-Goals

- **Client-side TTL extension**: Not adding `lokt renew <name>` command (this is internal to guard)
- **Distributed lock coordination**: This is single-host lock renewal, not distributed consensus
- **Changing default TTL behavior**: Locks without TTL still work as before (infinite, no renewal needed)

## Acceptance Criteria

- [ ] **Heartbeat interval calculation**: Given a guard with `--ttl 5m`, when the guard starts, then the heartbeat interval is set to 2m30s (half the TTL)
- [ ] **Lock renewal updates timestamp**: Given a guard holding a lock, when the heartbeat fires, then the lock file's `acquired_ts` is updated to current time (resetting the TTL countdown)
- [ ] **Renewal is atomic**: Given a guard running, when the lock file is deleted externally, then the next renewal attempt fails gracefully without crash
- [ ] **Stolen lock detection**: Given a guard running, when another process force-breaks and re-acquires the lock, then the renewal detects owner mismatch and logs a warning
- [ ] **Child completion stops heartbeat**: Given a guard running, when the child process exits, then the heartbeat goroutine stops immediately
- [ ] **Audit events for renewal**: Given a guard with auditor configured, when the heartbeat fires successfully, then a `renew` event is written to the audit log
- [ ] **No renewal without TTL**: Given a guard without `--ttl`, when the command runs, then no heartbeat goroutine is started (no overhead)

## Edge Cases

- **TTL too short (<1s)**: Use minimum practical interval (e.g., 500ms) or warn user
- **Renewal races with unlock**: Renewal and deferred unlock both running at guard exit - must handle gracefully
- **Lock deleted during renewal**: File gone, log warning, continue child execution
- **Clock skew**: Renewal writes new timestamp based on local clock (same host, so skew is not an issue)
- **Signal during renewal**: If SIGTERM arrives during renewal write, ensure child still gets forwarded signal

## Constraints

- **Atomic file writes**: Must use the same atomic write pattern (temp + rename + fsync) as existing lockfile writes
- **Owner verification before write**: Check that current lockfile has same owner/host/pid before overwriting
- **Goroutine lifecycle**: Heartbeat goroutine must be properly cleaned up on all exit paths
- **Signal handling**: Renewal goroutine must not block signal handling for child termination

---

## Implementation Sketch

### New Audit Event Type
```go
// In audit/audit.go
const EventRenew = "renew"
```

### Heartbeat Goroutine in cmdGuard
```go
// After acquiring lock, before starting child:
if *ttl > 0 {
    heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
    defer cancelHeartbeat()

    go renewHeartbeat(heartbeatCtx, rootDir, name, *ttl, auditor)
}
```

### Renewal Logic
```go
func renewHeartbeat(ctx context.Context, rootDir, name string, ttl time.Duration, auditor *audit.Writer) {
    interval := ttl / 2
    if interval < 500*time.Millisecond {
        interval = 500 * time.Millisecond
    }

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            err := renewLock(rootDir, name)
            if err != nil {
                // Log warning but don't exit - child may still complete
                fmt.Fprintf(os.Stderr, "warning: lock renewal failed: %v\n", err)
            } else {
                emitRenewEvent(auditor, ...)
            }
        }
    }
}
```

### Lock Renewal Function
```go
// In internal/lock/renew.go (new file)
func Renew(rootDir, name string) error {
    path := root.LockFilePath(rootDir, name)

    // Read current lock
    existing, err := lockfile.Read(path)
    if err != nil {
        return fmt.Errorf("read lock: %w", err)
    }

    // Verify we still own it
    id := identity.Current()
    if existing.Owner != id.Owner || existing.Host != id.Host || existing.PID != id.PID {
        return fmt.Errorf("lock stolen: now owned by %s@%s (pid %d)",
            existing.Owner, existing.Host, existing.PID)
    }

    // Update timestamp and rewrite atomically
    existing.AcquiredAt = time.Now()
    return lockfile.Write(path, existing)
}
```

---

## Notes

- The lockfile already has atomic write support via `lockfile.Write()`
- Consider whether renewal should use an advisory lock (flock) for extra safety - but keep it simple for now
- This feature directly addresses the pain point of "long builds lose their lock"
- Related: L-201 (auto-prune dead locks) could benefit from seeing heartbeat timestamps to identify truly dead processes

---

**Next:** Run `/kickoff L-200` to promote to Beads execution layer.
