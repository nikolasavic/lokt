package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventJSONSerialization(t *testing.T) {
	ts := time.Date(2026, 1, 27, 15, 30, 0, 0, time.UTC)
	event := Event{
		Timestamp: ts,
		Event:     EventAcquire,
		Name:      "build",
		Owner:     "alice",
		Host:      "host1",
		PID:       12345,
		TTLSec:    300,
		Extra:     map[string]any{"key": "value"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Verify RFC3339 timestamp format
	jsonStr := string(data)
	if !strings.Contains(jsonStr, "2026-01-27T15:30:00Z") {
		t.Errorf("Expected RFC3339 timestamp, got: %s", jsonStr)
	}

	// Verify field names match spec
	expectedFields := []string{`"ts":`, `"event":`, `"name":`, `"owner":`, `"host":`, `"pid":`, `"ttl_sec":`, `"extra":`}
	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Missing expected field %q in JSON: %s", field, jsonStr)
		}
	}

	// Verify roundtrip
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.Event != event.Event {
		t.Errorf("Event = %q, want %q", decoded.Event, event.Event)
	}
	if decoded.Name != event.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, event.Name)
	}
}

func TestEventOmitsEmptyFields(t *testing.T) {
	event := Event{
		Timestamp: time.Now(),
		Event:     EventRelease,
		Name:      "build",
		Owner:     "alice",
		Host:      "host1",
		PID:       12345,
		// TTLSec and Extra intentionally omitted
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "ttl_sec") {
		t.Errorf("Expected ttl_sec to be omitted when zero, got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "extra") {
		t.Errorf("Expected extra to be omitted when nil, got: %s", jsonStr)
	}
}

func TestWriterCreatesFileOnFirstEmit(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	event := Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "host1",
		PID:   12345,
	}

	w.Emit(&event)

	path := filepath.Join(dir, "audit.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Expected audit.log to be created")
	}
}

func TestWriterAppendsMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	events := []Event{
		{Event: EventAcquire, Name: "lock1", Owner: "alice", Host: "h1", PID: 1},
		{Event: EventDeny, Name: "lock1", Owner: "bob", Host: "h2", PID: 2},
		{Event: EventRelease, Name: "lock1", Owner: "alice", Host: "h1", PID: 1},
	}

	for i := range events {
		w.Emit(&events[i])
	}

	// Read and verify JSONL format (one JSON object per line)
	path := filepath.Join(dir, "audit.log")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Failed to open audit.log: %v", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		var decoded Event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", lineCount+1, err)
		}
		if decoded.Event != events[lineCount].Event {
			t.Errorf("Line %d: Event = %q, want %q", lineCount+1, decoded.Event, events[lineCount].Event)
		}
		lineCount++
	}

	if lineCount != len(events) {
		t.Errorf("Expected %d lines, got %d", len(events), lineCount)
	}
}

func TestWriterSetsTimestampIfMissing(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	before := time.Now()
	w.Emit(&Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "h1",
		PID:   1,
		// Timestamp intentionally omitted (zero value)
	})
	after := time.Now()

	path := filepath.Join(dir, "audit.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read audit.log: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Timestamp.Before(before) || decoded.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in expected range [%v, %v]", decoded.Timestamp, before, after)
	}
}

func TestWriterHandlesMissingDirectory(t *testing.T) {
	// Writer should not panic when directory doesn't exist.
	// It logs to stderr but doesn't return an error.
	w := NewWriter("/nonexistent/path/that/does/not/exist")

	// This should not panic
	w.Emit(&Event{
		Event: EventAcquire,
		Name:  "test",
		Owner: "alice",
		Host:  "h1",
		PID:   1,
	})
}

func TestEventConstants(t *testing.T) {
	// Verify all event constants are defined and non-empty
	constants := []string{
		EventAcquire,
		EventDeny,
		EventRelease,
		EventForceBreak,
		EventStaleBreak,
	}

	for _, c := range constants {
		if c == "" {
			t.Error("Event constant should not be empty")
		}
	}

	// Verify expected values
	if EventAcquire != "acquire" {
		t.Errorf("EventAcquire = %q, want %q", EventAcquire, "acquire")
	}
	if EventDeny != "deny" {
		t.Errorf("EventDeny = %q, want %q", EventDeny, "deny")
	}
	if EventRelease != "release" {
		t.Errorf("EventRelease = %q, want %q", EventRelease, "release")
	}
	if EventForceBreak != "force-break" {
		t.Errorf("EventForceBreak = %q, want %q", EventForceBreak, "force-break")
	}
	if EventStaleBreak != "stale-break" {
		t.Errorf("EventStaleBreak = %q, want %q", EventStaleBreak, "stale-break")
	}
}
