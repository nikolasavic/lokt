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
# The critical section does exactly this:
#   1. Read the shared counter to get position (WHERE to write)
#   2. If position >= ROWS * COLS: all cells filled, exit
#   3. Compute row = position / COLS, col = position % COLS
#   4. Derive the character via a shared "ping" file (see below)
#   5. Append that character to $STATE_DIR/row_NNNN
#   6. If col == COLS-1 (last column): row complete, append to output
#   7. Increment the counter
#
# Character derivation uses a nonce (random number) written to a
# shared file. The worker writes its nonce, then reads the file
# back. Under the lock, the read returns the worker's own nonce —
# the nonce matches, confirming exclusive access, and the worker
# uses the correct character hex[row % 16] for uniform rows.
#
# Without the lock, other workers overwrite the nonce between the
# write and the read. The worker reads SOMEONE ELSE'S nonce — the
# mismatch reveals the race, and the foreign nonce (a random number)
# becomes the character source. Since every worker writes a different
# random nonce, every cell gets an unpredictable character.
#
# One character per lock acquisition. With a 16x32 grid, that is
# 512 acquisitions, each contested by up to 8 workers. That is
# the point — maximum contention, minimal critical section.

cat > "$STATE_DIR/critical.sh" << 'CRITICAL_EOF'
#!/usr/bin/env bash
# ── Critical section ──────────────────────────────────────────
# Runs INSIDE the lokt guard lock (or unguarded in --no-lock mode).
# Reads the counter, derives position and character, writes the
# character to the row buffer, increments the counter.
#
# Arguments: <state_dir> <out_file> <cols> <rows>

state_dir="$1"
out_file="$2"
cols="$3"
rows="$4"
total=$(( rows * cols ))

# Read the position counter. In no-lock mode, races produce
# corrupt reads. Strip non-digits to survive.
i=$(cat "$state_dir/next" 2>/dev/null || true)
i="${i//[!0-9]/}"
i="${i:-0}"

# All cells filled? Nothing left to do.
if [ "$i" -ge "$total" ]; then
    exit 0
fi

# Determine WHERE to write.
row=$(( i / cols ))
col=$(( i % cols ))

# Row label: hex row number (0-f), used to identify rows in output.
hex_chars="0123456789abcdef"
label="${hex_chars:$(( row % 16 )):1}"

# ── Character derivation via shared nonce file ───────────────
# Write a random nonce to a shared file, then read it back.
#
# Under the lock: no other worker can intervene. We read our own
# nonce (it matches), confirming exclusive access. Character is
# hex[row % 16]. Each row gets a uniform fill: row 0 = all '0',
# row 1 = all '1', etc.
#
# Without the lock: between our write and our read, other workers
# overwrite the nonce with THEIR random numbers. We read someone
# else's nonce — the mismatch tells us the data is compromised.
# We use the foreign nonce (mod 16) as the character, which is
# effectively random. With 8 workers racing, every cell gets an
# unpredictable character — the output is noise.
my_nonce=$RANDOM
echo "$my_nonce" > "$state_dir/nonce"
# Brief pause: under the lock this is a no-op (no other writers).
# Without the lock, it gives other workers time to overwrite the
# nonce file, ensuring we almost always read a foreign value.
sleep 0.001
their_nonce=$(cat "$state_dir/nonce" 2>/dev/null || true)
their_nonce="${their_nonce//[!0-9]/}"
their_nonce="${their_nonce:-0}"
if [ "$their_nonce" -eq "$my_nonce" ]; then
    # Nonce matches — we have exclusive access. Use correct character.
    ch="${hex_chars:$(( (i / cols) % 16 )):1}"
else
    # Nonce mismatch — race detected. Use a fresh random number for
    # the character. Each worker has its own $RANDOM stream, so even
    # workers at the same position produce different characters.
    # Under the lock, this branch is never reached.
    ch="${hex_chars:$(( RANDOM % 16 )):1}"
fi

# Append this character to the row's buffer file. Each row
# accumulates $cols characters, one per critical section call.
row_file="$state_dir/row_$(printf '%04d' "$row")"
printf "%s" "$ch" >> "$row_file"

# If this was the last column, the row is complete. Format it
# as "<label> | <all characters>" and append to the output file.
# The tail -f process will pick it up and display it live.
# head -c caps the buffer at $cols characters. In lock mode the
# buffer is exactly $cols, so this is a no-op. In no-lock mode
# concurrent appends inflate the buffer — capping keeps output
# lines bounded so the terminal is not flooded.
if [ "$col" -eq $(( cols - 1 )) ]; then
    content=$(head -c "$cols" "$row_file")
    printf "%s | %s\n" "$label" "$content" >> "$out_file"
fi

# Advance the counter for the next worker.
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
