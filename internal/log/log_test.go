package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.log")

	if err := Setup(p); err != nil {
		t.Fatal(err)
	}
	defer Close()

	Debugf("hello %s", "world")

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[DEBUG] hello world") {
		t.Errorf("expected log line, got: %s", b)
	}
}

func TestSetupLevelGating(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "level.log")

	if err := SetupLevel(p, Info); err != nil {
		t.Fatal(err)
	}
	defer Close()

	Debugf("should be silent")     // below Info
	Infof("should appear %d", 42)  // at Info
	Warnf("should appear too")     // above Info

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "should be silent") {
		t.Errorf("Debugf should be silent at Info level")
	}
	if !strings.Contains(s, "[INFO] should appear 42") {
		t.Errorf("Infof should appear, got: %s", s)
	}
	if !strings.Contains(s, "[WARN] should appear too") {
		t.Errorf("Warnf should appear, got: %s", s)
	}
}

func TestSetLevel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "set.log")

	if err := Setup(p); err != nil {
		t.Fatal(err)
	}
	defer Close()

	// Debug is open; Debugf and Errorf both write.
	Debugf("debug msg")
	Errorf("error msg")
	SetLevel(Error)
	Debugf("debug after raise") // should be silent
	Errorf("error after raise")

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "debug msg") {
		t.Errorf("expected debug msg when level=Debug")
	}
	if !strings.Contains(s, "error msg") {
		t.Errorf("expected error msg when level=Debug")
	}
	if strings.Contains(s, "debug after raise") {
		t.Errorf("Debugf should be silent after SetLevel(Error)")
	}
	if !strings.Contains(s, "error after raise") {
		t.Errorf("Errorf should still appear after SetLevel(Error)")
	}
}

func TestDisabled(t *testing.T) {
	// No Setup — all functions are no-ops.
	Debugf("should not panic")
	Infof("should not panic")
	Warnf("should not panic")
	Errorf("should not panic")
}

func TestEnabledAt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "enabled.log")

	if err := SetupLevel(p, Warn); err != nil {
		t.Fatal(err)
	}
	defer Close()

	if Enabled() {
		t.Error("Enabled() should return false at Warn level")
	}
	if EnabledAt(Debug) {
		t.Error("EnabledAt(Debug) should return false at Warn level")
	}
	if EnabledAt(Info) {
		t.Error("EnabledAt(Info) should return false at Warn level")
	}
	if !EnabledAt(Warn) {
		t.Error("EnabledAt(Warn) should return true at Warn level")
	}
	if !EnabledAt(Error) {
		t.Error("EnabledAt(Error) should return true at Warn level")
	}
}

func TestLineEnding(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "line.log")

	if err := Setup(p); err != nil {
		t.Fatal(err)
	}
	defer Close()

	Debugf("line")
	Debugf("two words")

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %s", len(lines), s)
	}
}
