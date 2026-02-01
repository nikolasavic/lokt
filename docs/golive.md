# Lokt v1.0 Go-Live Plan

North star for the first major release. Milestones are ordered: each one
gates the next. Nothing ships to v1.0 until M0-M3 are green.

---

## Current State

The core is solid. Lock/unlock/status/guard/freeze/audit/doctor/wait/heartbeat
are all implemented with atomic file ops, TTL, stale detection, PID liveness,
auto-prune, and backoff+jitter. CI runs on Linux+macOS, goreleaser is
configured, and an install script exists.

What's missing is the stuff that separates "works on my machine" from
"I'd bet my production deploy on it."

---

## M0: Protocol Freeze

**Why this is first:** Once v1.0 ships, the lockfile JSON schema and audit
schema become a public contract. Every field we forgot to add becomes a
breaking change later. These items are cheap now and expensive after release.

| Item | Ticket/Bead | Description | Risk if skipped |
|------|-------------|-------------|-----------------|
| Lockfile `version` field | lokt-a7m | Add `"version": 1` to Lock struct. Readers that see an unknown version can bail with a clear error instead of silently misinterpreting fields. | Schema evolution becomes a nightmare. Every future change requires heuristic detection of old-vs-new format. |
| `expires_at` timestamp | lokt-bm6 | Write explicit expiry alongside `acquired_ts` + `ttl_sec`. Removes reader-side arithmetic and documents the wall-clock limitation (NTP jumps affect cross-process TTL checks). | Readers must recompute expiry. Subtle bugs when clocks adjust between acquire and stale-check. |
| Freeze namespace separation | lokt-bzz | Move freeze files from `locks/freeze-<name>.json` to `freezes/<name>.json`. | A lock named `freeze-deploy` collides with the freeze for `deploy`. Naming collision in a lock manager is a credibility problem. |
| `lock_id` in audit events | lokt-ao9 | UUID (or host+pid+seq) per acquisition so acquire/renew/release events can be correlated. | Audit log analysis requires brittle heuristics to pair events. Debugging production issues is harder than it needs to be. |

**Exit criteria:** `lokt doctor --json` output includes `"protocol_version": 1`.
Lockfile schema documented in README with explicit compatibility promise.

---

## M1: Prove It Works (Test Confidence)

**Why this matters:** Lokt is a coordination primitive. If it has a bug, the
systems depending on it have data races. A lock manager with only unit tests
is a lock manager nobody should trust in production.

| Item | Ticket/Bead | Description | Risk if skipped |
|------|-------------|-------------|-----------------|
| Multi-process contention test | L-183 | Spawn N real `lokt lock` processes, assert exactly 1 wins. Not goroutines — actual OS processes. | The core promise (mutual exclusion) is only tested in-process. A serialization bug in the CLI layer would go undetected. |
| Guard release-on-failure test | L-184 | `lokt guard build -- false` must release the lock and exit non-zero. Test with SIGTERM too. | Guard is the primary UX. If it leaks locks on failure, every CI pipeline using it accumulates stale locks. |
| Exit code contract test | lokt-6cn | Table-driven test covering every exit code path across all commands. | Exit codes are API. Scripts depend on `$?`. A refactor that changes exit 2→1 silently breaks every caller. |
| Renewal-under-contention test | lokt-36j | Short-TTL guard with a contender trying `--break-stale`. Heartbeat must prevent the break. | The heartbeat/stale-break interaction is the most complex race in the codebase. Untested = unknown. |
| Root discovery coverage | — | Increase `internal/root` test coverage from 30% to 70%+. Cover LOKT_ROOT, git common dir, .lokt/ fallback, and error paths. | Root discovery bugs mean locks go to the wrong directory. Two processes "holding the same lock" in different dirs = no mutual exclusion. |

**Exit criteria:** `go test -race ./...` passes with all new tests.
Coverage on `internal/lock` stays above 75%, `cmd/lokt` above 55%.

---

## M2: Distribution & Install

**Why this matters:** If users can't install it reliably, nothing else matters.
A broken install script or missing binary for their platform means they walk
away and never come back.

| Item | Ticket/Bead | Description | Risk if skipped |
|------|-------------|-------------|-----------------|
| Release pipeline dry-run | L-186/187 | Tag `v0.1.0-rc1`, verify goreleaser builds all 4 platforms (darwin/linux × amd64/arm64), binaries work, checksums match. | First real release fails publicly. Bad look. |
| Install script end-to-end test | L-188 | Test `scripts/install.sh` against the RC release on clean Ubuntu + macOS. Verify download, checksum, PATH detection. | Users run `curl \| sh` and it fails. Worst possible first impression. |
| Homebrew formula | NEW | `brew install lokt` or at minimum a tap. macOS developers expect this. | Friction for the largest desktop developer demographic. |
| Binary smoke test in CI | NEW | Post-build step that runs `./lokt version`, `./lokt doctor`, and a lock/unlock cycle against the built binary. | Goreleaser builds a binary that segfaults on startup. We don't find out until users do. |

**Exit criteria:** RC release exists on GitHub. `scripts/install.sh` works
against it. At least one person outside the team has installed and run
`lokt doctor` successfully.

---

## M3: Operational Readiness

**Why this matters:** Production users don't just run software — they debug it
at 2am when something is stuck. If they can't figure out what's wrong, they
rip it out and replace it with a bash `mkdir` lock.

| Item | Ticket/Bead | Description | Risk if skipped |
|------|-------------|-------------|-----------------|
| Troubleshooting guide | NEW | `docs/troubleshooting.md` — stale locks after crash, NFS warnings, permission errors, "lock held but process is dead", clock skew effects. | Users hit a problem, Google it, find nothing, give up. |
| Document guard exit code propagation | — | README doesn't mention that guard exits with the child's code (including 128+signal). Scripts depend on this. | Users write `lokt guard build -- make && deploy` and the deploy runs even when make fails because they didn't know guard preserves exit codes — wait, it does, but they need to know *how*. |
| Threat model & scope doc | NEW | One paragraph: lokt is for cooperative coordination on a single host/shared filesystem. It is not access control, not distributed consensus, not secure against malicious actors. | Someone uses lokt where they need flock or etcd, it fails, they blame lokt. Clear scope prevents misuse. |
| Audit log permissions | — | Create `audit.log` with 0600 instead of 0644. Lock files are fine as world-readable (status info), but audit events may contain operational details. | Audit log readable by any user on shared systems. Minor but looks sloppy for a "rock-solid" tool. |
| `audit --tail` completion | L-172 | Backlog says in-progress. Either finish or cut it from v1.0 scope. | Half-shipped feature visible in `--help`. |

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
| Backlog/story-draft cleanup | — | Most story drafts are for completed work. Archive them. Sync backlog.md statuses (L-172, L-188 show wrong status). |
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

1. **Lock namespace collision with freeze prefix.** Someone names a lock
   `freeze-build` and weird things happen. This is the kind of bug that
   erodes trust because it's a design flaw, not a code bug.

2. **No integration tests for the core promise.** We've tested that
   `Acquire()` the Go function works. We haven't tested that `lokt lock`
   the binary works when two processes race. That's the product.

3. **Schema without version field.** The moment we need to add a field
   post-v1.0, we're doing format detection by "does this key exist?"
   forever.

4. **Guard leaking locks on unclean exit paths.** The code looks correct
   (defer + signal handling), but it's only tested with goroutines, not
   real signals to real processes. A leaked lock in CI means a stuck
   pipeline and an angry user.

5. **Silent data loss if root discovery picks wrong directory.** Two
   processes in different working directories could resolve different
   roots. Each thinks it holds "the" lock. Neither does. This needs
   integration-level testing.
