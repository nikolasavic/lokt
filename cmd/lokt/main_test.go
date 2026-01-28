package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTailAuditLog_OutputsNewEvents(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	// Create initial audit log
	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatalf("Failed to create audit.log: %v", err)
	}
	_ = f.Close()

	// Start tailing in background with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "")
	}()

	// Give tailer time to start and seek to end
	time.Sleep(50 * time.Millisecond)

	// Write an event
	event := auditEvent{
		Timestamp: time.Now(),
		Event:     "acquire",
		Name:      "test-lock",
		Owner:     "alice",
		Host:      "host1",
		PID:       12345,
	}
	data, _ := json.Marshal(event)

	f, _ = os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	// Wait for tailer to finish
	exitCode := <-done

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if exitCode != ExitOK {
		t.Errorf("Expected exit code %d, got %d", ExitOK, exitCode)
	}

	if !strings.Contains(output, "test-lock") {
		t.Errorf("Expected output to contain 'test-lock', got: %s", output)
	}
}

func TestTailAuditLog_NameFilter(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatalf("Failed to create audit.log: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "wanted-lock")
	}()

	time.Sleep(50 * time.Millisecond)

	// Write two events: one matching filter, one not
	events := []auditEvent{
		{Timestamp: time.Now(), Event: "acquire", Name: "unwanted-lock", Owner: "alice", Host: "h1", PID: 1},
		{Timestamp: time.Now(), Event: "acquire", Name: "wanted-lock", Owner: "bob", Host: "h2", PID: 2},
	}

	f, _ = os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0644)
	for _, e := range events {
		data, _ := json.Marshal(e)
		_, _ = f.Write(append(data, '\n'))
	}
	_ = f.Close()

	<-done

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if strings.Contains(output, "unwanted-lock") {
		t.Errorf("Expected output to NOT contain 'unwanted-lock', got: %s", output)
	}
	if !strings.Contains(output, "wanted-lock") {
		t.Errorf("Expected output to contain 'wanted-lock', got: %s", output)
	}
}

func TestTailAuditLog_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatalf("Failed to create audit.log: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "")
	}()

	// Let it start
	time.Sleep(50 * time.Millisecond)

	// Cancel context (simulates SIGINT)
	cancel()

	select {
	case exitCode := <-done:
		if exitCode != ExitOK {
			t.Errorf("Expected exit code %d on context cancel, got %d", ExitOK, exitCode)
		}
	case <-time.After(time.Second):
		t.Error("Tailer did not exit after context cancel")
	}
}

func TestTailAuditLog_WaitsForFileCreation(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")
	// Note: file does NOT exist initially

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "")
	}()

	// Verify tailer is waiting (file doesn't exist)
	time.Sleep(100 * time.Millisecond)

	select {
	case <-done:
		t.Error("Tailer should be waiting for file, but it exited early")
	default:
		// Good - still waiting
	}

	// Create the file
	f, _ := os.Create(auditPath)
	_ = f.Close()

	// Should still be running (now tailing)
	time.Sleep(100 * time.Millisecond)

	select {
	case <-done:
		t.Error("Tailer should still be running after file creation")
	default:
		// Good
	}

	cancel()
	<-done
}

func TestTailAuditLog_DetectsTruncation(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	// Create file with initial content
	initialEvent := auditEvent{
		Timestamp: time.Now(),
		Event:     "acquire",
		Name:      "initial",
		Owner:     "alice",
		Host:      "h1",
		PID:       1,
	}
	data, _ := json.Marshal(initialEvent)
	err := os.WriteFile(auditPath, append(data, '\n'), 0644)
	if err != nil {
		t.Fatalf("Failed to write initial content: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "")
	}()

	time.Sleep(100 * time.Millisecond)

	// Truncate the file (simulates log rotation)
	_ = os.Truncate(auditPath, 0)

	time.Sleep(100 * time.Millisecond)

	// Write new content after truncation
	newEvent := auditEvent{
		Timestamp: time.Now(),
		Event:     "release",
		Name:      "after-truncate",
		Owner:     "bob",
		Host:      "h2",
		PID:       2,
	}
	data, _ = json.Marshal(newEvent)
	f, _ := os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	<-done

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	// Should see the event written after truncation
	if !strings.Contains(output, "after-truncate") {
		t.Errorf("Expected output to contain 'after-truncate' after file truncation, got: %s", output)
	}
}

func TestTailAuditLog_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	f, err := os.Create(auditPath)
	if err != nil {
		t.Fatalf("Failed to create audit.log: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan int)
	go func() {
		done <- tailAuditLog(ctx, auditPath, "")
	}()

	time.Sleep(50 * time.Millisecond)

	// Write malformed line followed by valid event
	f, _ = os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.WriteString("this is not valid json\n")
	event := auditEvent{
		Timestamp: time.Now(),
		Event:     "acquire",
		Name:      "valid-event",
		Owner:     "alice",
		Host:      "h1",
		PID:       1,
	}
	data, _ := json.Marshal(event)
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()

	exitCode := <-done

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if exitCode != ExitOK {
		t.Errorf("Expected exit code %d, got %d", ExitOK, exitCode)
	}

	// Malformed line should be skipped, valid event should appear
	if strings.Contains(output, "this is not valid json") {
		t.Errorf("Malformed line should not appear in output")
	}
	if !strings.Contains(output, "valid-event") {
		t.Errorf("Expected valid event in output, got: %s", output)
	}
}
