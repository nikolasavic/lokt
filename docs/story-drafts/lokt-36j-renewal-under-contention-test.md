# lokt-36j: Add renewal-under-contention test

status: draft
created: 2026-02-01
backlog-ref: beads issue lokt-36j

## Verification
- Level: required
- Environments: sandbox

---

## Problem

The guard command's heartbeat renewal mechanism prevents stale-break from incorrectly breaking active locks. However, there is currently no test that verifies this critical race condition: what happens when a contender attempts to break a lock with `--break-stale` while the holder's heartbeat is actively renewing it?

This gap leaves the most likely source of real bugs untested. If renewal races aren't properly synchronized, a contender could incorrectly break a live lock between renewal cycles, leading to double-acquisition and corrupted shared state.

External technical review identified this as test plan item 3: "renewal race test — the gap most likely to hide a real bug."

## Users

- **Developers**: Need confidence that heartbeat renewal prevents stale-break during active guard sessions
- **System operators**: Must be assured that concurrent processes using lokt won't experience lock stealing under normal operation
- **Security reviewers**: Require proof that the synchronization primitive works correctly under contention

## Requirements

1. **Test scenario**: Start a guard process with short TTL (e.g., 2s) that keeps running while heartbeat renewal is active
2. **Concurrent contender**: A background goroutine attempts to acquire the same lock with `--break-stale` flag during the guard's lifetime
3. **Race window verification**: The test must validate that even during the narrow window between renewals, the lock is never broken while the holder is alive
4. **Active renewal confirmation**: Verify that heartbeat renewal prevents stale-break from succeeding (contender must wait or fail, not break the lock)
5. **Test placement**: Add test to appropriate test file (likely `cmd/lokt/main_test.go` or new `cmd/lokt/guard_test.go`)

## Non-Goals

- Testing guard command's basic functionality (already covered): This focuses specifically on the renewal-vs-stale-break race
- Integration testing with real processes: Unit test using goroutines is sufficient
- Testing non-TTL guards: Renewal only applies when TTL is set
- Testing freeze functionality: Freeze is a separate mechanism from stale-break

## Acceptance Criteria

- [ ] **Race scenario coverage**: Given a guard process with TTL=2s is running with active heartbeat renewal, when a concurrent goroutine attempts acquire with --break-stale, then the contender does not succeed in breaking the lock while the guard process is alive
- [ ] **Renewal timing verification**: Given heartbeat runs at TTL/2 interval (1s for 2s TTL), when checking lock state between renewal cycles, then the lock remains held by the original owner throughout the guard's lifetime
- [ ] **Test stability**: Given the test runs 100 times consecutively, when checking results, then all runs pass without flakes (no race detector warnings)
- [ ] **Integration with test suite**: Given the test is added to the codebase, when running `go test ./...`, then the new test executes and passes within reasonable time (<5s)

## Edge Cases

- **Renewal failure during test**: If renewal fails mid-test (simulated failure), the test should detect when this causes stale-break to succeed (this validates the test catches real bugs)
- **Very short TTL (< 500ms)**: The heartbeat has a minimum interval of 500ms (per main.go:774), so TTLs < 1s may not renew fast enough; test should use TTL ≥ 2s to ensure renewal happens
- **Context cancellation during race**: When the guard process ends normally, the contender may succeed in breaking the now-stale lock; test must distinguish this from incorrect break during active renewal
- **Clock skew in test**: Use monotonic time comparisons (time.Since) to avoid wall-clock issues in CI environments

## Constraints

- **Goroutine-based test**: Must use goroutines for concurrency, not subprocess exec, to enable deterministic control and avoid flakiness
- **Existing heartbeat implementation**: Test validates the current runHeartbeat function (main.go:771-794) without modification
- **No sleep-based synchronization**: Use channels/mutexes for coordination to avoid flaky timing assumptions
- **Test execution time**: Target < 5s total duration (use 2s TTL, run for ~3s to allow multiple renewal cycles)
- **File-based locks**: The test inherits all file-based lock limitations (no true atomicity guarantees, potential NFS issues in CI)

---

## Notes

### Relevant Code Locations

- **Heartbeat implementation**: `cmd/lokt/main.go:768-794` (runHeartbeat function)
  - Runs at TTL/2 interval, minimum 500ms
  - Calls `lock.Renew()` to update acquired_ts
  - Logs warnings on failure but continues

- **Renewal logic**: `internal/lock/renew_test.go:16-65` (existing TestRenew_Success)
  - Shows how renewal updates AcquiredAt timestamp
  - Validates owner/host/PID checking

- **Stale-break behavior**: Related to unlock command's `--break-stale` flag (searches for expired locks and breaks them)

### Test Strategy

The test should:
1. Start a simulated guard with TTL=2s (creates lock, starts heartbeat goroutine)
2. Launch contender goroutine that loops attempting `--break-stale` acquire
3. Track all acquire attempts and their outcomes
4. After 3-4 seconds, cancel the guard (stop heartbeat)
5. Verify: no break-stale succeeded during heartbeat lifetime
6. Verify: contender eventually succeeds after heartbeat stops

### Alternative Approaches Considered

- **Subprocess-based test**: Spawn real `lokt guard` process - rejected due to timing complexity and flakiness
- **Table-driven scenarios**: Multiple TTL values - deferred to future enhancement, start with single 2s case
- **Stress test with many contenders**: N goroutines competing - overkill for basic correctness, can add later

---

**Next:** Run `/kickoff lokt-36j` to promote to Beads execution layer.
