package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) execCreate(ctx context.Context, container string, cmd []string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(container)+"/exec", map[string]any{
		"Cmd": cmd, "AttachStdout": true, "AttachStderr": true,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// execRun starts an exec instance and passes every output frame to fn.
func (c *Client) execRun(ctx context.Context, id string, fn func(stream string, data []byte) error) error {
	resp, err := c.do(ctx, http.MethodPost, "/exec/"+id+"/start", map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		frame := make([]byte, size)
		if _, err := io.ReadFull(br, frame); err != nil {
			return nil
		}
		name := "stdout"
		if hdr[0] == 2 {
			name = "stderr"
		}
		if err := fn(name, frame); err != nil {
			return err
		}
	}
}

func (c *Client) execExitCode(ctx context.Context, id string) (int, error) {
	resp, err := c.do(ctx, http.MethodGet, "/exec/"+id+"/json", nil)
	if err != nil {
		return -1, err
	}
	defer func() { _ = resp.Body.Close() }()
	var st struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return -1, err
	}
	return st.ExitCode, nil
}

// Exec runs cmd in a running container and returns its combined output and exit code.
func (c *Client) Exec(ctx context.Context, container string, cmd ...string) (string, int, error) {
	id, err := c.execCreate(ctx, container, cmd)
	if err != nil {
		return "", -1, err
	}
	var out strings.Builder
	if err := c.execRun(ctx, id, func(_ string, data []byte) error {
		if out.Len() < 1<<20 {
			out.Write(data)
		}
		return nil
	}); err != nil {
		return out.String(), -1, err
	}
	code, err := c.execExitCode(ctx, id)
	return strings.TrimSpace(out.String()), code, err
}

// ExecOK is Exec that fails on a non-zero exit code, with the output as the error message.
func (c *Client) ExecOK(ctx context.Context, container string, cmd ...string) (string, error) {
	out, code, err := c.Exec(ctx, container, cmd...)
	if err != nil {
		return out, err
	}
	if code != 0 {
		if out == "" {
			out = fmt.Sprintf("exit status %d", code)
		}
		return out, fmt.Errorf("%s", out)
	}
	return out, nil
}

// ExecLines runs cmd and passes each output line to fn until the command ends, ctx is
// cancelled or fn returns an error. Used for "tail -F".
func (c *Client) ExecLines(ctx context.Context, container string, fn func(line string) error, cmd ...string) error {
	id, err := c.execCreate(ctx, container, cmd)
	if err != nil {
		return err
	}
	var partial string
	return c.execRun(ctx, id, func(_ string, data []byte) error {
		text := partial + string(data)
		lines := strings.Split(text, "\n")
		partial = lines[len(lines)-1]
		for _, l := range lines[:len(lines)-1] {
			if err := fn(l); err != nil {
				return err
			}
		}
		return nil
	})
}
