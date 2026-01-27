# Lokt Quickstart

Lokt serializes high-risk CLI operations (deploys, migrations, terraform apply) across multiple terminals and agents on the same machine.

## Installation

```bash
# Build from source
go build -o lokt ./cmd/lokt

# Install system-wide
sudo cp lokt /usr/local/bin/

# Or install to user bin
cp lokt ~/bin/
```

## Basic Usage

### Guard a Command

The most common pattern - wrap any command to ensure only one runs at a time:

```bash
# Only one deploy can run at a time
lokt guard deploy -- ./scripts/deploy.sh

# With a TTL (auto-expires if process hangs)
lokt guard deploy --ttl 30m -- ./scripts/deploy.sh
```

If another process holds the lock:
```
error: lock "deploy" held by nikola@macbook (pid 12345) for 2m30s
```

### Manual Lock/Unlock

For more control over lock lifecycle:

```bash
# Acquire
lokt lock deploy --ttl 30m

# Check status
lokt status deploy

# Release
lokt unlock deploy
```

### Check Lock Status

```bash
# List all locks
lokt status

# Single lock details
lokt status deploy
```

Output:
```
name:     deploy
owner:    nikola
host:     macbook
pid:      12345 (alive)
age:      2m30s
ttl:      1800s
```

## Handling Stale Locks

Locks can become stale when:
- The TTL expires
- The holding process crashes (dead PID)

```bash
# Remove only if stale (safe)
lokt unlock deploy --break-stale

# Force remove (break-glass, use with caution)
lokt unlock deploy --force

# Auto-prune expired locks while listing
lokt status --prune-expired
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Lock held by another owner |
| 3 | Lock not found |
| 4 | Not lock owner |

Use exit code 2 to detect contention in scripts:

```bash
lokt lock deploy --ttl 30m
if [ $? -eq 2 ]; then
    echo "Deploy already in progress, exiting"
    exit 1
fi
```

---

## Sample Deployment Script

Here's a production-ready deployment script using lokt:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Configuration
LOCK_NAME="deploy"
LOCK_TTL="30m"
DEPLOY_ENV="${1:-staging}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# The actual deployment logic
do_deploy() {
    log_info "Starting deployment to ${DEPLOY_ENV}..."

    # Your deployment steps here
    # Example:
    # 1. Pull latest code
    log_info "Pulling latest code..."
    git pull origin main

    # 2. Build
    log_info "Building application..."
    # npm run build
    # go build ./...

    # 3. Run migrations
    log_info "Running database migrations..."
    # ./scripts/migrate.sh

    # 4. Deploy
    log_info "Deploying to ${DEPLOY_ENV}..."
    # kubectl apply -f k8s/
    # aws ecs update-service ...
    # rsync -avz ./dist/ server:/var/www/

    # 5. Health check
    log_info "Running health checks..."
    # curl -f https://myapp.com/health || exit 1

    log_info "Deployment complete!"
}

# Main entry point - wrapped with lokt guard
main() {
    log_info "Acquiring deploy lock (TTL: ${LOCK_TTL})..."

    # lokt guard handles:
    # - Acquiring the lock (fails fast if held)
    # - Running the command
    # - Releasing the lock on exit (success, failure, or signal)
    lokt guard "${LOCK_NAME}" --ttl "${LOCK_TTL}" -- bash -c "$(declare -f log_info log_warn log_error do_deploy); do_deploy"
}

main "$@"
```

### Simpler Version

If you prefer minimal wrapper:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Wrap entire deploy in lokt guard
exec lokt guard deploy --ttl 30m -- bash -c '
    set -euo pipefail

    echo "Deploying..."
    git pull origin main
    npm run build
    npm run deploy

    echo "Done!"
'
```

### CI/CD Integration

For CI systems where multiple jobs might deploy:

```bash
#!/usr/bin/env bash
# .github/scripts/deploy.sh

# Try to acquire lock, exit gracefully if another deploy is running
if ! lokt lock deploy --ttl 30m; then
    echo "Another deployment is in progress, skipping..."
    exit 0  # Exit success so CI doesn't fail
fi

# Ensure we release the lock on exit
trap 'lokt unlock deploy' EXIT

# Your deployment logic
./scripts/do-deploy.sh
```

---

## Where Locks Are Stored

Lokt stores locks in this priority order:

1. `$LOKT_ROOT` environment variable (if set)
2. `.git/lokt/locks/` (git common dir - shared across worktrees)
3. `.lokt/locks/` in current directory

This means all terminals/agents in the same repo share the same locks automatically.

## Tips

- **Always use TTL** for long-running operations to prevent permanent locks from crashes
- **Use descriptive lock names**: `deploy-prod`, `migrate-db`, `terraform-apply`
- **Check status first** if unsure: `lokt status`
- **Use `--break-stale`** instead of `--force` when possible - it's safer
