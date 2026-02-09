package main

import (
	"flag"
	"fmt"
	"os"
)

// trunkScript is the complete bash script emitted by `lokt demo trunk`.
// It simulates multiple AI agents performing trunk-based git workflows
// (pull, rebase, build, edit, commit, push) in the same working directory,
// using `lokt guard` for serialization. Run with --no-lock to see collisions.
const trunkScript = `#!/usr/bin/env bash
set -uo pipefail

# ══════════════════════════════════════════════════════════════════
# lokt trunk demo
#
# This script simulates multiple AI agents performing trunk-based
# git workflows in the SAME working directory. Each cycle:
#
#   PULL -> BUILD -> EDIT -> COMMIT -> PUSH
#
# By default, each cycle is wrapped in ` + "`" + `lokt guard` + "`" + ` so only one
# agent operates at a time. Run with --no-lock to remove the
# serialization and watch git collisions pile up.
#
# Usage:
#   ./lokt-trunk-demo.sh                        # with locking (clean)
#   ./lokt-trunk-demo.sh --no-lock              # without locking (chaos)
#   ./lokt-trunk-demo.sh --agents 5 --cycles 6  # larger run
#
# Requires: bash 3.2+, git, lokt (unless --no-lock)
# ══════════════════════════════════════════════════════════════════

# ── Configuration ─────────────────────────────────────────────────
AGENTS="${AGENTS:-3}"
CYCLES="${CYCLES:-4}"
NOLOCK="${NOLOCK:-0}"
LOCKNAME="${LOCKNAME:-demo.trunk}"

while [ $# -gt 0 ]; do
    case "$1" in
        --agents)    AGENTS="$2"; shift 2 ;;
        --cycles)    CYCLES="$2"; shift 2 ;;
        --no-lock)   NOLOCK=1; shift ;;
        --lock-name) LOCKNAME="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --agents N        Number of simulated agents (default: 3)"
            echo "  --cycles N        Cycles per agent (default: 4)"
            echo "  --no-lock         Skip lokt guard (shows git collisions)"
            echo "  --lock-name NAME  Lock name for lokt guard (default: demo.trunk)"
            echo "  --help            Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Try: $0 --help" >&2
            exit 1
            ;;
    esac
done

# ── Preflight ─────────────────────────────────────────────────────
if [ "$NOLOCK" -ne 1 ]; then
    if ! command -v lokt >/dev/null 2>&1; then
        echo "error: lokt not found on PATH" >&2
        echo "" >&2
        echo "Install lokt first, or run with --no-lock to skip locking." >&2
        exit 1
    fi
fi

if ! command -v git >/dev/null 2>&1; then
    echo "error: git not found on PATH" >&2
    exit 1
fi

# ── Setup temp environment ────────────────────────────────────────
DEMO_DIR=$(mktemp -d -t lokt-trunk.XXXXXX)

# Isolated lokt root so demo locks don't pollute the real root.
export LOKT_ROOT="$DEMO_DIR/lokt-root"
mkdir -p "$LOKT_ROOT"

# Create bare remote and clone workspace.
git init --bare "$DEMO_DIR/remote.git" >/dev/null 2>&1

git clone "$DEMO_DIR/remote.git" "$DEMO_DIR/workspace" >/dev/null 2>&1
cd "$DEMO_DIR/workspace" || exit 1

git config user.email "demo@lokt.dev"
git config user.name "lokt-demo"
git checkout -b main >/dev/null 2>&1

# Initial files.
echo "0" > version.txt
touch changelog.txt
git add version.txt changelog.txt
git commit -m "init: v0" >/dev/null 2>&1
git push -u origin main >/dev/null 2>&1

# Event log for live output.
EVENT_LOG="$DEMO_DIR/events.log"
touch "$EVENT_LOG"

# Status tracking files.
mkdir -p "$DEMO_DIR/status"
touch "$DEMO_DIR/status/successes"
touch "$DEMO_DIR/status/failures"

# ── Write cycle.sh ────────────────────────────────────────────────
# The per-agent critical section: pull, build, edit, commit, push.
# Each step reports ok/FAIL. On failure, recovers working tree.

cat > "$DEMO_DIR/cycle.sh" << 'CYCLE_EOF'
#!/usr/bin/env bash
set -uo pipefail

AGENT="$1"
CYCLE="$2"
TOTAL_CYCLES="$3"
EVENT_LOG="$4"
STATUS_DIR="$5"
WORKSPACE="$6"

cd "$WORKSPACE" || exit 1

# Collect step results in memory; emit atomically at the end.
steps=""
failed=0

# ── PULL ──
if git pull --rebase origin main >/dev/null 2>&1; then
    steps="pull ok"
else
    steps="pull FAIL"
    failed=1
    # Recover from failed rebase.
    git rebase --abort >/dev/null 2>&1
    git reset --hard origin/main >/dev/null 2>&1
fi

if [ "$failed" -eq 0 ]; then
    # ── BUILD (simulate) ──
    ver=$(cat version.txt 2>/dev/null || echo "?")
    # Brief pause to simulate build time and widen the race window.
    sleep 0.$(( (RANDOM % 3) + 1 ))
    steps="$steps  build ok"

    # ── EDIT ──
    new_ver=$(( ver + 1 ))
    echo "$new_ver" > version.txt
    echo "v${new_ver} by ${AGENT} (cycle ${CYCLE})" >> changelog.txt
    steps="$steps  edit ok"

    # ── COMMIT ──
    if git add version.txt changelog.txt >/dev/null 2>&1 && \
       git commit -m "v${new_ver} by ${AGENT}" >/dev/null 2>&1; then
        steps="$steps  commit ok"
    else
        steps="$steps  commit FAIL"
        failed=1
        git reset --hard origin/main >/dev/null 2>&1
    fi
fi

if [ "$failed" -eq 0 ]; then
    # ── PUSH ──
    if git push origin main >/dev/null 2>&1; then
        steps="$steps  push ok"
    else
        steps="$steps  push FAIL"
        failed=1
        git reset --hard origin/main >/dev/null 2>&1
    fi
fi

# Emit one atomic log line.
printf "[%-8s] %-60s (cycle %d/%d)\n" "$AGENT" "$steps" "$CYCLE" "$TOTAL_CYCLES" >> "$EVENT_LOG"

# Track success/failure.
if [ "$failed" -eq 0 ]; then
    echo "1" >> "$STATUS_DIR/successes"
else
    echo "1" >> "$STATUS_DIR/failures"
fi
CYCLE_EOF

chmod +x "$DEMO_DIR/cycle.sh"

# ── Cleanup trap ──────────────────────────────────────────────────
TAIL_PID=""
AGENT_PIDS=""

cleanup() {
    if [ -n "$TAIL_PID" ] && kill -0 "$TAIL_PID" 2>/dev/null; then
        kill "$TAIL_PID" 2>/dev/null
        wait "$TAIL_PID" 2>/dev/null || true
    fi
    for pid in $AGENT_PIDS; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null
        fi
    done
    for pid in $AGENT_PIDS; do
        wait "$pid" 2>/dev/null || true
    done
    rm -rf "$DEMO_DIR"
}

trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT

# ── Header ────────────────────────────────────────────────────────
echo "lokt trunk demo"
echo "═══════════════"
if [ "$NOLOCK" -eq 1 ]; then
    echo "mode:   NO LOCK — git collisions expected"
else
    echo "mode:   LOCKED ($LOCKNAME)"
fi
echo "agents: $AGENTS  cycles: $CYCLES  total: $(( AGENTS * CYCLES ))"
echo ""

# ── Start live output ─────────────────────────────────────────────
tail -f "$EVENT_LOG" &
TAIL_PID=$!

SECONDS=0

# ── Spawn agents ─────────────────────────────────────────────────
run_agent() {
    local agent="$1"
    local c=1
    while [ "$c" -le "$CYCLES" ]; do
        if [ "$NOLOCK" -eq 1 ]; then
            bash "$DEMO_DIR/cycle.sh" \
                "$agent" "$c" "$CYCLES" \
                "$EVENT_LOG" "$DEMO_DIR/status" "$DEMO_DIR/workspace" \
                2>/dev/null || true
        else
            LOKT_OWNER="$agent" lokt guard --wait --ttl 30s "$LOCKNAME" -- \
                bash "$DEMO_DIR/cycle.sh" \
                "$agent" "$c" "$CYCLES" \
                "$EVENT_LOG" "$DEMO_DIR/status" "$DEMO_DIR/workspace" \
                2>/dev/null || true
        fi
        # Small jitter between cycles.
        sleep 0.$(( RANDOM % 5 ))
        c=$(( c + 1 ))
    done
}

i=1
while [ "$i" -le "$AGENTS" ]; do
    run_agent "agent-$i" &
    AGENT_PIDS="$AGENT_PIDS $!"
    i=$(( i + 1 ))
done

# ── Wait for completion ──────────────────────────────────────────
for pid in $AGENT_PIDS; do
    wait "$pid" 2>/dev/null || true
done

sleep 0.3

if [ -n "$TAIL_PID" ] && kill -0 "$TAIL_PID" 2>/dev/null; then
    kill "$TAIL_PID" 2>/dev/null
    wait "$TAIL_PID" 2>/dev/null || true
fi
TAIL_PID=""

# ── Summary ───────────────────────────────────────────────────────
expected=$(( AGENTS * CYCLES ))

cd "$DEMO_DIR/workspace" 2>/dev/null || true
git pull --rebase origin main >/dev/null 2>&1 || true
final_ver=$(cat version.txt 2>/dev/null || echo "?")
changelog_lines=$(wc -l < changelog.txt 2>/dev/null || echo "0")
changelog_lines="${changelog_lines// /}"

success_count=$(wc -l < "$DEMO_DIR/status/successes" 2>/dev/null || echo "0")
failure_count=$(wc -l < "$DEMO_DIR/status/failures" 2>/dev/null || echo "0")
success_count="${success_count// /}"
failure_count="${failure_count// /}"

echo ""
echo "── results ─────────────────────────────────────"
echo "version.txt:   v${final_ver}  (expected: v${expected})"
echo "changelog:     ${changelog_lines} entries  (expected: ${expected})"
echo "successes:     ${success_count} / ${expected} cycles"
echo "failures:      ${failure_count} / ${expected} cycles"
echo "elapsed:       ${SECONDS}s"
echo ""

if [ "$final_ver" = "$expected" ] && [ "$changelog_lines" = "$expected" ] && [ "$failure_count" = "0" ]; then
    echo "verdict: CLEAN"
else
    echo "verdict: BROKEN"
fi
`

func cmdDemoTrunk(args []string) int {
	fs := flag.NewFlagSet("demo trunk", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: lokt demo trunk")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Generate the trunk-based dev demo script in the current directory.")
		fmt.Fprintln(os.Stderr, "The generated script has its own flags — run it with --help to see them.")
	}
	_ = fs.Parse(args)

	const filename = "lokt-trunk-demo.sh"
	if err := os.WriteFile(filename, []byte(trunkScript), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
	if err := os.Chmod(filename, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}

	fmt.Printf("Wrote %s\n", filename)
	fmt.Println()
	fmt.Println("Run it:")
	fmt.Println("  ./lokt-trunk-demo.sh                        # with locking (clean)")
	fmt.Println("  ./lokt-trunk-demo.sh --no-lock              # without locking (chaos)")
	fmt.Println("  ./lokt-trunk-demo.sh --agents 5 --cycles 6  # larger run")
	fmt.Println("  ./lokt-trunk-demo.sh --help                 # all options")
	return ExitOK
}
