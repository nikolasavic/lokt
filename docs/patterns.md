# Lokt Patterns

Patterns for building agent-proof workflows. These patterns work because they enforce behavior at the system level—agents don't need to remember conventions when the filesystem says no.

## Test Sharding

Speed up test runs by sharding across multiple agents. Each agent claims a shard lock and runs a subset of tests in parallel.

### Basic Pattern (3 Agents)

```bash
# Agent 1
lokt guard test-shard-1 --ttl 5m -- go test ./internal/...

# Agent 2 (parallel)
lokt guard test-shard-2 --ttl 5m -- go test ./cmd/...

# Agent 3 (parallel)
lokt guard test-shard-3 --ttl 5m -- go test ./pkg/...
```

Each agent runs independently. The locks prevent duplicate work if an agent restarts or a new agent joins mid-run.

### Exit Code Aggregation

When you need to know if all shards passed:

```bash
#!/usr/bin/env bash
set -uo pipefail

# Run shards, capture exit codes
lokt guard test-shard-1 --ttl 5m -- go test ./internal/...
EXIT_1=$?

lokt guard test-shard-2 --ttl 5m -- go test ./cmd/...
EXIT_2=$?

lokt guard test-shard-3 --ttl 5m -- go test ./pkg/...
EXIT_3=$?

# Aggregate results
if [ $EXIT_1 -eq 0 ] && [ $EXIT_2 -eq 0 ] && [ $EXIT_3 -eq 0 ]; then
    echo "All shards passed"
    exit 0
else
    echo "Failures: shard-1=$EXIT_1 shard-2=$EXIT_2 shard-3=$EXIT_3"
    exit 1
fi
```

### Dynamic Sharding

For larger test suites, split packages programmatically:

```bash
#!/usr/bin/env bash
# Usage: ./test-shard.sh <shard-id> <total-shards>
SHARD_ID=$1
TOTAL_SHARDS=$2

# Get all packages, select every Nth starting at SHARD_ID
PACKAGES=$(go list ./... | awk "NR % $TOTAL_SHARDS == $SHARD_ID")

lokt guard "test-shard-$SHARD_ID" --ttl 10m -- go test $PACKAGES
```

### Tips

- **Always use TTL**: If an agent crashes, the lock expires and another can retry
- **Naming convention**: `<task>-shard-<N>` makes locks easy to identify
- **Uneven splits are fine**: Some shards finishing early is better than sequential runs

---

## Cache Checking

Skip expensive operations when cached results exist. Use locks as cache markers with `exists` for silent checking.

### Basic Pattern

```bash
COMMIT=$(git rev-parse HEAD)

# Check if already done
if lokt exists "tests-passed-$COMMIT"; then
    echo "Tests already passed for $COMMIT"
    exit 0
fi

# Run expensive operation
go test ./...

# Mark complete (TTL = cache lifetime)
lokt lock "tests-passed-$COMMIT" --ttl 24h
```

### Why `exists` Instead of `status`

| Command | Output | Use case |
|---------|--------|----------|
| `lokt status name` | Prints lock details | Human inspection |
| `lokt exists name` | Silent, exit code only | Scripts and conditionals |

```bash
# Clean one-liner
lokt exists "lint-passed-$COMMIT" && exit 0

# vs. the old way
lokt status "lint-passed-$COMMIT" &>/dev/null && exit 0
```

### Multi-Operation Caching

Cache multiple operations independently:

```bash
COMMIT=$(git rev-parse HEAD)

# Each operation checks its own cache
lokt exists "fmt-$COMMIT"  || { go fmt ./...        && lokt lock "fmt-$COMMIT"  --ttl 24h; }
lokt exists "lint-$COMMIT" || { golangci-lint run   && lokt lock "lint-$COMMIT" --ttl 24h; }
lokt exists "test-$COMMIT" || { go test ./...       && lokt lock "test-$COMMIT" --ttl 24h; }
```

### Cache Invalidation

Caches auto-expire via TTL. For manual invalidation:

```bash
# Clear specific cache
lokt unlock "tests-passed-abc123" --force

# Clear all test caches (use with caution)
lokt status --json | jq -r '.[] | select(.name | startswith("tests-")) | .name' | xargs -I{} lokt unlock {} --force
```

---

## Scripting Integration

Patterns for integrating lokt into shell scripts and CI pipelines.

### Exit Code Handling

Lokt uses consistent exit codes for scripting:

| Code | Meaning | Typical action |
|------|---------|----------------|
| 0 | Success | Continue |
| 1 | General error | Abort |
| 2 | Lock held by another | Wait or skip |
| 3 | Lock not found | Create or ignore |
| 4 | Not lock owner | Use --force if authorized |

```bash
lokt lock deploy --ttl 30m
case $? in
    0) echo "Lock acquired, proceeding..." ;;
    2) echo "Deploy in progress, exiting"; exit 0 ;;
    *) echo "Unexpected error"; exit 1 ;;
esac
```

### Trap Pattern for Manual Locks

When using `lock`/`unlock` instead of `guard`, ensure cleanup on exit:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Acquire lock
lokt lock deploy --ttl 30m

# Release on any exit (success, failure, signal)
trap 'lokt unlock deploy' EXIT

# Your work here
./scripts/deploy.sh
```

`guard` handles this automatically—prefer it when possible:

```bash
# Equivalent, but cleaner
lokt guard deploy --ttl 30m -- ./scripts/deploy.sh
```

### CI/CD: Graceful Skip

In CI, you often want to skip gracefully rather than fail when another job is deploying:

```bash
#!/usr/bin/env bash
# .github/scripts/deploy.sh

if ! lokt lock deploy --ttl 30m; then
    echo "Another deployment running, skipping this job"
    exit 0  # Success exit so CI stays green
fi

trap 'lokt unlock deploy' EXIT
./scripts/do-deploy.sh
```

### CI/CD: Wait for Lock

When you need to wait rather than skip:

```bash
#!/usr/bin/env bash
# Wait up to 10 minutes for lock, with backoff

lokt guard deploy --ttl 30m --wait --timeout 10m -- ./scripts/deploy.sh
```

### Combining Patterns

Real-world example: cached, sharded test run with CI integration:

```bash
#!/usr/bin/env bash
set -euo pipefail

COMMIT=$(git rev-parse HEAD)
SHARD_ID=${CI_NODE_INDEX:-1}

# Skip if this shard already passed for this commit
if lokt exists "test-shard-$SHARD_ID-$COMMIT"; then
    echo "Shard $SHARD_ID already passed for $COMMIT"
    exit 0
fi

# Run tests with lock (prevents duplicate runs)
lokt guard "test-shard-$SHARD_ID" --ttl 10m -- go test ./...
EXIT_CODE=$?

# Cache success
if [ $EXIT_CODE -eq 0 ]; then
    lokt lock "test-shard-$SHARD_ID-$COMMIT" --ttl 24h
fi

exit $EXIT_CODE
```

---

## Script Wrappers

The key insight for multi-agent coordination: **agents shouldn't need to know about lokt**. Embed guards in scripts they naturally call, making coordination invisible and automatic.

### Guarded Wrappers

Wrap dangerous operations. Agents call the script, lokt handles serialization:

```bash
#!/usr/bin/env bash
# scripts/deploy.sh - agents call this without thinking about locks
exec lokt guard deploy --ttl 30m -- ./scripts/_deploy-impl.sh "$@"
```

```bash
#!/usr/bin/env bash
# scripts/migrate.sh
exec lokt guard db-migrate --ttl 10m -- ./scripts/_migrate-impl.sh "$@"
```

```bash
#!/usr/bin/env bash
# scripts/build.sh - prevent parallel build conflicts
exec lokt guard build --ttl 5m -- go build -o bin/app ./cmd/app
```

Agents run `./scripts/deploy.sh` and get automatic serialization. No prompting needed, can't forget to lock.

### Git Push Serialization

Parallel agents pushing causes "rejected - non-fast-forward" chaos:

```bash
#!/usr/bin/env bash
# scripts/safe-push.sh
lokt guard git-push --ttl 2m -- bash -c '
    git pull --rebase origin main && git push origin main
'
```

Or as a git alias:

```ini
# ~/.gitconfig
[alias]
    safe-push = !lokt guard git-push --ttl 2m -- git push
```

### Cached Analysis

Lint, type-check, security scans—run once, skip for subsequent agents:

```bash
#!/usr/bin/env bash
# scripts/lint.sh
COMMIT=$(git rev-parse HEAD)

lokt exists "lint-$COMMIT" && { echo "Already linted"; exit 0; }

lokt guard lint --ttl 5m -- golangci-lint run
lokt lock "lint-$COMMIT" --ttl 24h
```

### Freeze for Maintenance

Block all agents from a resource during manual work:

```bash
# Human runs before manual DB maintenance
lokt freeze db-migrate --ttl 1h

# All agent migrate.sh calls now fail fast
# When done:
lokt unfreeze db-migrate
```

### Resource Pool

For limited resources (API rate limits, connection pools):

```bash
#!/usr/bin/env bash
# scripts/call-api.sh - max 2 concurrent API callers
for slot in 1 2; do
    if lokt lock "api-slot-$slot" --ttl 30s 2>/dev/null; then
        trap "lokt unlock api-slot-$slot" EXIT
        curl -X POST https://api.example.com/...
        exit $?
    fi
done
echo "All API slots busy, retrying..."
sleep 5
exec "$0" "$@"
```

### Common Wrappers

Scripts that should have guards baked in:

| Script | Lock name | Why |
|--------|-----------|-----|
| `deploy.sh` | `deploy` | One deploy at a time |
| `migrate.sh` | `db-migrate` | Schema changes serialize |
| `build.sh` | `build` | Avoid parallel build conflicts |
| `safe-push.sh` | `git-push` | Prevent rebase races |
| `lint.sh` | `lint` + cache | Deduplicate expensive analysis |
| `terraform.sh` | `terraform` | State file conflicts |

---

## Quick Reference

| Pattern | Command |
|---------|---------|
| Exclusive execution | `lokt guard <name> -- <cmd>` |
| Check if cached | `lokt exists <name> && exit 0` |
| Mark as done | `lokt lock <name> --ttl <duration>` |
| Shard naming | `<task>-shard-<N>` or `<task>-<commit>` |
| CI graceful skip | Check exit code 2, exit 0 |
| Cleanup on exit | `trap 'lokt unlock <name>' EXIT` |
