package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriterEmit_ReadOnlyDir(t *testing.T) {
	// Create a read-only directory to trigger open error
	dir := t.TempDir()
	readonlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0700) })

	w := NewWriter(readonlyDir)

	// Should not panic — errors are logged to stderr
	w.Emit(&Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "h1",
		PID:   1,
	})

	// Audit file should NOT exist
	path := filepath.Join(readonlyDir, "audit.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("audit.log should not exist in read-only directory")
	}
}

func TestWriterEmit_MarshalError(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// A channel value in Extra causes json.Marshal to fail
	w.Emit(&Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "h1",
		PID:   1,
		Extra: map[string]any{
			"bad": make(chan int), // channels can't be marshaled to JSON
		},
	})

	// Audit file should NOT be created (marshal failed before open)
	path := filepath.Join(dir, "audit.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("audit.log should not exist when marshal fails")
	}
}

func TestWriterEmit_WriteError(t *testing.T) {
	dir := t.TempDir()

	// Pre-create audit.log as a directory to trigger write error
	auditPath := filepath.Join(dir, "audit.log")
	if err := os.MkdirAll(auditPath, 0700); err != nil {
		t.Fatal(err)
	}

	w := NewWriter(dir)

	// Should not panic — OpenFile will fail since path is a directory
	w.Emit(&Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "h1",
		PID:   1,
	})
}
