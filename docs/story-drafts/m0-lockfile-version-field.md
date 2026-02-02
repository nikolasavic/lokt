# M0: Lockfile Version Field

status: draft
created: 2026-01-31
backlog-ref: docs/golive.md (M0 Protocol Freeze)

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Lokt's lockfile JSON schema has no version indicator. Once v1.0 ships, the
schema becomes a public contract. Any future field addition or format change
forces readers to use heuristic detection ("does this key exist?") to
distinguish old-vs-new format. This is fragile and error-prone.

A `version` field added **before** v1.0 gives every future reader a clean
branch point: check the version, handle what you know, reject what you don't.

## Users

- **lokt CLI (future versions)**: Needs to read lockfiles written by older
  versions and bail cleanly on lockfiles from newer, incompatible versions.
- **External tooling**: Any tool parsing `<root>/locks/*.json` directly
  (scripts, monitoring, other agents) benefits from a machine-readable
  schema version.
- **Operators**: When debugging lock issues, seeing `"version": 1` in the
  JSON confirms which schema they're dealing with.

## Requirements

1. **Add `Version` field to `Lock` struct** — `"version": 1` in serialized
   JSON. Field MUST always be written (no `omitempty`). Position: first field
   in the struct so it appears first in JSON output.

2. **Read tolerance for missing version** — When reading a lockfile with no
   `version` field, treat it as version 0 (pre-v1.0 format). Do NOT reject
   it. This provides backward compatibility with any locks written before this
   change.

3. **Read rejection for unknown versions** — When reading a lockfile with
   `version > 1`, return a typed error (`ErrUnsupportedVersion`) with a clear
   message: "lockfile version %d not supported (max: 1); upgrade lokt".

4. **Version constant** — Define `const CurrentLockfileVersion = 1` in the
   `lockfile` package. All creation sites use this constant, never a literal.

5. **Doctor protocol check** — `lokt doctor --json` output includes
   `"protocol_version": 1` in the top-level object. This is the M0 exit
   criterion from golive.md.

6. **Update CLI local struct** — The duplicate `lockFile` struct in
   `cmd/lokt/main.go` must also gain the `Version` field to stay in sync.

## Non-Goals

- **Schema migration of existing locks**: We do NOT rewrite locks on disk.
  Old locks without `version` are read as version 0 and work fine. They'll
  get version 1 on next acquire/renew.
- **Versioning the audit log schema**: Audit events are a separate concern
  (could be a follow-up in M0 but not this story).
- **Versioning the freeze lock format**: Freeze locks use the same `Lock`
  struct, so they get versioning for free. No separate treatment.
- **Semantic versioning negotiation**: No handshake protocol. This is a
  simple integer version stamp, not a capability exchange.

## Acceptance Criteria

- [ ] **Version written on acquire**: Given a new lock acquisition, when the
  lockfile is read from disk, then it contains `"version": 1` as the first
  JSON field.

- [ ] **Version written on freeze**: Given a freeze command, when the freeze
  lockfile is read from disk, then it contains `"version": 1`.

- [ ] **Version written on renew**: Given a heartbeat renewal, when the
  lockfile is rewritten, then it contains `"version": 1`.

- [ ] **Missing version tolerated**: Given a lockfile on disk with no
  `version` field (pre-v1.0 format), when lokt reads it (status, unlock,
  guard check, etc.), then it succeeds without error, treating version as 0.

- [ ] **Unknown version rejected**: Given a lockfile on disk with
  `"version": 99`, when lokt tries to read/acquire/release it, then it
  returns exit code indicating error with message containing "version" and
  "not supported".

- [ ] **Doctor reports protocol version**: Given `lokt doctor --json`, when
  output is parsed, then the top-level object includes
  `"protocol_version": 1`.

- [ ] **Existing tests pass**: Given `go test -race ./...`, when run after
  changes, then all existing tests pass (no regressions).

## Edge Cases

- **Concurrent old+new lokt binaries** — An old binary (no version field)
  acquires a lock. New binary reads it: must work (version 0 tolerance).
  New binary acquires a lock. Old binary reads it: `json.Unmarshal` ignores
  unknown fields by default in Go, so old binary works fine. No issue.

- **Lock renew upgrades version** — A lock created by old binary (no version)
  gets renewed by new binary (heartbeat). The rewritten file will now have
  `version: 1`. This is correct and desirable — gradual upgrade.

- **Corrupted version field** — A lockfile with `"version": "abc"` (wrong
  type) hits the existing corrupt-file handling path (`ErrCorrupted`). No
  special handling needed.

- **Version field in status output** — The `lokt status` and
  `lokt status --json` commands should include the version field in their
  output so operators can see it.

## Constraints

- **Struct field ordering matters for JSON**: Go's `encoding/json` serializes
  fields in struct definition order. `Version` must be the first field in the
  `Lock` struct so `"version"` appears first in the JSON, making it easy to
  spot visually and parse incrementally.

- **Two Lock struct definitions**: The codebase has a duplicate `lockFile`
  struct in `cmd/lokt/main.go` (to avoid import cycles). Both must be updated
  in lockstep.

- **No `omitempty` on Version**: The field must always be written, even if
  value is 0 (though we'll always write 1). This ensures the field is always
  present in new lockfiles.

---

## Affected Files (from codebase research)

| File | Change |
|------|--------|
| `internal/lockfile/lockfile.go:15-24` | Add `Version` field to `Lock` struct, add `CurrentLockfileVersion` const, add `ErrUnsupportedVersion`, update `Read()` to validate |
| `internal/lock/acquire.go:59-71` | Set `Version: lockfile.CurrentLockfileVersion` on new locks |
| `internal/lock/acquire.go:106-110` | Set version on reentrant refresh |
| `internal/lock/freeze.go:70-80` | Set version on freeze lock creation |
| `internal/lock/renew.go:27` | Set version on renewal (upgrades old locks) |
| `cmd/lokt/main.go:867-888` | Add `Version` field to local `lockFile` struct |
| `cmd/lokt/main.go:1594-1631` | Add `protocol_version` to doctor JSON output |
| `internal/lockfile/lockfile_test.go` | Add tests for version read/write/rejection |
| `internal/doctor/doctor.go` | Expose protocol version constant (or import from lockfile) |

---

## Notes

- This is the first item in M0 Protocol Freeze. It should land before
  `expires_at` (lokt-bm6) since that change also adds a field to the Lock
  struct — better to add version first so `expires_at` lands in a
  version-aware codebase.
- The M0 exit criterion is: `lokt doctor --json` output includes
  `"protocol_version": 1`. Lockfile schema documented in README with
  explicit compatibility promise.
- Related M0 items: lokt-bm6 (expires_at), lokt-bzz (freeze namespace),
  lokt-ao9 (lock_id in audit).

---

**Next:** Run `/kickoff` to promote to Beads execution layer.
