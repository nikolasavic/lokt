# lokt-bzz: Move freeze files to separate freezes/ namespace

status: draft
created: 2026-02-01
backlog-ref: docs/golive.md (M0: Protocol Freeze)

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Freeze files currently live in the `locks/` directory with a `freeze-` prefix
(`locks/freeze-deploy.json`). Since lock name validation allows hyphens
(`[A-Za-z0-9._-]+`), a user can run `lokt lock freeze-deploy` and it creates
`locks/freeze-deploy.json` — the exact same path that `lokt freeze deploy`
writes to.

This is a namespace collision in a mutual-exclusion tool. The consequences:

1. `lokt lock freeze-deploy` silently creates what looks like a freeze lock
2. `lokt status` shows it with `[FROZEN]` marker (because `IsFreezeLock`
   checks the prefix, not the creation path)
3. `lokt guard deploy` sees the "freeze" and refuses to run, even though
   no one actually froze anything
4. `lokt unfreeze deploy` can remove a legitimate user lock

This is a protocol-level design flaw, not a code bug. It must be fixed before
v1.0 because the directory layout is a public contract.

## Users

- **CI pipeline operator**: Runs `lokt freeze deploy --ttl 30m` during
  maintenance. Needs confidence that freeze blocks exactly what it should,
  nothing more.
- **Developer using lock names freely**: Shouldn't have to know that "freeze-"
  is a reserved prefix. Names like `freeze-ci`, `freeze-old` are natural.
- **Tooling author**: Enumerating all freezes should be a directory listing
  of `freezes/`, not "scan `locks/` and filter by prefix."

## Requirements

1. **Separate `freezes/` directory**: Freeze files stored at
   `<root>/freezes/<name>.json` instead of `<root>/locks/freeze-<name>.json`.
   The filename is the user-provided name (no prefix).

2. **Remove `FreezePrefix` coupling**: The `freeze-` prefix becomes an
   internal migration detail, not a runtime concept. `IsFreezeLock()` is
   replaced by checking which directory the file came from.

3. **`EnsureDirs` creates both directories**: `<root>/locks/` and
   `<root>/freezes/` are created together on first use.

4. **Backward compatibility (transition period)**: On read paths
   (`CheckFreeze`, `Unfreeze`, `status`), check the new location first,
   then fall back to the old `locks/freeze-<name>.json` location. On write
   paths (`Freeze`), always write to the new location. This lets users
   upgrade without manually migrating existing freeze files.

5. **`lokt doctor` reports stale legacy freeze files**: If any
   `locks/freeze-*.json` files exist, doctor emits a warning suggesting
   they be cleaned up (they'll expire via TTL naturally).

6. **Audit events unchanged**: Freeze audit events already use the
   unprefixed name (`deploy`, not `freeze-deploy`), so no audit schema
   change is needed.

## Non-Goals

- **Automated migration command**: Freeze files have TTLs and expire
  naturally. A migration tool isn't worth the complexity. The fallback
  read path handles the transition.
- **Rejecting `freeze-*` lock names**: Tempting, but unnecessary once
  namespaces are separate. Users should be able to name locks whatever
  they want.
- **Changing freeze semantics**: This is purely a storage layout change.
  Freeze behavior (TTL required, guard blocking, auto-prune expired)
  stays identical.

## Acceptance Criteria

- [ ] **New directory created**: Given a fresh lokt root, when any command
  runs, then both `<root>/locks/` and `<root>/freezes/` directories exist.

- [ ] **Freeze writes to new location**: Given `lokt freeze deploy --ttl 15m`,
  when the command succeeds, then `<root>/freezes/deploy.json` exists and
  `<root>/locks/freeze-deploy.json` does not.

- [ ] **Unfreeze reads new location**: Given an active freeze at
  `<root>/freezes/deploy.json`, when `lokt unfreeze deploy` runs, then the
  file is removed from `freezes/`.

- [ ] **Fallback reads old location**: Given a legacy freeze at
  `<root>/locks/freeze-deploy.json` (and nothing in `freezes/`), when
  `lokt guard deploy` runs, then it is still blocked by the freeze.

- [ ] **Fallback unfreeze**: Given a legacy freeze at
  `<root>/locks/freeze-deploy.json`, when `lokt unfreeze deploy` runs,
  then the legacy file is removed.

- [ ] **No collision**: Given `lokt lock freeze-deploy` followed by
  `lokt freeze deploy --ttl 5m`, when both succeed, then two separate files
  exist: `locks/freeze-deploy.json` (regular lock) and `freezes/deploy.json`
  (freeze lock). `lokt status` shows them as distinct entries.

- [ ] **Status lists from both directories**: Given locks in `locks/` and
  freezes in `freezes/`, when `lokt status` runs, then both are shown with
  correct type markers (`[FROZEN]` for freezes only).

- [ ] **Status JSON distinguishes source**: Given freeze locks in `freezes/`,
  when `lokt status --json` runs, then freeze entries have `"freeze": true`.

- [ ] **Doctor warns on legacy files**: Given `locks/freeze-deploy.json`
  exists, when `lokt doctor` runs, then output includes a warning about
  legacy freeze files.

- [ ] **Guard checks new location first**: Given a freeze at
  `<root>/freezes/deploy.json`, when `lokt guard deploy -- cmd` runs, then
  it is blocked with FrozenError.

## Edge Cases

- **Both locations have a freeze**: New location wins. Legacy is ignored
  (stale from pre-upgrade). The new-path freeze is authoritative.

- **Legacy freeze expired but new freeze active**: CheckFreeze finds
  the new-path freeze, returns FrozenError. Legacy is irrelevant.

- **`freezes/` directory missing (old lokt binary wrote locks only)**:
  `CheckFreeze` handles missing directory gracefully (returns nil, same
  as "no freeze"). `EnsureDirs` creates it when needed.

- **Permissions differ between `locks/` and `freezes/`**: Both created
  with 0700, same as today.

- **Concurrent freeze + legacy fallback**: Atomic O_CREATE|O_EXCL
  on the new path. No TOCTOU with the legacy path because we only
  *read* legacy, never write.

## Constraints

- **Atomic file operations**: Same temp+rename+fsync pattern used today.
- **No new dependencies**: Pure filesystem operations.
- **Backward compat window**: Legacy fallback can be removed in v2.0.
  For v1.0 it stays.
- **Must be deployed with M0 (Protocol Freeze)**: This changes the
  on-disk layout, which is part of the public contract.

---

## Implementation Sketch

### internal/root/root.go

```go
const (
    LocksDir   = "locks"
    FreezesDir = "freezes"  // NEW
)

func EnsureDirs(root string) error {
    if err := os.MkdirAll(filepath.Join(root, LocksDir), 0700); err != nil {
        return err
    }
    return os.MkdirAll(filepath.Join(root, FreezesDir), 0700)
}

// NEW
func FreezesPath(root string) string {
    return filepath.Join(root, FreezesDir)
}

// NEW
func FreezeFilePath(root, name string) string {
    return filepath.Join(root, FreezesDir, name+".json")
}
```

### internal/lock/freeze.go

Key changes:
- `Freeze()`: write to `root.FreezeFilePath(rootDir, name)` instead of
  `root.LockFilePath(rootDir, FreezePrefix+name)`
- `Unfreeze()`: check new path first, fall back to legacy path
- `CheckFreeze()`: check new path first, fall back to legacy path
- `IsFreezeLock()`: deprecated or removed — status command checks source
  directory instead
- `FrozenError.Error()`: no longer strips `FreezePrefix` from name
  (name is already clean)

### Legacy fallback pattern (used in CheckFreeze, Unfreeze):

```go
func freezePath(rootDir, name string) string {
    return root.FreezeFilePath(rootDir, name)
}

func legacyFreezePath(rootDir, name string) string {
    return root.LockFilePath(rootDir, FreezePrefix+name)
}

func readFreezeFile(rootDir, name string) (*lockfile.Lock, string, error) {
    // Try new location first
    path := freezePath(rootDir, name)
    lk, err := lockfile.Read(path)
    if err == nil {
        return lk, path, nil
    }
    if !os.IsNotExist(err) {
        return nil, path, err
    }
    // Fall back to legacy location
    path = legacyFreezePath(rootDir, name)
    lk, err = lockfile.Read(path)
    return lk, path, err
}
```

### cmd/lokt/main.go (status command)

Status currently scans `locks/` and uses `IsFreezeLock()` to detect freezes.
After this change:
1. Scan `locks/` — all entries are regular locks
2. Scan `freezes/` — all entries are freeze locks
3. For backward compat: entries in `locks/` with `freeze-` prefix are
   shown with a `[LEGACY FREEZE]` marker (or just `[FROZEN]`)

### Files touched

| File | Change |
|------|--------|
| `internal/root/root.go` | Add `FreezesDir`, `FreezesPath()`, `FreezeFilePath()`, update `EnsureDirs()` |
| `internal/lock/freeze.go` | Rewrite path resolution, add fallback, update `FrozenError`, deprecate `IsFreezeLock()` |
| `cmd/lokt/main.go` | Status scans both dirs, freeze/unfreeze use new paths |
| `internal/lock/freeze_test.go` | Update all path assertions, add collision test, add legacy fallback tests |
| `cmd/lokt/status_test.go` | Update freeze detection tests |
| `cmd/lokt/why_test.go` | Update freeze-related test assertions |
| `internal/doctor/doctor.go` | Add legacy freeze file warning check |

---

## Notes

- Audit events already emit unprefixed names (`deploy` not `freeze-deploy`),
  so no audit migration needed. This was a good design choice originally.
- The `FreezePrefix` constant can stay as a private `legacyFreezePrefix` for
  the fallback code path. Remove entirely in v2.0.
- This pairs naturally with the lockfile `version` field (lokt-a7m). Both are
  M0 protocol changes that should ship together.

---

**Next:** Run `/kickoff lokt-bzz` to promote to Beads execution layer.
