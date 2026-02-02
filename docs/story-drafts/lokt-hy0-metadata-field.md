# lokt-hy0: Add metadata field to lockfile schema for result storage

status: draft
created: 2026-02-01
backlog-ref: .beads/issues.jsonl

## Verification
- Level: required
- Environments: sandbox

---

## Problem

Agents need to cache expensive operation results (test output, lint results, analysis) and share them across sessions. Currently, lock files only signal "operation completed" but can't store WHERE the result is or HOW to retrieve it.

This forces ad-hoc conventions (hardcoded paths, separate index files) that break down when multiple agents work concurrently. The lockfile itself is the natural place to store result metadata - it's already atomic, durable, and has TTL support.

## Users

- **Agent workflows**: Store and retrieve cached analysis results
- **CI systems**: Track build artifact locations by commit hash
- **Test runners**: Share test output across parallel agents

## Requirements

1. Add optional `metadata` field to lockfile schema (version 1 - no bump needed)
2. Support arbitrary JSON object as metadata value
3. Extend `lokt lock` command with `--meta key=value` flag (repeatable)
4. Extend `lokt status` JSON output to include metadata
5. Add `lokt get-meta <name> <key>` command to extract single value
6. Maintain backward compatibility (old lockfiles without metadata still work)

## Non-Goals

- Nested metadata keys (`--meta foo.bar.baz=value`) - flat keys only
- Metadata size limits - rely on filesystem limits
- Metadata search/query - use `status --json | jq` for that
- Metadata updates without lock recreation - metadata is immutable once written

## Acceptance Criteria

- [ ] **Store metadata on lock**: Given I run `lokt lock test-result --meta path=/tmp/out.txt --meta commit=abc123`, then lockfile contains `{"metadata": {"path": "/tmp/out.txt", "commit": "abc123"}}`
- [ ] **Retrieve via status**: Given lock has metadata, when I run `lokt status test-result --json`, then output includes metadata field
- [ ] **Extract single value**: Given lock has metadata key "path", when I run `lokt get-meta test-result path`, then output is `/tmp/out.txt` only
- [ ] **Backward compatibility**: Given old lockfile without metadata field, when I run `lokt status`, then it works without error
- [ ] **Cache pattern works**: Given `lokt lock lint-result --meta file=/tmp/lint.txt --ttl 600`, when second agent runs `cat $(lokt get-meta lint-result file)`, then cached results are used

## Edge Cases

- Lock doesn't exist - `get-meta` returns error, not empty string
- Metadata key doesn't exist - `get-meta` returns error with helpful message
- Metadata value is empty string - `get-meta` returns empty string (valid)
- Metadata with special characters - properly JSON-escaped
- Multiple `--meta` flags with same key - last one wins (document this)

## Constraints

- Must not break existing lockfile readers (backward compatible)
- JSON schema version stays at 1 (metadata is additive)
- Metadata stored in lockfile itself (no separate files)
- `get-meta` must be scriptable (no extra output, just value)

---

## Notes

### Schema Extension
```json
{
  "version": 1,
  "name": "test-result",
  "owner": "agent-1",
  "host": "localhost",
  "pid": 12345,
  "acquired_ts": "2026-02-01T10:00:00Z",
  "ttl_sec": 600,
  "expires_at": "2026-02-01T10:10:00Z",
  "metadata": {
    "result_path": "/tmp/test-output.txt",
    "commit": "abc123",
    "exit_code": "0"
  }
}
```

### Usage Example
```bash
# Agent 1: Run tests and cache results
COMMIT=$(git rev-parse HEAD)
RESULT_FILE="/tmp/test-$COMMIT.txt"
go test ./... > "$RESULT_FILE"
lokt lock "test-passed-$COMMIT" --meta result="$RESULT_FILE" --meta exit_code=0 --ttl 3600

# Agent 2: Use cached results
COMMIT=$(git rev-parse HEAD)
if lokt exists "test-passed-$COMMIT"; then
  echo "Using cached test results:"
  cat $(lokt get-meta "test-passed-$COMMIT" result)
  exit $(lokt get-meta "test-passed-$COMMIT" exit_code)
fi
```

### Implementation Notes
- Add `Metadata map[string]string` to lockfile.Lock struct
- Parse `--meta key=value` in lock command (split on first `=`)
- Add `get-meta` subcommand to cmd/lokt/main.go
- Update lockfile_test.go with metadata roundtrip tests

---

**Next:** Run `/kickoff lokt-hy0` to promote to Beads execution layer.
