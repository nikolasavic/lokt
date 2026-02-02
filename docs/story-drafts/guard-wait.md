# Guard --wait flag

status: draft
created: 2026-01-27

## Problem

The `guard` command fails immediately if the lock is held. But `lock` has `--wait` and `--timeout` flags. Guard should have parity.

## Requirements

1. Add `--wait` flag to guard command
2. Add `--timeout` flag to guard command  
3. Use existing `AcquireWithWait` infrastructure

## Acceptance Criteria

- [ ] `lokt guard --wait build -- ./cmd` waits for lock then runs command
- [ ] `lokt guard --wait --timeout 5s build -- ./cmd` waits up to 5s
- [ ] Exit codes match lock command behavior
- [ ] Document in usage()

## Implementation

Mirror the `cmdLock` implementation - add flags, use `AcquireWithWait` with context.

---

**Next:** /kickoff
