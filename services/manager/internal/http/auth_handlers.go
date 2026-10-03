package httpapi

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/models"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// Session cookies are scoped to "/" because proxied services (Studio, Mailpit) live outside
// /api. They are stripped from every request before it is forwarded upstream.
func (s *Server) setSession(c fiber.Ctx, u *models.User) error {
	access, err := s.auth.IssueAccess(u)
	if err != nil {
		return err
	}
	refresh, err := s.auth.IssueRefresh(u.ID)
	if err != nil {
		return err
	}
	s.setAccessCookie(c, access)
	c.Cookie(&fiber.Cookie{
		Name: refreshCookie, Value: refresh, Path: "/", HTTPOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: "Strict", Expires: time.Now().Add(auth.RefreshTTL),
	})
	return nil
}

func (s *Server) setAccessCookie(c fiber.Ctx, access string) {
	c.Cookie(&fiber.Cookie{
		Name: accessCookie, Value: access, Path: "/", HTTPOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: "Lax", Expires: time.Now().Add(auth.AccessTTL),
	})
}

func expireCookie(c fiber.Ctx, name, path string) {
	c.Cookie(&fiber.Cookie{Name: name, Value: "", Path: path, HTTPOnly: true, MaxAge: -1, Expires: time.Unix(0, 0)})
}

func (s *Server) clearSession(c fiber.Ctx) {
	expireCookie(c, accessCookie, "/")
	expireCookie(c, refreshCookie, "/")
	expireCookie(c, studioCookie, "/")
}

func (s *Server) setupStatus(c fiber.Ctx) error {
	var n int64
	s.db.Model(&models.User{}).Count(&n)
	return c.JSON(fiber.Map{"needs_setup": n == 0})
}

func validateCredentials(cr *credentials) error {
	cr.Email = strings.ToLower(strings.TrimSpace(cr.Email))
	if !strings.Contains(cr.Email, "@") || len(cr.Email) > 255 {
		return fiber.NewError(fiber.StatusBadRequest, "a valid email is required")
	}
	if len(cr.Password) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "password must be at least 8 characters")
	}
	return nil
}

func (s *Server) setup(c fiber.Ctx) error {
	var cr credentials
	if err := bindJSON(c, &cr); err != nil {
		return err
	}
	if err := validateCredentials(&cr); err != nil {
		return err
	}
	hash, err := auth.HashPassword(cr.Password)
	if err != nil {
		return err
	}
	u := models.User{Email: cr.Email, Name: cr.Name, PasswordHash: hash, Role: models.RoleAdmin}

	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var n int64
	s.db.Model(&models.User{}).Count(&n)
	if n > 0 {
		return fiber.NewError(fiber.StatusConflict, "setup already completed")
	}
	if err := s.db.Create(&u).Error; err != nil {
		return err
	}
	if err := s.setSession(c, &u); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(u)
}

func (s *Server) login(c fiber.Ctx) error {
	var cr credentials
	if err := bindJSON(c, &cr); err != nil {
		return err
	}
	cr.Email = strings.ToLower(strings.TrimSpace(cr.Email))
	var u models.User
	if err := s.db.Where("email = ?", cr.Email).First(&u).Error; err != nil || !auth.CheckPassword(u.PasswordHash, cr.Password) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid email or password")
	}
	if err := s.setSession(c, &u); err != nil {
		return err
	}
	return c.JSON(u)
}

func (s *Server) refresh(c fiber.Ctx) error {
	raw := c.Cookies(refreshCookie)
	if raw == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "no refresh token")
	}
	u, err := s.auth.Rotate(raw)
	if err != nil {
		s.clearSession(c)
		return fiber.NewError(fiber.StatusUnauthorized, "session expired")
	}
	if err := s.setSession(c, u); err != nil {
		return err
	}
	return c.JSON(u)
}

func (s *Server) logout(c fiber.Ctx) error {
	if raw := c.Cookies(refreshCookie); raw != "" {
		s.auth.Revoke(raw)
	}
	s.clearSession(c)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) me(c fiber.Ctx) error {
	var u models.User
	if err := s.db.First(&u, currentClaims(c).UserID).Error; err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "user not found")
	}
	return c.JSON(u)
}
