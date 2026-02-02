# lokt-ao9: Add lock_id to Audit Events

status: draft
created: 2026-02-01
backlog-ref: .beads/ (lokt-ao9), docs/golive.md (M0 Protocol Freeze)

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Lokt's audit log records 12 event types across the lock lifecycle (acquire,
renew, release, deny, force-break, stale-break, auto-prune, corrupt-break,
freeze, unfreeze, force-unfreeze, freeze-deny). Today, the only way to
correlate events from a single lock acquisition is heuristic matching on
`{name, owner, host, pid}` + timestamp ordering. This is fragile:

- A long-running guard with heartbeat renewal produces many renew events.
  Pairing them with the original acquire requires timestamp-range guessing.
- If the same owner acquires the same lock multiple times (reentrant or after
  release), events from different acquisition lifecycles are indistinguishable.
- Force-break and stale-break events reference the *breaker's* identity, not the
  original acquisition. There's no field linking the break to the acquire it
  terminated.
- Post-processing tools (dashboards, anomaly detection, hold-duration metrics)
  must implement brittle heuristics that break when events overlap.

A unique `lock_id` generated once at acquisition time and carried through every
subsequent event (renew, release, break) makes correlation trivial: group by
`lock_id`, sort by timestamp, done.

This is a **M0 Protocol Freeze** item. The audit JSONL schema becomes a public
contract at v1.0. Adding `lock_id` post-release would be a breaking change for
any tooling that parses the audit log.

## Users

- **Operators debugging production locks**: Filter audit log by `lock_id` to see
  the full lifecycle of a single acquisition — when it was acquired, how many
  renewals, why/when it ended.
- **Monitoring/alerting pipelines**: Compute hold durations accurately
  (`release.ts - acquire.ts` for the same `lock_id`). Detect abandoned locks
  (acquire without matching release).
- **Developers reading raw audit logs**: `jq 'select(.lock_id=="abc")'` replaces
  multi-field timestamp-range queries.
- **Guard command (internal)**: Passes `lock_id` from acquire through heartbeat
  renewals to final release, ensuring the full guard session is one traceable
  unit.

## Requirements

1. **Add `LockID` field to `audit.Event` struct** — `"lock_id"` in serialized
   JSON, type `string`, with `omitempty` (empty for events that don't belong to
   an acquisition lifecycle, e.g., `freeze-deny`). Position: after `Name` field
   for logical grouping.

2. **Generate lock_id at acquisition time** — When `Acquire()` succeeds (new
   lock creation), generate a unique ID. Format: 128-bit random hex string
   (32 characters) via `crypto/rand`. Opaque correlation key — no parsing,
   no format ambiguity. Host/PID/timestamp are already separate fields in every
   event; duplicating them in lock_id adds no value. No external dependency.

3. **Return lock_id from Acquire** — The `Acquire()` function (or a new return
   type) must make the generated `lock_id` available to callers so they can pass
   it to subsequent operations (renew, release). Options:
   - (a) Return `(string, error)` — breaking change to `Acquire` signature.
   - (b) Add `LockID` field to `AcquireOptions` populated as out-parameter.
   - (c) Store `lock_id` in the lockfile itself and read it back on
     renew/release.

   **Recommended: option (c)**. Store `lock_id` in the lockfile. This way
   renew/release read it from disk and include it in their audit events without
   needing callers to thread it through. It also makes `lock_id` visible in
   `lokt status --json` output for free.

4. **Add `LockID` field to `lockfile.Lock` struct** — `"lock_id"` in serialized
   JSON, type `string`, with `omitempty`. Generated on new acquire, preserved on
   reentrant acquire and renew (same acquisition lifecycle), cleared on release
   (file deleted). Additive within version 1 (backward compatible).

5. **Thread lock_id through lifecycle events** — Events that should carry
   `lock_id`:
   - `acquire`: the newly generated ID
   - `renew`: read from existing lockfile (same acquisition)
   - `release`: read from existing lockfile before deletion
   - `force-break`: read from existing lockfile before deletion
   - `stale-break`: read from existing lockfile before deletion
   - `auto-prune`: read from existing lockfile (the pruned lock's ID)
   - `corrupt-break`: empty (file is unreadable, no ID to extract)

6. **Thread lock_id through freeze lifecycle events** — Freeze locks also get a
   `lock_id`:
   - `freeze`: newly generated ID on freeze creation
   - `unfreeze` / `force-unfreeze`: read from freeze lockfile before deletion
   - `freeze-deny`: read from existing freeze lockfile (the *freeze's* lock_id,
     so the deny can be correlated to the freeze that caused it)

7. **Backward compatibility for lockfiles without lock_id** — Old lockfiles
   (pre-ao9) won't have this field. `json.Unmarshal` produces empty string.
   Emit helpers should pass through whatever is in the lockfile — empty string
   means `omitempty` drops it from the audit JSON. No version bump needed.

## Non-Goals

- **UUID library dependency**: Using `crypto/rand` hex avoids adding
  `google/uuid` or similar. If we later want RFC 4122 UUIDs, that's a v1.1
  enhancement (the field is a string, so the format can evolve).
- **Version bump to 2**: `lock_id` is additive and optional (`omitempty`).
  Old readers ignore unknown JSON keys. Stays within version 1.
- **Audit log schema version field**: The audit JSONL format has no explicit
  version. Adding `lock_id` as `omitempty` is backward compatible — old parsers
  ignore it, new parsers use it when present.
- **Correlating deny events to a lock_id**: A `deny` event fires when someone
  *fails* to acquire. The deny itself doesn't have a lock_id (the denier never
  held the lock). The `holder_*` fields in `extra` provide the holder's
  identity, and the holder's lockfile contains their `lock_id`. This is
  sufficient — adding the holder's `lock_id` to the deny event is a nice-to-have
  for v1.1.
- **Retroactive lock_id injection into existing lockfiles**: Old locks get a
  `lock_id` on next acquire/renew, not via migration.

## Acceptance Criteria

- [ ] **lock_id in lockfile on acquire**: Given `lokt lock foo --ttl 5m`, when
  the lockfile is read from disk, then it contains a non-empty `"lock_id"`
  string field (32-character hex string).

- [ ] **lock_id omitted when missing**: Given a lockfile written by an older
  binary (no `lock_id`), when read by the new binary, then `LockID` is empty
  string and omitted from re-serialized JSON.

- [ ] **lock_id in acquire event**: Given a successful `lokt lock foo`, when the
  audit log is read, then the `acquire` event contains a `"lock_id"` field
  matching the lockfile's `lock_id`.

- [ ] **lock_id in renew event**: Given a guard with heartbeat renewal, when the
  audit log is read, then all `renew` events for that guard session share the
  same `lock_id` as the initial `acquire` event.

- [ ] **lock_id in release event**: Given `lokt unlock foo`, when the audit log
  is read, then the `release` event contains the same `lock_id` as the
  `acquire` event for that lock.

- [ ] **lock_id in force-break event**: Given `lokt unlock foo --force`, when the
  audit log is read, then the `force-break` event contains the broken lock's
  `lock_id`.

- [ ] **lock_id in stale-break event**: Given `lokt unlock foo --break-stale`,
  when the audit log is read, then the `stale-break` event contains the broken
  lock's `lock_id`.

- [ ] **lock_id in auto-prune event**: Given a dead-PID auto-prune during
  acquire, when the audit log is read, then the `auto-prune` event contains the
  pruned lock's `lock_id`.

- [ ] **lock_id absent from corrupt-break event**: Given a corrupted lockfile
  that gets removed, when the audit log is read, then the `corrupt-break` event
  has no `lock_id` field (or empty).

- [ ] **lock_id in freeze event**: Given `lokt freeze foo --ttl 15m`, when the
  freeze lockfile is read, then it contains a `lock_id` field.

- [ ] **lock_id in unfreeze event**: Given `lokt unfreeze foo`, when the audit
  log is read, then the `unfreeze` event contains the freeze's `lock_id`.

- [ ] **lock_id in freeze-deny event**: Given a guard blocked by freeze, when the
  audit log is read, then the `freeze-deny` event contains the freeze's
  `lock_id` (in extra or as top-level field).

- [ ] **lock_id preserved on reentrant acquire**: Given a lock owned by the same
  owner, when `lokt lock foo` is run again (reentrant), then the existing
  `lock_id` is preserved (same acquisition lifecycle continues). The `renew`
  audit event carries the same `lock_id`.

- [ ] **lock_id visible in status --json**: Given `lokt status foo --json`, when
  the output is parsed, then it includes the `lock_id` field from the lockfile.

- [ ] **Existing tests pass**: Given `go test -race ./...`, when run after all
  changes, then all existing tests pass with no regressions.

## Edge Cases

- **Old lockfile without lock_id** — `json.Unmarshal` sets `LockID` to `""`.
  Renew reads this empty value, writes it back (still empty). Release reads it,
  emits event with empty lock_id (omitted from JSON via `omitempty`). The lock
  gets a real `lock_id` on next fresh acquisition. No crash, no inconsistency.

- **Reentrant acquire preserves lock_id** — The reentrant path reads `existing`
  before overwriting, so it copies `existing.LockID` to the new lock struct.
  The `renew` audit event carries the same `lock_id`, keeping the correlation
  chain intact: `acquire → renew(reentrant) → renew(heartbeat)* → release`.
  If the existing lockfile has no `lock_id` (pre-ao9), the reentrant path
  generates a new one (backfill).

- **crypto/rand failure** — `crypto/rand.Read` can fail if the OS entropy pool
  is exhausted (extremely rare). If it fails, fall back to a timestamp-based ID
  (`fmt.Sprintf("%x", time.Now().UnixNano())`) and log a warning to stderr.
  A degraded lock_id is better than a failed acquisition.

- **ReleaseByOwner with lock_id** — The `ReleaseByOwner()` function iterates
  locks and releases matching ones. Each release should emit the lock's own
  `lock_id`. Currently, it reads the lockfile to check ownership, so `lock_id`
  is already available.

## Constraints

- **Additive-only within version 1**: Both `lockfile.Lock.LockID` and
  `audit.Event.LockID` must be optional (`omitempty`) to maintain backward
  compatibility. Readers must handle absence.
- **No external dependencies**: Use `crypto/rand` (stdlib) for ID generation.
  16 bytes → 32-char hex string.
- **Audit event size**: The `lock_id` string adds ~45 bytes per event
  (`"lock_id":"<32 chars>"`). Well within the PIPE_BUF atomic write limit
  (~4096 bytes).
- **Prerequisite: other M0 items landed**: version field (lokt-a7m), expires_at
  (lokt-bm6), and freeze namespace (lokt-bzz) are all closed.

---

## Affected Files (from codebase research)

| File | Change |
|------|--------|
| `internal/lockfile/lockfile.go:19-29` | Add `LockID string` field to `Lock` struct |
| `internal/audit/audit.go:30-39` | Add `LockID string` field to `Event` struct |
| `internal/lock/acquire.go:59-74` | Generate `lock_id` on new lock creation |
| `internal/lock/acquire.go:110-121` | Preserve existing `lock_id` on reentrant refresh (copy from `existing`) |
| `internal/lock/acquire.go:269-282` | Pass `lock_id` to acquire audit event |
| `internal/lock/acquire.go:284-303` | Deny event: no lock_id (denier has none) |
| `internal/lock/acquire.go:305-318` | Corrupt-break: no lock_id (file unreadable) |
| `internal/lock/acquire.go:320-339` | Auto-prune: pass pruned lock's lock_id |
| `internal/lock/renew.go:23-54` | Read lock_id from existing lock, pass to audit event |
| `internal/lock/renew.go:56-69` | Update `emitRenewEvent` to accept lock_id |
| `internal/lock/release.go:70-148` | Read lock_id from existing lock before removal |
| `internal/lock/release.go:220-242` | Pass lock_id to release/force-break/stale-break events |
| `internal/lock/release.go:150-203` | Pass lock_id in `ReleaseByOwner` release events |
| `internal/lock/release.go:205-218` | Corrupt-break release: no lock_id |
| `internal/lock/freeze.go:55-137` | Generate lock_id on freeze creation |
| `internal/lock/freeze.go:293-315` | Pass lock_id to unfreeze event |
| `internal/lock/freeze.go:317-334` | Pass freeze's lock_id to freeze-deny event |
| `internal/lockfile/lockfile_test.go` | Tests: lock_id write/read, omitempty, backward compat |
| `internal/audit/audit_test.go` | Tests: lock_id serialized in JSONL, omitempty |

---

## Design Decisions (resolved)

1. **lock_id format**: 128-bit `crypto/rand` hex (32-char string). Opaque
   correlation key — no parsing, no hostname ambiguity. Host/PID/timestamp
   already exist as separate fields in every event.

2. **Reentrant acquire preserves lock_id**: Same-owner re-acquire copies
   `existing.LockID` into the refreshed lock. This keeps the
   `acquire → renew* → release` chain intact. Generating a new ID would create
   orphaned chains (acquire with no release, renew with no acquire).

---

## Notes

- This is the last M0 Protocol Freeze item. Completing it unblocks the M0 exit
  criteria.
- The lock_id field is stored in the lockfile, making it available to `status`,
  `doctor`, and any future command without needing to change their audit plumbing.
- Freeze events get lock_id too, since freeze locks use the same `lockfile.Lock`
  struct and have their own lifecycle (freeze → deny* → unfreeze).

---

**Next:** Run `/kickoff lokt-ao9` to promote to Beads execution layer.
