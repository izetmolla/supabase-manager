package httpapi

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/configtoml"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/projects"
)

type smtpTestInput struct {
	configtoml.SMTP
	To string `json:"to"`
}

func (s *Server) getSMTPDefaults(c fiber.Ctx) error {
	return c.JSON(s.projects.SMTPDefaults())
}

func (s *Server) putSMTPDefaults(c fiber.Ctx) error {
	var in configtoml.SMTP
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	out, err := s.projects.SetSMTPDefaults(in)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "smtp.defaults", "system", fiber.Map{"enabled": out.Enabled, "host": out.Host})
	return c.JSON(out)
}

func (s *Server) testSMTPDefaults(c fiber.Ctx) error {
	return s.sendTestEmail(c, nil)
}

func (s *Server) getProjectSMTP(c fiber.Ctx) error {
	out, err := s.projects.ProjectSMTP(project(c))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(out)
}

// putProjectSMTP saves the project's SMTP server; ?defaults=1 copies the system default.
func (s *Server) putProjectSMTP(c fiber.Ctx) error {
	var in configtoml.SMTP
	useDefaults := c.Query("defaults") == "1"
	if !useDefaults {
		if err := bindJSON(c, &in); err != nil {
			return err
		}
	}
	p := project(c)
	out, err := s.projects.SetProjectSMTP(p, in, useDefaults)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "config.smtp", p.Slug, fiber.Map{"enabled": out.Enabled, "host": out.Host, "defaults": useDefaults})
	return c.JSON(out)
}

func (s *Server) testProjectSMTP(c fiber.Ctx) error {
	return s.sendTestEmail(c, project(c))
}

func (s *Server) sendTestEmail(c fiber.Ctx, p *models.Project) error {
	var in smtpTestInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	if err := projects.SendTestEmail(ctx, in.SMTP, s.projects.SMTPPassFor(p, in.SMTP), in.To); err != nil {
		if _, ok := err.(*projects.ValidationError); ok {
			return mapErr(err)
		}
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}
