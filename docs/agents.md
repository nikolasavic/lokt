# Agent Integration Guide

## The Problem

When multiple AI agents work in the same repository, they step on each other.
Two agents run `make build` at the same time and corrupt the binary. Three agents
push to `main` simultaneously and trigger a cascade of rebase failures. An agent
runs a database migration while another is halfway through its own. There is no
human referee watching the terminal. Agents cannot be trusted to remember
conventions or coordinate through chat. The filesystem has to enforce the rules.

Lokt solves this by wrapping shared operations in file-based locks. One agent
runs the build; the second agent gets a clear rejection with the holder's name,
PID, and how long they have had the lock. When the first agent finishes (or
crashes), the lock releases automatically.

This guide covers integrating lokt into your AI agent workflow using `lokt prime`,
a command that teaches agents about lock coordination at the start of every
session.

---

## Quick Setup

There are two integration paths. Choose based on your tool.

### Automatic (Claude Code)

Claude Code supports session hooks that run commands at startup. With this
approach, `lokt prime` runs automatically at every session start and injects
live lock awareness into the agent's context.

**Step 1: Install lokt**

```bash
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh
```

Or build from source:

```bash
go build -o lokt ./cmd/lokt
```

**Step 2: Create wrapper scripts**

Create shell scripts that wrap your mutating operations with `lokt guard`.
See [Creating Wrapper Scripts](#creating-wrapper-scripts) below for templates.

**Step 3: Add the SessionStart hook**

Add the following to `.claude/settings.json` (project-level) or
`~/.claude/settings.json` (global):

```json
{
  "hooks": {
    "SessionStart": [
      {
        "type": "command",
        "command": "lokt prime"
      }
    ]
  }
}
```

**Step 4: Verify**

Start a new Claude Code session in the repository. You should see lokt
context injected into the conversation, including a table of guarded
operations, behavioral rules for lock denials, your agent identity, and a
live snapshot of currently held locks.

**What this gives you:**

- Every session starts with lokt awareness
- After context compaction, awareness is restored automatically on next session
- Live lock status visible at session start
- Zero maintenance -- `lokt prime` output evolves as you add wrapper scripts

### Manual (Cursor, Windsurf, Copilot, Cline, Aider)

For tools without session hooks, `lokt prime --format=<tool>` generates a
static snippet that you paste into the tool's configuration file.

**Step 1: Install lokt**

```bash
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh
```

**Step 2: Create wrapper scripts**

Create shell scripts that wrap your mutating operations with `lokt guard`.
See [Creating Wrapper Scripts](#creating-wrapper-scripts) below for templates.

**Step 3: Generate and paste the snippet**

Run the appropriate command and paste the output into your configuration file:

| Tool | Command | Config File |
|------|---------|-------------|
| Cursor | `lokt prime --format=cursorrules` | `.cursorrules` or `.cursor/rules/lokt.mdc` |
| Windsurf | `lokt prime --format=windsurfrules` | `.windsurfrules` |
| GitHub Copilot | `lokt prime --format=copilot` | `.github/copilot-instructions.md` |
| Cline | `lokt prime --format=clinerules` | `.clinerules/lokt.md` |
| Aider | `lokt prime --format=aider` | `.aider.conf.yml` |

Example for Cursor:

```bash
lokt prime --format=cursorrules >> .cursorrules
```

The snippet is additive -- paste it as a new section in your existing config
file. Do not replace the entire file.

**Step 4: Verify**

Start a new session with the agent and ask it to run a build. The agent
should use the wrapper script (e.g., `./scripts/build.sh`) instead of the
raw command (e.g., `make build`). If it uses the raw command, check that the
config file was saved and the snippet is present.

**Re-run after changes:** Unlike the hook path, the snippet is static. If
you add or rename wrapper scripts, re-run `lokt prime --format=<tool>` and
update the config file.

---

## Creating Wrapper Scripts

The key insight: agents should not need to know about lokt. Embed guards in
scripts they naturally call, and coordination becomes invisible and automatic.

### Template

Every wrapper script follows the same pattern:

```bash
#!/usr/bin/env bash
exec lokt guard <lock-name> --ttl <duration> -- <command> "$@"
```

The `exec` replaces the shell process with `lokt guard`, which acquires the
lock, runs the command, and releases the lock on exit -- even if the command
fails or is killed by a signal.

The `--ttl` flag sets a time-to-live on the lock. A background heartbeat
renews the lock at TTL/2 intervals while the command runs, so a 5-minute TTL
does not cap the command's runtime. It means the lock auto-expires if the
process hangs or the machine loses power.

### Example: Build

```bash
#!/usr/bin/env bash
# scripts/build.sh -- agents call this, lokt handles serialization
exec lokt guard build --ttl 5m -- make build "$@"
```

### Example: Safe Push

```bash
#!/usr/bin/env bash
# scripts/safe-push.sh -- serialize git push with pull-rebase
exec lokt guard git-push --ttl 2m -- bash -c 'git pull --rebase origin main && git push origin main'
```

### Example: Deploy

```bash
#!/usr/bin/env bash
# scripts/deploy.sh -- one deploy at a time
exec lokt guard deploy --ttl 30m -- ./scripts/_deploy-impl.sh "$@"
```

### Example: Database Migration

```bash
#!/usr/bin/env bash
# scripts/migrate.sh -- concurrent schema changes corrupt the database
exec lokt guard db-migrate --ttl 10m -- ./scripts/_migrate-impl.sh "$@"
```

### Example: Terraform Apply

```bash
#!/usr/bin/env bash
# scripts/terraform.sh -- state file conflicts are catastrophic
exec lokt guard terraform --ttl 15m -- terraform apply "$@"
```

After creating each script, make it executable:

```bash
chmod +x scripts/build.sh scripts/safe-push.sh scripts/deploy.sh
```

### Waiting Instead of Failing

By default, wrapper scripts fail immediately when the lock is held. If you
prefer agents to wait in line, add `--wait` and `--timeout`:

```bash
#!/usr/bin/env bash
# scripts/build.sh -- wait up to 10 minutes for the build lock
exec lokt guard build --ttl 5m --wait --timeout 10m -- make build "$@"
```

With `--wait`, the agent blocks with exponential backoff and jitter until
the lock is free or the timeout expires. This is useful when the operation
is required to proceed (e.g., deploy) rather than optional (e.g., lint).

Choose the right default for each operation:

| Operation | Recommended | Why |
|-----------|-------------|-----|
| Build | Fail-fast | Agent can do other work while waiting |
| Deploy | Wait with timeout | Deployment must eventually happen |
| Push | Fail-fast | Agent can rebase and retry manually |
| Migration | Wait with timeout | Migration is usually a prerequisite |

### How Auto-Discovery Works

`lokt prime` scans `scripts/`, `bin/`, `.github/scripts/`, and the project
root for `.sh` files containing `lokt guard`. It extracts the lock name and
guarded command and builds the wrapper table automatically. No configuration
file or registration step is needed.

When you add a new wrapper script, `lokt prime` picks it up on the next run.
For the hook path (Claude Code), this happens automatically at every session
start. For the snippet path, re-run `lokt prime --format=<tool>` to update.

---

## What to Guard

Not everything needs a lock. The decision depends on whether concurrent
execution causes corruption, wasted work, or external side effects.

### Decision Table

| Operation | Guard? | Why |
|-----------|--------|-----|
| Build | **Always** | Parallel builds corrupt shared output directories |
| `git push` | **Always** | Concurrent pushes cause non-fast-forward rejections and rebase wars |
| Deploy | **Always** | Two deploys create undefined state in production |
| DB migration | **Always** | Concurrent schema changes corrupt the database |
| Terraform / IaC | **Always** | State file conflicts are catastrophic and hard to recover from |
| `npm install` / `pip install` | **Always** | Concurrent writes to `node_modules/` or virtualenvs cause corruption |
| Lint / type-check | **Consider** | Expensive but idempotent -- use caching to deduplicate across agents |
| Test suite | **Consider** | Can shard across agents, or cache to skip repeated runs |
| `git status`, `git diff` | **Never** | Read-only operations, no conflicts possible |
| File reads, code edits | **Never** | Git handles merge conflicts at commit time |
| `git log`, `git blame` | **Never** | Read-only, safe for any number of concurrent agents |

### Guidance on "Consider" Operations

For lint and test, you have two options:

**Option A: Guard with caching.** Run once, cache the result, skip for
subsequent agents:

```bash
#!/usr/bin/env bash
# scripts/lint.sh
COMMIT=$(git rev-parse HEAD)
lokt exists "lint-$COMMIT" && { echo "Already linted"; exit 0; }
lokt guard lint --ttl 5m -- golangci-lint run
lokt lock "lint-$COMMIT" --ttl 24h
```

**Option B: Shard across agents.** Each agent claims a shard and runs a
subset of tests in parallel:

```bash
lokt guard test-shard-1 --ttl 5m -- go test ./internal/...
lokt guard test-shard-2 --ttl 5m -- go test ./cmd/...
```

See [patterns.md](patterns.md) for detailed caching and sharding patterns.

---

## Agent Identity

Each agent identifies itself in lock files and audit logs. Clear identity
makes it easy to tell which agent holds a lock and what happened overnight.

### Setting LOKT_OWNER

By default, lokt uses the OS username. When running multiple agents on the
same machine, set `LOKT_OWNER` to distinguish them:

```bash
export LOKT_OWNER="claude-1"
lokt guard build --ttl 5m -- make build
# Lock shows: claude-1@macbook
```

### Naming Conventions

Use `{tool}-{number}` for clarity:

| Agent | LOKT_OWNER |
|-------|------------|
| Claude Code (session 1) | `claude-1` |
| Claude Code (session 2) | `claude-2` |
| Cursor | `cursor-1` |
| Copilot | `copilot-1` |
| Aider | `aider-1` |

### Where Identity Appears

Identity shows up in three places:

**Lock denial messages:**

```
error: lock "build" held by claude-1@macbook (pid 48201) for 12s
```

**Status output:**

```bash
$ lokt status
build               claude-1@macbook  12s
deploy              cursor-1@macbook  3m22s
```

**Audit log:**

```bash
$ lokt audit --since 1h
{"ts":"...","event":"acquire","name":"build","owner":"claude-1","host":"macbook","pid":48201}
```

### Per-Worktree Identity

When using git worktrees for parallel agents, set a different `LOKT_OWNER`
in each worktree's environment. All worktrees share the same lock directory
(via git's common dir), so distinct identities are essential:

```bash
# Terminal 1 (main worktree)
export LOKT_OWNER="claude-1"

# Terminal 2 (second worktree)
export LOKT_OWNER="claude-2"
```

### Agent ID (Automatic)

In addition to `LOKT_OWNER`, lokt auto-generates a unique agent ID from the
process PID and start time (format: `agent-XXXX`). This distinguishes
concurrent processes with the same `LOKT_OWNER`. You can override it with
`LOKT_AGENT_ID` if needed, but the auto-generated value works for most
setups.

---

## What `lokt prime` Outputs

Understanding the output helps you verify the integration is working and
troubleshoot issues.

### Default Output (Hook Mode)

When `lokt prime` runs without `--format`, it produces dynamic markdown
designed for injection into an agent's context window:

```markdown
# Lokt Coordination Active

This repo uses lokt for lock coordination. Multiple agents share this workspace.

## Guarded Operations

Use these wrapper scripts instead of raw commands:

| Operation | Use this | NOT this |
|-----------|----------|----------|
| build | `./scripts/build.sh` | `make build` |
| git-push | `./scripts/safe-push.sh` | `git pull --rebase && git push` |
| deploy | `./scripts/deploy.sh` | `./scripts/_deploy-impl.sh` |

## If a command fails with "lock held by another"

Another agent is running the same operation. Do NOT retry immediately.
- If the task can wait: move to other work and come back later
- If urgent: tell the user the resource is locked

## Lock diagnostics

- `lokt status` -- see who holds what
- `lokt why <name>` -- explain why a lock can't be acquired

## Your Identity

You are: claude-1@macbook

## Current Status

- **build** held by cursor-1@macbook for 45s
```

The output is intentionally lean (under 300 words). Agents need a lookup
table and behavioral rules, not a tutorial.

### Snippet Mode (--format)

When `--format` is specified, the output is a static snippet tailored to
the target tool. It omits live status (no hook to refresh it) and adapts
to each tool's syntax and constraints:

| Format | Target File | Notes |
|--------|-------------|-------|
| `claude-md` | CLAUDE.md | Markdown section, ~500 chars |
| `cursorrules` | .cursorrules | Imperative rules, ~800 chars |
| `windsurfrules` | .windsurfrules | Ultra-compact, under 2000 chars |
| `copilot` | .github/copilot-instructions.md | Markdown section (same as claude-md) |
| `clinerules` | .clinerules/lokt.md | Markdown with YAML frontmatter |
| `aider` | .aider.conf.yml | YAML directives for lint-cmd/test-cmd |

### Example: cursorrules Snippet

Running `lokt prime --format=cursorrules` in a project with wrapper scripts
produces output like:

```
# Lokt Lock Coordination

This project uses lokt for lock coordination. Multiple agents share this workspace.

MANDATORY: Use wrapper scripts for all mutating shared operations:
- ALWAYS use `./scripts/build.sh` instead of `make build`
- ALWAYS use `./scripts/safe-push.sh` instead of `git pull --rebase && git push`

If a command fails with "lock held by another", do NOT retry immediately.
Move to other work and come back later, or tell the user the resource is locked.

Lock diagnostics: `lokt status` or `lokt why <name>`
```

### Hook vs. Snippet: Which to Choose

| | Hook (automatic) | Snippet (manual) |
|------|-----------------|------------------|
| **Setup** | One-time hook config | Paste into config file |
| **Maintenance** | Zero -- auto-updates | Re-run after wrapper changes |
| **Live status** | Yes (current locks shown) | No (static text) |
| **Context freshness** | Re-injected every session | Persists until you edit |
| **Supported tools** | Claude Code | Cursor, Windsurf, Copilot, Cline, Aider |

---

## Human Controls

Lokt gives humans three levers to manage running agents: a kill switch, an
audit trail, and a status dashboard.

### Kill Switch (Freeze)

Need to stop all agents from touching a resource immediately? Freeze it:

```bash
lokt freeze deploy --ttl 30m
```

Every `lokt guard deploy` call now fails instantly with exit code 2:

```
error: lock "deploy" is frozen until 2026-02-07T15:30:00Z
```

Agents get a clear message and can move to other work. When you are ready
to resume:

```bash
lokt unfreeze deploy
```

Freezes require a TTL -- a forgotten freeze cannot block agents forever.
If you walk away, the freeze expires automatically.

### Audit Trail

Every lock operation is logged to an append-only JSONL file. When five
agents ran overnight and something broke:

```bash
# What happened in the last 8 hours?
lokt audit --since 8h

# Filter by lock name
lokt audit --name deploy --since 1h

# Follow in real-time while agents are running
lokt audit --tail
```

Events include: `acquire`, `deny`, `release`, `force-break`, `stale-break`,
`renew`, `freeze`, `unfreeze`.

### Status Dashboard

See who holds what right now:

```bash
# All held locks
lokt status

# Single lock details (owner, PID, age, TTL, expiry)
lokt status build

# Machine-readable for scripting
lokt status --json

# Explain why a lock cannot be acquired
lokt why build

# Clean up expired locks
lokt status --prune-expired
```

### Validate Setup

If anything seems wrong, run the health check:

```bash
lokt doctor
```

This validates the lokt root directory, filesystem writability, and clock
sanity.

---

## Troubleshooting

### 1. Agent ignores locks and calls raw commands

**Symptom:** The agent runs `make build` directly instead of
`./scripts/build.sh`.

**Cause:** The wrapper table is not in the agent's context. Either the
hook is not configured (Claude Code) or the snippet is missing from the
config file (other tools).

**Fix:**

- Claude Code: Verify `.claude/settings.json` contains the SessionStart
  hook. Start a new session and look for "Lokt Coordination Active" in the
  context.
- Other tools: Re-run `lokt prime --format=<tool>` and verify the output
  is present in the config file.
- Verify wrapper scripts exist and contain `lokt guard`:

```bash
grep -r "lokt guard" scripts/
```

### 2. Lock stuck from a crashed agent

**Symptom:** `lokt status` shows a lock held by an agent that is no longer
running. New agents fail with "lock held by another."

**Cause:** The agent's process died without releasing the lock. This
happens when a terminal is closed, the machine reboots, or the agent
process is killed.

**Fix (automatic):** Lokt detects dead PIDs on the same host automatically.
The next `lokt guard` or `lokt lock` call will silently remove the stale
lock and acquire it.

**Fix (manual):** If the dead PID is not detected (e.g., the lock was
created on a different host):

```bash
# Remove only if stale (expired TTL or dead PID)
lokt unlock build --break-stale

# Force remove regardless (break-glass, use with caution)
lokt unlock build --force
```

**Fix (diagnostic):**

```bash
lokt why build
```

This explains the exact reason the lock is held and suggests specific
commands to resolve it.

### 3. Agent waits forever for a lock

**Symptom:** An agent using `--wait` appears stuck and never proceeds.

**Cause:** The lock holder is alive and still running, or a freeze is
active on the lock name.

**Fix:**

- Check what is blocking: `lokt why <name>`
- Check for forgotten freezes: `lokt status` (look for `[FROZEN]` entries)
- If frozen: `lokt unfreeze <name>`
- If the holder is alive but should not be: terminate the holder process,
  then the lock releases automatically
- Add `--timeout` to `--wait` to prevent indefinite blocking:

```bash
lokt guard build --ttl 5m --wait --timeout 10m -- make build
```

### 4. Multiple agents have the same identity

**Symptom:** Audit logs and lock denials show the same owner for different
agents. Ownership checks behave unexpectedly.

**Cause:** `LOKT_OWNER` is not set, so all agents use the OS username.
Or multiple agents share the same `LOKT_OWNER` value.

**Fix:** Set a unique `LOKT_OWNER` in each agent's environment:

```bash
# Agent 1
export LOKT_OWNER="claude-1"

# Agent 2
export LOKT_OWNER="claude-2"
```

Note that even with the same `LOKT_OWNER`, lokt auto-generates a unique
agent ID per process (`agent-XXXX` based on PID and start time). Ownership
checks use owner + host + PID, so two agents with the same `LOKT_OWNER`
will not accidentally release each other's locks. But distinct names make
diagnostics and audit logs much clearer.

### 5. Lokt root not found

**Symptom:** Any lokt command fails with "lokt root not found."

**Cause:** Lokt cannot find its root directory. It looks in this order:
`$LOKT_ROOT` environment variable, then `.git/lokt/` (git common dir),
then `.lokt/` in the current directory.

**Fix:**

```bash
# Diagnose the issue
lokt doctor

# If in a git repo, lokt creates .git/lokt/ on first use.
# Verify you are inside a git repository:
git rev-parse --git-dir

# If not in a git repo, set LOKT_ROOT explicitly:
export LOKT_ROOT=/path/to/shared/.lokt
```

---

## Exit Codes Reference

Scripts and agents can use exit codes to handle lock outcomes programmatically:

| Code | Meaning | Typical Action |
|------|---------|----------------|
| 0 | Success | Continue |
| 1 | General error | Abort and report |
| 2 | Lock held by another owner (or frozen) | Wait, skip, or notify user |
| 3 | Lock not found | Create or ignore |
| 4 | Not lock owner | Use `--force` if authorized |

Example:

```bash
lokt lock build --ttl 5m
case $? in
    0) echo "Lock acquired, proceeding..." ;;
    2) echo "Build already running, skipping"; exit 0 ;;
    *) echo "Unexpected error"; exit 1 ;;
esac
```

---

## Complete Example: Two-Agent Setup

This walkthrough sets up two Claude Code agents coordinating in the same
repository. The entire process takes under five minutes.

### 1. Install lokt

```bash
curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh
```

### 2. Create wrapper scripts

```bash
mkdir -p scripts

cat > scripts/build.sh << 'EOF'
#!/usr/bin/env bash
exec lokt guard build --ttl 5m -- make build "$@"
EOF

cat > scripts/safe-push.sh << 'EOF'
#!/usr/bin/env bash
exec lokt guard git-push --ttl 2m -- bash -c 'git pull --rebase origin main && git push origin main'
EOF

chmod +x scripts/build.sh scripts/safe-push.sh
```

### 3. Configure the SessionStart hook

```bash
mkdir -p .claude

cat > .claude/settings.json << 'EOF'
{
  "hooks": {
    "SessionStart": [
      {
        "type": "command",
        "command": "lokt prime"
      }
    ]
  }
}
EOF
```

### 4. Set agent identities

In the first terminal:

```bash
export LOKT_OWNER="claude-1"
```

In the second terminal:

```bash
export LOKT_OWNER="claude-2"
```

### 5. Test the collision

In terminal 1, hold a build lock:

```bash
lokt guard build --ttl 5m -- sleep 30
```

In terminal 2, try the same:

```bash
./scripts/build.sh
```

Terminal 2 should see:

```
error: lock "build" held by claude-1@<hostname> (pid XXXXX) for Xs
```

When terminal 1's command finishes, terminal 2 can acquire the lock.

### 6. Verify audit

```bash
lokt audit --since 5m
```

You should see `acquire` and `deny` events with the correct owner
identities.

---

## Platform Notes

Lokt requires a Unix-like operating system. It is tested on macOS and Linux
(amd64 and arm64). PID liveness detection and atomic file operations rely on
POSIX semantics.

**Windows:** Native Windows is not supported. WSL (Windows Subsystem for
Linux) works -- run lokt and your agents inside WSL.

**Network filesystems (NFS, SMB):** Lokt relies on `O_CREATE|O_EXCL`
atomicity, which some network filesystems do not guarantee. Run
`lokt doctor` to check. Local filesystems (ext4, APFS, HFS+) are fully
supported.

**Monorepos:** Each lokt root has its own lock namespace. In a monorepo,
all agents share one namespace. Wrapper scripts in different directories
with different lock names work naturally -- `lokt prime` discovers them
all.

---

## Next Steps

- [quickstart.md](quickstart.md) -- installation, basic usage, guard,
  lock/unlock, freeze, audit
- [patterns.md](patterns.md) -- test sharding, cache checking, script
  wrappers, resource pools, CI patterns
- `lokt --help` -- full command reference
- `lokt doctor` -- validate your setup
