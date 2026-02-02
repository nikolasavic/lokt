# Lokt v1.0 Go-Live Plan

North star for the first major release. Milestones are ordered: each one
gates the next. Nothing ships to v1.0 until M0-M3 are green.

---

## Current State

The core is solid. Lock/unlock/status/guard/freeze/audit/doctor/wait/heartbeat
are all implemented with atomic file ops, TTL, stale detection, PID liveness,
auto-prune, and backoff+jitter. CI runs on Linux+macOS, goreleaser is
configured, and an install script exists.

**M0 is complete.** Protocol schema is frozen: lockfile version field,
`expires_at` timestamp, freeze namespace separation, and `lock_id` audit
correlation are all shipped and verified.

**M1 is in progress.** Exit code contract tests, renewal-under-contention,
multi-process contention, and guard release-on-failure tests are all done.
Root discovery coverage remains.

---

## M0: Protocol Freeze

**Why this is first:** Once v1.0 ships, the lockfile JSON schema and audit
schema become a public contract. Every field we forgot to add becomes a
breaking change later. These items are cheap now and expensive after release.

| Item | Ticket/Bead | Status | Description |
|------|-------------|--------|-------------|
| Lockfile `version` field | lokt-a7m | ✅ Done | `"version": 1` in Lock struct with read-time validation. |
| `expires_at` timestamp | lokt-bm6 | ✅ Done | Explicit expiry timestamp alongside `acquired_ts` + `ttl_sec`. |
| Freeze namespace separation | lokt-bzz | ✅ Done | Freeze files moved to `freezes/<name>.json` with legacy fallback. |
| `lock_id` in audit events | lokt-ao9 | ✅ Done | 128-bit random hex ID per acquisition, threaded through all lifecycle events. |

**Exit criteria:** `lokt doctor --json` output includes `"protocol_version": 1`.
Lockfile schema documented in README with explicit compatibility promise.

---

## M1: Prove It Works (Test Confidence)

**Why this matters:** Lokt is a coordination primitive. If it has a bug, the
systems depending on it have data races. A lock manager with only unit tests
is a lock manager nobody should trust in production.

| Item | Ticket/Bead | Status | Description | Risk if skipped |
|------|-------------|--------|-------------|-----------------|
| Multi-process contention test | L-183 / lokt-xmp | ✅ Done | 10 real OS processes race via `lokt guard`, assert exactly 1 wins. Stability test runs 10 rounds. | The core promise (mutual exclusion) is only tested in-process. A serialization bug in the CLI layer would go undetected. |
| Guard release-on-failure test | L-184 / lokt-1v1 | ✅ Done | Guard releases lock on child failure (exit 1, exit 42) and SIGTERM (exit 143). Lock file removal verified. | Guard is the primary UX. If it leaks locks on failure, every CI pipeline using it accumulates stale locks. |
| Exit code contract test | lokt-6cn | ✅ Done | Table-driven test covering every exit code path across all commands. 21 test cases in `exitcode_test.go`. | Exit codes are API. Scripts depend on `$?`. A refactor that changes exit 2→1 silently breaks every caller. |
| Renewal-under-contention test | lokt-36j | ✅ Done | Short-TTL guard with a contender trying `--break-stale`. Heartbeat prevents the break. | The heartbeat/stale-break interaction is the most complex race in the codebase. Untested = unknown. |
| Root discovery coverage | — | ⬜ Todo | Increase `internal/root` test coverage from 30% to 70%+. Cover LOKT_ROOT, git common dir, .lokt/ fallback, and error paths. | Root discovery bugs mean locks go to the wrong directory. Two processes "holding the same lock" in different dirs = no mutual exclusion. |

**Exit criteria:** `go test -race ./...` passes with all new tests.
Coverage on `internal/lock` stays above 75%, `cmd/lokt` above 55%.

---

## M2: Distribution & Install

**Why this matters:** If users can't install it reliably, nothing else matters.
A broken install script or missing binary for their platform means they walk
away and never come back.

| Item | Ticket/Bead | Status | Description | Risk if skipped |
|------|-------------|--------|-------------|-----------------|
| Release pipeline dry-run | L-186/187 | ✅ Done | Goreleaser builds all 4 platforms, binaries work, checksums match. Tested with `v0.0.1-test`. | First real release fails publicly. Bad look. |
| Install script end-to-end test | L-188 | ✅ Done | `scripts/install.sh` tested against release on Ubuntu + macOS. | Users run `curl \| sh` and it fails. Worst possible first impression. |
| Homebrew formula | NEW | ⬜ Todo | `brew install lokt` or at minimum a tap. macOS developers expect this. | Friction for the largest desktop developer demographic. |
| Binary smoke test in CI | NEW | ⬜ Todo | Post-build step that runs `./lokt version`, `./lokt doctor`, and a lock/unlock cycle against the built binary. | Goreleaser builds a binary that segfaults on startup. We don't find out until users do. |

**Exit criteria:** RC release exists on GitHub. `scripts/install.sh` works
against it. At least one person outside the team has installed and run
`lokt doctor` successfully.

---

## M3: Operational Readiness

**Why this matters:** Production users don't just run software — they debug it
at 2am when something is stuck. If they can't figure out what's wrong, they
rip it out and replace it with a bash `mkdir` lock.

| Item | Ticket/Bead | Status | Description | Risk if skipped |
|------|-------------|--------|-------------|-----------------|
| Troubleshooting guide | NEW | ⬜ Todo | `docs/troubleshooting.md` — stale locks after crash, NFS warnings, permission errors, "lock held but process is dead", clock skew effects. | Users hit a problem, Google it, find nothing, give up. |
| Document guard exit code propagation | — | ⬜ Todo | README doesn't mention that guard exits with the child's code (including 128+signal). Scripts depend on this. | Users write `lokt guard build -- make && deploy` and the deploy runs even when make fails because they didn't know guard preserves exit codes — wait, it does, but they need to know *how*. |
| Threat model & scope doc | NEW | ⬜ Todo | One paragraph: lokt is for cooperative coordination on a single host/shared filesystem. It is not access control, not distributed consensus, not secure against malicious actors. | Someone uses lokt where they need flock or etcd, it fails, they blame lokt. Clear scope prevents misuse. |
| Audit log permissions | — | ⬜ Todo | Create `audit.log` with 0600 instead of 0644. Lock files are fine as world-readable (status info), but audit events may contain operational details. | Audit log readable by any user on shared systems. Minor but looks sloppy for a "rock-solid" tool. |
| `audit --tail` completion | L-172 | ✅ Done | `lokt audit --tail` is implemented and functional. | Half-shipped feature visible in `--help`. |

**Exit criteria:** A new user can install lokt, hit a stale lock problem, and
resolve it using only the troubleshooting guide without asking for help.

---

## M4: Polish (nice-to-have for v1.0, can ship in v1.1)

These improve the experience but aren't trust-critical.

| Item | Ticket | Description |
|------|--------|-------------|
| Wait UX (`--quiet` / periodic updates) | L-143 | Progress lines during `--wait`. Currently silent. |
| Windows support story | NEW | Either test on Windows and claim support, or add a clear "Unix-only" note. PID liveness and network FS detection are Unix-specific today. |
| Performance characteristics doc | NEW | Lock acquisition latency, contention throughput, scalability limits. |
| Backlog/story-draft cleanup | — | Most story drafts are for completed work. Archive them. |
| `LOKT_REQUIRE_LOCAL_FS` strict mode | NEW | Hard-fail (not just warn) on network filesystems. For paranoid users. |
| Bash/Zsh completions | NEW | Shell completions for commands and lock names. |

---

## Release Checklist (when M0-M3 are done)

```
[ ] All M0-M3 items closed
[ ] go test -race ./... passes
[ ] golangci-lint run clean
[ ] README reflects all commands, flags, exit codes, and schema
[ ] CHANGELOG.md written (or goreleaser auto-generates)
[ ] Tag v1.0.0
[ ] Verify GitHub release has all 4 platform binaries + checksums
[ ] Verify install.sh works against v1.0.0
[ ] Verify brew install works (if formula exists)
[ ] Run lokt doctor on fresh Ubuntu + macOS
[ ] Announce
```

---

## What Keeps Me Up at Night

Things that could damage the "rock-solid" reputation if we ship without
addressing them:

1. ~~**Lock namespace collision with freeze prefix.**~~ ✅ Fixed (lokt-bzz).
   Freezes now live in `freezes/` directory, no naming collision possible.

2. ~~**No integration tests for the core promise.**~~ ✅ Fixed (L-183).
   10-process contention test via `lokt guard` with stability runs.

3. ~~**Schema without version field.**~~ ✅ Fixed (lokt-a7m).
   `"version": 1` in every lockfile, with read-time validation.

4. ~~**Guard leaking locks on unclean exit paths.**~~ ✅ Fixed (L-184).
   Child failure, exit code propagation, and SIGTERM all tested with real
   processes.

5. **Silent data loss if root discovery picks wrong directory.** Two
   processes in different working directories could resolve different
   roots. Each thinks it holds "the" lock. Neither does. This needs
   integration-level testing. *(M1 — still open)*
