# L-103: Lock Name Validation

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md (L-103)

## Verification
- Level: none
- Environments: local (unit tests sufficient)

---

## Problem

Lock names are passed directly to file path construction without validation. A malicious or accidental name like `../../../etc/passwd` or `/tmp/evil` could write outside the locks directory.

## Users

- **Developers**: Need clear error messages when using invalid lock names
- **Operators**: Need confidence that lokt can't be exploited for path traversal

## Requirements

1. Validate lock names match pattern `[A-Za-z0-9._-]+`
2. Reject names containing `..` (path traversal)
3. Reject absolute paths (starting with `/`)
4. Reject empty names
5. Return clear error message on invalid name

## Non-Goals

- Unicode support: ASCII only for v1
- Length limits: Not needed yet

## Acceptance Criteria

- [ ] **Valid names accepted**: Given valid name `deploy-prod`, when `lokt lock` runs, then lock acquired
- [ ] **Path traversal rejected**: Given name `../etc/passwd`, when `lokt lock` runs, then error "invalid lock name"
- [ ] **Absolute path rejected**: Given name `/tmp/evil`, when `lokt lock` runs, then error "invalid lock name"
- [ ] **Empty rejected**: Given empty name, when `lokt lock` runs, then error "invalid lock name"
- [ ] **Special chars rejected**: Given name `foo;rm -rf`, when `lokt lock` runs, then error "invalid lock name"

## Edge Cases

- `foo.bar` — valid (dots allowed)
- `foo-bar_baz` — valid (hyphens, underscores allowed)
- `.hidden` — valid (leading dot ok)
- `foo..bar` — invalid (contains `..`)

## Constraints

- Validation should happen early (before any file operations)
- Same validation for all commands (lock, unlock, status, guard)
