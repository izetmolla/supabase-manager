package supervisor

import (
	"syscall"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProcessLifecycle(t *testing.T) {
	p := New(syscall.SIGTERM, "sh", "-c", `echo "[emerg] boom"; trap 'exit 0' HUP TERM; while :; do sleep 0.05; done`)
	since := time.Now()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop(5 * time.Second)
	waitFor(t, "running", func() bool { return p.State().Running })
	waitFor(t, "output", func() bool { return len(p.OutputSince(since, "[emerg]")) == 1 })
	if got := p.OutputSince(time.Now().Add(time.Hour), "[emerg]"); len(got) != 0 {
		t.Fatalf("OutputSince ignored the time filter: %q", got)
	}

	p.Stop(5 * time.Second)
	if st := p.State(); st.Running || st.LastError != "" {
		t.Fatalf("after Stop: %+v (a deliberate stop is not an error)", st)
	}
	if err := p.Restart(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running after restart", func() bool { return p.State().Running })
}

func TestProcessRestartsAfterCrash(t *testing.T) {
	p := New(syscall.SIGTERM, "sh", "-c", `sleep 0.1; exit 3`)
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop(5 * time.Second)
	waitFor(t, "a restart", func() bool { return p.State().Restarts >= 1 })
	if st := p.State(); st.LastError == "" {
		t.Fatalf("a crash left no error: %+v", st)
	}
}
