package proxy

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSafePath(t *testing.T) {
	root := "/etc/sm"
	for _, bad := range []string{"", ".", "..", "../x", "a/../../x", "/etc/passwd"} {
		if _, err := safePath(root, bad); err == nil {
			t.Errorf("safePath(%q) accepted a path outside the root", bad)
		}
	}
	for name, want := range map[string]string{"nginx.conf": "/etc/sm/nginx.conf", "certs/a.key": "/etc/sm/certs/a.key", "a/../b": "/etc/sm/b"} {
		got, err := safePath(root, name)
		if err != nil || got != want {
			t.Errorf("safePath(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestReplaceTree(t *testing.T) {
	dir := t.TempDir()
	first := withChecksum(map[string]File{
		"traefik.yml":        {Content: []byte("static")},
		"dynamic/old.yml":    {Content: []byte("old")},
		"certs/site.key":     {Content: []byte("key"), Mode: 0o644},
		".sm-checksum":       {Content: []byte("ignored")},
		"dynamic/routes.yml": {Content: []byte("v1")},
	}, "sum1")
	if err := replaceTree(dir, first); err != nil {
		t.Fatal(err)
	}
	if got := readChecksum(dir); got != "sum1" {
		t.Fatalf("checksum = %q, want sum1", got)
	}
	second := withChecksum(map[string]File{
		"traefik.yml":        {Content: []byte("static")},
		"certs/site.key":     {Content: []byte("key"), Mode: 0o600},
		"dynamic/routes.yml": {Content: []byte("v2")},
	}, "sum2")
	if err := replaceTree(dir, second); err != nil {
		t.Fatal(err)
	}
	tree, err := readTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range tree {
		names = append(names, n)
	}
	slices.Sort(names)
	if want := []string{".sm-checksum", "certs/site.key", "dynamic/routes.yml", "traefik.yml"}; !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	if string(tree["dynamic/routes.yml"].Content) != "v2" || readChecksum(dir) != "sum2" {
		t.Fatal("content was not replaced")
	}
	info, err := os.Stat(filepath.Join(dir, "certs/site.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600 (modes must follow the manager on rewrite)", info.Mode().Perm())
	}
}

func TestParseRawdataErrors(t *testing.T) {
	body := []byte(`{
		"routers": {
			"h1-r1-web@file": {"status": "disabled", "error": ["the service \"missing@file\" does not exist"]},
			"api@internal": {"error": ["ignored, not ours"]},
			"h2-r1-web@file": {"status": "enabled"}
		},
		"services": {"s1@file": {"error": ["bad url"]}},
		"middlewares": {}
	}`)
	got, err := parseRawdataErrors(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`router h1-r1-web@file: the service "missing@file" does not exist`, "service s1@file: bad url"}
	if !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
	if _, err := parseRawdataErrors([]byte("not json")); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}
