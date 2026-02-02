# lokt-al5: Add `lokt why <name>` Command

status: kicked-off
created: 2026-01-28
backlog-ref: beads:lokt-al5

## Verification
- Level: required
- Environments: sandbox

---

## Problem

When a lock acquisition fails, lokt prints a terse error like `lock "build" held by alice@laptop (pid 12345) for 2m30s`. The user is left to figure out the implications themselves: Is it stale? Can I break it? Is there a freeze? Should I wait? Should I force-break? Each denial reason has different recovery options, but today the user must know the lokt internals to choose the right action.

This hurts most in multi-agent scenarios (the primary use case) where an agent hits a denial, doesn't understand what to do, and either gives up or picks the wrong recovery strategy.

## Users

- **AI agents (Claude Code, etc.)**: Need machine-parseable diagnostics to decide recovery strategy (wait, retry, escalate)
- **Human operators**: Need a quick one-command answer to "why can't I get this lock?" instead of mentally cross-referencing status output with stale rules
- **CI/CD pipelines**: Need clear diagnostic output in logs when guard commands fail unexpectedly

## Requirements

1. New `lokt why <name>` CLI command that reads a lock and prints a human-readable explanation of why it cannot be acquired
2. Cover ALL denial scenarios with specific, actionable output:
   - Lock held by another owner (same host / cross-host)
   - Lock frozen (with remaining TTL)
   - Lock stale but not auto-breakable (cross-host, no TTL)
   - Lock expired (TTL elapsed)
   - Lock held by dead process (same host)
   - Lock corrupted (malformed JSON)
   - Lock does not exist (not held — acquisition would succeed)
3. For each scenario, suggest the specific recovery command (e.g., `lokt unlock --break-stale build`, `lokt unlock --force build`, `wait for freeze to expire in 4m15s`)
4. Display parsed metadata: owner, host, PID, acquired time, age, TTL, remaining TTL, PID liveness status
5. Support `--json` flag for machine-readable output (agents need structured data)
6. Emit no audit events (read-only diagnostic command)

## Non-Goals

- **Not a lock acquisition attempt**: `why` is purely diagnostic — no side effects, no file mutations
- **Not a replacement for `status`**: `status` lists all locks; `why` diagnoses one lock's impact on the caller
- **Not a fix/repair command**: `why` only diagnoses and suggests; it does not break, release, or modify locks
- **No audit log querying**: Don't show historical events (that's `audit --since`); focus on current state only
- **No `--wait` or polling**: This is a one-shot diagnostic

## Acceptance Criteria

- [ ] **Happy path (no lock)**: Given no lock exists for name "build", when `lokt why build` is run, then output states the lock is free and acquisition would succeed, exit code 0
- [ ] **Held by other (same host, alive)**: Given "build" is held by alice@laptop (PID alive, same host), when `lokt why build` is run, then output shows holder identity, PID is alive, age, and suggests `--wait` or `--force`, exit code 2
- [ ] **Held by other (cross-host)**: Given "build" is held by alice@server (different host), when `lokt why build` is run, then output shows holder identity, notes PID liveness cannot be verified remotely, and suggests `--wait`, `--force`, or `--break-stale` (if TTL expired), exit code 2
- [ ] **Frozen**: Given "build" is frozen by ci@server with 7m remaining, when `lokt why build` is run, then output shows freeze owner, remaining TTL, and suggests waiting or `lokt unfreeze build`, exit code 2
- [ ] **Frozen + regular lock**: Given "build" is both frozen AND has a regular lock held, when `lokt why build` is run, then output mentions BOTH the freeze and the held lock (freeze takes priority in explanation)
- [ ] **Expired TTL**: Given "build" lock has TTL expired, when `lokt why build` is run, then output notes it's expired, how long past expiry, and suggests `lokt unlock --break-stale build` or `lokt lock --wait build` (auto-prune), exit code 2
- [ ] **Dead PID (same host)**: Given "build" is held by a dead PID on same host, when `lokt why build` is run, then output notes PID is dead, and suggests `lokt unlock --break-stale build`, exit code 2
- [ ] **Corrupted lock file**: Given "build" lock file contains invalid JSON, when `lokt why build` is run, then output notes corruption and suggests `lokt unlock --force build`, exit code 2
- [ ] **JSON output**: Given any of the above scenarios, when `lokt why build --json` is run, then output is valid JSON with fields: name, status, reason, holder (if applicable), suggestions (array of recovery commands), and all relevant metadata
- [ ] **Exit codes consistent**: `why` uses exit 0 (lock free), exit 2 (lock blocked), exit 3 (invalid name / root not found), exit 64 (usage error)

## Edge Cases

- **Lock disappears between read and output** — race condition is acceptable; `why` is a best-effort snapshot. Print whatever was read.
- **Freeze lock exists but is expired** — auto-prune the freeze (consistent with guard behavior) and report lock as free (or report the underlying regular lock if one exists)
- **Lock held by current user/PID** — special case: output should note "you already hold this lock" instead of generic denial
- **Multiple issues (frozen + held + expired)** — report all issues, freeze first (highest priority blocker)
- **No lokt root found** — print clear error about missing root, suggest `lokt doctor`
- **Name validation failure** — reject invalid names with same rules as other commands

## Constraints

- **Read-only**: Must not modify any files (no lock mutations, no audit writes)
- **Consistent patterns**: Follow existing CLI patterns — `flag.NewFlagSet`, same exit codes, same error formatting style
- **Reuse existing packages**: Use `lockfile.Read()`, `stale.Check()`, `lock.CheckFreeze()`, `identity.Current()` — no duplicate logic
- **Single file addition**: Command implementation should live in `cmd/lokt/main.go` alongside other commands (following existing pattern)

---

## Technical Notes

### Existing Infrastructure to Reuse

| Need | Package | Function/Type |
|------|---------|---------------|
| Read lock | `internal/lockfile` | `Read()` returns `*Lock` |
| Check stale | `internal/stale` | `Check()` returns `Result{Stale, Reason}` |
| Check freeze | `internal/lock` | `CheckFreeze()` returns `FrozenError` |
| Current identity | `internal/identity` | `Current()` returns `Identity` |
| Root discovery | `internal/root` | `Discover()` returns root path |
| Lock path | `internal/lock` | `LockPath()` / path helpers |
| Name validation | `internal/lock` | `ValidateName()` |

### JSON Output Schema (Proposed)

```json
{
  "name": "build",
  "status": "blocked",
  "reasons": [
    {
      "type": "frozen",
      "message": "Frozen by ci@server for 2m30s (4m30s remaining)",
      "freeze_owner": "ci",
      "freeze_host": "server",
      "freeze_age_sec": 150,
      "freeze_remaining_sec": 270
    },
    {
      "type": "held",
      "message": "Held by alice@laptop (PID 12345) for 45s",
      "holder_owner": "alice",
      "holder_host": "laptop",
      "holder_pid": 12345,
      "holder_age_sec": 45,
      "holder_ttl_sec": 300,
      "holder_remaining_sec": 255,
      "holder_expired": false,
      "pid_status": "alive"
    }
  ],
  "suggestions": [
    "Wait for freeze to expire in 4m30s",
    "lokt unfreeze build",
    "lokt unfreeze --force build"
  ]
}
```

### Proposed Text Output Format

```
$ lokt why build

Lock "build" is BLOCKED:

  FROZEN by ci@server
    Age:       2m30s
    Remaining: 4m30s

  HELD by alice@laptop (PID 12345, alive)
    Age: 45s
    TTL: 5m (4m15s remaining)

Suggestions:
  - Wait for freeze to expire (4m30s remaining)
  - lokt unfreeze build          (if you are ci@server)
  - lokt unfreeze --force build  (break-glass)
```

```
$ lokt why build

Lock "build" is FREE — acquisition would succeed.
```

---

## Notes

- This is purely a UX/diagnostic feature. All data already exists in lock files and stale detection; this command is formatting and a new CLI entrypoint.
- The `--json` flag is critical for AI agent consumption. Agents need structured `reasons` and `suggestions` to make autonomous recovery decisions.
- Exit code 0 for "free" allows scripts to use `lokt why <name> && lokt lock <name>` as a check-then-acquire pattern (though this has TOCTOU limitations — document this).
- Consider whether `lokt why` should also be available as a library function (`lock.Why()`) for programmatic use inside guard, but this is optional and can be a follow-up.

---

**Next:** Run `/kickoff lokt-al5` to promote to Beads execution layer.
