package main

import (
	"flag"
	"fmt"
	"os"
)

// hexwallScript is the complete bash script emitted by `lokt demo`.
// It spawns workers that race to build a hex wall using `lokt guard`
// for coordination. Run with --no-lock to see corruption.
const hexwallScript = `#!/usr/bin/env bash
set -euo pipefail

# ══════════════════════════════════════════════════════════════════
# lokt hexwall demo
#
# This script spawns worker processes that race to build a "hex
# wall" — a grid of hex characters (0-f), built one character at a
# time. Each character requires exclusive access to a shared
# counter file.
#
# By default, workers use ` + "`" + `lokt guard` + "`" + ` to coordinate. The wall
# comes out clean and ordered. Run with --no-lock to remove the
# locking and watch the output shred itself.
#
# Usage:
#   ./lokt-hexwall-demo.sh              # with locking (clean)
#   ./lokt-hexwall-demo.sh --no-lock    # without locking (chaos)
#   ./lokt-hexwall-demo.sh --rows 16 --cols 32 --workers 16
#
# Requires: bash 3.2+, lokt (unless --no-lock)
# ══════════════════════════════════════════════════════════════════

# ── Configuration ─────────────────────────────────────────────────
# Defaults can be overridden via environment variables or flags.
# Flags take precedence over env vars.
#
# Example:
#   WORKERS=4 ./lokt-hexwall-demo.sh --rows 4
#
# WORKERS  — number of background processes competing for cells
# ROWS     — number of rows in the hex wall
# COLS     — number of characters per row (the hex fill width)
# NOLOCK   — set to 1 to bypass lokt (shows the chaos)
# LOCKNAME — the lock name passed to lokt guard

WORKERS="${WORKERS:-8}"
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
            echo "  --workers N       Number of worker processes (default: 8)"
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
# The critical section models a multi-step computation over shared
# state — the kind of thing locks exist to protect. It reads a
# position counter, breaks the character derivation into three
# intermediate results stored in shared files, reads them back,
# and combines them for the final character.
#
# The three work files hold values that, when consistent, combine
# to produce the correct character: (a + b + c) % 16 = row % 16.
# Under the lock, all three are from the same worker — they cancel
# correctly, and each row gets a uniform fill (row 0 = '0', etc.).
#
# Without the lock, other workers overwrite the work files between
# our writes and our reads. We read a mix of values from different
# workers at different grid positions. The values no longer cancel —
# the character is unpredictable.
#
# To make this worse, the counter is temporarily set to an
# intermediate value between our read and our final write-back.
# Under the lock, no one sees this. Without the lock, other workers
# read the intermediate counter, jump to wrong grid positions, and
# write intermediates for those positions — cross-contaminating
# everyone's work files.
#
# This mirrors real systems: a map-reduce accumulator, a running
# sum, a multi-field database update — any computation that stores
# partial results in shared memory needs a lock to prevent other
# threads from seeing (and acting on) incomplete state.
#
# One character per lock acquisition. With a 16x32 grid, that is
# 512 acquisitions, each contested by up to 8 workers. That is
# the point — maximum contention, minimal critical section.

cat > "$STATE_DIR/critical.sh" << 'CRITICAL_EOF'
#!/usr/bin/env bash
# ── Critical section ──────────────────────────────────────────
# Runs INSIDE the lokt guard lock (or unguarded in --no-lock mode).
# Models a multi-step computation: reads position, stores
# intermediate results in shared files, reads them back, combines
# for the character, writes to row buffer, fixes the counter.
#
# Arguments: <state_dir> <out_file> <cols> <rows>

state_dir="$1"
out_file="$2"
cols="$3"
rows="$4"
total=$(( rows * cols ))

# Read the position counter. In no-lock mode, this might be an
# intermediate value from another worker — that is the point.
i=$(cat "$state_dir/next" 2>/dev/null || true)
i="${i//[!0-9]/}"
i="${i:-0}"

# All cells filled? Nothing left to do.
# Intermediate values from other workers are always < total
# (they use RANDOM % total), so this only triggers on the real
# "done" value when the counter reaches total.
if [ "$i" -ge "$total" ]; then
    exit 0
fi

# Determine WHERE to write.
row=$(( i / cols ))
col=$(( i % cols ))

# Row label: hex row number (0-f), used to identify rows in output.
hex_chars="0123456789abcdef"
label="${hex_chars:$(( row % 16 )):1}"

# ── Multi-step computation over shared state ─────────────────
# Break the character derivation into three intermediate values
# stored in shared files. The formula:
#
#   character = hex[ (a + b + c) % 16 ]
#
# where a = row % 16, b = col % 16, c = (16 - b) % 16.
# When all three are consistent: (a + b + c) % 16 = a % 16
# because b + c ≡ 0 (mod 16). So character = hex[row % 16].
#
# Under the lock: all three values are ours. They cancel. Clean.
# Without the lock: values come from different workers at different
# positions. They do not cancel. The character is noise.

# Phase 1: Store our intermediates in the shared work files.
wa=$(( row % 16 ))
wb=$(( col % 16 ))
wc=$(( (16 - wb) % 16 ))
echo "$wa" > "$state_dir/work_a"
echo "$wb" > "$state_dir/work_b"
echo "$wc" > "$state_dir/work_c"

# Phase 2: Counter enters intermediate state.
# Temporarily set to an arbitrary grid position — represents
# "computation in progress." Under the lock, no one sees this.
# Without the lock, other workers read it, jump to the wrong
# position, and write their own intermediates to the work files.
echo $(( RANDOM % total )) > "$state_dir/next"

# Phase 3: Read back the computation results.
# Under the lock: we read our own values from phase 1.
# Without the lock: other workers (misdirected by phase 2) have
# overwritten some work files with values from different positions.
# The b+c cancellation breaks, and the character is wrong.
ra=$(cat "$state_dir/work_a" 2>/dev/null || true)
rb=$(cat "$state_dir/work_b" 2>/dev/null || true)
rc=$(cat "$state_dir/work_c" 2>/dev/null || true)
ra="${ra//[!0-9]/}"; ra="${ra:-0}"
rb="${rb//[!0-9]/}"; rb="${rb:-0}"
rc="${rc//[!0-9]/}"; rc="${rc:-0}"

ch="${hex_chars:$(( (ra + rb + rc) % 16 )):1}"

# Phase 4: Write the character to the row buffer.
row_file="$state_dir/row_$(printf '%04d' "$row")"
printf "%s" "$ch" >> "$row_file"

# Flush this row when the buffer has enough characters.
# We check buffer size instead of column position because counter
# poisoning in phase 2 can misdirect workers to any grid position.
# A column check would fire prematurely from misdirected workers.
# In lock mode the buffer reaches $cols exactly once (sequential).
# In no-lock mode concurrent appends inflate it — we still flush
# once, and head -c caps the output at $cols characters.
# mkdir is atomic — exactly one worker succeeds per row,
# preventing duplicate output lines.
buf_size=$(wc -c < "$row_file" 2>/dev/null || echo 0)
if [ "$buf_size" -ge "$cols" ]; then
    if mkdir "$state_dir/done_$(printf '%04d' "$row")" 2>/dev/null; then
        content=$(head -c "$cols" "$row_file")
        printf "%s | %s\n" "$label" "$content" >> "$out_file"
    fi
fi

# Phase 5: Finalize — restore counter to correct next value.
echo $(( i + 1 )) > "$state_dir/next"
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
    local iters=0
    # Cap iterations so workers always terminate. In lock mode the
    # counter reaches TOTAL well before this limit. In no-lock mode
    # the counter resets due to races and may never reach TOTAL —
    # the cap ensures bounded output with visible corruption.
    local max_iters=$(( TOTAL * 2 ))

    while [ "$iters" -lt "$max_iters" ]; do
        iters=$(( iters + 1 ))

        if [ "$NOLOCK" -eq 1 ]; then
            # Suppress stderr — in no-lock mode, concurrent writes
            # corrupt the counter files causing harmless parse errors.
            bash "$STATE_DIR/critical.sh" \
                "$STATE_DIR" "$STATE_DIR/out" \
                "$COLS" "$ROWS" 2>/dev/null || true
        else
            # Redirect stderr to suppress "lock held" contention messages.
            # These are expected — the whole point is that workers retry
            # until the lock is free. We only care about the final output.
            lokt guard --ttl 30s "$LOCKNAME" -- \
                bash "$STATE_DIR/critical.sh" \
                "$STATE_DIR" "$STATE_DIR/out" \
                "$COLS" "$ROWS" 2>/dev/null || true
        fi

        # Quick exit check: are all positions filled? This read is racy
        # (no lock), but that is fine — it is just a hint. The real
        # bounds check is inside critical.sh, under the lock.
        # Strip non-digits to handle corrupt reads in no-lock mode.
        cur=$(cat "$STATE_DIR/next" 2>/dev/null || true)
        cur="${cur//[!0-9]/}"
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

# ── Final flush ─────────────────────────────────────────────────
# In no-lock mode, counter races can prevent some rows from being
# flushed during execution (no worker happened to process the last
# column for that row). Flush any remaining rows now.

r=0
while [ "$r" -lt "$ROWS" ]; do
    row_pad=$(printf '%04d' "$r")
    if [ -f "$STATE_DIR/row_$row_pad" ] && ! [ -d "$STATE_DIR/done_$row_pad" ]; then
        hex_chars="0123456789abcdef"
        label="${hex_chars:$(( r % 16 )):1}"
        content=$(head -c "$COLS" "$STATE_DIR/row_$row_pad")
        printf "%s | %s\n" "$label" "$content" >> "$STATE_DIR/out"
    fi
    r=$(( r + 1 ))
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
