package main

import (
	"bytes"
	"testing"
)

func TestSpinnerDisabledWhenQuiet(t *testing.T) {
	var buf bytes.Buffer
	// Quiet always disables, regardless of TTY status of the writer.
	s := newSpinner(nil, "x", true)
	if s.enabled {
		t.Error("spinner should be disabled when quiet")
	}
	// A bytes.Buffer is not a terminal, so non-quiet is still disabled.
	s2 := &spinner{w: &buf, enabled: isTerminal(&buf)}
	if s2.enabled {
		t.Error("spinner should be disabled for non-TTY writer")
	}
}

func TestSpinnerStartStopNoopWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	s := newSpinner(&buf, "label", false) // non-TTY -> disabled
	s.Start()
	s.Stop()
	if buf.Len() != 0 {
		t.Errorf("disabled spinner wrote output: %q", buf.String())
	}
}

func TestIsTerminalNonFile(t *testing.T) {
	var buf bytes.Buffer
	if isTerminal(&buf) {
		t.Error("bytes.Buffer reported as terminal")
	}
}
