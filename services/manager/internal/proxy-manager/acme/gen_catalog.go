//go:build ignore

// gen_catalog builds dns_catalog.json from the provider descriptions shipped with lego.
//
//	cd services/manager/internal/proxy-manager/acme && go run gen_catalog.go
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type field struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

type provider struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	Credentials []field `json:"credentials"`
	Additional  []field `json:"additional"`
}

func fields(m map[string]string) []field {
	out := make([]field, 0, len(m))
	for k, v := range m {
		out = append(out, field{Key: k, Description: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func main() {
	dir, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/go-acme/lego/v4").Output()
	if err != nil {
		log.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(strings.TrimSpace(string(dir)), "providers", "dns", "*", "*.toml"))
	if err != nil {
		log.Fatal(err)
	}
	var out []provider
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		var doc struct {
			Name          string
			URL           string
			Code          string
			Configuration struct {
				Credentials map[string]string
				Additional  map[string]string
			}
		}
		if err := toml.Unmarshal(b, &doc); err != nil {
			log.Printf("skip %s: %v", f, err)
			continue
		}
		// manual needs a person at the terminal; exec runs a local program.
		if doc.Code == "" || doc.Code == "manual" || doc.Code == "exec" {
			continue
		}
		out = append(out, provider{
			Code: doc.Code, Name: doc.Name, URL: doc.URL,
			Credentials: fields(doc.Configuration.Credentials),
			Additional:  fields(doc.Configuration.Additional),
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	b, err := json.Marshal(out)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("dns_catalog.json", b, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d providers", len(out))
}
