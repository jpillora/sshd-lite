package log_test

import (
	"log/slog"
	"testing"

	"github.com/jpillora/sshd-lite/sshd/sshtest/log"
)

func TestCapture(t *testing.T) {
	c := log.NewCapture()

	c.Add(log.Entry{
		Level:   slog.LevelInfo,
		Message: "test message",
	})

	if c.Count() != 1 {
		t.Errorf("expected 1 entry, got %d", c.Count())
	}

	if err := c.Assert("test message"); err != nil {
		t.Errorf("Assert failed: %v", err)
	}

	if err := c.Assert("nonexistent"); err == nil {
		t.Error("Assert should fail for nonexistent message")
	}
}

func TestCaptureLogger(t *testing.T) {
	c := log.NewCapture()
	logger := c.Logger()

	logger.Info("info message")
	logger.Error("error message")
	logger.Debug("debug message")

	if c.Count() != 3 {
		t.Errorf("expected 3 entries, got %d", c.Count())
	}

	if err := c.AssertLevel(slog.LevelInfo, "info message"); err != nil {
		t.Errorf("AssertLevel failed: %v", err)
	}

	if err := c.AssertLevel(slog.LevelError, "error message"); err != nil {
		t.Errorf("AssertLevel failed: %v", err)
	}
}

func TestCaptureFind(t *testing.T) {
	c := log.NewCapture()

	c.Add(log.Entry{Level: slog.LevelInfo, Message: "first message"})
	c.Add(log.Entry{Level: slog.LevelInfo, Message: "second message"})
	c.Add(log.Entry{Level: slog.LevelInfo, Message: "third message"})

	entry, found := c.Find("second")
	if !found {
		t.Fatal("should find entry")
	}
	if entry.Message != "second message" {
		t.Errorf("wrong message: %s", entry.Message)
	}

	_, found = c.Find("nonexistent")
	if found {
		t.Error("should not find nonexistent")
	}
}

func TestCaptureFindAll(t *testing.T) {
	c := log.NewCapture()

	c.Add(log.Entry{Level: slog.LevelInfo, Message: "test one"})
	c.Add(log.Entry{Level: slog.LevelInfo, Message: "test two"})
	c.Add(log.Entry{Level: slog.LevelInfo, Message: "other"})

	entries := c.FindAll("test")
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestCaptureClear(t *testing.T) {
	c := log.NewCapture()

	c.Add(log.Entry{Level: slog.LevelInfo, Message: "message"})
	c.Clear()

	if c.Count() != 0 {
		t.Error("Clear should remove all entries")
	}
}

func TestCaptureString(t *testing.T) {
	c := log.NewCapture()
	c.Add(log.Entry{Level: slog.LevelInfo, Message: "test"})

	s := c.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

func TestEntryMethods(t *testing.T) {
	entry := log.Entry{
		Level:   slog.LevelInfo,
		Message: "test message",
	}

	if !entry.Contains("test") {
		t.Error("Contains should return true")
	}

	if entry.Contains("nonexistent") {
		t.Error("Contains should return false")
	}

	if !entry.Matches(slog.LevelInfo, "test") {
		t.Error("Matches should return true")
	}

	if entry.Matches(slog.LevelError, "test") {
		t.Error("Matches should return false for wrong level")
	}
}

func TestHandlerWithAttrs(t *testing.T) {
	c := log.NewCapture()
	logger := c.Logger().With("key", "value")

	logger.Info("message")

	entries := c.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Attrs["key"] != "value" {
		t.Errorf("expected key=value in attrs, got %v", entries[0].Attrs)
	}
}
