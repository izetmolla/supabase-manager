package supabase

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/supabase-manager/manager/internal/models"
	"gorm.io/gorm"
)

var ErrBusy = errors.New("another command is already running for this project")

const maxStoredOutput = 512 * 1024

// FinishFunc is called after a background job ends.
type FinishFunc func(job *models.Job)

// PrepareFunc runs inside a job before the CLI command; log writes to the job output.
type PrepareFunc func(log func(string)) error

// AfterFunc runs inside a job after the CLI command succeeded. run executes a further CLI command
// whose output goes to the same job.
type AfterFunc func(ctx context.Context, run func(args ...string) error, log func(string)) error

// Hooks are optional steps around a job's CLI command.
type Hooks struct {
	Prepare PrepareFunc
	After   AfterFunc
}

type Runner struct {
	db *gorm.DB

	mu      sync.Mutex
	bin     string
	busy    map[uint]uint // project id -> running job id
	streams map[uint]*stream
}

func NewRunner(bin string, db *gorm.DB) *Runner {
	return &Runner{bin: bin, db: db, busy: map[uint]uint{}, streams: map[uint]*stream{}}
}

func (r *Runner) Bin() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bin
}

// SetBin switches the CLI binary used by commands started afterwards.
func (r *Runner) SetBin(bin string) {
	r.mu.Lock()
	r.bin = bin
	r.mu.Unlock()
}

// Busy reports whether any project job is running.
func (r *Runner) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.busy) > 0
}

// RunningJob returns the id of the job currently running for a project, or 0.
func (r *Runner) RunningJob(projectID uint) uint {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy[projectID]
}

func (r *Runner) command(ctx context.Context, workdir string, env []string, args []string) *exec.Cmd {
	full := append(append([]string{}, args...), "--workdir", workdir, "--yes")
	cmd := exec.CommandContext(ctx, r.Bin(), full...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	cmd.Env = append(cmd.Env, env...)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// Run executes a short command synchronously and returns stdout. It does not take
// the project lock, so read-only commands (status, migration list) work while a
// long job is running.
func (r *Runner) Run(ctx context.Context, workdir string, env []string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(ctx, workdir, env, args)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), errors.New(msg)
	}
	return stdout.Bytes(), nil
}

// Start launches a command as a background job. Only one job runs per project.
func (r *Runner) Start(projectID, userID uint, workdir string, env []string, timeout time.Duration, onFinish FinishFunc, args ...string) (*models.Job, error) {
	return r.StartPrepared(projectID, userID, workdir, env, timeout, nil, onFinish, args...)
}

// StartPrepared is Start with a prepare step that runs inside the job before the command; when
// it fails, the job fails without running the command.
func (r *Runner) StartPrepared(projectID, userID uint, workdir string, env []string, timeout time.Duration, prepare PrepareFunc, onFinish FinishFunc, args ...string) (*models.Job, error) {
	return r.StartHooks(projectID, userID, workdir, env, timeout, Hooks{Prepare: prepare}, onFinish, args...)
}

// StartHooks is Start with steps before and after the command (see Hooks).
func (r *Runner) StartHooks(projectID, userID uint, workdir string, env []string, timeout time.Duration, hooks Hooks, onFinish FinishFunc, args ...string) (*models.Job, error) {
	r.mu.Lock()
	if r.busy[projectID] != 0 {
		r.mu.Unlock()
		return nil, ErrBusy
	}
	job := &models.Job{
		ProjectID: projectID,
		Command:   "supabase " + strings.Join(args, " "),
		Status:    models.JobRunning,
		StartedBy: userID,
	}
	if err := r.db.Create(job).Error; err != nil {
		r.mu.Unlock()
		return nil, err
	}
	st := newStream()
	r.busy[projectID] = job.ID
	r.streams[job.ID] = st
	r.mu.Unlock()

	go r.execute(job, st, workdir, env, timeout, hooks, onFinish, args)
	return job, nil
}

func (r *Runner) execute(job *models.Job, st *stream, workdir string, env []string, timeout time.Duration, hooks Hooks, onFinish FinishFunc, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var err error
	if hooks.Prepare != nil {
		err = hooks.Prepare(st.write)
	}
	if err == nil {
		err = r.run(ctx, st, job.Command, workdir, env, args)
	}
	if err == nil && hooks.After != nil {
		err = hooks.After(ctx, func(more ...string) error {
			return r.run(ctx, st, "supabase "+strings.Join(more, " "), workdir, env, more)
		}, st.write)
	}

	exit := 0
	status := models.JobSucceeded
	if err != nil {
		status = models.JobFailed
		exit = -1
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			exit = ee.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			st.write("error: command timed out after " + timeout.String())
		} else {
			st.write("error: " + err.Error())
		}
	}
	now := time.Now()
	job.Status = status
	job.ExitCode = exit
	job.FinishedAt = &now
	job.Output = st.text(maxStoredOutput)
	r.db.Model(job).Updates(map[string]any{"status": status, "exit_code": exit, "finished_at": now, "output": job.Output})

	st.close(status)
	r.mu.Lock()
	delete(r.busy, job.ProjectID)
	r.mu.Unlock()

	if onFinish != nil {
		onFinish(job)
	}

	// Keep the in-memory stream briefly so late subscribers still get the tail.
	time.AfterFunc(2*time.Minute, func() {
		r.mu.Lock()
		delete(r.streams, job.ID)
		r.mu.Unlock()
	})
}

func (r *Runner) run(ctx context.Context, st *stream, command, workdir string, env []string, args []string) error {
	cmd := r.command(ctx, workdir, env, args)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	st.write("$ " + command)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(scanLinesOrCR)
		for sc.Scan() {
			if line := strings.TrimRight(sc.Text(), " "); line != "" {
				st.write(line)
			}
		}
		_, _ = io.Copy(io.Discard, pr)
	}()

	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
	}
	_ = pw.Close()
	<-readDone
	return err
}

// Subscribe returns the output so far and a channel of new lines for a live job.
// ok is false when the job is not in memory, in which case callers should read
// the stored output from the database.
func (r *Runner) Subscribe(jobID uint) (history []string, lines <-chan string, done <-chan string, cancel func(), ok bool) {
	r.mu.Lock()
	st := r.streams[jobID]
	r.mu.Unlock()
	if st == nil {
		return nil, nil, nil, func() {}, false
	}
	h, ch, d, c := st.subscribe()
	return h, ch, d, c, true
}

func scanLinesOrCR(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
