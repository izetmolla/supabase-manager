package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/supabase-manager/manager/config"
	httpapi "github.com/supabase-manager/manager/internal/http"
	proxymanager "github.com/supabase-manager/manager/internal/proxy-manager"
)

func proxyCmd(args []string) error {
	if len(args) == 0 {
		return usageError("missing proxy subcommand")
	}
	switch args[0] {
	case "panel-host", "panel-domain":
		return proxyPanelHost(args[1:])
	default:
		return usageError(fmt.Sprintf("unknown proxy subcommand %q", args[0]))
	}
}

// proxyPanelHost asks the running server to create the panel's proxy host and deploy it; the
// deploy needs the proxy agents, which are connected to the server, not to this process.
func proxyPanelHost(args []string) error {
	fs := flag.NewFlagSet("proxy panel-host", flag.ContinueOnError)
	email := fs.String("email", "", "email for the Let's Encrypt account (HTTPS)")
	noTLS := fs.Bool("no-tls", false, "serve the panel over HTTP only")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError("proxy panel-host takes exactly one <domain|ip>")
	}
	base, err := localURL()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(proxymanager.PanelHostInput{Domain: pos[0], Email: *email, NoTLS: *noTLS})
	req, err := http.NewRequest(http.MethodPost, base+"/api/cli/proxy/panel-host", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpapi.CLITokenHeader, config.Load().CLIToken)
	res, err := (&http.Client{Timeout: 20 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("the server is not reachable at %s (is it running?): %w", base, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(res.Body)
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("server returned %s", res.Status)
	}
	sc := bufio.NewScanner(res.Body)
	last := ""
	for sc.Scan() {
		last = sc.Text()
		if url, ok := strings.CutPrefix(last, "OK "); ok {
			fmt.Printf("Panel is served at %s\n", url)
			return nil
		}
		if msg, ok := strings.CutPrefix(last, "ERROR "); ok {
			return errors.New(msg)
		}
		fmt.Println(last)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("the server closed the connection before finishing (last line: %q)", last)
}
