package traefik

import (
	"encoding/json"
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec/spectest"
)

func TestRenderGolden(t *testing.T) {
	cases := map[string]*spec.State{
		"full":    spectest.Full(spec.KindTraefik),
		"minimal": spectest.Minimal(spec.KindTraefik),
		"alpn":    spectest.WithALPN(spec.KindTraefik),
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			spectest.Golden(t, name, Render(st))
		})
	}
}

func TestRenderDeterministic(t *testing.T) {
	first := spectest.Dump(Render(spectest.Full(spec.KindTraefik)))
	for range 20 {
		if spectest.Dump(Render(spectest.Full(spec.KindTraefik))) != first {
			t.Fatal("rendering the same state twice produced different output")
		}
	}
}

func TestRenderValidJSON(t *testing.T) {
	out := Render(spectest.Full(spec.KindTraefik))
	for _, f := range []string{"traefik.yml", RoutingFile, "dynamic/tls.yml"} {
		var v map[string]any
		if err := json.Unmarshal([]byte(out.Files[f]), &v); err != nil {
			t.Errorf("%s is not valid JSON: %v", f, err)
		}
	}
}

func TestRenderALPNWarns(t *testing.T) {
	if len(Render(spectest.WithALPN(spec.KindTraefik)).Warnings) == 0 {
		t.Fatal("expected a warning for TLS-ALPN-01 on Traefik")
	}
}
