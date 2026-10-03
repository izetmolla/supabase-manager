package supabase

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.119.0", "2.119.0", 0},
		{"2.9.0", "2.119.0", -1},
		{"2.120.1", "2.120.0", 1},
		{"3.0.0", "2.999.999", 1},
		{"2.119.0-beta.1", "2.119.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestExtractTarGzSkipsUnsafeEntries(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	add := func(name string, typ byte, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: typ, Mode: 0o755, Size: int64(len(body)), Linkname: "/etc/passwd"}); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write([]byte(body))
		}
	}
	add("supabase", tar.TypeReg, "bin")
	add("../evil", tar.TypeReg, "x")
	add("sub/nested", tar.TypeReg, "x")
	add("link", tar.TypeSymlink, "")
	add(".hidden", tar.TypeReg, "x")
	for _, c := range []io.Closer{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := extractTarGz(archive, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files["supabase"] {
		t.Fatalf("files = %v, want only supabase", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("path traversal entry was extracted")
	}
}
