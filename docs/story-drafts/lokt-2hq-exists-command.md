# lokt-2hq: Add lokt exists command for cleaner cache checking

status: draft
created: 2026-02-01
backlog-ref: .beads/issues.jsonl

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

Multi-agent workflows need to check if cached results exist (test runs, lint results, build artifacts) without polluting stderr with status output. Current approach uses `lokt status <name> &>/dev/null` which works but is clunky and non-obvious to readers.

Agents waste tokens by re-running expensive operations (tests, linting, analysis) when cached results already exist. A clean exit-code-based check enables scriptable cache invalidation.

## Users

- **Agent scripts**: Need clean boolean check for cache hits
- **CI pipelines**: Skip redundant test runs based on git commit hash
- **Human operators**: Quick check if operation already completed

## Requirements

1. Add `lokt exists <name>` command that checks lock existence
2. Return exit code 0 if lock exists, non-zero if not
3. Produce no stdout/stderr output (silent operation)
4. Support same name validation as other commands
5. Work with any lock (not just cache locks)

## Non-Goals

- Pattern matching (`exists 'test-*'`) - can add later if needed
- Metadata querying - use `status --json` for that
- Wait/blocking behavior - use `guard --wait` for that

## Acceptance Criteria

- [ ] **Basic existence check**: Given a lock exists, when I run `lokt exists <name>`, then exit code is 0 with no output
- [ ] **Non-existent lock**: Given no lock exists, when I run `lokt exists <name>`, then exit code is non-zero with no output
- [ ] **Cache pattern works**: Given tests passed for commit abc123, when I run `if lokt exists test-passed-abc123; then skip_tests; fi`, then tests are skipped
- [ ] **Name validation**: Given invalid lock name, when I run `lokt exists "../bad"`, then exit with validation error
- [ ] **Silent operation**: Given any lock state, when I run `lokt exists <name>`, then stdout and stderr are empty

## Edge Cases

- Lock created but empty (race condition) - should return 1 (not exist)
- Lock expired but file still exists - should return 0 (file exists, expiry is separate concern)
- Invalid lock name - exit with error code (not 0 or standard "not found")
- LOKT_ROOT not found - exit with error

## Constraints

- Must reuse existing name validation logic
- Should not read lock contents (cheaper than status)
- Exit codes must align with existing exit code conventions (see cmd/lokt/exitcode_test.go)

---

## Notes

This is the highest-value addition for multi-agent caching patterns. Enables:
```bash
# Before (clunky)
if lokt status "test-passed-$(git rev-parse HEAD)" &>/dev/null; then
  echo "Tests already passed"
  exit 0
fi

# After (clean)
lokt exists "test-passed-$(git rev-parse HEAD)" && exit 0
```

Implementation should be ~20 lines:
- Parse name argument
- Validate name
- Resolve root
- Check if file exists at `<root>/locks/<name>.json`
- Return exit code

---

**Next:** Run `/kickoff lokt-2hq` to promote to Beads execution layer.
