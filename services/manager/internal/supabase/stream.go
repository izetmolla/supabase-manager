package supabase

import (
	"strings"
	"sync"
)

// stream fans out job output lines to any number of subscribers.
type stream struct {
	mu     sync.Mutex
	lines  []string
	size   int
	subs   map[chan string]struct{}
	done   chan string
	status string
	closed bool
}

func newStream() *stream {
	return &stream{subs: map[chan string]struct{}{}, done: make(chan string, 1)}
}

func (s *stream) write(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	s.size += len(line) + 1
	for ch := range s.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (s *stream) close(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.status = status
	s.done <- status
	close(s.done)
}

func (s *stream) subscribe() ([]string, <-chan string, <-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := append([]string(nil), s.lines...)
	ch := make(chan string, 256)
	done := make(chan string, 1)
	if s.closed {
		done <- s.status
		close(done)
		return history, ch, done, func() {}
	}
	s.subs[ch] = struct{}{}
	go func() {
		st, ok := <-s.done
		if !ok {
			s.mu.Lock()
			st = s.status
			s.mu.Unlock()
		}
		done <- st
		close(done)
	}()
	return history, ch, done, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// text returns the output, keeping the tail when it exceeds max bytes.
func (s *stream) text(max int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := strings.Join(s.lines, "\n")
	if len(out) > max {
		out = "...(truncated)\n" + out[len(out)-max:]
	}
	return out
}
