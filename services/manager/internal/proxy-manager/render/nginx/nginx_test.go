package nginx

import (
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec/spectest"
)

func TestRenderGolden(t *testing.T) {
	cases := map[string]*spec.State{
		"full":    spectest.Full(spec.KindNginx),
		"minimal": spectest.Minimal(spec.KindNginx),
		"alpn":    spectest.WithALPN(spec.KindNginx),
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			spectest.Golden(t, name, Render(st))
		})
	}
}

func TestRenderDeterministic(t *testing.T) {
	first := spectest.Dump(Render(spectest.Full(spec.KindNginx)))
	for range 20 {
		if spectest.Dump(Render(spectest.Full(spec.KindNginx))) != first {
			t.Fatal("rendering the same state twice produced different output")
		}
	}
}

func TestRenderMarksKeysSecret(t *testing.T) {
	out := Render(spectest.Full(spec.KindNginx))
	if !out.Secret["certs/1.key"] {
		t.Fatal("private key is not marked secret")
	}
	if out.Secret["certs/1.crt"] {
		t.Fatal("certificate is marked secret")
	}
}
