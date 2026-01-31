package main

import (
	"flag"
	"fmt"
	"os"
)

// hexwallScript is the complete bash script emitted by `lokt demo`.
// It spawns hundreds of workers that race to build a hex wall using
// `lokt guard` for coordination. Run with --no-lock to see corruption.
const hexwallScript = `#!/usr/bin/env bash
set -euo pipefail

# ══════════════════════════════════════════════════════════════════
# lokt hexwall demo
#
# This script spawns hundreds of worker processes that race to
# build a "hex wall" — a grid of hex characters (0-f), built one
# character at a time. Each character requires exclusive access to
# a shared counter file.
#
# By default, workers use ` + "`" + `lokt guard` + "`" + ` to coordinate. The wall
# comes out clean and ordered. Run with --no-lock to remove the
# locking and watch the output shred itself.
#
# Usage:
#   ./lokt-hexwall-demo.sh              # with locking (clean)
#   ./lokt-hexwall-demo.sh --no-lock    # without locking (chaos)
#   ./lokt-hexwall-demo.sh --rows 256 --cols 64 --workers 1024
#
# Requires: bash 3.2+, lokt (unless --no-lock)
# ══════════════════════════════════════════════════════════════════

# ── Configuration ─────────────────────────────────────────────────
# Defaults can be overridden via environment variables or flags.
# Flags take precedence over env vars.
#
# Example:
#   WORKERS=128 ./lokt-hexwall-demo.sh --rows 8
#
# WORKERS  — number of background processes competing for cells
# ROWS     — number of rows in the hex wall
# COLS     — number of characters per row (the hex fill width)
# NOLOCK   — set to 1 to bypass lokt (shows the chaos)
# LOCKNAME — the lock name passed to lokt guard

WORKERS="${WORKERS:-512}"
ROWS="${ROWS:-16}"
COLS="${COLS:-32}"
NOLOCK="${NOLOCK:-0}"
LOCKNAME="${LOCKNAME:-demo.hexwall}"

while [ $# -gt 0 ]; do
    case "$1" in
        --workers)  WORKERS="$2"; shift 2 ;;
        --rows)     ROWS="$2"; shift 2 ;;
        --cols)     COLS="$2"; shift 2 ;;
        --no-lock)  NOLOCK=1; shift ;;
        --lock-name) LOCKNAME="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --workers N       Number of worker processes (default: 512)"
            echo "  --rows N          Number of rows in the wall (default: 16)"
            echo "  --cols N          Width of each row's hex fill (default: 32)"
            echo "  --no-lock         Bypass lokt (shows corruption from races)"
            echo "  --lock-name NAME  Lock name for lokt guard (default: demo.hexwall)"
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
# Before spawning anything, make sure we have what we need.
# In lock mode, lokt must be on PATH. We always need a writable
# temp directory for shared state.
#
# If these checks fail, we bail out with a clear message. Better
# to fail here than to spawn 512 workers and watch them crash.

if [ "$NOLOCK" -ne 1 ]; then
    if ! command -v lokt >/dev/null 2>&1; then
        echo "error: lokt not found on PATH" >&2
        echo "" >&2
        echo "Install lokt first, or run with --no-lock to skip locking." >&2
        echo "  https://github.com/nikolasavic/lokt" >&2
        exit 1
    fi
fi

# Handle trivial cases: nothing to build if the grid is empty.
if [ "$ROWS" -eq 0 ] || [ "$COLS" -eq 0 ]; then
    exit 0
fi

# ── Shared state directory ────────────────────────────────────────
# Create a temporary directory for all shared state:
#   next         — shared cell counter (an integer, starts at 0)
#   row_NNNN     — per-row buffer files (characters accumulate here)
#   out          — completed rows, one per line (tail -f reads this)
#   critical.sh  — the script that runs inside the lock
#
# We use mktemp -d with a template that works on both macOS and
# Linux. The whole directory is removed on exit (see cleanup trap).

STATE_DIR=$(mktemp -d -t hexwall.XXXXXX)

# Initialize the counter at 0 and create the output file.
echo 0 > "$STATE_DIR/next"
touch "$STATE_DIR/out"

# ── Write the critical section script ─────────────────────────────
# This is the code that runs INSIDE the lock. We write it to a
# temp file so lokt guard can exec it via "bash critical.sh".
#
# Why a separate script file? Because lokt guard runs an external
# command — it cannot run a bash function. Writing the critical
# section to a file is portable, readable, and debuggable. You can
# even add debug prints to it and re-run the demo.
#
# The critical section does exactly this:
#   1. Read the cell counter from $STATE_DIR/next
#   2. If counter >= ROWS * COLS: all cells filled, exit
#   3. Compute row = counter / COLS, col = counter % COLS
#   4. Compute the hex nibble: row % 16 -> character (0-9, a-f)
#   5. Append that one character to $STATE_DIR/row_NNNN
#   6. If col == COLS-1 (last column): the row is complete —
#      format it as "<nibble> | <characters>" and append to output
#   7. Increment the counter
#
# One character per lock acquisition. With a 16x32 grid, that is
# 512 acquisitions, each contested by up to 512 workers. That is
# the point — maximum contention, minimal critical section.

cat > "$STATE_DIR/critical.sh" << 'CRITICAL_EOF'
#!/usr/bin/env bash
# ── Critical section ──────────────────────────────────────────
# Runs INSIDE the lokt guard lock (or unguarded in --no-lock mode).
# Claims one cell, writes one character, increments the counter.
#
# Arguments: <next_file> <state_dir> <out_file> <cols> <rows>

next_file="$1"
state_dir="$2"
out_file="$3"
cols="$4"
rows="$5"
total=$(( rows * cols ))

# Read the current cell counter. In no-lock mode, a race can
# produce an empty read — default to 0 (re-process is harmless).
i=$(cat "$next_file" 2>/dev/null || true)
i="${i:-0}"

# All cells filled? Nothing left to do.
if [ "$i" -ge "$total" ]; then
    exit 0
fi

# Compute which row and column this cell belongs to.
row=$(( i / cols ))
col=$(( i % cols ))

# The hex nibble cycles every 16 rows: 0,1,2,...,e,f,0,1,...
nib=$(( row % 16 ))

# Pick the hex character for this nibble. We index into a string
# because bash 3.2 does not have associative arrays or printf %x.
hex_chars="0123456789abcdef"
ch="${hex_chars:$nib:1}"

# Append this character to the row's buffer file. Each row
# accumulates $cols characters, one per critical section call.
row_file="$state_dir/row_$(printf '%04d' "$row")"
printf "%s" "$ch" >> "$row_file"

# If this was the last column, the row is complete. Format it
# as "<nibble> | <all characters>" and append to the output file.
# The tail -f process will pick it up and display it live.
if [ "$col" -eq $(( cols - 1 )) ]; then
    content=$(cat "$row_file")
    printf "%s | %s\n" "$ch" "$content" >> "$out_file"
fi

# Advance the counter for the next worker.
echo $(( i + 1 )) > "$next_file"
CRITICAL_EOF

chmod +x "$STATE_DIR/critical.sh"

# ── Cleanup trap ──────────────────────────────────────────────────
# On exit — whether normal completion, Ctrl-C, or any error — we:
#   1. Kill the tail -f process (stops output)
#   2. Kill all remaining worker processes
#   3. Remove the temporary state directory
#
# This prevents orphaned workers from lingering after Ctrl-C.
# We trap INT and TERM to convert them into a clean exit, then
# trap EXIT to do the actual cleanup. This ensures cleanup runs
# exactly once regardless of how the script terminates.

TAIL_PID=""
WORKER_PIDS=""

cleanup() {
    # Kill tail first so no more output appears.
    if [ -n "$TAIL_PID" ] && kill -0 "$TAIL_PID" 2>/dev/null; then
        kill "$TAIL_PID" 2>/dev/null
        wait "$TAIL_PID" 2>/dev/null || true
    fi

    # Kill all workers that might still be running.
    for pid in $WORKER_PIDS; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null
        fi
    done
    for pid in $WORKER_PIDS; do
        wait "$pid" 2>/dev/null || true
    done

    # Remove the temporary state directory and all its contents.
    rm -rf "$STATE_DIR"
}

# INT/TERM -> clean exit -> triggers EXIT trap.
trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT

# ── Start live output ─────────────────────────────────────────────
# We use tail -f to stream completed rows to the terminal in real
# time. Rows appear as workers finish them — you see the wall
# being built incrementally.
#
# Why tail -f instead of printing inside the lock? Because TTY I/O
# is slow. If we printed inside the critical section, every lock
# acquisition would block on terminal output, inflating lock hold
# time and adding contention. Instead, workers write to a file
# (fast), and tail handles the display (outside the lock).

tail -f "$STATE_DIR/out" &
TAIL_PID=$!

# ── Record start time ────────────────────────────────────────────
# bash's SECONDS builtin counts whole seconds since assignment.
# We use it for the summary line at the end.

SECONDS=0

# ── Spawn workers ─────────────────────────────────────────────────
# Launch $WORKERS background processes. Each one loops, claiming
# one cell at a time until all cells are filled.
#
# In lock mode:    lokt guard <name> --ttl 30s -- bash critical.sh
# In no-lock mode: bash critical.sh  (no guard, races everywhere)
#
# No sharding, no partitioning. Every worker can write any cell.
# All $WORKERS workers compete for every single character in the
# wall. That is what makes the demo compelling — the contention
# is real, not simulated.
#
# Each worker runs as a function in a background subshell. We
# collect PIDs so the cleanup trap can kill them on Ctrl-C.

TOTAL=$(( ROWS * COLS ))

worker() {
    while true; do
        if [ "$NOLOCK" -eq 1 ]; then
            bash "$STATE_DIR/critical.sh" \
                "$STATE_DIR/next" "$STATE_DIR" "$STATE_DIR/out" \
                "$COLS" "$ROWS" || true
        else
            # Redirect stderr to suppress "lock held" contention messages.
            # These are expected — the whole point is that workers retry
            # until the lock is free. We only care about the final output.
            lokt guard --ttl 30s "$LOCKNAME" -- \
                bash "$STATE_DIR/critical.sh" \
                "$STATE_DIR/next" "$STATE_DIR" "$STATE_DIR/out" \
                "$COLS" "$ROWS" 2>/dev/null || true
        fi

        # Quick exit check: are all cells filled? This read is racy
        # (no lock), but that is fine — it is just a hint. The real
        # bounds check is inside critical.sh, under the lock.
        cur=$(cat "$STATE_DIR/next" 2>/dev/null || true)
        cur="${cur:-0}"
        if [ "$cur" -ge "$TOTAL" ]; then
            return 0
        fi

        # Tiny sleep to avoid busy-spinning on the lock file.
        # 1ms is enough to let other workers get scheduled.
        sleep 0.001
    done
}

i=0
while [ "$i" -lt "$WORKERS" ]; do
    worker &
    WORKER_PIDS="$WORKER_PIDS $!"
    i=$(( i + 1 ))
done

# ── Wait for completion ───────────────────────────────────────────
# Block until every worker has exited. In lock mode, this takes a
# few seconds. In no-lock mode, it is faster (no lock contention)
# but the output is garbage.

for pid in $WORKER_PIDS; do
    wait "$pid" 2>/dev/null || true
done

# ── Summary ───────────────────────────────────────────────────────
# Give tail a moment to flush any remaining output, then stop it.
# Print a summary line confirming the grid dimensions, worker count,
# and elapsed time.

sleep 0.2

if [ -n "$TAIL_PID" ] && kill -0 "$TAIL_PID" 2>/dev/null; then
    kill "$TAIL_PID" 2>/dev/null
    wait "$TAIL_PID" 2>/dev/null || true
fi
TAIL_PID=""

echo ""
if [ "$NOLOCK" -eq 1 ]; then
    echo "hexwall (NO LOCK): ${ROWS} rows x ${COLS} cols, ${WORKERS} workers, ${SECONDS}s"
else
    echo "hexwall: ${ROWS} rows x ${COLS} cols, ${WORKERS} workers, ${SECONDS}s"
fi
`

func cmdDemo(args []string) int {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: lokt demo")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Generate the hexwall demo script in the current directory.")
		fmt.Fprintln(os.Stderr, "The generated script has its own flags — run it with --help to see them.")
	}
	_ = fs.Parse(args)

	const filename = "lokt-hexwall-demo.sh"
	if err := os.WriteFile(filename, []byte(hexwallScript), 0o600); err != nil {
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
	fmt.Println("  ./lokt-hexwall-demo.sh              # with locking (clean)")
	fmt.Println("  ./lokt-hexwall-demo.sh --no-lock    # without locking (chaos)")
	fmt.Println("  ./lokt-hexwall-demo.sh --help       # all options")
	return ExitOK
}
