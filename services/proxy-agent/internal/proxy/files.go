package proxy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File is one configuration file sent by the manager.
type File struct {
	Content []byte
	Mode    os.FileMode
}

const checksumFile = ".sm-checksum"

// safePath resolves a manager-supplied relative path inside root.
func safePath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid file path %q", name)
	}
	return filepath.Join(root, clean), nil
}

// writeTree writes files into dir, which must not exist yet or be empty.
func writeTree(dir string, files map[string]File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, f := range files {
		p, err := safePath(dir, name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(p, f.Content, mode); err != nil {
			return err
		}
		if err := os.Chmod(p, mode); err != nil {
			return err
		}
	}
	return nil
}

// readTree loads every regular file under dir, keyed by slash-separated relative path.
func readTree(dir string) (map[string]File, error) {
	out := map[string]File{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = File{Content: b, Mode: info.Mode().Perm()}
		return nil
	})
	return out, err
}

// replaceTree makes dir contain exactly files: stale files are removed, the rest rewritten.
func replaceTree(dir string, files map[string]File) error {
	old, err := readTree(dir)
	if err != nil {
		return err
	}
	for name := range old {
		if _, keep := files[name]; !keep {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return writeTree(dir, files)
}

func readChecksum(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, checksumFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func withChecksum(files map[string]File, sum string) map[string]File {
	out := make(map[string]File, len(files)+1)
	for k, v := range files {
		if k == checksumFile {
			continue
		}
		out[k] = v
	}
	out[checksumFile] = File{Content: []byte(sum + "\n"), Mode: 0o644}
	return out
}
