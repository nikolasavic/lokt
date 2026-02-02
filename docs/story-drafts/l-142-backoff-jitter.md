# L-142: Add backoff + jitter to wait polling

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

With fixed 100ms polling, multiple waiting processes all retry at the same intervals. When a lock is released, they all race simultaneously - the "thundering herd" problem.

## Users

- **Multi-agent systems**: Multiple AI agents waiting for same resource
- **CI pipelines**: Parallel jobs competing for locks

## Requirements

1. Replace fixed polling with exponential backoff
2. Add jitter to desynchronize competing waiters
3. Cap maximum backoff interval (don't wait too long between retries)
4. Keep initial poll fast (responsive acquisition)

## Non-Goals

- Configurable backoff parameters (keep it simple for now)
- Progress output (L-143)

## Acceptance Criteria

- [ ] Initial poll interval ~50-100ms (fast first retry)
- [ ] Exponential growth with each retry
- [ ] Random jitter (±25% or similar)
- [ ] Max interval capped (e.g., 2 seconds)
- [ ] Existing tests still pass

## Implementation Notes

### Backoff Formula
```
interval = min(baseInterval * 2^attempt, maxInterval) + jitter
```

### Suggested Parameters
- Base: 50ms
- Max: 2s
- Jitter: ±25%

---

**Next:** Run `/kickoff L-142`
