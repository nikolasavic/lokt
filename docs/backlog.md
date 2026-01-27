## Lokt Backlog (Go)

# Status Legend

| Status | Meaning |
|--------|---------|
| `done` | Completed and deployed |
| `in-progress` | Currently being worked on |
| `ready` | Ready to start, no blockers |
| `blocked` | Has dependencies or blockers |
| `backlog` | Not yet prioritized for current milestone |

---

## Infrastructure & Foundation

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-001  | Repo init + Go module + layout           | done | `cmd/`, `internal/`, basic CI scaffolding. |
| L-002  | CLI skeleton (cobra or stdlib flags)     | done | Root command + `version` using stdlib flag. |
| L-003  | Error + exit code conventions            | done | Consistent stderr/stdout, typed errors, exit codes 0-4/64. |
| L-004  | Build info in `version` (ldflags)        | done | commit/date/version injection via ldflags. |
| L-005  | FS/path helpers package                  | done | `internal/root`, `internal/lockfile` with atomic write helpers. |

## Root Discovery & Identity

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-100  | Resolve Lokt root (env → git → cwd)      | done | `LOKT_ROOT` → `git --git-common-dir` → `.lokt/`. |
| L-101  | Git common dir discovery + relpath fix   | done | Handles relative `--git-common-dir` output. |
| L-102  | Ensure dirs + permissions                | done | Creates root + `locks/` with 0700. |
| L-103  | Lock name validation/sanitization        | done | Reject `..`/abs paths; allow `[A-Za-z0-9._-]`. beads:lokt-lb2 |
| L-104  | Agent identity provider                  | done | `internal/identity`: owner/host/pid from env/user. |

## Lockfile Protocol

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-110  | Lockfile JSON schema v1                 | done | `{name, owner, host, pid, acquired_ts, ttl_sec}`. |
| L-111  | Read/parse lockfile helpers             | done | `lockfile.Read()` with robust parsing. |
| L-112  | Write lockfile atomically + fsync       | done | `lockfile.Write()` with atomic write + fsync. |

## Core Commands: lock / unlock / status

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-120  | `lokt lock <name>` atomic acquire        | done | `O_CREATE|O_EXCL`; returns HeldError on deny. |
| L-121  | `lock` prints holder metadata on deny    | done | HeldError includes owner/host/pid/age. |
| L-122  | `lokt unlock <name>` owner-checked       | done | Verifies owner before release. |
| L-123  | `unlock --force` break-glass             | done | Removes without ownership validation. |
| L-124  | `lokt status` list all locks             | done | Shows age/ttl/expired; stable text output. |
| L-125  | `lokt status <name>` single lock view    | done | Detailed view with PID liveness. |
| L-126  | `status --json`                          | done | Machine-readable output. beads:lokt-z8x |

## TTL & Stale Handling

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-130  | Parse duration flags (`--ttl`, etc.)     | done | TTL validation, rejects negative values. |
| L-131  | Compute expiry + mark expired in status  | done | Status shows [EXPIRED] marker. |
| L-132  | `unlock --break-stale` (expired only)    | done | Also breaks dead-PID locks on same host. |
| L-133  | PID liveness check (unix)                | done | `internal/stale` package with `kill(pid,0)`. |
| L-134  | Non-unix fallback stale policy           | done | Windows returns true (conservative). |
| L-135  | `status --prune-expired`                 | done | Auto-removes expired locks while listing. |

## Waiting + Backoff

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-140  | `lock --wait`                            | done | Poll until free. beads:lokt-eie |
| L-141  | `--timeout` for wait                     | in-progress | Abort after deadline with clear exit code. beads:lokt-4u3 |
| L-142  | Backoff + jitter                         | done | Avoid thundering herd. beads:lokt-7iv |
| L-143  | Wait UX (`--quiet` / periodic updates)   | backlog | Optional progress lines. |

## Guard Command

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-150  | `guard <name> -- <cmd...>`               | done | Acquire → exec → release. |
| L-151  | Propagate child exit code                | done | Lokt exits with child status (including 128+sig). |
| L-152  | Ensure unlock on all paths               | done | Defer + signal handling (SIGINT/SIGTERM). |
| L-153  | Guard flag parity (`--ttl/--wait/...`)   | done | Has `--ttl` flag, same semantics as `lock`. |
| L-154  | Guard env + cwd correctness              | done | Pass-through stdin/stdout/stderr + working dir. |

## Freeze Switch

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-160  | `freeze <name> --ttl 15m`                | backlog | Create `freeze-<name>` lock. |
| L-161  | `unfreeze <name>`                        | backlog | Owner-checked; supports `--force`. |
| L-162  | Guard checks freeze before acquiring     | backlog | Abort if active freeze exists. |
| L-163  | Auto-prune expired freeze in guard       | backlog | Remove expired freeze deterministically. |
| L-164  | Status highlights freeze locks           | backlog | Distinct labeling in output/JSON. |

## Audit Log

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-170  | Audit JSONL schema v1                   | done | `{ts,event,name,owner,host,pid,ttl_sec,extra}`. beads:lokt-ddd |
| L-171  | Emit audit events (acq/deny/release/...) | done | Append-only `audit.log`. beads:lokt-cy3 |
| L-172  | `audit --tail`                           | backlog | Tail-follow audit log. |
| L-173  | `audit --since <ts|dur>`                 | done | Basic filtering. beads:lokt-khv |

## Quality, Tests, Packaging

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-180  | Unit tests: atomic acquire               | done | Race 10 contenders; asserts exactly one wins. |
| L-181  | Unit tests: unlock ownership/force       | done | Tests NotOwner + Force behavior. |
| L-182  | Unit tests: TTL + break-stale            | done | Tests expired TTL, dead PID, cross-host. |
| L-183  | Integration: two processes contend       | backlog | `exec` two `lokt lock` commands. |
| L-184  | Integration: guard releases on failure   | backlog | Child exits nonzero; lock removed. |
| L-185  | Lint/format config                       | done | gofmt, govet, golangci-lint configured. |
| L-186  | CI (linux/mac)                           | done | Build + test matrix. beads:lokt-uyq |
| L-187  | Release packaging                         | done | goreleaser binaries. beads:lokt-uyq |
| L-188  | Install script / distribution            | backlog | `curl | sh` or brew. |
| L-189  | Docs + examples                           | done | README.txt with CLI usage and examples. |
