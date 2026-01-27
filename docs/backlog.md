## Lokt Backlog (Go)

## Infrastructure & Foundation

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-001  | Repo init + Go module + layout           | backlog | `cmd/`, `internal/`, `pkg/`, basic CI scaffolding. |
| L-002  | CLI skeleton (cobra or stdlib flags)     | backlog | Root command + `version`. |
| L-003  | Error + exit code conventions            | backlog | Consistent stderr/stdout, typed errors. |
| L-004  | Build info in `version` (ldflags)        | backlog | commit/date/version injection. |
| L-005  | FS/path helpers package                  | backlog | Cross-platform path + atomic write helpers. |

## Root Discovery & Identity

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-100  | Resolve Lokt root (env → git → cwd)      | backlog | `LOKT_ROOT` → `git --git-common-dir` → `.lokt/`. |
| L-101  | Git common dir discovery + relpath fix   | backlog | Handle relative `--git-common-dir` output. |
| L-102  | Ensure dirs + permissions                | backlog | Create root + `locks/` safely (0700 dirs where possible). |
| L-103  | Lock name validation/sanitization        | backlog | Reject `..`/abs paths; allow `[A-Za-z0-9._-]`. |
| L-104  | Agent identity provider                  | backlog | owner/host/pid; owner from env/user. |

## Lockfile Protocol

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-110  | Lockfile JSON schema v1                 | backlog | `{name, owner, host, pid, acquired_ts, ttl_sec}`. |
| L-111  | Read/parse lockfile helpers             | backlog | Robust parsing + good errors. |
| L-112  | Write lockfile atomically + fsync       | backlog | Single write + `Sync()` for correctness. |

## Core Commands: lock / unlock / status

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-120  | `lokt lock <name>` atomic acquire        | backlog | `O_CREATE|O_EXCL`; return “held” errors. |
| L-121  | `lock` prints holder metadata on deny    | backlog | Read existing lockfile and include owner/age. |
| L-122  | `lokt unlock <name>` owner-checked       | backlog | Verify owner (optionally host/pid). |
| L-123  | `unlock --force` break-glass             | backlog | Remove without ownership validation. |
| L-124  | `lokt status` list all locks             | backlog | Show age/ttl/expired; stable text output. |
| L-125  | `lokt status <name>` single lock view    | backlog | Proper not-found handling. |
| L-126  | `status --json`                          | backlog | Machine-readable output. |

## TTL & Stale Handling

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-130  | Parse duration flags (`--ttl`, etc.)     | in-progress | beads:lokt-b0a |
| L-131  | Compute expiry + mark expired in status  | backlog | `acquired_ts + ttl_sec`. |
| L-132  | `unlock --break-stale` (expired only)    | backlog | Refuse if not expired. |
| L-133  | PID liveness check (unix)                | backlog | `kill(pid,0)` to avoid breaking live locks. |
| L-134  | Non-unix fallback stale policy           | backlog | Conservative behavior + docs. |
| L-135  | `status --prune-expired`                 | backlog | Optional auto-clean expired locks. |

## Waiting + Backoff

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-140  | `lock --wait`                            | backlog | Poll until free. |
| L-141  | `--timeout` for wait                     | backlog | Abort after deadline with clear exit code. |
| L-142  | Backoff + jitter                         | backlog | Avoid thundering herd. |
| L-143  | Wait UX (`--quiet` / periodic updates)   | backlog | Optional progress lines. |

## Guard Command

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-150  | `guard <name> -- <cmd...>`               | backlog | Acquire → exec → release. |
| L-151  | Propagate child exit code                | backlog | Lokt exits with child status. |
| L-152  | Ensure unlock on all paths               | backlog | Defer + signal handling. |
| L-153  | Guard flag parity (`--ttl/--wait/...`)   | backlog | Same semantics as `lock`. |
| L-154  | Guard env + cwd correctness              | backlog | Pass-through environment + working dir. |

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
| L-170  | Audit JSONL schema v1                   | backlog | `{ts,event,name,owner,host,pid,ttl_sec,extra}`. |
| L-171  | Emit audit events (acq/deny/release/...) | backlog | Append-only `audit.log`. |
| L-172  | `audit --tail`                           | backlog | Tail-follow audit log. |
| L-173  | `audit --since <ts|dur>`                 | backlog | Basic filtering. |

## Quality, Tests, Packaging

| Ticket | Title                                   | Status  | Notes |
| ------ | --------------------------------------- | ------- | ----- |
| L-180  | Unit tests: atomic acquire               | backlog | Race two contenders; assert one wins. |
| L-181  | Unit tests: unlock ownership/force       | backlog | Correct failures + force behavior. |
| L-182  | Unit tests: TTL + break-stale            | backlog | Expiry logic correctness. |
| L-183  | Integration: two processes contend       | backlog | `exec` two `lokt lock` commands. |
| L-184  | Integration: guard releases on failure   | backlog | Child exits nonzero; lock removed. |
| L-185  | Lint/format config                       | backlog | gofmt, govet, golangci-lint. |
| L-186  | CI (linux/mac)                           | backlog | Build + test matrix. |
| L-187  | Release packaging                         | backlog | goreleaser binaries. |
| L-188  | Install script / distribution            | backlog | `curl | sh` or brew. |
| L-189  | Docs + examples                           | backlog | README + wrapper patterns. |
