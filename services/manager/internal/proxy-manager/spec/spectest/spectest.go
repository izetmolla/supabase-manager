// Package spectest provides proxy states that exercise every renderer feature, for golden tests.
package spectest

import "github.com/supabase-manager/manager/internal/proxy-manager/spec"

// Full returns a state using every host kind, route option, security feature and stream type.
func Full(kind string) *spec.State {
	return &spec.State{
		Instance: spec.Instance{
			ID:             1,
			Name:           "edge",
			Kind:           kind,
			BindIP:         "10.0.0.5",
			HTTPPort:       80,
			HTTPSPort:      443,
			AdminPort:      8090,
			ManagerURL:     "http://127.0.0.1:8080",
			ALPNSolverAddr: "127.0.0.1:5443",
		},
		Upstreams: map[uint]*spec.Upstream{
			1: {
				ID: 1, Name: "app", Algorithm: spec.AlgoLeastConn, Scheme: "http", Sticky: true,
				Targets: []spec.Target{
					{Host: "10.0.1.1", Port: 3000, Weight: 3},
					{Host: "10.0.1.2", Port: 3000, Weight: 1},
					{Host: "10.0.1.3", Port: 3000, Weight: 1, Backup: true},
				},
				Health: spec.HealthCheck{Enabled: true, Path: "/healthz", Interval: 10, Timeout: 2, ExpectStatus: 200},
			},
			2: {
				ID: 2, Name: "api", Algorithm: spec.AlgoRoundRobin, Scheme: "https", TLSSkipVerify: true,
				Targets: []spec.Target{{Host: "api.internal", Port: 8443, Weight: 1}},
			},
			3: {
				ID: 3, Name: "postgres", Algorithm: spec.AlgoIPHash, Scheme: "http",
				Targets: []spec.Target{{Host: "10.0.2.1", Port: 5432, Weight: 1}},
			},
		},
		AccessLists: map[uint]*spec.AccessList{
			1: {
				ID:      1,
				Users:   []spec.AccessUser{{Username: "admin", Hash: "$apr1$abcdefgh$FBwExRW4dCc8aL.OvjpIE1"}},
				Allow:   []string{"10.0.0.0/8"},
				Deny:    []string{"10.9.0.0/16"},
				Satisfy: "any",
			},
		},
		Certs: map[uint]*spec.Cert{
			1: {ID: 1, Domains: []string{"example.com", "*.example.com"}, CertPEM: "CERT\n", KeyPEM: "KEY\n"},
		},
		Hosts: []spec.Host{
			{
				ID: 1, Kind: spec.HostProxy, Domains: []string{"example.com", "www.example.com"}, Upstream: 1,
				CertID: 1, ForceHTTPS: true, HSTS: true, HSTSSubdomains: true, HTTP2: true, WebSocket: true,
				ConnectTimeout: 5, ReadTimeout: 120, MaxBodyMB: 50, AccessList: 1,
				IPRules:         []spec.IPRule{{Action: "deny", CIDR: "192.0.2.0/24"}, {Action: "allow", CIDR: "0.0.0.0/0"}},
				RateLimit:       spec.RateLimit{Enabled: true, RPS: 20, Burst: 40},
				RequestHeaders:  []spec.Header{{Name: "X-Env", Value: "prod"}},
				ResponseHeaders: []spec.Header{{Name: "X-Frame-Options", Value: "DENY"}, {Name: "Server", Value: ""}},
				CORS: spec.CORS{
					Enabled: true, Origins: []string{"https://app.example.com"}, Methods: "GET,POST",
					Headers: "Authorization,Content-Type", Credentials: true, MaxAge: 600,
				},
				Routes: []spec.Route{
					{
						ID: 1, PathType: spec.PathPrefix, Path: "/api", Upstream: 2, StripPrefix: true,
						Headers:        []spec.HeaderMatch{{Name: "X-Version", Value: "2"}},
						RequestHeaders: []spec.Header{{Name: "X-Route", Value: "api"}},
					},
					{ID: 2, PathType: spec.PathExact, Path: "/status", Upstream: 1, AccessList: 0},
					{
						ID: 3, PathType: spec.PathRegex, Path: "^/v[0-9]+/", Upstream: 2,
						RewriteRegex: "^/v[0-9]+/(.*)", RewriteReplacement: "/$1",
						Headers: []spec.HeaderMatch{{Name: "User-Agent", Value: "~^curl"}},
					},
				},
				RawNginx:   "add_header X-Raw nginx;",
				RawTraefik: `{"http":{"middlewares":{"raw-mw":{"headers":{"customResponseHeaders":{"X-Raw":"traefik"}}}}}}`,
			},
			{
				ID: 2, Kind: spec.HostRedirect, Domains: []string{"old.example.com"},
				RedirectURL: "https://example.com", RedirectCode: 301, PreservePath: true,
			},
			{ID: 3, Kind: spec.HostError, Domains: []string{"gone.example.com"}, ErrorCode: 410, ErrorBody: "Gone"},
			{ID: 4, Kind: spec.HostProxy, Domains: []string{"plain.example.org"}, Upstream: 2, MaxBodyMB: -1},
		},
		Streams: []spec.Stream{
			{ID: 1, Protocol: "tcp", ListenPort: 5433, Upstream: 3},
			{ID: 2, Protocol: "udp", ListenPort: 5353, Upstream: 2},
		},
	}
}

// Minimal returns an instance with nothing configured.
func Minimal(kind string) *spec.State {
	return &spec.State{
		Instance: spec.Instance{
			ID: 2, Name: "empty", Kind: kind, HTTPPort: 8081, AdminPort: 8091,
			ManagerURL: "http://127.0.0.1:8080",
		},
		Upstreams:   map[uint]*spec.Upstream{},
		AccessLists: map[uint]*spec.AccessList{},
		Certs:       map[uint]*spec.Cert{},
	}
}

// WithALPN returns Full with TLS-ALPN-01 enabled.
func WithALPN(kind string) *spec.State {
	st := Full(kind)
	st.Instance.TLSALPN = true
	return st
}
