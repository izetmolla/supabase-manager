package configtoml

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// File is a config.toml that is read with go-toml and edited line by line, so
// comments and formatting outside the edited keys are preserved.
type File struct {
	Path string
	text string
	data map[string]any
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := &File{Path: path}
	if err := f.SetText(string(b)); err != nil {
		return nil, err
	}
	return f, nil
}

func Parse(text string) (map[string]any, error) {
	data := map[string]any{}
	if err := toml.Unmarshal([]byte(text), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (f *File) Text() string { return f.text }

func (f *File) SetText(text string) error {
	data, err := Parse(text)
	if err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}
	f.text = text
	f.data = data
	return nil
}

// Save writes the file, keeping the previous version as config.toml.bak.
func (f *File) Save() error {
	if _, err := Parse(f.text); err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}
	if old, err := os.ReadFile(f.Path); err == nil {
		if err := os.WriteFile(f.Path+".bak", old, 0o644); err != nil {
			return err
		}
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(f.text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

func (f *File) Get(path ...string) any {
	var cur any = f.data
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func (f *File) HasSection(section string) bool {
	_, ok := f.Get(strings.Split(section, ".")...).(map[string]any)
	return ok
}

func (f *File) String(def string, path ...string) string {
	if v, ok := f.Get(path...).(string); ok {
		return v
	}
	return def
}

func (f *File) Bool(def bool, path ...string) bool {
	if v, ok := f.Get(path...).(bool); ok {
		return v
	}
	return def
}

func (f *File) Int(def int, path ...string) int {
	switch v := f.Get(path...).(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

func (f *File) Strings(path ...string) []string {
	out := []string{}
	if arr, ok := f.Get(path...).([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// Set assigns key = value inside [section] ("" is the root table). An existing
// uncommented key is replaced in place, otherwise the key is inserted right after
// the section header, and a missing section is appended to the file.
func (f *File) Set(section, key string, value any) error {
	enc, err := encodeValue(value)
	if err != nil {
		return err
	}
	line := key + " = " + enc
	lines := strings.Split(f.text, "\n")

	start, end := sectionBounds(lines, section)
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "["+section+"]", line, "")
		return f.SetText(strings.Join(lines, "\n"))
	}

	for i := start; i < end; i++ {
		if lineKey(lines[i]) != key {
			continue
		}
		last := valueEnd(lines, i, end)
		repl := append([]string{}, lines[:i]...)
		repl = append(repl, line)
		repl = append(repl, lines[last+1:]...)
		return f.SetText(strings.Join(repl, "\n"))
	}

	insertAt := start
	if section == "" {
		insertAt = 0
	}
	repl := append([]string{}, lines[:insertAt]...)
	repl = append(repl, line)
	repl = append(repl, lines[insertAt:]...)
	return f.SetText(strings.Join(repl, "\n"))
}

// sectionBounds returns the line range [start, end) of the section body, or -1
// if the section header does not exist.
func sectionBounds(lines []string, section string) (int, int) {
	start := -1
	if section == "" {
		start = 0
	}
	for i, l := range lines {
		name, ok := headerName(l)
		if !ok {
			continue
		}
		if start >= 0 {
			return start, i
		}
		if name == section {
			start = i + 1
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(lines)
}

func headerName(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "[") || strings.HasPrefix(t, "[[") {
		return "", false
	}
	if i := strings.Index(t, "#"); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if !strings.HasSuffix(t, "]") {
		return "", false
	}
	return strings.TrimSpace(t[1 : len(t)-1]), true
}

func lineKey(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
		return ""
	}
	i := strings.Index(t, "=")
	if i <= 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(t[:i]), `"`)
}

// valueEnd finds the last line of a value that may be a multi-line array.
func valueEnd(lines []string, i, end int) int {
	depth := bracketDepth(lines[i][strings.Index(lines[i], "=")+1:])
	j := i
	for depth > 0 && j+1 < end {
		j++
		depth += bracketDepth(lines[j])
	}
	return j
}

func bracketDepth(s string) int {
	depth := 0
	inStr := false
	var quote rune
	for idx, r := range s {
		switch {
		case inStr:
			if r == quote && (idx == 0 || s[idx-1] != '\\') {
				inStr = false
			}
		case r == '"' || r == '\'':
			inStr, quote = true, r
		case r == '#':
			return depth
		case r == '[':
			depth++
		case r == ']':
			depth--
		}
	}
	return depth
}

func encodeValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return quote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("unsupported TOML value type %T", v)
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
