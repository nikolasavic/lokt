package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCmdAudit_NoFlags(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdAudit, nil)
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage message, got: %s", stderr)
	}
}

func TestCmdAudit_SinceAndTailMutuallyExclusive(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdAudit, []string{"--since", "1h", "--tail"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("expected mutual exclusion error, got: %s", stderr)
	}
}

func TestCmdAudit_InvalidSince(t *testing.T) {
	setupTestRoot(t)

	_, stderr, code := captureCmd(cmdAudit, []string{"--since", "not-a-duration"})
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr, "invalid --since") {
		t.Errorf("expected invalid since error, got: %s", stderr)
	}
}

func TestCmdAudit_SinceDuration_EmptyLog(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	// Create empty audit log
	auditPath := filepath.Join(rootDir, "audit.log")
	if err := os.WriteFile(auditPath, nil, 0600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := captureCmd(cmdAudit, []string{"--since", "1h"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected empty output for empty log, got: %s", stdout)
	}
}

func TestCmdAudit_SinceDuration_FiltersEvents(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	auditPath := filepath.Join(rootDir, "audit.log")
	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write old event (2 hours ago)
	oldEvent := auditEvent{
		Timestamp: time.Now().Add(-2 * time.Hour),
		Event:     "acquire",
		Name:      "old-lock",
		Owner:     "alice",
		Host:      "h1",
		PID:       1,
	}
	data, _ := json.Marshal(oldEvent)
	_, _ = f.Write(append(data, '\n'))

	// Write recent event (5 minutes ago)
	recentEvent := auditEvent{
		Timestamp: time.Now().Add(-5 * time.Minute),
		Event:     "release",
		Name:      "recent-lock",
		Owner:     "bob",
		Host:      "h2",
		PID:       2,
	}
	data, _ = json.Marshal(recentEvent)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	// Query since 1h
	stdout, _, code := captureCmd(cmdAudit, []string{"--since", "1h"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if strings.Contains(stdout, "old-lock") {
		t.Errorf("old event should be filtered out")
	}
	if !strings.Contains(stdout, "recent-lock") {
		t.Errorf("recent event should appear, got: %s", stdout)
	}
}

func TestCmdAudit_SinceTimestamp(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	auditPath := filepath.Join(rootDir, "audit.log")
	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	event := auditEvent{
		Timestamp: now,
		Event:     "acquire",
		Name:      "ts-lock",
		Owner:     "alice",
		Host:      "h1",
		PID:       1,
	}
	data, _ := json.Marshal(event)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	// Query since a timestamp before the event
	since := now.Add(-1 * time.Minute).Format(time.RFC3339)
	stdout, _, code := captureCmd(cmdAudit, []string{"--since", since})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "ts-lock") {
		t.Errorf("event should appear, got: %s", stdout)
	}
}

func TestCmdAudit_SinceWithNameFilter(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	auditPath := filepath.Join(rootDir, "audit.log")
	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	events := []auditEvent{
		{Timestamp: now, Event: "acquire", Name: "wanted", Owner: "a", Host: "h", PID: 1},
		{Timestamp: now, Event: "acquire", Name: "unwanted", Owner: "b", Host: "h", PID: 2},
	}
	for _, e := range events {
		data, _ := json.Marshal(e)
		_, _ = f.Write(append(data, '\n'))
	}
	_ = f.Close()

	stdout, _, code := captureCmd(cmdAudit, []string{"--since", "1h", "--name", "wanted"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "wanted") {
		t.Errorf("wanted event should appear")
	}
	if strings.Contains(stdout, "unwanted") {
		t.Errorf("unwanted event should be filtered")
	}
}

func TestCmdAudit_NoAuditLog(t *testing.T) {
	setupTestRoot(t) // no audit.log created

	_, _, code := captureCmd(cmdAudit, []string{"--since", "1h"})
	if code != ExitOK {
		t.Errorf("expected exit %d for missing audit log, got %d", ExitOK, code)
	}
}

func TestCmdAudit_MalformedLines(t *testing.T) {
	rootDir, _ := setupTestRoot(t)

	auditPath := filepath.Join(rootDir, "audit.log")
	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatal(err)
	}

	// Malformed line followed by valid event
	_, _ = f.WriteString("not valid json at all\n")
	event := auditEvent{
		Timestamp: time.Now(),
		Event:     "acquire",
		Name:      "valid-lock",
		Owner:     "alice",
		Host:      "h1",
		PID:       1,
	}
	data, _ := json.Marshal(event)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	stdout, _, code := captureCmd(cmdAudit, []string{"--since", "1h"})
	if code != ExitOK {
		t.Errorf("expected exit %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout, "valid-lock") {
		t.Errorf("valid event should appear after malformed line")
	}
}

func TestParseSince_Duration(t *testing.T) {
	ts, err := parseSince("1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be approximately 1 hour ago
	diff := time.Since(ts)
	if diff < 59*time.Minute || diff > 61*time.Minute {
		t.Errorf("expected ~1h ago, got %v", diff)
	}
}

func TestParseSince_RFC3339(t *testing.T) {
	input := "2026-01-15T10:00:00Z"
	ts, err := parseSince(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := time.Parse(time.RFC3339, input)
	if !ts.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, ts)
	}
}

func TestParseSince_Invalid(t *testing.T) {
	_, err := parseSince("not-a-time")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}
