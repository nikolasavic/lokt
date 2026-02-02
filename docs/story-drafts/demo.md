# Lokt Demo: Terminal Mosaic

## Goal

Demonstrate Lokt correctness and robustness under extreme concurrency by producing a **visually obvious artifact** that:

1. **Completes deterministically** (exactly all tiles once, no gaps/dupes)
2. Shows **real-time progress** in terminal (colored tile grid + stats)
3. Exercises Lokt features beyond basic locking:

   * `guard` with TTL + renew events
   * `status --json` integration (current holder / expiry)
   * `audit --tail` streaming
   * `--wait/--timeout` contention behavior
   * `freeze/unfreeze`
   * crash recovery / dead PID pruning
   * corrupted lock handling (optional)

The demo should also support a “no-lock” mode that *visibly fails* (tearing/corruption) for contrast.

---

# User Experience

## Primary command

```bash
lokt demo mosaic \
  --name mosaic \
  --grid 64x36 \
  --tile 16 \
  --workers 512 \
  --ttl 2s \
  --wait \
  --timeout 2s \
  --fps 20 \
  --tile-delay 120ms \
  --chunk-size 256 \
  --chunk-delay 3ms \
  --seed 1337
```

### What the user sees

* A 64×36 tile mosaic rendered live as colored blocks.
* A sidebar with:

  * progress `done/total` and ETA
  * throughput `tiles/sec`
  * lock contention stats (wait p50/p95, timeout count)
  * current lock holder + expiry (polled via `lokt status --json`)
  * freeze indicator + remaining time
  * recent event stream (“renew”, “deny: frozen”, “auto-prune”, etc.)

### Interaction (during run)

Keyboard shortcuts (single-key, optional but very demo-friendly):

* `f` → freeze for 10s (`lokt freeze <name> --ttl 10s`)
* `u` → unfreeze (`lokt unfreeze <name>`)
* `k` → kill current holder (find via status JSON, send SIGKILL)
* `q` → quit (cleanly stop workers + renderer)

---

# Architecture

Single `lokt demo mosaic` command orchestrates 3 internal roles (processes or goroutines; either is fine):

1. **Coordinator**

   * Creates demo directory and initializes mosaic backing files.
   * Spawns worker processes (recommended) or goroutines (ok).
   * Starts Renderer.

2. **Workers (N = --workers)**

   * Each worker repeatedly claims the “next tile index” and commits it.
   * Uses Lokt locking for serialization (`guard` or internal lock API).

3. **Renderer (1)**

   * Reads progress events from an append-only ledger (JSONL) or from `audit --tail`.
   * Renders tile grid + stats at fixed FPS using ANSI truecolor blocks.
   * Periodically polls `lokt status --json` to show owner/expiry/frozen.

**Recommendation:** use a separate **demo ledger** (`mosaic.ledger.jsonl`) for tile commits, and rely on `lokt audit --tail` only for lock-level events. This keeps parsing simple and stable.

---

# Files and Locations

All demo artifacts live under:

```
<LOKT_ROOT>/demo/mosaic/
  mosaic.ppm                 (optional backing store; see below)
  mosaic.ledger.jsonl         (tile commit log; authoritative for renderer)
  mosaic.state.json           (small state file: expected total, seed, etc.)
  mosaic.stats.jsonl          (optional perf samples)
```

Renderer should not require PPM to render. It should render from ledger only.

---

# Visual Artifact Definition

## Tile grid

* Grid size: `GX × GY` tiles (`--grid 64x36`)
* Total tiles: `T = GX * GY`
* Each tile is rendered as **one terminal cell** (or 2 chars wide for aspect ratio).

## Tile identity / order

Tile completion order is intentionally **sequential** and “sudoku-like obvious”:

* Each commit claims a unique index `i ∈ [0, T-1]`.
* Mapping: `x = i % GX`, `y = i / GX`.

Correctness is obvious:

* If the lock works, the mosaic fills in cleanly left-to-right, top-to-bottom.
* If there’s corruption (duplicate i, missing i), you’ll see holes or overdraw.

(You can optionally support other orderings: Hilbert curve / spiral for extra wow, but sequential is easiest to verify.)

---

# Tile Generation (deterministic, pretty, cheap)

Each tile has a deterministic RGB color computed from `(i, seed)`:

Example formula (fast, stable, “looks good”):

* Use a simple hash -> HSV -> RGB
* Or compute using sine palettes.

Spec requirement:

* Given `--seed`, the final mosaic must be **bit-for-bit identical** in terms of tile colors (as rendered from ledger).

Recommended function (language-agnostic):

1. `h = splitmix64(seed ^ uint64(i))`
2. `R = (h >>  0) & 0xFF`
   `G = (h >>  8) & 0xFF`
   `B = (h >> 16) & 0xFF`
3. Optionally “prettify”:

   * increase saturation / clamp brightness (avoid too dark)
   * or map to HSV with hue = h%360, sat=0.8, val=0.9

Renderer will use this RGB directly.

---

# Ledger Format (authoritative progress)

Append-only JSONL file: `mosaic.ledger.jsonl`

Each tile commit appends exactly one line:

```json
{
  "i": 1234,
  "x": 18,
  "y": 19,
  "rgb": [12, 220, 199],
  "owner": "user@host",
  "pid": 48123,
  "ts": "2026-01-28T20:15:12.123Z",
  "prev": "hexhash",
  "h": "hexhash"
}
```

## Hash chain (visual proof + corruption detection)

* `prev` is previous entry’s `h`, or `"GENESIS"`.
* `h = SHA256(prev || canonical_json_without_h)` hex-encoded.

Verification at end:

* entries count == T
* i is contiguous 0..T-1 exactly once
* chain verifies
* all lines parse

If any of these fail: demo prints a big red **FAILED** with details.

---

# Worker Algorithm

Workers run until the mosaic completes (i reaches T-1) or coordinator stops.

Each iteration:

1. **Outside lock**

   * jitter sleep `rand(0..tile-delay)` (from `--tile-delay`)
   * compute tile `rgb = f(i, seed)` *but i isn’t known yet* → either:

     * compute after claiming i (inside lock) but keep it tiny, or
     * claim i quickly, release lock, compute rgb, re-lock to commit (two-phase)

**Recommended**: two-phase to showcase TTL/renew with longer compute.

### Two-phase protocol (recommended)

A) Acquire `claim` lock, claim next i, write a “claim” record (optional), release.
B) Compute tile data outside lock (sleep/jitter).
C) Acquire `commit` lock, append ledger commit for i, release.

But this introduces ordering complexity (commits could be out-of-order). If you want strict sequential fill, keep claim+commit under one lock.

### Single-phase protocol (simpler, still great)

Inside a single `lokt guard <name>` critical section:

* determine next i by reading a tiny `next-index` file OR by reading last ledger line
* compute rgb quickly (pure function)
* append ledger entry
* (optional) do “chunked writes” to backing store to dramatize tearing risk
  Outside lock:
* sleep/jitter to control overall speed

**This preserves strictly increasing i and a satisfying fill pattern.** Do this first.

## Next-index source

Avoid scanning the ledger tail expensively:

* Maintain `mosaic.next` file containing a decimal integer.
* Under lock:

  * read next
  * increment and write back (fsync)
  * use claimed i

This makes it scale.

---

# Optional: Backing Store Writes (to dramatize “no-lock” corruption)

Even though renderer can use ledger-only, you can optionally also write a real binary artifact (`mosaic.ppm`) to prove corruption resistance.

### PPM spec (optional)

* P6 format
* fixed header
* random-access tile write using `WriteAt` by rows

### Chunked write (wow + makes races visible without lock)

Within lock, write tile data in many small chunks:

* `--chunk-size 256` bytes
* `--chunk-delay 3ms` between chunks

If you run in `--mode nolock`, chunks from different workers interleave and the image becomes torn/corrupt (if someone later opens it). Renderer is still driven by ledger so live display stays stable.

(If you want renderer to *also* show tearing, have it sample pixels from PPM; but for “terminal-only”, ledger-driven is enough and far more reliable.)

---

# Renderer Specification (Terminal UI)

## Rendering

* Runs at `--fps` (default 20).
* Uses ANSI escape sequences:

  * clear screen + cursor home each frame
  * draws a grid of `GY` rows by `GX` columns
  * each tile cell prints background color `rgb` and a space `"  "` (2 chars wide)
  * unset tiles are dark gray or `.`

Example cell:

* `ESC[48;2;R;G;Bm  ESC[0m`

## Data source

Renderer tails `mosaic.ledger.jsonl` incrementally (seek + read new bytes).

* Parse only complete lines.
* Update tile state array `[GY][GX]`.

## Sidebar stats (minimum)

* `Tiles: done/total (pct)`
* `Rate: tiles/sec (moving avg 1s + 10s)`
* `Lock: p50/p95 wait (if available), timeouts count`
* `Holder: owner pid (from status --json), expires in X`
* `Frozen: yes/no (remaining)`
* `Events: last 5` (from audit tail OR derived)

### Showing Lokt features in UI

* Poll `lokt status <name> --json` every ~250ms–1s.
* Tail `lokt audit --name <name> --tail` and show recent event types:

  * `renew`
  * `deny (frozen)`
  * `auto-prune`
  * `stale-break`
  * `corrupt-break`

If implementing a second tail is annoying, coordinator can subscribe to audit internally (if Lokt is in-process). Either is acceptable.

---

# Demonstration Scenarios (scriptable)

## 1) Happy path (default)

* High worker count, smooth fill, renew events occur if TTL small enough.

## 2) Freeze demo (wow)

At 30% completion:

* coordinator triggers `lokt freeze <name> --ttl 10s`
* workers attempting acquire get exit code 2, audit shows denies
* renderer shows “FROZEN (10s)” and progress stops
  At expiry or manual unfreeze:
* progress resumes automatically

## 3) Crash recovery demo

At 60%:

* find current holder via `status --json` and SIGKILL pid
* next acquire should trigger dead PID prune (or stale-break depending on design)
* audit shows recovery event
* progress continues

## 4) Timeout pressure demo

Configure a subset of workers with:

* `--timeout 100ms`
* show timeouts rising under contention, while others keep moving

## 5) No-lock contrast (optional mode)

Run:

```bash
lokt demo mosaic --mode nolock ...
```

Expected:

* ledger hash chain breaks OR duplicates appear OR next-index corrupts
* renderer shows obvious failure (holes, overwrites, invalid chain)
* end summary prints failed invariants

---

# Correctness Criteria (must pass)

At completion:

* Ledger entries count == `T`
* Indices are exactly `{0..T-1}` with no duplicates
* Entries appear in strictly increasing i (if single-phase)
* Hash chain verifies
* Every JSON line parses
* Renderer shows full grid (no unset tiles)

If any fail:

* print failure summary + first offending index
* exit non-zero

---

# Performance/Scalability Targets

Minimum “wow” baseline (local machine):

* `workers >= 256`, ideally `512–2048`
* `grid >= 64x36` (2304 tiles) so it’s visibly substantial
* maintain stable UI at 20 FPS
* overall run time adjustable: ~15–60 seconds via `--tile-delay`

---

# Engineering Notes / Edge Cases

* Renderer must tolerate partial writes (read only complete newline-terminated JSON).
* Use `fsync` on ledger append during commit (optional but good).
* Use `O_APPEND` and a single `Write()` per line to avoid interleaving.
* Keep lock critical section small and deterministic.
* Ensure `lokt demo mosaic` cleans up on SIGINT (stop workers, restore terminal).

---

# Deliverables

1. `lokt demo mosaic` command (or `cmd/lokt/demo_mosaic.go`)
2. Worker implementation (in-process or spawned subprocess)
3. Terminal renderer (ANSI truecolor)
4. End-of-run verifier (ledger invariants + hash chain)
5. Optional chaos hooks (freeze/kill)
6. Optional modes: `lokt|nolock|broken`

---

If you want one extra flourish: support `--order hilbert|spiral|scanline`. Hilbert fill looks *magical* in the terminal and still preserves “obvious correctness” because the final grid is complete and the ledger invariants prove no gaps/dupes.

