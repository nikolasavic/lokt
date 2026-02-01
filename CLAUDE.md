# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Lokt is a file-based lock manager written in Go. It provides CLI commands for acquiring, releasing, and managing locks to coordinate concurrent processes (e.g., multiple AI agents working in the same repository).

## Development Workflow

This project uses beads for task tracking. For most changes, follow this flow:

```
┌─────────────────────────────────────────────────────────────┐
│  1. Pick work from bd ready or user request                 │
│                         ↓                                   │
│  2. Implement (small changes) or /plan (complex changes)    │
│                         ↓                                   │
│  3. Test: go test ./... && golangci-lint run                │
│                         ↓                                   │
│  4. /commit (APPROVAL checkpoint)                           │
│                         ↓                                   │
│  5. bd sync --flush-only                                    │
└─────────────────────────────────────────────────────────────┘
```

### When to Plan vs Just Implement

**Use /plan or EnterPlanMode for:**
- New commands or subcommands
- Changes affecting multiple packages
- Protocol changes (lockfile format, audit log schema)
- Anything touching atomic file operations or concurrency

**Skip planning for:**
- Single-file bug fixes with clear scope
- Documentation updates
- Flag additions to existing commands
- Test additions for existing behavior
- Direct user requests with explicit instructions

**When in doubt:** Ask the user if they want planning or a quick fix.

### Agent Behavior

The agent should guide the workflow, not just respond:

1. **Before implementing**: For non-trivial changes, suggest planning first
2. **After completing work**: Run tests and lint before offering to commit
3. **When skipping steps**: Briefly note why (e.g., "Skipping /plan for this flag addition")

### Agent Rules (MUST follow)

**Mandatory Checkpoints (require human approval):**

| Action | Correct Approach | NEVER Do |
|--------|------------------|----------|
| Commits | Use `/commit` skill | Raw `git commit` |
| Push to remote | Ask user to approve | Raw `git push` |
| Breaking changes | Discuss impact first | Silent protocol changes |

**Lint-on-Edit (MANDATORY before commit/push):** After every Go code change, run both:
```bash
go test ./...        # Run tests
golangci-lint run    # Lint + format check (includes gofmt, goimports)
```
Never commit or push without both passing. No exceptions.

## Tooling

### Git Hooks

Hooks are versioned in `util/hooks/` and enforce quality on commit:

| Hook | Checks |
|------|--------|
| `pre-commit` | gofmt, golangci-lint, beads flush |
| `commit-msg` | Conventional commit format, blocks LLM ads |
| `post-merge` | Beads sync after pull |

```bash
./util/install-hooks.sh           # Install hooks
./util/install-hooks.sh status    # Show status
./util/install-hooks.sh uninstall # Remove hooks
```

New clones should run `./util/install-hooks.sh` after cloning.

### Build Script

For versioned builds with ldflags:
```bash
./scripts/build.sh          # Build with git describe version
./scripts/build.sh v1.0.0   # Build with explicit version
```

### Linter Configuration

Project uses `.golangci.yml` (v2 format) with golangci-lint v2.

**Linters** (on top of defaults): errname, errorlint, gocritic, gosec, misspell, revive, unconvert

**Formatters** (run as part of `golangci-lint run`): gofmt, goimports

### Editor Config

`.editorconfig` ensures consistent formatting:
- Go: tabs
- YAML/JSON/Markdown: 2 spaces
- Shell: 4 spaces

### Beads Task Tracking

This project uses **beads** (`bd`) for task tracking:

```bash
bd ready                    # Find available work (no blockers)
bd create --title="..." --type=task --priority=2  # Create task
bd show <id>                # View issue details
bd update <id> --status=in_progress  # Claim work
bd close <id>               # Complete work
bd sync --flush-only        # Export to JSONL
```

**Priority values:** 0-4 (0=critical, 2=medium, 4=backlog). NOT "high"/"medium"/"low".

## Build Commands

```bash
# Build (ALWAYS use the build script - includes version info + lock)
./scripts/build.sh

# Run tests
go test ./...

# Run single test
go test -run TestName ./path/to/package

# Lint + format (MUST pass before commit/push)
golangci-lint run
```

**Important:** Always use `./scripts/build.sh` for builds. The script:
1. Embeds version/commit/date via ldflags
2. Auto-acquires `build` lock via lokt guard (prevents concurrent builds)
3. First build bootstraps without lock (lokt doesn't exist yet)

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
{"version":1, "name":"...", "owner":"...", "host":"...", "pid":123, "acquired_ts":"...", "ttl_sec":300, "expires_at":"..."}
```

Fields: `version` (always 1), `name`, `owner`, `host`, `pid`, `pid_start_ns` (omitempty), `acquired_ts` (RFC3339), `ttl_sec` (omitempty, 0 = no expiry), `expires_at` (omitempty, computed as `acquired_ts + ttl_sec` at write time).

Acquisition uses `O_CREATE|O_EXCL` for atomic create-or-fail semantics with `fsync` for durability.

### Core Commands
- `lokt lock <name>` - Acquire lock atomically; print holder info on deny
- `lokt unlock <name>` - Release lock (owner-checked); `--force` for break-glass
- `lokt status [name]` - List locks with age/TTL/expired status; `--json` for machine output
- `lokt guard <name> -- <cmd...>` - Acquire, exec child, release on exit (propagate exit code)

### TTL & Staleness
Locks have optional TTL. When TTL is set, `expires_at` is computed at write time and stored in the lockfile. Expiry checks use the explicit `expires_at` timestamp (`time.Now().After(expires_at)`) when present, falling back to `acquired_ts + ttl_sec` arithmetic for old lockfiles.

Expired locks can be broken with `--break-stale`. On Unix, PID liveness (`kill(pid, 0)`) helps detect stale locks from crashed processes.

**Clock skew and monotonic time**: The `expires_at` and `acquired_ts` fields use wall-clock time (RFC3339). Within a single Go process, `time.Since()` uses the monotonic clock component and is safe from NTP adjustments. However, cross-process stale detection compares deserialized wall-clock values and is susceptible to NTP jumps or clock skew between hosts. This is an inherent limitation of file-based coordination. Mitigations: PID liveness detection catches crashed processes on the same host; guard heartbeat renewal (`TTL/2` interval) keeps live locks fresh; `--wait` retries with backoff until the lock becomes available.

### Freeze Switch
`lokt freeze <name>` creates a special lock that blocks all `guard` commands for that name until `unfreeze` or TTL expiry.

### Audit Log
Append-only JSONL at `<root>/audit.log` with events: acquire, deny, release, force-break, etc.

## Key Conventions

- **Exit codes**: Consistent codes for held-by-other, not-found, expired, etc.
- **Lock names**: Validated pattern `[A-Za-z0-9._-]+`; reject `..` and absolute paths
- **Owner identity**: From `LOKT_OWNER` env, falling back to OS username
- **Atomic writes**: All file operations use atomic write + fsync pattern

## Git Workflow

**This is a trunk-based development project.** Commit directly to `main`. No feature branches.

### Trunk-Based Rules (MUST follow)

1. **Commit directly to main** - no feature branches, no PRs
2. **Push to main** - `git push origin main` after commits
3. **For parallel agents** - use git worktrees, not branches

```bash
# Single agent workflow
git pull --ff-only origin main
# ... do work, commit ...
git push origin main

# Multiple agents (use worktrees for isolation)
git worktree add ../lokt-agent-2 main
cd ../lokt-agent-2
# ... work in separate directory, same main branch ...
```

### Other Rules

- **Conventional commits required** - see format below
- Keep commits atomic and focused
- Use `/commit` skill for proper formatting

### Conventional Commit Format

```
<type>(<scope>): <description>

[optional body]
```

**Types:**
| Type | Use for |
|------|---------|
| `feat` | New feature or capability |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `test` | Adding or updating tests |
| `chore` | Maintenance (deps, config, scripts) |

**Scope** (optional): `cli`, `lock`, `guard`, `stale`, `audit`, `root`

**Examples:**
```
feat(cli): add --break-stale flag to unlock command
fix(lock): handle race condition in atomic acquire
docs: update CLAUDE.md with workflow guidance
refactor(guard): extract signal handling to helper
test(stale): add PID liveness edge cases
chore(deps): update cobra to v1.8.0
```
