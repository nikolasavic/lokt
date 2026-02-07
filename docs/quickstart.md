# Lokt Quickstart

Lokt coordinates AI agents competing for shared resources in the same repo. Wrap any command in `lokt guard` and only one agent runs it at a time.

## Installation

```bash
# One-liner install (macOS + Linux, amd64/arm64)
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh

# Or build from source
go build -o lokt ./cmd/lokt
```

## Wrap Your First Command

The primary interface is `guard` — acquire a lock, run a command, release on exit:

```bash
lokt guard build --ttl 5m -- make build
```

This acquires the `build` lock, runs `make build`, and releases the lock when done — even if the command fails or is killed. The `--ttl 5m` sets a TTL so the lock auto-expires if the process hangs.

With `--ttl`, a background heartbeat renews the lock at TTL/2 intervals. A 5-minute TTL doesn't mean your build is capped at 5 minutes — it means the lock stays fresh as long as the process is alive.

## What Happens When Agents Collide

When a second agent tries to acquire a lock that's already held:

```bash
$ lokt guard build --ttl 5m -- make build
error: lock "build" held by agent-1@macbook (pid 48201) for 12s
```

Exit code 2 means "held by another." The agent gets a clear message: who holds it, their PID, and how long they've had it.

### Wait instead of failing

```bash
lokt guard build --ttl 5m --wait --timeout 10m -- make build
```

The agent blocks until the lock is free (or the timeout expires), with exponential backoff and jitter to avoid thundering herd.

## Agent Crashed — Now What?

When an agent crashes, its lock stays on disk. Lokt handles this automatically:

**Dead PID detection:** On the next acquire attempt, Lokt checks if the holder's PID is still alive. If the process is dead (same host), the stale lock is silently removed and the new agent acquires it.

**TTL expiry:** If the TTL has elapsed, the lock is considered expired. Any agent can break it:

```bash
# Remove only if stale (expired TTL or dead PID)
lokt unlock build --break-stale

# Force remove regardless (break-glass, use with caution)
lokt unlock build --force
```

**Auto-prune:** When listing locks, expired ones can be cleaned up automatically:

```bash
lokt status --prune-expired
```

## Manual Lock/Unlock

For cases where `guard` doesn't fit (multi-step workflows, cross-script coordination):

```bash
# Agent acquires the lock
lokt lock db-migrate --ttl 10m

# Do work...
./scripts/migrate.sh

# Release
lokt unlock db-migrate
```

Prefer `guard` when possible — it handles cleanup on exit automatically.

## Freeze — Human Override

Need to stop all agents from touching a resource? Freeze it:

```bash
# Block all guard commands for "deploy" for 30 minutes
lokt freeze deploy --ttl 30m

# Agents see:
# error: lock "deploy" is frozen until 2026-02-07T15:30:00Z

# Resume when ready
lokt unfreeze deploy
```

Freezes require a TTL — a forgotten freeze can't block agents forever.

## Audit — What Happened Overnight?

Every lock operation is logged to an append-only JSONL file. When 5 agents ran overnight and something broke:

```bash
# What happened in the last 8 hours?
lokt audit --since 8h

# Filter by lock name
lokt audit --name build --since 1h

# Follow in real-time
lokt audit --tail
```

Events include: acquire, deny, release, force-break, stale-break, renew, freeze, unfreeze.

## Lock Status and Diagnostics

```bash
# List all held locks
lokt status

# Single lock details
lokt status build

# Machine-readable
lokt status --json

# Explain why a lock can't be acquired
lokt why build

# Validate setup (directory, filesystem, clock)
lokt doctor
```

## Setting Up Agent Identity

Each agent identifies itself in lock files. By default, Lokt uses the OS username. To distinguish agents on the same machine:

```bash
export LOKT_OWNER="agent-1"
lokt guard build --ttl 5m -- make build
# Lock shows: agent-1@macbook
```

Set `LOKT_OWNER` in each agent's environment so locks and audit entries are clearly attributed.

## Where Locks Are Stored

Lokt discovers the lock directory in this order:

1. `$LOKT_ROOT` environment variable (if set)
2. `.git/lokt/` (git common dir — shared across worktrees)
3. `.lokt/` in the current directory

All agents in the same repo share the same lock namespace automatically. Git worktrees share the same lock directory via git's common dir.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Lock held by another owner (or frozen) |
| 3 | Lock not found |
| 4 | Not lock owner |

Use exit codes for scripting:

```bash
lokt lock build --ttl 5m
case $? in
    0) echo "Lock acquired, proceeding..." ;;
    2) echo "Build already running, skipping"; exit 0 ;;
    *) echo "Unexpected error"; exit 1 ;;
esac
```

## Next Steps

- [docs/patterns.md](patterns.md) — multi-agent workflow patterns (test sharding, script wrappers, resource pools)
