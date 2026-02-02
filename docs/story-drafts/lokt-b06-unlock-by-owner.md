# lokt-b06: Release locks by owner (unlock --owner / --all)

## Problem

When an AI agent session ends (crashes, times out, or completes), it may leave behind multiple locks. Currently, cleanup requires knowing every lock name and issuing individual `lokt unlock` commands. There's no way to say "release everything I own" or "release everything owned by session:AAA".

This matters for multi-agent workflows where each agent uses a unique `LOKT_OWNER` identity (e.g., `session:abc123`) and the orchestrator needs single-command cleanup.

## Requirements

### Two forms of owner-based release

1. **Explicit owner filter:** `lokt unlock --owner <owner-name>`
   - Scans all locks, releases those matching the given owner
   - Does not require `LOKT_OWNER` to be set
   - Works for cleanup by orchestrators

2. **Current identity shorthand:** `lokt unlock --all`
   - Releases all locks owned by the current identity (from `LOKT_OWNER` env or OS username)
   - Convenience for "clean up after myself"

### Behavior details

- Both forms iterate over all `.json` files in `<root>/locks/`
- Each matching lock is released normally (same as single `lokt unlock <name>`)
- Each release emits an audit event
- Non-matching locks are silently skipped
- If no locks match, exit 0 with message "no locks matched"
- Report count of released locks: "released N lock(s)"
- `--json` flag outputs structured result

### Constraints

- `--owner` and `--all` are mutually exclusive
- `--owner`/`--all` cannot be combined with a positional lock name
- `--force` and `--break-stale` do NOT combine with `--owner`/`--all` (too dangerous for batch operations)
- Owner matching is exact string comparison (no wildcards)

## Acceptance Criteria

- [ ] `lokt unlock --owner session:AAA` releases all locks with owner=session:AAA
- [ ] `LOKT_OWNER=session:AAA lokt unlock --all` releases all locks owned by current identity
- [ ] Each released lock emits an audit event
- [ ] Output shows count of released locks
- [ ] Exit 0 when no locks match (informational, not error)
- [ ] `--json` outputs `{"released": ["name1", "name2"], "count": N}`
- [ ] Mutual exclusion: --owner vs --all vs positional name
- [ ] --force/--break-stale rejected when used with --owner/--all
- [ ] Tests cover: match-all, match-some, match-none, mutual exclusion errors

## Edge Cases

- Locks directory doesn't exist (no locks) → exit 0, "no locks matched"
- Lock file becomes unreadable during iteration → skip with stderr warning, continue
- Lock released by another process mid-iteration → skip (already gone), don't count
- Corrupted lock file during iteration → skip with warning
- `--all` with no LOKT_OWNER and unknown OS user → uses "unknown" as owner (same as identity.Current())

## Verification

- Level: agent
- Environments: local
