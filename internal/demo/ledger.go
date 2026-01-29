package demo

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// LedgerEntry represents a single tile commit in the mosaic ledger.
type LedgerEntry struct {
	Index int       `json:"i"`
	X     int       `json:"x"`
	Y     int       `json:"y"`
	RGB   [3]byte   `json:"rgb"`
	Owner string    `json:"owner"`
	PID   int       `json:"pid"`
	TS    time.Time `json:"ts"`
	Prev  string    `json:"prev"`
	Hash  string    `json:"h"`
}

const genesisHash = "GENESIS"

// ComputeHash computes the SHA256 hash for a ledger entry.
// The hash covers all fields except "h" itself.
func ComputeHash(e *LedgerEntry) string {
	// Build a canonical representation without the hash field.
	canonical := struct {
		Index int       `json:"i"`
		X     int       `json:"x"`
		Y     int       `json:"y"`
		RGB   [3]byte   `json:"rgb"`
		Owner string    `json:"owner"`
		PID   int       `json:"pid"`
		TS    time.Time `json:"ts"`
		Prev  string    `json:"prev"`
	}{
		Index: e.Index,
		X:     e.X,
		Y:     e.Y,
		RGB:   e.RGB,
		Owner: e.Owner,
		PID:   e.PID,
		TS:    e.TS,
		Prev:  e.Prev,
	}
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(append([]byte(e.Prev), data...))
	return fmt.Sprintf("%x", sum)
}

// LedgerWriter appends entries to a JSONL ledger file with hash chaining.
// Safe for concurrent use.
type LedgerWriter struct {
	mu       sync.Mutex
	path     string
	prevHash string
}

// NewLedgerWriter creates a writer for the given ledger file path.
// If the file already has entries, it resumes from the last hash.
func NewLedgerWriter(path string) *LedgerWriter {
	return &LedgerWriter{
		path:     path,
		prevHash: genesisHash,
	}
}

// Append writes a ledger entry, computing and chaining its hash.
// Thread-safe: serializes access to the hash chain.
func (w *LedgerWriter) Append(e *LedgerEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e.Prev = w.prevHash
	e.Hash = ComputeHash(e)

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("ledger marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // path is controlled
	if err != nil {
		return fmt.Errorf("ledger open: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("ledger write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("ledger sync: %w", err)
	}

	w.prevHash = e.Hash
	return nil
}

// PrevHash returns the current chain tip hash.
func (w *LedgerWriter) PrevHash() string {
	return w.prevHash
}
