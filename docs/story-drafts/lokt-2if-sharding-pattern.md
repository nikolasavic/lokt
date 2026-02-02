# lokt-2if: Document parallel test sharding pattern with guard

status: draft
created: 2026-02-01
backlog-ref: .beads/issues.jsonl

## Verification
- Level: none
- Environments: -

---

## Problem

Multi-agent teams can run tests 3-10x faster by sharding across agents, but there's no documented pattern for how to do this safely with lokt. Agents waste time running full test suites sequentially when they could be running different test packages in parallel.

The primitives already exist (`lokt guard` for exclusive execution, exit code propagation), but without clear examples and conventions, agents default to sequential execution.

## Users

- **Agent teams**: Want faster test feedback by running tests in parallel
- **CI systems**: Need to shard test suites across multiple runners
- **Human developers**: Want to understand parallelization patterns for other tasks

## Requirements

1. Document test sharding pattern in README.md or new docs/patterns.md
2. Show concrete examples with multiple agents
3. Explain lock naming convention for shards (`<task>-shard-<N>`)
4. Document exit code handling and aggregation
5. Include examples for both package-level and test-level sharding
6. Show how to verify all shards completed successfully

## Non-Goals

- Automatic shard detection/allocation - agents manually pick shards
- Built-in shard orchestration command - use existing `guard`
- Language-specific test runner integration - patterns work for any tool

## Acceptance Criteria

- [ ] **Pattern documented**: Given I read README.md, when I search for "parallel" or "sharding", then I find clear examples
- [ ] **Package sharding example**: Given example shows 3 agents, when they run different packages, then pattern uses `lokt guard test-shard-N -- go test ./pkg`
- [ ] **Exit code aggregation**: Given examples show how to check if all shards passed, then pattern uses `lokt status` or exit code tracking
- [ ] **Naming convention**: Given examples use lock names, when I look at them, then all follow `<task>-shard-<N>` pattern
- [ ] **Real commands**: Given examples include actual bash/commands, when I copy-paste them, then they work

## Edge Cases

- One shard fails while others pass - document how to detect partial failure
- Agent crashes mid-shard - lock remains held (document TTL usage)
- Uneven shard distribution - show how to split work roughly evenly
- Dynamic shard count - show how to use loops for N agents

## Constraints

- Must not require code changes to lokt (doc-only)
- Examples should work on macOS and Linux
- Should show both simple (2-3 shards) and complex (many shards) cases

---

## Notes

### Example Pattern (3-agent test sharding)

```bash
# Agent 1: Test internal packages
lokt guard test-shard-1 --ttl 300 -- go test ./internal/...
EXIT_1=$?

# Agent 2: Test cmd packages (parallel)
lokt guard test-shard-2 --ttl 300 -- go test ./cmd/...
EXIT_2=$?

# Agent 3: Test pkg packages (parallel)
lokt guard test-shard-3 --ttl 300 -- go test ./pkg/...
EXIT_3=$?

# Aggregation (run after all shards)
if [ $EXIT_1 -eq 0 ] && [ $EXIT_2 -eq 0 ] && [ $EXIT_3 -eq 0 ]; then
  echo "All test shards passed"
  exit 0
else
  echo "Some shards failed: 1=$EXIT_1 2=$EXIT_2 3=$EXIT_3"
  exit 1
fi
```

### Dynamic Sharding

```bash
# Split test packages into N shards
PACKAGES=($(go list ./...))
SHARD_SIZE=$((${#PACKAGES[@]} / 3))

# Agent N runs its shard
SHARD_ID=1  # or 2, 3
START=$((SHARD_ID * SHARD_SIZE))
END=$(((SHARD_ID + 1) * SHARD_SIZE))
SHARD_PKGS="${PACKAGES[@]:$START:$SHARD_SIZE}"

lokt guard "test-shard-$SHARD_ID" -- go test $SHARD_PKGS
```

### Documentation Structure

Add to README.md under new "## Patterns" section:

1. **Parallel Test Sharding**
   - Why: Speed up test runs N×
   - How: Use `guard` with shard-specific lock names
   - Example: 3-agent package sharding
   - Gotchas: Exit code aggregation, TTL for crash recovery

2. **Test Result Caching**
   - Why: Skip redundant test runs
   - How: Use lock name with commit hash
   - Example: See lokt-2hq pattern

3. **Analysis Deduplication**
   - Why: Avoid duplicate expensive operations
   - How: In-progress + completion lock pattern
   - Example: Lint caching across agents

---

**Next:** Run `/kickoff lokt-2if` to promote to Beads execution layer.
