package demo

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/identity"
	"github.com/nikolasavic/lokt/internal/lock"
)

// Worker fills mosaic tiles under Lokt locking.
type Worker struct {
	ID        int
	Config    *MosaicConfig
	RootDir   string
	DemoDir   string
	Auditor   *audit.Writer
	Ledger    *LedgerWriter
	StatePath string
	Rng       *rand.Rand
}

// Run executes the worker loop: acquire lock, claim index, compute color,
// append ledger, release lock, sleep with jitter. Stops when all tiles are
// filled or context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	total := w.Config.TotalTiles()
	lockName := w.Config.Name
	maxIter := total * 2 // safety limit for nolock mode
	iter := 0

	for {
		iter++
		if iter > maxIter {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}

		// Jitter sleep between iterations
		if w.Config.TileDelay > 0 {
			jitter := time.Duration(w.Rng.Intn(w.Config.TileDelay)) * time.Millisecond //nolint:gosec // demo jitter
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(jitter):
			}
		}

		// Acquire lock (with wait if configured)
		var acqErr error
		switch {
		case w.Config.Mode == "nolock":
			// Skip locking entirely in nolock mode
		case w.Config.Wait:
			acqErr = w.acquireWithWait(ctx, lockName)
		default:
			acqErr = lock.Acquire(w.RootDir, lockName, lock.AcquireOptions{
				TTL:     time.Duration(w.Config.TTL) * time.Second,
				Auditor: w.Auditor,
			})
		}

		if acqErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Lock denied — retry after backoff
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}

		// Critical section: read index, compute tile, append ledger, update index
		idx, err := ReadNextIndex(w.StatePath)
		if err != nil {
			w.releaseLock(lockName)
			return fmt.Errorf("worker %d: read index: %w", w.ID, err)
		}

		if idx >= total {
			// All tiles filled
			w.releaseLock(lockName)
			return nil
		}

		rgb := TileColor(idx, w.Config.Seed)
		id := identity.Current()

		entry := &LedgerEntry{
			Index: idx,
			X:     idx % w.Config.GridX,
			Y:     idx / w.Config.GridX,
			RGB:   rgb,
			Owner: id.Owner,
			PID:   id.PID,
			TS:    time.Now(),
		}

		if err := w.Ledger.Append(entry); err != nil {
			w.releaseLock(lockName)
			return fmt.Errorf("worker %d: append ledger: %w", w.ID, err)
		}

		if err := WriteNextIndex(w.StatePath, idx+1); err != nil {
			w.releaseLock(lockName)
			return fmt.Errorf("worker %d: write index: %w", w.ID, err)
		}

		// Release lock
		w.releaseLock(lockName)
	}
}

func (w *Worker) acquireWithWait(ctx context.Context, lockName string) error {
	acqCtx := ctx
	if w.Config.Timeout > 0 {
		var cancel context.CancelFunc
		acqCtx, cancel = context.WithTimeout(ctx, time.Duration(w.Config.Timeout)*time.Second)
		defer cancel()
	}
	return lock.AcquireWithWait(acqCtx, w.RootDir, lockName, lock.AcquireOptions{
		TTL:     time.Duration(w.Config.TTL) * time.Second,
		Auditor: w.Auditor,
	})
}

func (w *Worker) releaseLock(name string) {
	if w.Config.Mode == "nolock" {
		return
	}
	err := lock.Release(w.RootDir, name, lock.ReleaseOptions{
		Auditor: w.Auditor,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker %d: release: %v\n", w.ID, err)
	}
}

// Coordinator manages the worker pool.
type Coordinator struct {
	Config    *MosaicConfig
	RootDir   string
	DemoDir   string
	Auditor   *audit.Writer
	Ledger    *LedgerWriter
	StatePath string
}

// Start spawns workers and waits for completion or context cancellation.
// Returns the number of workers that reported errors.
func (c *Coordinator) Start(ctx context.Context) int {
	type result struct {
		id  int
		err error
	}

	results := make(chan result, c.Config.Workers)

	for i := 0; i < c.Config.Workers; i++ {
		go func(id int) {
			w := &Worker{
				ID:        id,
				Config:    c.Config,
				RootDir:   c.RootDir,
				DemoDir:   c.DemoDir,
				Auditor:   c.Auditor,
				Ledger:    c.Ledger,
				StatePath: c.StatePath,
				Rng:       rand.New(rand.NewSource(int64(c.Config.Seed) + int64(id))), //nolint:gosec // demo seeding
			}
			results <- result{id: id, err: w.Run(ctx)}
		}(i)
	}

	errCount := 0
	for i := 0; i < c.Config.Workers; i++ {
		r := <-results
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "worker %d error: %v\n", r.id, r.err)
			errCount++
		}
	}
	return errCount
}
