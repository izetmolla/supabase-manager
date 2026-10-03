package logs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestFollow(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "1.log")
	appendFile(t, a, "a1\na2\na3\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := make(chan Line, 100)
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, []string{filepath.Join(dir, "*.log")}, 2, func(b []Line) error {
			for _, l := range b {
				got <- l
			}
			return nil
		})
	}()
	next := func() Line {
		t.Helper()
		select {
		case l := <-got:
			return l
		case <-ctx.Done():
			t.Fatal("timed out waiting for a log line")
			return Line{}
		}
	}
	expect := func(source, text string) {
		t.Helper()
		if l := next(); l.Source != source || l.Text != text {
			t.Fatalf("line = %+v, want %s %q", l, source, text)
		}
	}

	expect("1.log", "a2")
	expect("1.log", "a3")

	// A partial line is held back until it is complete.
	appendFile(t, a, "a4\npart")
	expect("1.log", "a4")
	appendFile(t, a, "ial\n")
	expect("1.log", "partial")

	// New files are followed from their start.
	appendFile(t, filepath.Join(dir, "2.log"), "b1\n")
	expect("2.log", "b1")

	// A truncated file is followed from its new start.
	if err := os.Truncate(a, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * pollInterval)
	appendFile(t, a, "after\n")
	expect("1.log", "after")

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Follow returned %v", err)
	}
}

func TestTrim(t *testing.T) {
	dir := t.TempDir()
	big, small := filepath.Join(dir, "big.log"), filepath.Join(dir, "small.log")
	appendFile(t, big, "0123456789")
	appendFile(t, small, "01")
	Trim([]string{filepath.Join(dir, "*.log")}, 5)
	if info, _ := os.Stat(big); info.Size() != 0 {
		t.Fatalf("big.log has %d bytes after Trim", info.Size())
	}
	if info, _ := os.Stat(small); info.Size() != 2 {
		t.Fatalf("small.log has %d bytes after Trim", info.Size())
	}
}
