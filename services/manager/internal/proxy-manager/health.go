package proxymanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

// TargetHealth is the last check result of one upstream target. The manager checks targets
// itself because open-source nginx has no active health checks.
type TargetHealth struct {
	Address   string    `json:"address"`
	State     string    `json:"state"` // up | down
	Error     string    `json:"error,omitempty"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
}

type healthMonitor struct {
	s       *Service
	mu      sync.RWMutex
	results map[uint][]TargetHealth
	last    map[uint]time.Time
}

func newHealthMonitor(s *Service) *healthMonitor {
	return &healthMonitor{s: s, results: map[uint][]TargetHealth{}, last: map[uint]time.Time{}}
}

func (m *healthMonitor) snapshot() map[uint][]TargetHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[uint][]TargetHealth, len(m.results))
	for k, v := range m.results {
		out[k] = append([]TargetHealth(nil), v...)
	}
	return out
}

func (m *healthMonitor) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results, m.last = map[uint][]TargetHealth{}, map[uint]time.Time{}
}

// Health returns the latest results per upstream id.
func (s *Service) Health() map[uint][]TargetHealth { return s.health.snapshot() }

func (m *healthMonitor) run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

func (m *healthMonitor) tick(ctx context.Context) {
	var ups []ProxyUpstream
	if err := m.s.db.Find(&ups).Error; err != nil {
		return
	}
	seen := map[uint]bool{}
	r := &resolver{s: m.s}
	for i := range ups {
		u := &ups[i]
		if !u.Health.Enabled {
			continue
		}
		seen[u.ID] = true
		interval := time.Duration(max(u.Health.Interval, 5)) * time.Second
		if time.Since(m.last[u.ID]) < interval {
			continue
		}
		m.last[u.ID] = time.Now()
		su, _ := r.upstream(u)
		results := make([]TargetHealth, len(su.Targets))
		var wg sync.WaitGroup
		for j, t := range su.Targets {
			wg.Go(func() { results[j] = check(ctx, su, u.Health, t) })
		}
		wg.Wait()
		m.mu.Lock()
		m.results[u.ID] = results
		m.mu.Unlock()
	}
	m.mu.Lock()
	for id := range m.results {
		if !seen[id] {
			delete(m.results, id)
			delete(m.last, id)
		}
	}
	m.mu.Unlock()
}

// check probes a target with an HTTP request when the health check has a path and with a TCP
// connect otherwise.
func check(ctx context.Context, u *spec.Upstream, hc spec.HealthCheck, t spec.Target) TargetHealth {
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	res := TargetHealth{Address: addr, CheckedAt: time.Now()}
	timeout := time.Duration(max(hc.Timeout, 1)) * time.Second
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	var err error
	if hc.Path == "" {
		var conn net.Conn
		conn, err = (&net.Dialer{}).DialContext(c, "tcp", addr)
		if err == nil {
			_ = conn.Close()
		}
	} else {
		err = httpCheck(c, u, hc, addr)
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.State, res.Error = "down", err.Error()
	} else {
		res.State = "up"
	}
	return res
}

func httpCheck(ctx context.Context, u *spec.Upstream, hc spec.HealthCheck, addr string) error {
	scheme := u.Scheme
	if scheme != "https" {
		scheme = "http"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+addr+hc.Path, nil)
	if err != nil {
		return err
	}
	cl := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: u.TLSSkipVerify}}, //nolint:gosec // per-upstream setting
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if hc.ExpectStatus != 0 {
		if resp.StatusCode != hc.ExpectStatus {
			return fmt.Errorf("status %d, expected %d", resp.StatusCode, hc.ExpectStatus)
		}
	} else if resp.StatusCode >= 500 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
