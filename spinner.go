package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// spinner renders a TTY-friendly liveliness indicator with an elapsed timer to
// an io.Writer (stderr in practice). It is a no-op when disabled.
type spinner struct {
	w       io.Writer
	enabled bool
	label   string

	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	started bool
}

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// newSpinner builds a spinner. It is enabled only when not quiet and w refers to
// a terminal.
func newSpinner(w io.Writer, label string, quiet bool) *spinner {
	return &spinner{
		w:       w,
		enabled: !quiet && isTerminal(w),
		label:   label,
	}
}

// Start begins animating in a background goroutine. Safe to call when disabled.
func (s *spinner) Start() {
	if s == nil || !s.enabled {
		return
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.mu.Unlock()

	go s.run()
}

func (s *spinner) run() {
	defer close(s.done)
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			frame := spinnerFrames[i%len(spinnerFrames)]
			elapsed := time.Since(start).Truncate(time.Second)
			fmt.Fprintf(s.w, "\r\033[K%c %s (%s)", frame, s.label, elapsed)
			i++
		}
	}
}

// Stop halts the animation and clears the line.
func (s *spinner) Stop() {
	if s == nil || !s.enabled {
		return
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	stop, done := s.stop, s.done
	s.mu.Unlock()

	close(stop)
	<-done
	fmt.Fprint(s.w, "\r\033[K")
}

// isTerminal reports whether w is an *os.File attached to a character device.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
