// Package logs follows and trims the proxy's access log files.
package logs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Line is one access log line and the file it came from.
type Line struct {
	Source string
	Text   string
}

const (
	pollInterval = 500 * time.Millisecond
	maxRead      = 1 << 20
)

type file struct {
	offset  int64
	partial []byte
}

func glob(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		m, _ := filepath.Glob(p)
		out = append(out, m...)
	}
	slices.Sort(out)
	return out
}

// lastLines returns up to n complete lines from the end of the file and its size.
func lastLines(path string, n int) ([]string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, 0
	}
	size := info.Size()
	if n <= 0 || size == 0 {
		return nil, size
	}
	start := max(size-256*1024, 0)
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, size
	}
	buf = bytes.TrimRight(buf, "\n")
	parts := bytes.Split(buf, []byte("\n"))
	if start > 0 && len(parts) > 0 {
		parts = parts[1:] // the first line may be cut
	}
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out, size
}

// Follow sends the last n lines of every file matching patterns, then new lines as they are
// written, until ctx is cancelled or emit fails. Files that appear later are followed from
// their start; truncated files are followed from their new end.
func Follow(ctx context.Context, patterns []string, n int, emit func([]Line) error) error {
	state := map[string]*file{}
	var first []Line
	for _, p := range glob(patterns) {
		lines, size := lastLines(p, n)
		for _, l := range lines {
			first = append(first, Line{Source: filepath.Base(p), Text: l})
		}
		state[p] = &file{offset: size}
	}
	if len(first) > 0 {
		if err := emit(first); err != nil {
			return err
		}
	}
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		var batch []Line
		for _, p := range glob(patterns) {
			st := state[p]
			if st == nil {
				st = &file{}
				state[p] = st
			}
			batch = append(batch, st.read(p)...)
		}
		if len(batch) > 0 {
			if err := emit(batch); err != nil {
				return err
			}
		}
	}
}

func (st *file) read(path string) []Line {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if info.Size() < st.offset {
		st.offset, st.partial = 0, nil
	}
	if info.Size() == st.offset {
		return nil
	}
	buf := make([]byte, min(info.Size()-st.offset, maxRead))
	got, err := f.ReadAt(buf, st.offset)
	if err != nil && err != io.EOF {
		return nil
	}
	st.offset += int64(got)
	data := append(st.partial, buf[:got]...)
	var out []Line
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		if i > 0 {
			out = append(out, Line{Source: filepath.Base(path), Text: string(data[:i])})
		}
		data = data[i+1:]
	}
	st.partial = append([]byte(nil), data...)
	return out
}

// Trim empties files matching patterns that grew beyond maxBytes. The proxies append with
// O_APPEND, so they keep writing at the new end.
func Trim(patterns []string, maxBytes int64) {
	for _, p := range glob(patterns) {
		if info, err := os.Stat(p); err == nil && info.Size() > maxBytes {
			_ = os.Truncate(p, 0)
		}
	}
}
