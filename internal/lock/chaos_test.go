package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikolasavic/lokt/internal/audit"
	"github.com/nikolasavic/lokt/internal/lockfile"
)

// Chaos tests for L-190 / lokt-6yy: corrupted lockfile handling.
//
// Verify that every corruption variant is handled gracefully (no panics)
// and that auto-recoverable cases allow subsequent acquire to succeed.

// corruptionCase defines a single corruption scenario.
type corruptionCase struct {
	name    string
	content []byte
	// isAutoRecoverable means Acquire auto-prunes and succeeds.
	// When false, the corruption is not detected as ErrCorrupted
	// (e.g., empty file, valid-JSON-wrong-schema) and Acquire returns
	// a HeldError instead.
	isAutoRecoverable bool
}

var corruptionCases = []corruptionCase{
	{
		name:              "truncated_json",
		content:           []byte(`{"version":1,"name":"test","owner":"alice`),
		isAutoRecoverable: true,
	},
	{
		name:              "invalid_json",
		content:           []byte(`not json at all {{{`),
		isAutoRecoverable: true,
	},
	{
		name:              "binary_garbage",
		content:           []byte{0x00, 0x01, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47, 0xDE, 0xAD, 0xBE, 0xEF},
		isAutoRecoverable: true,
	},
	{
		name:              "large_file_1mb",
		content:           makeLargeGarbage(1 << 20), // 1 MiB
		isAutoRecoverable: true,
	},
	{
		name:    "empty_file",
		content: []byte{}, // 0 bytes — generic error, not ErrCorrupted
		// Empty file is treated as "being written" → synthetic HeldError.
		isAutoRecoverable: false,
	},
	{
		name:    "wrong_schema",
		content: []byte(`{"foo":"bar","baz":42}`), // valid JSON, zero-value Lock
		// Parses into Lock with all zero values — treated as held, not corrupted.
		isAutoRecoverable: false,
	},
}

func makeLargeGarbage(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(i % 251) // non-zero, non-JSON bytes
	}
	return b
}

// TestChaos_Acquire_NoPanic verifies that no corruption variant causes a panic
// in the Acquire path.
func TestChaos_Acquire_NoPanic(t *testing.T) {
	for _, tc := range corruptionCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			locksDir := filepath.Join(root, "locks")
			if err := os.MkdirAll(locksDir, 0750); err != nil {
				t.Fatal(err)
			}

			lockName := "chaos-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			// Must not panic — that's the primary assertion.
			err := Acquire(root, lockName, AcquireOptions{})

			if tc.isAutoRecoverable {
				if err != nil {
					t.Fatalf("Acquire should auto-recover from %s corruption, got: %v", tc.name, err)
				}
				// Lock should now be valid and owned by us.
				got, readErr := lockfile.Read(path)
				if readErr != nil {
					t.Fatalf("Read after recovery: %v", readErr)
				}
				if got.PID != os.Getpid() {
					t.Errorf("Lock PID = %d, want %d", got.PID, os.Getpid())
				}
			} else {
				// Non-recoverable: expect a HeldError (synthetic).
				var held *HeldError
				if !errors.As(err, &held) {
					t.Fatalf("Expected HeldError for %s, got: %v", tc.name, err)
				}
			}
		})
	}
}

// TestChaos_Release_NoPanic verifies that Release handles corruption
// gracefully across all modes: normal, force, and break-stale.
func TestChaos_Release_NoPanic(t *testing.T) {
	modes := []struct {
		name string
		opts ReleaseOptions
	}{
		{"normal", ReleaseOptions{}},
		{"force", ReleaseOptions{Force: true}},
		{"break_stale", ReleaseOptions{BreakStale: true}},
	}

	for _, tc := range corruptionCases {
		for _, mode := range modes {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				root := t.TempDir()
				locksDir := filepath.Join(root, "locks")
				if err := os.MkdirAll(locksDir, 0750); err != nil {
					t.Fatal(err)
				}

				lockName := "chaos-" + tc.name
				path := filepath.Join(locksDir, lockName+".json")
				if err := os.WriteFile(path, tc.content, 0600); err != nil {
					t.Fatal(err)
				}

				// Must not panic.
				err := Release(root, lockName, mode.opts)

				// Force and break-stale should remove corrupted files
				// that produce ErrCorrupted.
				if tc.isAutoRecoverable && (mode.opts.Force || mode.opts.BreakStale) {
					if err != nil {
						t.Fatalf("Release(%s) with %s should remove corrupted lock, got: %v",
							tc.name, mode.name, err)
					}
					if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
						t.Errorf("Lock file should be removed after %s release", mode.name)
					}
				}
			})
		}
	}
}

// TestChaos_TryBreakStale_NoPanic verifies that tryBreakStale handles
// corruption gracefully.
func TestChaos_TryBreakStale_NoPanic(t *testing.T) {
	for _, tc := range corruptionCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			locksDir := filepath.Join(root, "locks")
			if err := os.MkdirAll(locksDir, 0750); err != nil {
				t.Fatal(err)
			}

			lockName := "chaos-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			// Must not panic.
			removed := tryBreakStale(root, lockName)

			if tc.isAutoRecoverable {
				if !removed {
					t.Errorf("tryBreakStale should remove %s corruption", tc.name)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Error("Lock file should be deleted")
				}
			}
		})
	}
}

// TestChaos_Acquire_RecoverAndAudit verifies the full recovery flow:
// corrupted → auto-prune with audit → acquire succeeds with audit.
func TestChaos_Acquire_RecoverAndAudit(t *testing.T) {
	for _, tc := range corruptionCases {
		if !tc.isAutoRecoverable {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			auditor := audit.NewWriter(root)
			locksDir := filepath.Join(root, "locks")
			if err := os.MkdirAll(locksDir, 0750); err != nil {
				t.Fatal(err)
			}

			lockName := "chaos-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			err := Acquire(root, lockName, AcquireOptions{Auditor: auditor})
			if err != nil {
				t.Fatalf("Acquire() error = %v", err)
			}

			// Verify valid lock now exists.
			got, err := lockfile.Read(path)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got.Version != lockfile.CurrentLockfileVersion {
				t.Errorf("Version = %d, want %d", got.Version, lockfile.CurrentLockfileVersion)
			}
			if got.Name != lockName {
				t.Errorf("Name = %q, want %q", got.Name, lockName)
			}

			// Verify audit events: corrupt-break followed by acquire.
			events := readAuditEvents(t, root)
			if len(events) < 2 {
				t.Fatalf("Expected >= 2 audit events, got %d", len(events))
			}
			if events[0].Event != audit.EventCorruptBreak {
				t.Errorf("First event = %q, want %q", events[0].Event, audit.EventCorruptBreak)
			}
			if events[1].Event != audit.EventAcquire {
				t.Errorf("Second event = %q, want %q", events[1].Event, audit.EventAcquire)
			}
		})
	}
}

// TestChaos_Release_CorruptedAudit verifies that force-releasing a corrupted
// lock emits the correct audit event.
func TestChaos_Release_CorruptedAudit(t *testing.T) {
	for _, tc := range corruptionCases {
		if !tc.isAutoRecoverable {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			auditor := audit.NewWriter(root)
			locksDir := filepath.Join(root, "locks")
			if err := os.MkdirAll(locksDir, 0750); err != nil {
				t.Fatal(err)
			}

			lockName := "chaos-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			err := Release(root, lockName, ReleaseOptions{
				Force:   true,
				Auditor: auditor,
			})
			if err != nil {
				t.Fatalf("Release() error = %v", err)
			}

			// File should be gone.
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Error("Lock file should be removed")
			}

			// Verify audit event.
			events := readAuditEvents(t, root)
			if len(events) != 1 {
				t.Fatalf("Expected 1 audit event, got %d", len(events))
			}
			if events[0].Event != audit.EventCorruptBreak {
				t.Errorf("Event = %q, want %q", events[0].Event, audit.EventCorruptBreak)
			}
		})
	}
}

// TestChaos_WrongSchema_Details verifies the specific behavior for
// valid-JSON-wrong-schema: it parses into a zero-value Lock, which is
// treated as an active lock (not corrupted).
func TestChaos_WrongSchema_Details(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Write valid JSON with no lock fields.
	path := filepath.Join(locksDir, "wrong-schema.json")
	if err := os.WriteFile(path, []byte(`{"foo":"bar","baz":42}`), 0600); err != nil {
		t.Fatal(err)
	}

	err := Acquire(root, "wrong-schema", AcquireOptions{})

	// Should get HeldError because the zero-value Lock is treated as held.
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("Expected HeldError, got: %v", err)
	}

	// The HeldError should contain a lock with zero values (no owner, no PID).
	if held.Lock.Owner != "" {
		t.Errorf("Lock.Owner = %q, want empty", held.Lock.Owner)
	}
	if held.Lock.PID != 0 {
		t.Errorf("Lock.PID = %d, want 0", held.Lock.PID)
	}
}

// TestChaos_EmptyFile_Details verifies the specific behavior for
// empty lockfiles: treated as "being written" (synthetic HeldError),
// not as corrupted.
func TestChaos_EmptyFile_Details(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(locksDir, "empty.json")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	err := Acquire(root, "empty", AcquireOptions{})

	// Should get HeldError (synthetic) because empty is NOT ErrCorrupted.
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("Expected HeldError, got: %v", err)
	}

	// The synthetic HeldError has lock name but no ownership info.
	if held.Lock.Name != "empty" {
		t.Errorf("Lock.Name = %q, want %q", held.Lock.Name, "empty")
	}
}

// TestChaos_BreakStale_ThenAcquire verifies the end-to-end recovery path:
// corrupted lock → Release with BreakStale → Acquire succeeds.
func TestChaos_BreakStale_ThenAcquire(t *testing.T) {
	for _, tc := range corruptionCases {
		if !tc.isAutoRecoverable {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			locksDir := filepath.Join(root, "locks")
			if err := os.MkdirAll(locksDir, 0750); err != nil {
				t.Fatal(err)
			}

			lockName := "chaos-e2e-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			// Step 1: break-stale release removes the corrupted file.
			err := Release(root, lockName, ReleaseOptions{BreakStale: true})
			if err != nil {
				t.Fatalf("Release(BreakStale) error = %v", err)
			}

			// Step 2: acquire should now succeed cleanly.
			err = Acquire(root, lockName, AcquireOptions{})
			if err != nil {
				t.Fatalf("Acquire after break-stale error = %v", err)
			}

			// Verify valid lock.
			got, readErr := lockfile.Read(path)
			if readErr != nil {
				t.Fatalf("Read() error = %v", readErr)
			}
			if got.PID != os.Getpid() {
				t.Errorf("PID = %d, want %d", got.PID, os.Getpid())
			}
		})
	}
}

// TestChaos_ErrorMessages verifies that error messages for non-recoverable
// corruption are informative (not raw stack traces or empty strings).
func TestChaos_ErrorMessages(t *testing.T) {
	root := t.TempDir()
	locksDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(locksDir, 0750); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		content    []byte
		wantSubstr string
	}{
		{
			name:       "truncated_json_release",
			content:    []byte(`{"version":1,"name":"test"`),
			wantSubstr: "corrupted",
		},
		{
			name:       "binary_garbage_release",
			content:    []byte{0xFF, 0xFE, 0xFD},
			wantSubstr: "corrupted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lockName := "chaos-msg-" + tc.name
			path := filepath.Join(locksDir, lockName+".json")
			if err := os.WriteFile(path, tc.content, 0600); err != nil {
				t.Fatal(err)
			}

			// Normal release (no force/break-stale) should report corruption.
			err := Release(root, lockName, ReleaseOptions{})
			if err == nil {
				t.Fatal("Expected error for corrupted lock with normal release")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("Error %q should contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}
