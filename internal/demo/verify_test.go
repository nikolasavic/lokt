package demo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyLedger_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ledger.jsonl")

	w := NewLedgerWriter(path)
	total := 10
	seed := uint64(1337)

	for i := 0; i < total; i++ {
		rgb := TileColor(i, seed)
		entry := &LedgerEntry{
			Index: i,
			X:     i % 5,
			Y:     i / 5,
			RGB:   rgb,
			Owner: "test",
			PID:   12345,
			TS:    time.Now(),
		}
		if err := w.Append(entry); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	result, err := VerifyLedger(path, total)
	if err != nil {
		t.Fatalf("VerifyLedger: %v", err)
	}
	if !result.OK {
		t.Errorf("expected OK, got failures: %v", result.Failures)
	}
	if result.EntryCount != total {
		t.Errorf("expected %d entries, got %d", total, result.EntryCount)
	}
}

func TestVerifyLedger_WrongCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ledger.jsonl")

	w := NewLedgerWriter(path)
	entry := &LedgerEntry{
		Index: 0,
		X:     0,
		Y:     0,
		RGB:   [3]byte{100, 200, 50},
		Owner: "test",
		PID:   1,
		TS:    time.Now(),
	}
	if err := w.Append(entry); err != nil {
		t.Fatal(err)
	}

	result, err := VerifyLedger(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Error("expected failure for wrong count")
	}
}

func TestVerifyLedger_DuplicateIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ledger.jsonl")

	w := NewLedgerWriter(path)
	for j := 0; j < 2; j++ {
		entry := &LedgerEntry{
			Index: 0, // duplicate
			X:     0,
			Y:     0,
			RGB:   [3]byte{100, 200, 50},
			Owner: "test",
			PID:   1,
			TS:    time.Now(),
		}
		if err := w.Append(entry); err != nil {
			t.Fatal(err)
		}
	}

	result, err := VerifyLedger(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Error("expected failure for duplicate index")
	}
}

func TestVerifyLedger_BrokenChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ledger.jsonl")

	// Write two separate chains (break the chain by using two writers).
	w1 := NewLedgerWriter(path)
	e1 := &LedgerEntry{
		Index: 0, X: 0, Y: 0, RGB: [3]byte{1, 2, 3},
		Owner: "a", PID: 1, TS: time.Now(),
	}
	if err := w1.Append(e1); err != nil {
		t.Fatal(err)
	}

	// Second writer starts fresh (prev=GENESIS instead of e1.Hash).
	w2 := NewLedgerWriter(path)
	e2 := &LedgerEntry{
		Index: 1, X: 1, Y: 0, RGB: [3]byte{4, 5, 6},
		Owner: "b", PID: 2, TS: time.Now(),
	}
	if err := w2.Append(e2); err != nil {
		t.Fatal(err)
	}

	result, err := VerifyLedger(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Error("expected failure for broken hash chain")
	}
}

func TestVerifyLedger_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ledger.jsonl")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := VerifyLedger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Errorf("empty file with 0 expected should be OK, got: %v", result.Failures)
	}
}
