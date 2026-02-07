# Lokt

File-based lock coordination for AI agent swarms.

Multiple AI agents working in the same repo will step on each other — parallel builds corrupt artifacts, concurrent pushes trigger rebase wars, simultaneous migrations break schemas. There's no human referee. Agents can't be trusted to "remember" conventions. The filesystem has to say no.

Lokt is a zero-infrastructure CLI that serializes access to shared resources using atomic lockfiles. It handles TTL expiry, crash recovery via PID liveness detection, and logs every operation to an append-only audit trail.

## 30-Second Quickstart

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh

# Wrap any command — only one agent runs at a time
lokt guard build --ttl 5m -- make build

# Another agent tries the same thing:
# error: lock "build" held by agent-1@macbook (pid 48201) for 12s
```

That's it. The first agent runs. The second agent gets a clear rejection with who holds the lock and for how long. When the first agent finishes (or crashes), the lock releases.

## How It Works

Locks are JSON files under your repo's `.git/lokt/` directory. All terminals and agents in the same repo share the same lock namespace automatically.

- **Atomic acquisition** — `O_CREATE|O_EXCL` ensures exactly one winner
- **TTL + heartbeat** — `guard --ttl` auto-renews at TTL/2 so long builds don't lose their lock
- **Crash recovery** — dead PID detection auto-prunes locks from crashed agents
- **Audit trail** — every acquire, deny, release, and break logged to JSONL

## Commands

```
lokt guard <name> -- <cmd>     Acquire lock, run command, release on exit
lokt lock <name>               Acquire a lock
lokt unlock <name>             Release a lock
lokt status [name]             Show held locks
lokt why <name>                Explain why a lock can't be acquired
lokt exists <name>             Silent lock check (exit code only)
lokt freeze <name> --ttl 15m   Block all guard commands for a name
lokt unfreeze <name>           Remove a freeze
lokt audit                     Query the audit log
lokt doctor                    Validate lokt setup
```

### Key Flags

```
--ttl <duration>     Lock lifetime (e.g., 5m, 1h). Auto-renews under guard.
--wait               Block until the lock is free instead of failing immediately.
--timeout <duration> Maximum wait time (with --wait).
--break-stale        Remove a lock only if it's expired or the holder is dead.
--force              Break-glass removal, no ownership check.
--json               Machine-readable output.
```

## Common Patterns

### Serialize builds across agents

```bash
lokt guard build --ttl 5m -- make build
```

### Serialize git push (prevent rebase races)

```bash
lokt guard git-push --ttl 2m -- git pull --rebase && git push
```

### Embed in scripts — agents don't need to know about lokt

```bash
#!/usr/bin/env bash
# scripts/build.sh — agents call this, lokt handles serialization
exec lokt guard build --ttl 5m -- make build
```

### Freeze during incidents

```bash
lokt freeze deploy --ttl 30m    # block all deploy guards
lokt unfreeze deploy             # resume when ready
```

### Audit what happened overnight

```bash
lokt audit --since 8h
lokt audit --name build --since 1h
```

See [docs/quickstart.md](docs/quickstart.md) for full usage guide and [docs/patterns.md](docs/patterns.md) for multi-agent workflow patterns.

## When to Use Lokt

| Scenario | Lokt? | Alternative |
|----------|-------|-------------|
| Multiple AI agents, same machine | **Yes** | Nothing else fits |
| Parallel terminals, local dev | Yes | `flock` if you don't need TTL/audit |
| CI job concurrency | No | GitHub Actions `concurrency:` groups |
| Distributed locking across hosts | No | etcd, Redis, ZooKeeper |
| Terraform state locking | No | Terraform backends |
| Cron job dedup | Maybe | `flock` is simpler |

## Installation

```bash
# One-liner install (macOS + Linux, amd64/arm64)
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh

# Pin a version
LOKT_VERSION=v0.3.0 curl -fsSL .../install.sh | sh

# Build from source
go build -o lokt ./cmd/lokt
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Lock held by another owner (or frozen) |
| 3 | Lock not found |
| 4 | Not lock owner |

## Philosophy

- **Agents can't remember conventions** — the filesystem enforces them
- **Crash recovery is non-negotiable** — agents die, locks must expire
- **Zero infrastructure** — no daemon, no server, no config file
- **Audit by default** — when 5 agents ran overnight, you need to know what happened
