package spectest

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

// Dump flattens an output into one text document with files in sorted order.
func Dump(out *spec.Output) string {
	var b strings.Builder
	paths := make([]string, 0, len(out.Files))
	for p := range out.Files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		b.WriteString("==> " + p + " <==\n")
		b.WriteString(out.Files[p])
		if !strings.HasSuffix(out.Files[p], "\n") {
			b.WriteString("\n")
		}
	}
	if len(out.Warnings) > 0 {
		b.WriteString("==> warnings <==\n")
		for _, w := range out.Warnings {
			b.WriteString(w + "\n")
		}
	}
	return b.String()
}

// Golden compares the rendered output with testdata/<name>.golden; run tests with -update to
// accept changes.
func Golden(t *testing.T, name string, out *spec.Output) {
	t.Helper()
	got := Dump(out)
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file; run go test with -update and review the diff\n%s", path, firstDiff(string(want), got))
	}
}

func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "line " + strconv.Itoa(i+1) + ":\n  want: " + wl + "\n  got:  " + gl
		}
	}
	return ""
}
