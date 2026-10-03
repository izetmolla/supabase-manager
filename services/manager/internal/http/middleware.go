package httpapi

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/models"
)

const (
	accessCookie  = "sm_session"
	refreshCookie = "sm_renew"
	studioCookie  = "sm_studio"
	localsClaims  = "claims"
)

func (s *Server) requireAuth(c fiber.Ctx) error {
	token := c.Cookies(accessCookie)
	if token == "" {
		if h := c.Get(fiber.HeaderAuthorization); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
	}
	claims, err := s.auth.ParseAccess(token)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "session expired")
	}
	c.Locals(localsClaims, claims)
	return c.Next()
}

func (s *Server) requireAdmin(c fiber.Ctx) error {
	if currentClaims(c).Role != models.RoleAdmin {
		return fiber.NewError(fiber.StatusForbidden, "admin role required")
	}
	return c.Next()
}

func currentClaims(c fiber.Ctx) *auth.Claims {
	if cl, ok := c.Locals(localsClaims).(*auth.Claims); ok {
		return cl
	}
	return &auth.Claims{}
}

func (s *Server) audit(c fiber.Ctx, action, target string, meta any) {
	var m string
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			m = string(b)
		}
	}
	s.db.Create(&models.AuditLog{ActorID: currentClaims(c).UserID, Action: action, Target: target, Metadata: m})
}

func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := err.Error()
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		msg = e.Message
	}
	return c.Status(code).JSON(fiber.Map{"error": msg})
}

func bindJSON(c fiber.Ctx, out any) error {
	if err := c.Bind().JSON(out); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	return nil
}
