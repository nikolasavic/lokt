# L-210: Hexwall Demo

status: draft
created: 2026-01-29
backlog-ref: docs/backlog.md L-210

## Verification
- Level: required
- Environments: local (macOS + Linux)

---

## Problem

Lokt has no demo. The mosaic demo (reverted in `d77ac74`) was too ambitious: full ANSI TUI, in-process goroutine workers, hash-chain ledger, interactive keyboard controls, PPM backing store. That scope was disproportionate to lokt's core value prop.

Users need a simple, compelling way to see **why `lokt` matters** in under 30 seconds. The contrast between "with lock" (clean output) and "without lock" (garbled mess) should be immediately obvious and require zero explanation.

Critically, the demo must be **inspectable**. A user should be able to open the script, read it, modify it, and convince *themselves* that lokt works. A compiled Go binary doesn't do that. A well-commented bash script does.

## Users

- **Evaluators**: Someone who just installed lokt and wants proof it works. They run the script, see the clean wall, run it again with `--no-lock`, see chaos. Convinced.
- **Skeptics**: Someone who doesn't trust the demo. They open the script, read the comments, change the worker count, add a `sleep`, and re-run. The script is the proof — not the binary.
- **Presenters**: Someone showing lokt in a talk or README GIF. Short, deterministic, visually striking.
- **Contributors**: Developers reading the script to understand how `lokt guard` works in practice. The script is executable documentation.

---

## Requirements

### R1: `lokt demo` generates the script

Running `lokt demo` writes a self-contained bash script (`lokt-hexwall-demo.sh`) into the current working directory. The user then runs that script directly:

```bash
lokt demo                                # writes ./lokt-hexwall-demo.sh
./lokt-hexwall-demo.sh                   # clean wall (lokt guards the state)
./lokt-hexwall-demo.sh --no-lock         # chaos (no locking, races everywhere)
```

The Go CLI's only job is to emit the file. The script does all the work.

**Rationale**: The user gets a file they own. They can read it, modify it, re-run it, email it. The demo proves lokt works by being transparent — not by being a black box. `lokt demo` is just a delivery mechanism.

### R2: Heavily commented for readability

The script is written for people who may not be fluent in bash. Every section has a block comment explaining:
- What it does
- Why it does it that way
- What would break if you removed it

Comments should be conversational, not terse. Think "annotated tutorial" not "code comments." The script doubles as a learning resource for `lokt guard`.

Example tone:
```bash
# ── Spawn workers ──────────────────────────────────────────────
# We launch $WORKERS background processes. Each one loops trying
# to claim the next cell in the grid. With 512 workers fighting
# over one lock, contention is extreme — exactly what we want.
#
# Every worker can write any cell. There's no partitioning.
# All 512 workers hammer the same lock for every single character
# in the wall. That's 16 × 32 = 512 lock acquisitions, each one
# contested by up to 512 processes.
```

### R3: Per-cell contention model

Each character in the hex wall is a separately contested cell. There are no shards — every worker can write any cell. Workers race to claim the next cell from a shared counter.

- Total cells = ROWS × COLS (default 16 × 32 = 512)
- Shared counter goes from 0 to (ROWS × COLS - 1)
- Each lock acquisition: read counter, compute (row, col), write one character, increment counter
- Cell `i` maps to: `row = i / COLS`, `col = i % COLS`, `nibble = row % 16`
- All 512 workers loop and compete for every cell — maximum contention

This means:
- **512 lock acquisitions** for the default grid (one per character)
- **Every acquisition does real work** (no shard checks, no no-ops)
- **All workers contest every cell** (no partitioning reduces contention)
- **No-lock mode is catastrophic**: counter races cause duplicate cells, skipped cells, garbled row buffers

### R4: Hex wall output format

The final output is a grid of lines, each of the form:

```
a | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

The left column is the hex nibble (0-9, a-f), pipe separator, then the nibble character repeated COLS times. Rows cycle through nibbles: row 0 = `0`, row 1 = `1`, ..., row 15 = `f`, row 16 = `0`, etc.

The wall is built **one character at a time**. Each cell write appends one character to a row buffer file. When the last character of a row is written (col == COLS - 1), the complete row is formatted and flushed to the output file.

### R5: Shared state via files

- `STATE_DIR` — temp directory created per run (`mktemp -d`)
- `$STATE_DIR/next` — shared cell counter (integer 0..ROWS*COLS-1)
- `$STATE_DIR/row_NNNN` — per-row buffer files accumulating characters
- `$STATE_DIR/out` — completed rows, append-only (one line per finished row)
- Cleanup via `trap ... EXIT`

### R6: Lock mode (default)

Workers use `lokt guard demo:hexwall -- bash $STATE_DIR/critical.sh <args>` to serialize access to the shared counter. Each lock acquisition claims one cell, writes one character. Output is clean, ordered, deterministic.

### R7: No-lock mode (`--no-lock`)

Workers access the shared counter without any locking. Expected results:
- Duplicate characters (multiple workers read same counter value)
- Missing characters (counter incremented past values no worker saw)
- Rows with wrong length (too many or too few characters)
- Out-of-order row flushes
- Garbled or partial lines

The visual contrast with lock mode should be immediately obvious.

### R8: Live streaming via tail

- A background `tail -f $STATE_DIR/out` process streams completed rows to the terminal in real time
- Rows appear as they finish (when their last character is written)
- Keeps lock hold time minimal (no TTY I/O inside critical section)
- The tail process is killed on cleanup

### R9: Flags via script arguments

The script parses its own flags (simple `while` + `case`):

| Flag | Default | Description |
|------|---------|-------------|
| `--workers N` | 512 | Number of worker processes |
| `--rows N` | 16 | Number of rows in the wall |
| `--cols N` | 32 | Width of each row's hex fill |
| `--no-lock` | off | Bypass lokt (show the trainwreck) |
| `--lock-name NAME` | `demo:hexwall` | Lock name for lokt |
| `--help` | — | Print usage and exit |

Env var overrides also accepted (`WORKERS=256 ./lokt-hexwall-demo.sh`). Flags take precedence.

### R10: Summary line at completion

After the wall, print a one-line summary:

```
hexwall: 16 rows × 32 cols, 512 workers, 0.4s
```

Users see the wall first (confirms correctness), then the summary confirms what they already know.

### R11: Portability

- Works on macOS and Linux with bash 3.2+ and coreutils
- No `usleep` dependency (use `sleep 0.001` as fallback)
- No `declare -f` in lokt guard invocations
- No GNU-only flags (`mktemp -d -t prefix.XXXXXX` works on both)
- No ANSI codes, no ncurses, no terminal manipulation beyond `tail -f`
- No bash 4+ features (no associative arrays, no `${var,,}`, no `readarray`)

### R12: Preflight check

Before spawning workers, the script checks:
1. `lokt` is on PATH (unless `--no-lock`)
2. Temp dir is writable

On failure: clear error message with context, exit 1.

---

## Non-Goals

- **No TUI / ANSI rendering**: Plain text lines only.
- **No hash-chain verification**: Correctness is visually obvious.
- **No interactive controls**: No keyboard shortcuts during the run.
- **No binary artifacts**: No PPM, no binary backing store.
- **No sidebar stats**: No throughput/contention display. Just the wall.
- **No audit log integration**: Don't tail or display lokt audit events.
- **No `go:embed`**: The script is not embedded in the Go binary.
- **No shard assignment**: Every worker can write any cell. Maximum contention.

---

## Acceptance Criteria

### AC1: Clean wall in lock mode
Given lokt is on PATH, when the user runs `./lokt-hexwall-demo.sh`, then the output is a 16-row hex wall with rows in order (0, 1, 2, ..., f), each row correctly formatted as `<nibble> | <nibble x 32>`, with no duplicates, gaps, or garbled lines.

### AC2: Garbled wall in no-lock mode
Given the user runs `./lokt-hexwall-demo.sh --no-lock`, then the output visibly differs from lock mode: wrong character counts per row, duplicate or missing rows, garbled content, or partial lines.

### AC3: Per-cell lock granularity
Given default settings, the demo performs 512 lock acquisitions (16 rows × 32 cols), each contested by up to 512 worker processes. Verified by: adding a counter or observing lock contention in `lokt audit`.

### AC4: Live streaming output
Given the demo is running, completed rows appear on terminal as they finish (not batched at the end). Output appears incrementally.

### AC5: Custom dimensions
Given `./lokt-hexwall-demo.sh --rows 256 --cols 64 --workers 128`, the wall has 256 rows, each 64 characters wide in the fill, and 128 workers are spawned.

### AC6: Summary line
Given the demo completes, a summary line prints after the wall: row count, column count, worker count, elapsed time.

### AC7: Readable by non-bash-experts
Every functional section has a block comment explaining what it does, why, and what depends on it. The script reads like an annotated tutorial.

### AC8: Cross-platform
Works on macOS and Linux with bash 3.2+. No GNU-only utilities or bash 4+ features.

### AC9: Clean exit and cleanup
On normal completion or Ctrl-C: all worker processes killed, tail process stopped, STATE_DIR removed. No orphan processes.

### AC10: User can modify and re-run
A user can edit the script (change WORKERS, add sleeps, change ROWS) and re-run. The script is a hackable artifact.

### AC11: `lokt demo` emits the script
Running `lokt demo` writes `lokt-hexwall-demo.sh` to the current directory and prints instructions for running it.

### AC12: Preflight catches missing lokt
Given lokt is NOT on PATH, the script prints a helpful error and exits 1 before spawning workers.

---

## Edge Cases

- **lokt not on PATH** — Preflight check, clear error, exit 1.
- **Previous demo lock still held** — Workers use `--ttl 30s` on guard so stale locks expire. Script may force-unlock at startup with a comment explaining why.
- **Filesystem full** — Accept as hard failure (OS error). No special handling.
- **ROWS not divisible by 16** — Works fine. Nibble cycles: row 17 gets nibble 1 again.
- **ROWS=0 or COLS=0** — No output, exit 0 immediately.
- **WORKERS=1** — Works correctly. Single worker claims all 512 cells sequentially.
- **Very large WORKERS (2048+)** — May hit process limits. Script doesn't cap; OS errors are the user's to manage.
- **Ctrl-C during worker spawn** — Trap handles partial spawn. Kill only collected PIDs.
- **Concurrent runs** — Two runs contend on same lock name. Use `--lock-name` to namespace, or accept it.
- **Row buffer corruption (no-lock)** — Multiple workers appending to the same row buffer file simultaneously. Characters interleave or duplicate. Exactly the chaos we want to show.

---

## Constraints

### C1: Go CLI only emits the file
`lokt demo` writes the script and prints instructions. No Go code runs during the demo itself. The script is the demo.

### C2: Script must be self-contained
Single file. No sourcing other scripts. Dependencies: bash, coreutils (`mktemp`, `cat`, `printf`, `sleep`, `kill`, `tail`, `tr`, `wc`), and `lokt`.

### C3: Critical section must be minimal
Inside `lokt guard`: read counter, compute cell position, append one character to row buffer, maybe flush completed row, increment counter. Lock hold time: single-digit milliseconds.

### C4: Avoid `declare -f` in guard invocations
Write a helper script to `$STATE_DIR/critical.sh` at startup. Each guard invocation runs `bash $STATE_DIR/critical.sh <args>`. Portable, readable, debuggable.

### C5: Comments are a first-class deliverable
The script's comments are part of the spec. Review them for clarity and completeness as you would review code.

---

## Technical Design

### File layout (in repo)

```
cmd/lokt/main.go                # Add "demo" subcommand (emits the script)
```

The script itself is not checked into the repo as a standalone file — it's emitted by `lokt demo`. The script content lives as a string constant (or heredoc) in the Go source.

### Go CLI: `lokt demo`

Minimal addition to `cmd/lokt/main.go`:
1. Add `"demo"` case to command switch
2. Write the script string to `./lokt-hexwall-demo.sh`
3. `chmod +x` the file
4. Print: `"Wrote lokt-hexwall-demo.sh — run it with ./lokt-hexwall-demo.sh"`

No flags beyond `--help`. The script has its own flags.

### Script structure

```bash
#!/usr/bin/env bash
set -euo pipefail

# ══════════════════════════════════════════════════════════════
# lokt hexwall demo
#
# This script spawns hundreds of worker processes that race to
# build a "hex wall" — a grid of hex characters (0-f), built
# one character at a time. Each character requires exclusive
# access to a shared counter file.
#
# By default, workers use `lokt guard` to coordinate. The wall
# comes out clean and ordered. Run with --no-lock to remove the
# locking and watch the output shred itself.
#
# Usage:
#   ./lokt-hexwall-demo.sh              # with locking (clean)
#   ./lokt-hexwall-demo.sh --no-lock    # without locking (chaos)
#   ./lokt-hexwall-demo.sh --rows 256 --cols 64 --workers 1024
#
# Requires: bash 3.2+, lokt (https://github.com/...)
# ══════════════════════════════════════════════════════════════

# ── Configuration ──────────────────────────────────────────────
# Parse flags, fall back to env vars, then defaults.
# WORKERS=512, ROWS=16, COLS=32, NOLOCK=0, LOCKNAME=demo:hexwall

# ── Preflight ──────────────────────────────────────────────────
# Check lokt is available (unless --no-lock).
# Create temp STATE_DIR with counter file and output file.

# ── Write the critical section script ──────────────────────────
# This is the code that runs *inside* the lock. We write it to
# a temp file so `lokt guard` can exec it.
#
# The critical section does exactly this:
#   1. Read the cell counter from $STATE_DIR/next
#   2. If counter >= ROWS * COLS: done, exit
#   3. Compute row = counter / COLS, col = counter % COLS
#   4. Compute the hex nibble: row % 16 → character (0-9, a-f)
#   5. Append that character to $STATE_DIR/row_NNNN
#   6. If col == COLS-1 (last column): format the completed row
#      as "<nibble> | <characters>\n" and append to $STATE_DIR/out
#   7. Increment the counter
#
# One character per lock acquisition. 512 cells = 512 acquisitions,
# each one fought over by up to 512 workers. That's the demo.

# ── Start live output ──────────────────────────────────────────
# tail -f streams completed rows to the terminal as they finish.
# We do NOT print inside the lock — that would make the critical
# section slow (TTY I/O) and inflate lock hold time.

# ── Cleanup trap ───────────────────────────────────────────────
# On exit (normal or Ctrl-C): kill tail, kill workers, rm temp dir.

# ── Spawn workers ──────────────────────────────────────────────
# Launch $WORKERS background processes. Each loops, claiming one
# cell at a time until all cells are filled.
#
# In lock mode:    lokt guard <name> --ttl 30s -- bash critical.sh
# In no-lock mode: bash critical.sh  (no guard = races)
#
# No sharding, no partitioning. Every worker can write any cell.
# This means all 512 workers compete for every single character.

# ── Wait + summary ─────────────────────────────────────────────
# Wait for all workers, kill tail, print summary line.
```

### Critical section (`$STATE_DIR/critical.sh`)

Written to temp dir at startup. Receives args via env or positional params.

```bash
#!/usr/bin/env bash
# Critical section — runs INSIDE the lokt guard lock.
# Claims one cell, writes one character, increments counter.

next_file="$1"       # Path to shared counter file
out_dir="$2"         # Path to STATE_DIR (row buffers live here)
out_file="$3"        # Path to output file (completed rows)
cols="$4"            # Grid width
rows="$5"            # Grid height
total=$(( rows * cols ))

# Read the cell counter
i=$(cat "$next_file")

# All cells filled?
[ "$i" -ge "$total" ] && exit 0

# Compute position
row=$(( i / cols ))
col=$(( i % cols ))
nib=$(( row % 16 ))

# Pick the hex character for this row
hex_chars="0123456789abcdef"
ch="${hex_chars:$nib:1}"

# Append one character to this row's buffer
row_file="$out_dir/row_$(printf '%04d' "$row")"
printf "%s" "$ch" >> "$row_file"

# If this was the last column, the row is complete — flush it
if [ "$col" -eq $(( cols - 1 )) ]; then
    content=$(cat "$row_file")
    printf "%s | %s\n" "$ch" "$content" >> "$out_file"
fi

# Advance the counter
echo $(( i + 1 )) > "$next_file"
```

### Worker loop

```bash
worker() {
    local total=$(( ROWS * COLS ))

    while true; do
        if [ "$NOLOCK" -eq 1 ]; then
            bash "$CRITICAL" "$NEXT" "$STATE_DIR" "$OUT" "$COLS" "$ROWS" || true
        else
            lokt guard "$LOCKNAME" --ttl 30s -- \
                bash "$CRITICAL" "$NEXT" "$STATE_DIR" "$OUT" "$COLS" "$ROWS" || true
        fi

        # Exit hint (racy read is fine — real check is inside critical.sh)
        cur=$(cat "$NEXT" 2>/dev/null || echo 0)
        [ "$cur" -ge "$total" ] && return 0

        sleep 0.001
    done
}
```

---

## Notes

### Why this is simpler than the mosaic

| Dimension | Mosaic (reverted) | Hexwall |
|-----------|-------------------|---------|
| Language | Go (8 files, ~800+ LOC) | Bash (1 file, ~120-180 LOC) |
| Workers | Goroutines + in-process lock API | Background jobs + `lokt guard` CLI |
| Output | ANSI TUI at 20fps + sidebar | `tail -f` plain text |
| Verification | SHA256 hash chain + invariant checks | Visual (ordered vs chaos) |
| Interaction | Keyboard controls (f/u/k/q) | None (Ctrl-C to exit) |
| State files | Ledger, PPM, state JSON, stats JSONL | Counter, row buffers, output |
| User trust model | "trust the binary" | "read the script yourself" |
| Contention | Per-tile (2304 tiles, sharded) | Per-character (512 cells, unsharded) |

### The key insight: the script IS the proof

The mosaic demo asked users to trust a compiled Go binary. The hexwall demo gives users a bash script they can read end-to-end. The script is the argument: "here's what happens with the lock, here's what happens without it, change anything you want and see for yourself."

### Why per-cell is better than per-row

With per-row writes and shard gating, most lock acquisitions were no-ops (wrong shard, release immediately). That's artificial contention. With per-cell writes:
- **Every lock acquisition does real work** — no wasted cycles
- **512 acquisitions** instead of 16 — the lock is hot for the entire run
- **No-lock mode is more destructive** — character-level races corrupt row buffers, not just row ordering
- **The demo proves more** — lokt coordinates at the finest granularity, not just coarse blocks

### Previous demo reference

The mosaic demo story is at `_notes/story-drafts/demo.md` (historical). Reverted in commit `d77ac74`.

---

**Next:** Run `/kickoff hexwall-demo` to promote to Beads execution layer.
