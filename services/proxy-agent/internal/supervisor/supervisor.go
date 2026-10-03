// Package supervisor runs the proxy process, restarts it when it exits unexpectedly and keeps
// its recent output for error detection after a reload.
package supervisor

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const recentLines = 200

type line struct {
	at   time.Time
	text string
}

// Process supervises one long-running command.
type Process struct {
	name string
	args []string
	stop syscall.Signal

	mu        sync.Mutex
	cmd       *exec.Cmd
	exited    chan struct{}
	wanted    bool
	startedAt time.Time
	restarts  uint32
	lastErr   string
	recent    []line
	changed   chan struct{}
}

// New returns a supervisor for name args; stop is the signal for a graceful shutdown.
func New(stop syscall.Signal, name string, args ...string) *Process {
	return &Process{name: name, args: args, stop: stop, changed: make(chan struct{}, 1)}
}

// Changed is signalled whenever the process starts or exits.
func (p *Process) Changed() <-chan struct{} { return p.changed }

func (p *Process) notify() {
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

// Start launches the process and keeps it running until Stop.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wanted = true
	if p.cmd != nil {
		return nil
	}
	return p.spawnLocked()
}

func (p *Process) spawnLocked() error {
	cmd := exec.Command(p.name, p.args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		p.lastErr = err.Error()
		return err
	}
	p.cmd, p.exited, p.startedAt = cmd, make(chan struct{}), time.Now()
	go p.copy(stdout, os.Stdout)
	go p.copy(stderr, os.Stderr)
	go p.wait(cmd, p.exited)
	p.notify()
	return nil
}

func (p *Process) copy(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		t := sc.Text()
		_, _ = io.WriteString(w, t+"\n")
		p.mu.Lock()
		p.recent = append(p.recent, line{time.Now(), t})
		if len(p.recent) > recentLines {
			p.recent = p.recent[len(p.recent)-recentLines:]
		}
		p.mu.Unlock()
	}
}

func (p *Process) wait(cmd *exec.Cmd, exited chan struct{}) {
	err := cmd.Wait()
	p.mu.Lock()
	close(exited)
	if p.cmd == cmd {
		p.cmd = nil
		p.startedAt = time.Time{}
		switch {
		case !p.wanted: // stopped on purpose
		case err != nil:
			p.lastErr = p.name + " exited: " + err.Error()
		default:
			p.lastErr = p.name + " exited"
		}
	}
	restart := p.wanted && p.cmd == nil
	p.mu.Unlock()
	p.notify()
	if !restart {
		return
	}
	// Crash loop protection: wait before starting again.
	time.Sleep(2 * time.Second)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.wanted && p.cmd == nil {
		p.restarts++
		_ = p.spawnLocked()
	}
}

// Stop ends the process gracefully, killing it after timeout.
func (p *Process) Stop(timeout time.Duration) {
	p.mu.Lock()
	p.wanted = false
	cmd, exited := p.cmd, p.exited
	p.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(p.stop)
	select {
	case <-exited:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-exited
	}
}

// Restart stops and starts the process.
func (p *Process) Restart(timeout time.Duration) error {
	p.Stop(timeout)
	return p.Start()
}

// Signal sends sig to the running process.
func (p *Process) Signal(sig syscall.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return false
	}
	return p.cmd.Process.Signal(sig) == nil
}

// State describes the process for status reports.
type State struct {
	Running   bool
	StartedAt time.Time
	Restarts  uint32
	LastError string
}

func (p *Process) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return State{Running: p.cmd != nil, StartedAt: p.startedAt, Restarts: p.restarts, LastError: p.lastErr}
}

// OutputSince returns the output lines written after t that contain any of the markers.
func (p *Process) OutputSince(t time.Time, markers ...string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, l := range p.recent {
		if l.at.Before(t) {
			continue
		}
		for _, m := range markers {
			if strings.Contains(l.text, m) {
				out = append(out, l.text)
				break
			}
		}
	}
	return out
}
