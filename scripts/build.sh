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

# Build function
do_build() {
    VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")"
    DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    echo "Building lokt..."
    echo "  Version: $VERSION"
    echo "  Commit:  $COMMIT"
    echo "  Date:    $DATE"

    go build \
        -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" \
        -o lokt \
        ./cmd/lokt

    echo "Done: ./lokt"
}

# Use lokt guard if available (eat our own dog food)
# Prefer local ./lokt over PATH (may be newer during development)
if [[ -x "./lokt" ]]; then
    exec ./lokt guard --wait build -- bash -c "$(declare -f do_build); do_build $1"
elif command -v lokt &>/dev/null; then
    exec lokt guard --wait build -- bash -c "$(declare -f do_build); do_build $1"
else
    # First build - no lokt available yet
    echo "(first build - no lock available)"
    do_build "$1"
fi
