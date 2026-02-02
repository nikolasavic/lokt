# lokt-bm6: Write expires_at Timestamp

status: draft
created: 2026-02-01
backlog-ref: .beads/ (lokt-bm6), docs/golive.md (M0 Protocol Freeze)

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Lokt's lockfile requires readers to compute expiry via arithmetic:
`AcquiredAt + TTLSec > now`. Every consumer — status display, stale detection,
guard heartbeat, deny output, the `why` command — independently repeats this
calculation. This creates scattered TTL arithmetic across 7+ call sites, each a
potential source of off-by-one or rounding inconsistency.

Worse, the arithmetic crosses process boundaries using wall-clock time. Go's
`time.Since()` is monotonic-safe within a single process, but when Process A
writes `acquired_ts` and Process B reads it later, the comparison uses
deserialized wall-clock values — vulnerable to NTP jumps and clock skew.

An explicit `expires_at` timestamp computed once at write time (acquire/renew)
eliminates repeated arithmetic and makes the expiry contract visible in the
lockfile itself. Readers reduce to a single comparison: `now > expires_at`.

This is a **M0 Protocol Freeze** item. The lockfile JSON schema becomes a
public contract at v1.0. Adding `expires_at` now avoids a breaking change
later. The prerequisite `version` field (lokt-a7m) has already landed.

## Users

- **lokt CLI (status/stale commands)**: Simpler expiry checks — no arithmetic,
  just compare `expires_at` to current time.
- **External tooling (scripts, monitoring)**: Can read `expires_at` directly
  from JSON without needing to know the TTL computation rule.
- **Guard heartbeat**: Renew writes a new `expires_at`, making the extension
  visible in the lockfile without requiring readers to re-derive it.
- **Operators debugging locks**: `cat locks/build.json` shows the exact expiry
  wall-clock time — no mental math needed.

## Requirements

1. **Add `ExpiresAt` field to `Lock` struct** — `"expires_at"` in serialized
   JSON, type `time.Time`, with `omitempty` (zero value = no expiry, same as
   TTLSec=0). Position: after `TTLSec` in struct definition for logical
   grouping.

2. **Compute on write** — At every point that creates or rewrites a lockfile
   (acquire, reentrant refresh, freeze, renew), set
   `ExpiresAt = AcquiredAt.Add(TTLSec * time.Second)` when TTLSec > 0. When
   TTLSec is 0, leave ExpiresAt as zero value (omitted from JSON).

3. **Update `IsExpired()` to prefer `ExpiresAt`** — If `ExpiresAt` is set
   (non-zero), use `time.Now().After(ExpiresAt)`. Fall back to the existing
   arithmetic for old lockfiles where `ExpiresAt` is zero but `TTLSec > 0`.

4. **Read tolerance for missing `expires_at`** — Lockfiles written by older
   binaries (version 0 or version 1 pre-bm6) won't have this field.
   `json.Unmarshal` will leave it as zero value. The fallback in `IsExpired()`
   handles this. No version bump needed — it's an additive, optional field
   within version 1.

5. **Surface in JSON output** — The `--json` output of `status`, lock deny,
   and `why` commands should include `expires_at` (RFC3339) when present.
   The computed `holder_remaining_sec` can derive from `expires_at` instead
   of `TTLSec - age`.

6. **Surface in human output** — `lokt status <name>` detailed view should
   print `expires:  2026-02-01T14:30:00Z` (or relative: `expires in 4m32s`)
   when the field is present.

7. **Document monotonic time limitations** — Add a section to CLAUDE.md
   (Architecture > TTL & Staleness) and/or a comment block in `lockfile.go`
   explaining: wall-clock `expires_at` is susceptible to NTP jumps in
   cross-process scenarios; in-process `time.Since()` with monotonic component
   is safe; this is an inherent limitation of file-based coordination. Cross-host
   TTL checks are best-effort — PID liveness and heartbeats provide the real
   safety net.

## Non-Goals

- **Version bump to 2**: Adding an optional field (`omitempty`) within version
  1 is backward-compatible. Old readers ignore unknown JSON keys. No version
  bump needed.
- **Removing `ttl_sec` field**: Keep it. It's the source-of-truth for the
  intended duration. `expires_at` is a convenience derivation. Both are useful:
  `ttl_sec` for "how long was this lock meant to last" vs `expires_at` for
  "when does it die."
- **Monotonic clock persistence**: There is no portable way to persist
  monotonic timestamps to disk for cross-process use. This is a known,
  documented limitation.
- **Audit log schema changes**: Audit events may optionally include
  `expires_at` in the `extra` map, but changing the `Event` struct is out of
  scope (that's lokt-ao9 territory).
- **Retroactive rewrite of old locks**: Old locks get `expires_at` on next
  acquire/renew, not via migration.

## Acceptance Criteria

- [ ] **Field written on acquire**: Given `lokt lock foo --ttl 5m`, when the
  lockfile is read from disk, then it contains `"expires_at"` set to
  approximately `acquired_ts + 5 minutes` (RFC3339 format).

- [ ] **Field omitted when no TTL**: Given `lokt lock foo` (no `--ttl`), when
  the lockfile is read from disk, then `"expires_at"` is absent from the JSON
  (omitempty).

- [ ] **Field written on freeze**: Given `lokt freeze foo --ttl 10m`, when the
  freeze lockfile is read, then it contains `"expires_at"` set to approximately
  `acquired_ts + 10 minutes`.

- [ ] **Field refreshed on renew**: Given an existing lock with `expires_at` in
  the past-minus-margin, when the guard heartbeat renews it, then `expires_at`
  is updated to a future time (new `AcquiredAt + TTL`).

- [ ] **Field refreshed on reentrant acquire**: Given an existing lock owned by
  the same owner, when `lokt lock foo --ttl 5m` is run again, then `expires_at`
  is updated to reflect the new acquisition time.

- [ ] **IsExpired uses ExpiresAt**: Given a lockfile with `expires_at` in the
  past, when `IsExpired()` is called, then it returns true (using direct
  comparison, not TTLSec arithmetic).

- [ ] **Backward compat — old lockfile**: Given a lockfile without
  `"expires_at"` (written by older binary), when read by new binary, then
  `IsExpired()` still works correctly via the TTLSec fallback path.

- [ ] **JSON output includes expires_at**: Given `lokt status foo --json` on a
  lock with TTL, when output is parsed, then it includes `"expires_at"` field.

- [ ] **Human output shows expiry**: Given `lokt status foo` on a lock with
  TTL, when displayed, then output includes an expiry line (absolute time or
  relative countdown).

- [ ] **Monotonic time documented**: Given CLAUDE.md or README, when searched
  for "monotonic" or "clock skew", then a section explains the wall-clock
  limitation and mitigations.

- [ ] **Existing tests pass**: Given `go test -race ./...`, when run after all
  changes, then all existing tests pass with no regressions.

## Edge Cases

- **NTP backward jump after acquire** — `expires_at` was computed at write time
  using the pre-jump clock. After a backward jump, `time.Now()` may be before
  `expires_at` even though real wall time has passed the TTL. This is the
  documented limitation. Mitigation: PID liveness detection + heartbeat renewal.

- **NTP forward jump** — Lock appears expired prematurely. Guard heartbeat
  would have renewed it, but if the process crashed, stale detection correctly
  identifies it as expired. Acceptable behavior.

- **Mixed old/new binaries** — Old binary writes lock (no `expires_at`). New
  binary reads it: `ExpiresAt` is zero, falls back to TTLSec arithmetic. New
  binary renews it: `expires_at` now appears. Old binary reads renewed lock:
  ignores unknown `expires_at` field. No issues in either direction.

- **TTL of 0 after previously having TTL** — If a reentrant acquire changes
  from `--ttl 5m` to no TTL, `ExpiresAt` should be zero (omitted) and
  `TTLSec` should be 0. Lock becomes permanent.

- **Sub-second TTL precision** — `TTLSec` is integer seconds; `ExpiresAt` is
  `time.Time` (nanosecond precision). The computation
  `AcquiredAt.Add(TTLSec * time.Second)` produces a clean seconds boundary.
  No fractional-second surprises.

- **Remaining time in deny output** — When a lock is denied with `--json`,
  `holder_remaining_sec` should be derived from `ExpiresAt - now` (if
  `ExpiresAt` is set) rather than `TTLSec - age`. These should produce the
  same result but using `ExpiresAt` is more consistent.

## Constraints

- **Additive-only within version 1**: The `expires_at` field must be optional
  (`omitempty`) to maintain backward compatibility within version 1. Readers
  must handle its absence.
- **Write-time computation only**: `expires_at` is set at write time
  (acquire/renew), never computed lazily at read time. The on-disk value is
  the source of truth.
- **RFC3339 format**: Must use the same time serialization format as
  `acquired_ts` (Go's default `time.Time` JSON marshaling = RFC3339Nano).
- **Prerequisite: lokt-a7m landed**: The version field is already in the struct.
  No dependency issue.

---

## Affected Files (from codebase research)

| File | Change |
|------|--------|
| `internal/lockfile/lockfile.go:18-28` | Add `ExpiresAt` field to `Lock` struct |
| `internal/lockfile/lockfile.go:30-36` | Update `IsExpired()` to prefer `ExpiresAt` |
| `internal/lock/acquire.go:59-72` | Compute `ExpiresAt` on new lock creation |
| `internal/lock/acquire.go:105-116` | Compute `ExpiresAt` on reentrant refresh |
| `internal/lock/freeze.go` | Compute `ExpiresAt` on freeze lock creation |
| `internal/lock/renew.go:23-50` | Compute `ExpiresAt` on renewal |
| `cmd/lokt/main.go` (lockFile struct) | Add `ExpiresAt` to local duplicate struct |
| `cmd/lokt/main.go` (status display) | Show `expires_at` in human and JSON output |
| `cmd/lokt/main.go` (deny output) | Include `expires_at` in deny JSON; derive remaining from it |
| `cmd/lokt/main.go` (why command) | Use `ExpiresAt` for remaining time display |
| `internal/lockfile/lockfile_test.go` | Tests for new field: write/read, omitempty, backward compat |
| `CLAUDE.md` | Document monotonic time limitation in Architecture section |

---

## Notes

- This is the second M0 Protocol Freeze item. The version field (lokt-a7m)
  landed in commit b9f2dd1, so the schema is now version-aware.
- The `expires_at` field is a derived value (`acquired_ts + ttl_sec`), not
  independent data. If there's ever a discrepancy between `expires_at` and
  `acquired_ts + ttl_sec`, the `expires_at` value wins (it's the on-disk
  contract readers use).
- Remaining M0 items after this: lokt-bzz (freeze namespace separation),
  lokt-ao9 (lock_id UUID in audit events).

---

**Next:** Run `/kickoff lokt-bm6` to promote to Beads execution layer.
