# Hexwall Demo — A Visual Guide to Lock Contention

Lokt ships with a built-in demo that shows what happens when concurrent
processes share state without coordination. It builds a "hex wall" — a
grid of hexadecimal characters — using parallel worker processes. With
locking, the wall is clean. Without locking, it falls apart.

## Quick Start

```bash
lokt demo                             # generate the script
./lokt-hexwall-demo.sh                # run with locking (clean)
./lokt-hexwall-demo.sh --no-lock      # run without locking (chaos)
```

You'll see a wall of hex characters printed to your terminal in real time.
In lock mode every row is uniform (row 0 = all `0`s, row 1 = all `1`s,
etc.). In no-lock mode the rows are a jumble of mismatched characters.

## What the Demo Does

The demo spawns multiple worker processes (8 by default) that compete to
fill a grid, one character at a time. Each character requires exclusive
access to a shared counter file and three shared "work files." In lock
mode, workers use `lokt guard` to serialize access. In no-lock mode they
all read and write the same files simultaneously, stepping on each other.

The grid is `ROWS x COLS` characters (default 16x32 = 512 cells). Every
cell requires one lock acquisition. With 8 workers contending for each
acquisition, there are hundreds of contested lock attempts per run.

## The Critical Section: 5 Phases

A **critical section** is a block of code that must run to completion
without interruption from other processes. The demo's critical section
has five phases. Here is what happens in each, and why locking matters.

### Phase 1 — Write intermediates

The worker reads the shared counter to learn which cell to fill. It
computes three intermediate values (`a`, `b`, `c`) and writes them to
three shared files (`work_a`, `work_b`, `work_c`).

The values are chosen so that `(a + b + c) % 16` equals the row number.
Specifically: `a = row % 16`, `b = col % 16`, `c = (16 - b) % 16`.
Because `b + c` is always a multiple of 16, the sum reduces to just `a`,
which is the row number. This identity only holds when all three values
come from the same worker at the same grid position.

**With lock:** The work files contain our values and only our values.

**Without lock:** Another worker may overwrite `work_b` or `work_c`
between our writes, replacing them with values from a different grid
position.

### Phase 2 — Poison the counter

The counter is temporarily set to a random grid position. This models
"computation in progress" — the kind of intermediate state that real
systems produce during multi-step updates.

**With lock:** No other worker can read the counter while it holds this
garbage value. They are blocked, waiting for the lock.

**Without lock:** Other workers read the poisoned counter, believe they
are at the wrong grid position, and begin computing intermediates for
that position. This is where the real damage starts — a single poisoned
read cascades into incorrect writes across multiple work files.

### Phase 3 — Read back results

The worker reads the three work files back.

**With lock:** It reads its own values from phase 1. The cancellation
`b + c ≡ 0 (mod 16)` holds. The computed character equals the row number.

**Without lock:** Some work files have been overwritten by workers that
were misdirected in phase 2. The values come from different grid
positions, the cancellation breaks, and the character is wrong.

### Phase 4 — Write to row buffer

The computed character is appended to the row's buffer file. When the
buffer reaches `COLS` characters, the row is flushed to the output file.

**With lock:** Each row fills sequentially from left to right with the
correct character.

**Without lock:** Workers write characters for wrong positions (due to
counter poisoning), multiple workers may append to the same row
concurrently, and the buffer fills with a mix of correct and incorrect
characters.

### Phase 5 — Restore the counter

The counter is set to the correct next value (`i + 1`).

**With lock:** The next worker picks up exactly where we left off.

**Without lock:** Multiple workers write competing "next" values. Some
workers' phase-5 writes are overwritten by other workers' phase-2 poison
writes. The counter jumps around unpredictably.

## What Is a Critical Section?

A critical section is a sequence of operations that must execute
atomically — as if they were a single, indivisible step. If another
process interleaves its own operations in the middle, the shared state
becomes inconsistent.

Think of a bank teller serving one customer at a time. The teller reads
the account balance, computes the new balance, and writes it back. If
two tellers serve the same account simultaneously — both read $100, both
subtract $50, both write $50 — the bank loses $50. The solution is a
lock: only one teller handles the account at a time.

In the demo, the critical section is the five-phase computation. The
"bank account" is the set of shared files (counter, work files, row
buffers). The "tellers" are the worker processes.

## What Is Contention?

Contention occurs when multiple processes want the same lock at the same
time. Only one can proceed; the rest must wait.

Higher contention means more waiting, but it also means the lock is doing
its job — preventing the corruption that would occur if everyone
proceeded simultaneously.

In the demo:
- **8 workers, 512 cells** = high contention. Workers frequently collide.
- **1 worker, 512 cells** = zero contention. No races possible.
- **16 workers, 512 cells** = even higher contention. More waiting, same
  correct result.

Real-world contention examples:
- Multiple CI/CD pipelines deploying the same service
- Parallel terraform applies against the same state
- Database transactions updating the same row

## Why Locks Matter

The demo proves a simple claim: **if a computation has multiple steps
over shared state, concurrent access without a lock produces incorrect
results.**

The lock provides **mutual exclusion** — the guarantee that only one
process executes the critical section at a time. This is not about speed
(locking is slower). It is about correctness. The locked demo produces
the right answer every time. The unlocked demo produces garbage every
time.

## Counter Poisoning

Phase 2 is the most destructive step in the no-lock scenario. When the
counter is temporarily set to a random value:

1. Worker A reads position 42, begins computing, poisons counter to 317.
2. Worker B reads 317, thinks it should fill position 317, writes
   intermediates for that position into the shared work files.
3. Worker A reads back work files — gets Worker B's values for
   position 317 instead of its own values for position 42.
4. Worker A writes a wrong character to row 42's buffer.
5. Worker B also writes a wrong character (to row 317 if it exists, or
   an overflowed position).

This cascade is self-amplifying. Every poisoned read creates more
poisoned writes, which create more poisoned reads. Within a few
iterations, the entire grid is corrupted.

## Work File Splits

The three intermediate values use a deterministic formula:

```
a = row % 16
b = col % 16
c = (16 - b) % 16
```

When all three come from the same grid position:

```
(a + b + c) % 16 = (row + col + 16 - col) % 16 = (row + 16) % 16 = row % 16
```

The `col` terms cancel. The result depends only on the row number. That
is why every character in a locked row is the same hex digit — it is the
row number mod 16.

When values come from different positions (the no-lock case), the `col`
terms do not cancel. The result is `(row_x + col_y + 16 - col_z) % 16`
where x, y, z are different grid positions. This produces essentially
random output.

## The mkdir Dedup

When a row buffer reaches `COLS` characters, the worker needs to flush
it to the output file. But in a concurrent environment, multiple workers
might see the buffer reach that threshold simultaneously. Writing the
same row twice would produce duplicate output.

The demo solves this with `mkdir`:

```bash
if mkdir "$STATE_DIR/done_$row" 2>/dev/null; then
    # flush the row — we won the race
fi
```

On POSIX systems, `mkdir` is atomic — exactly one caller succeeds when
multiple processes attempt to create the same directory simultaneously.
The others get an error. This provides a lock-free, race-free way to
claim a one-time action.

This pattern is useful beyond demos. Any time you need exactly-once
semantics in a shell script without external tooling, `mkdir` is a
reliable primitive.

## Tuning Parameters

| Flag | Default | Effect |
|------|---------|--------|
| `--workers N` | 8 | More workers = more contention. Above ~16 you hit diminishing returns as workers mostly wait for each other. |
| `--rows N` | 16 | More rows = taller wall. Each row is independently filled, so this scales linearly. |
| `--cols N` | 32 | More columns = wider rows. Each cell is one lock acquisition, so wider rows increase total work. |
| `--no-lock` | off | Disables `lokt guard`. Workers run without coordination. |
| `--lock-name NAME` | `demo.hexwall` | Changes the lock name used by `lokt guard`. |

For a quick sanity check:

```bash
./lokt-hexwall-demo.sh --rows 4 --cols 32
```

For maximum visual impact:

```bash
./lokt-hexwall-demo.sh --rows 16 --cols 64 --workers 16
```

## Interpreting the Output

### Lock mode (clean)

```
0 | 00000000000000000000000000000000
1 | 11111111111111111111111111111111
2 | 22222222222222222222222222222222
3 | 33333333333333333333333333333333
```

Every row is filled with a single repeating character — the row number
in hex. Row 0 is all `0`s, row 10 is all `a`s, row 15 is all `f`s. The
wall is uniform because every character was computed from consistent
intermediate values, protected by the lock.

### No-lock mode (chaos)

```
0 | 0a3f10b280c000f0a92d051e400e0000
1 | 011113a10d931110c141010b16110010
2 | 22822af602b0d3b2822a2b0622202222
3 | 333004333035a33233f333339ab05093
```

Rows contain a mix of characters. Some are correct (by chance), others
are wrong. The pattern varies between runs because the corruption depends
on scheduling — which workers happen to interleave in which order.

The more workers you run, the more chaotic the output. With 1 worker
and `--no-lock`, the output is actually correct (no concurrency, no
races). Add a second worker and corruption appears immediately.

## Applying to Real Systems

The demo models a pattern found across computing:

**Databases.** A transaction reads a balance, computes interest, writes
the new balance. Without isolation (the database equivalent of locking),
a concurrent transaction sees the intermediate state and produces a wrong
result. This is called a **dirty read**.

**Map-reduce.** Multiple reducers accumulate partial results into a
shared counter. Without a lock, concurrent increments lose updates. A
counter incremented 1000 times by 10 workers might end up at 980 instead
of 1000. This is called a **lost update**.

**CI/CD pipelines.** Two pipelines deploy the same service
simultaneously. One writes config, the other writes the binary. The
service starts with mismatched config and binary — the equivalent of
reading `work_a` from one worker and `work_b` from another.

**Shared memory.** Multiple threads update a struct with several fields.
Without a lock, one thread reads a struct where some fields were written
by thread A and others by thread B. The struct is internally
inconsistent — a **torn read**.

In every case, the fix is the same: serialize access to the shared state.
A lock, a mutex, a transaction, a queue — the mechanism varies, but the
principle is identical. Only one writer at a time.
