package httpapi

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/models"
)

func (s *Server) listUsers(c fiber.Ctx) error {
	var users []models.User
	if err := s.db.Order("id").Find(&users).Error; err != nil {
		return err
	}
	return c.JSON(users)
}

type userInput struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func validRole(r string) bool { return r == models.RoleAdmin || r == models.RoleMember }

func (s *Server) createUser(c fiber.Ctx) error {
	var in userInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cr := credentials{Email: in.Email, Password: in.Password}
	if err := validateCredentials(&cr); err != nil {
		return err
	}
	if in.Role == "" {
		in.Role = models.RoleMember
	}
	if !validRole(in.Role) {
		return fiber.NewError(fiber.StatusBadRequest, "role must be admin or member")
	}
	hash, err := auth.HashPassword(cr.Password)
	if err != nil {
		return err
	}
	u := models.User{Email: cr.Email, Name: in.Name, PasswordHash: hash, Role: in.Role}
	if err := s.db.Create(&u).Error; err != nil {
		return fiber.NewError(fiber.StatusConflict, "a user with this email already exists")
	}
	s.audit(c, "user.create", u.Email, fiber.Map{"role": u.Role})
	return c.Status(fiber.StatusCreated).JSON(u)
}

func (s *Server) findUser(c fiber.Ctx) (*models.User, error) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid user id")
	}
	var u models.User
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
	}
	return &u, nil
}

func (s *Server) adminCount() int64 {
	var n int64
	s.db.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&n)
	return n
}

func (s *Server) updateUser(c fiber.Ctx) error {
	u, err := s.findUser(c)
	if err != nil {
		return err
	}
	var in userInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.Name != "" {
		u.Name = in.Name
	}
	if in.Role != "" {
		if !validRole(in.Role) {
			return fiber.NewError(fiber.StatusBadRequest, "role must be admin or member")
		}
		if u.Role == models.RoleAdmin && in.Role != models.RoleAdmin && s.adminCount() <= 1 {
			return fiber.NewError(fiber.StatusBadRequest, "cannot demote the last admin")
		}
		u.Role = in.Role
	}
	if in.Password != "" {
		if len(in.Password) < 8 {
			return fiber.NewError(fiber.StatusBadRequest, "password must be at least 8 characters")
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			return err
		}
		u.PasswordHash = hash
		s.auth.RevokeAllForUser(u.ID)
	}
	if err := s.db.Save(u).Error; err != nil {
		return err
	}
	s.audit(c, "user.update", u.Email, fiber.Map{"role": u.Role})
	return c.JSON(u)
}

func (s *Server) deleteUser(c fiber.Ctx) error {
	u, err := s.findUser(c)
	if err != nil {
		return err
	}
	if u.ID == currentClaims(c).UserID {
		return fiber.NewError(fiber.StatusBadRequest, "you cannot delete your own account")
	}
	if u.Role == models.RoleAdmin && s.adminCount() <= 1 {
		return fiber.NewError(fiber.StatusBadRequest, "cannot delete the last admin")
	}
	s.auth.RevokeAllForUser(u.ID)
	if err := s.db.Delete(u).Error; err != nil {
		return err
	}
	s.audit(c, "user.delete", u.Email, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) listAudit(c fiber.Ctx) error {
	var logs []models.AuditLog
	if err := s.db.Order("id desc").Limit(200).Find(&logs).Error; err != nil {
		return err
	}
	return c.JSON(logs)
}
