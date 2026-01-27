#!/bin/bash
#
# Build lokt with version info embedded
#
# Usage:
#   ./scripts/build.sh              # Build with git info
#   ./scripts/build.sh v1.0.0       # Build with explicit version
#

set -e

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# Version info
VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Build
echo "Building lokt..."
echo "  Version: $VERSION"
echo "  Commit:  $COMMIT"
echo "  Date:    $DATE"

go build \
    -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" \
    -o lokt \
    ./cmd/lokt

echo "Done: ./lokt"
