# L-183: Multi-process contention integration test

status: draft
created: 2026-02-01
backlog-ref: docs/backlog.md
golive-ref: docs/golive.md (M1)

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Lokt's core promise — mutual exclusion — is only tested in-process via
goroutines calling `Acquire()`. The actual product is the `lokt lock` binary.
A serialization bug in the CLI layer (argument parsing, environment handling,
root discovery) would go undetected by current tests. This is item #2 in the
"What Keeps Me Up at Night" section of golive.md.

## Users

- **Maintainer**: needs confidence that the binary, not just the library,
  provides mutual exclusion under real OS-level process contention.
- **CI**: needs a test that catches regressions in the CLI ↔ lock layer.

## Requirements

1. Spawn N real OS processes (via `os/exec`) all racing to `lokt lock` the
   same lock name simultaneously.
2. Assert exactly 1 process exits with code 0 (acquired) and the rest exit
   with code 2 (held by other).
3. The winning process's identity must match the lockfile on disk.
4. Test must use the actual compiled `lokt` binary, not in-process function
   calls.
5. Test must be deterministic — no flaky timing dependencies. Use a barrier
   (temp file or signal) to synchronize the start of all processes.
6. Test must clean up: unlock after test, remove temp root.

## Non-Goals

- **Distributed/multi-host contention**: out of scope. Same host only.
- **Wait/polling behavior under contention**: tested separately (acquire_test.go).
- **Guard contention**: separate concern (L-184 covers guard).
- **Performance benchmarking**: not measuring latency, just correctness.

## Acceptance Criteria

- [ ] **exactly-one-winner**: Given N=10 concurrent OS processes all running
  `lokt lock race-test`, when they execute simultaneously, then exactly 1
  exits with code 0 and N-1 exit with code 2.

- [ ] **lockfile-matches-winner**: Given the winning process, when the lockfile
  is read from disk, then its `owner` field matches the winner's LOKT_OWNER
  and `pid` matches the winner's OS PID.

- [ ] **real-binary**: Given the test, when it runs, then it invokes a compiled
  `lokt` binary via `os/exec.Command` (not in-process function calls).

- [ ] **race-detector-clean**: Given `go test -race ./...`, when the test runs,
  then the race detector reports no data races.

- [ ] **different-owners**: Given each process has a unique LOKT_OWNER, when
  contention occurs, then the deny output includes the correct holder identity.

- [ ] **repeated-stability**: Given the test runs 10 times in a loop, when
  executed, then all 10 runs produce exactly 1 winner (no flakiness).

## Edge Cases

- All N processes start before any can finish the O_EXCL create — use a
  barrier file to synchronize start.
- The test binary doesn't exist — use `go build` in TestMain or a test helper
  to compile the binary into a temp dir before running tests.
- Stale test processes after test failure — use TTL on lock and t.Cleanup to
  ensure cleanup.
- Race between process spawn and barrier removal — barrier should be removed
  atomically, processes poll for its absence.

## Constraints

- Must build the `lokt` binary as a test fixture. Use `exec.Command("go",
  "build", ...)` in a TestMain or sync.Once helper, targeting a temp directory.
  This avoids requiring a pre-built binary in PATH.
- Each process needs a unique LOKT_OWNER so contention is between different
  identities (same owner would trigger reentrant acquire).
- N=10 is sufficient for confidence without being slow. Parameterize if needed.
- Test file should live in `cmd/lokt/` alongside other CLI integration tests.

---

## Design Decisions

### Binary compilation strategy
Use `sync.Once` in a test helper to compile the binary once per test run into
a temp directory. Store the path in a package-level var. This is idiomatic Go
for integration tests that need a binary (same pattern as `cmd/go` tests in
the Go stdlib).

### Synchronization barrier
Create a "barrier" file before spawning processes. Each process wrapper script
or the test itself waits for the barrier to be removed before calling
`lokt lock`. This ensures all processes are ready before any attempts the lock.
Alternative: just spawn all processes and accept that OS scheduling provides
sufficient overlap with N=10. Given that `O_EXCL` is the contention point and
file creation is fast, concurrent spawning without explicit barrier should be
adequate for N=10.

### Test location
`cmd/lokt/contention_test.go` — groups with other CLI integration tests.

---

## Notes

- The `captureCmd` helper in `helpers_test.go` won't work here since it calls
  Go functions in-process. This test specifically needs `os/exec`.
- Each spawned process needs: `LOKT_ROOT=<tmpdir>`, `LOKT_OWNER=agent-<N>`.
- Check exit codes via `exec.ExitError.ExitCode()`.
- Consider `--json` output for structured verification of holder identity.
- This is the first `os/exec`-based test in the codebase. The helper pattern
  established here will be reusable for L-184 (guard release-on-failure).

---

**Next:** Run `/kickoff L-183` to promote to Beads execution layer.
