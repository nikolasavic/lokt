#!/bin/bash
#
# Install git hooks for lokt
#
# Usage:
#   ./util/install-hooks.sh           # Install hooks
#   ./util/install-hooks.sh status    # Show hook status
#   ./util/install-hooks.sh uninstall # Remove hooks
#

set -e

REPO_ROOT="$(git rev-parse --show-toplevel)"
HOOKS_SRC="$REPO_ROOT/util/hooks"
HOOKS_DST="$REPO_ROOT/.git/hooks"

HOOKS=(pre-commit commit-msg post-merge pre-push)

status() {
    echo "Hook status:"
    for hook in "${HOOKS[@]}"; do
        if [[ -x "$HOOKS_DST/$hook" ]]; then
            if [[ -L "$HOOKS_DST/$hook" ]]; then
                echo "  $hook: installed (symlink)"
            else
                echo "  $hook: installed (copy)"
            fi
        elif [[ -f "$HOOKS_DST/$hook" ]]; then
            echo "  $hook: exists but not executable"
        else
            echo "  $hook: not installed"
        fi
    done
}

install() {
    echo "Installing hooks..."
    for hook in "${HOOKS[@]}"; do
        if [[ -f "$HOOKS_SRC/$hook" ]]; then
            cp "$HOOKS_SRC/$hook" "$HOOKS_DST/$hook"
            chmod +x "$HOOKS_DST/$hook"
            echo "  Installed: $hook"
        else
            echo "  Missing source: $hook"
        fi
    done
    echo "Done."
}

uninstall() {
    echo "Removing hooks..."
    for hook in "${HOOKS[@]}"; do
        if [[ -f "$HOOKS_DST/$hook" ]]; then
            rm "$HOOKS_DST/$hook"
            echo "  Removed: $hook"
        fi
    done
    echo "Done."
}

case "${1:-install}" in
    status)
        status
        ;;
    install)
        install
        ;;
    uninstall)
        uninstall
        ;;
    *)
        echo "Usage: $0 [install|uninstall|status]"
        exit 1
        ;;
esac
