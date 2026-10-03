package httpapi

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/models"
	proxymanager "github.com/supabase-manager/manager/internal/proxy-manager"
)

// CLITokenHeader carries config.CLIToken on requests from the supabase-manager CLI.
const CLITokenHeader = "X-CLI-Token"

// requireCLI admits loopback requests carrying the CLI token, i.e. commands run inside the
// manager's container with its secrets (docker exec).
func (s *Server) requireCLI(c fiber.Ctx) error {
	ip := c.RequestCtx().RemoteIP()
	token := c.Get(CLITokenHeader)
	if !ip.IsLoopback() || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.CLIToken)) != 1 {
		return fiber.NewError(fiber.StatusForbidden, "CLI access only")
	}
	return c.Next()
}

// cliPanelHost publishes the manager through the proxies and streams the deploy as plain text.
// The last line is "OK <url>" or "ERROR <message>".
func (s *Server) cliPanelHost(c fiber.Ctx) error {
	var in proxymanager.PanelHostInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	res, err := s.pm.EnsurePanelHost(in)
	if err != nil {
		return pmError(err)
	}
	if res.Created {
		meta, _ := json.Marshal(fiber.Map{"domains": res.Host.Domains, "source": "cli"})
		s.db.Create(&models.AuditLog{Action: "proxy.host.save", Target: fmt.Sprint(res.Host.ID), Metadata: string(meta)})
	}
	c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		logf := func(line string) {
			for l := range strings.SplitSeq(strings.TrimRight(line, "\n"), "\n") {
				_, _ = w.WriteString(l + "\n")
			}
			_ = w.Flush()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := s.pm.ApplyPanelHost(ctx, res, logf); err != nil {
			logf("ERROR " + err.Error())
			return
		}
		logf("OK " + res.URL)
	})
}
