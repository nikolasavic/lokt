# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Lokt is a file-based lock manager written in Go. It provides CLI commands for acquiring, releasing, and managing locks to coordinate concurrent processes (e.g., multiple AI agents working in the same repository).

## Build Commands

```bash
# Build
go build -o lokt ./cmd/lokt

# Build with version info (ldflags)
go build -ldflags "-X main.version=... -X main.commit=... -X main.date=..." -o lokt ./cmd/lokt

# Run tests
go test ./...

# Run single test
go test -run TestName ./path/to/package

# Lint
golangci-lint run
```

## Project Layout

```
cmd/lokt/       # CLI entrypoint
internal/       # Private packages (lock protocol, root discovery, etc.)
pkg/            # Public packages (if any)
```

## Architecture

### Root Discovery
Lock files are stored in a Lokt root directory, resolved in order:
1. `LOKT_ROOT` environment variable
2. Git common dir (`.git/lokt/` via `git rev-parse --git-common-dir`)
3. `.lokt/` in current working directory

### Lockfile Protocol
Locks are JSON files in `<root>/locks/<name>.json`:
```json
{"name":"...", "owner":"...", "host":"...", "pid":123, "acquired_ts":"...", "ttl_sec":300}
```

Acquisition uses `O_CREATE|O_EXCL` for atomic create-or-fail semantics with `fsync` for durability.

### Core Commands
- `lokt lock <name>` - Acquire lock atomically; print holder info on deny
- `lokt unlock <name>` - Release lock (owner-checked); `--force` for break-glass
- `lokt status [name]` - List locks with age/TTL/expired status; `--json` for machine output
- `lokt guard <name> -- <cmd...>` - Acquire, exec child, release on exit (propagate exit code)

### TTL & Staleness
Locks have optional TTL. Expired locks can be broken with `--break-stale`. On Unix, PID liveness (`kill(pid, 0)`) helps detect stale locks from crashed processes.

### Freeze Switch
`lokt freeze <name>` creates a special lock that blocks all `guard` commands for that name until `unfreeze` or TTL expiry.

### Audit Log
Append-only JSONL at `<root>/audit.log` with events: acquire, deny, release, force-break, etc.

## Key Conventions

- **Exit codes**: Consistent codes for held-by-other, not-found, expired, etc.
- **Lock names**: Validated pattern `[A-Za-z0-9._-]+`; reject `..` and absolute paths
- **Owner identity**: From `LOKT_OWNER` env, falling back to OS username
- **Atomic writes**: All file operations use atomic write + fsync pattern
