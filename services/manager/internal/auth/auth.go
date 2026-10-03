package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/supabase-manager/manager/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 7 * 24 * time.Hour
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID uint   `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type Service struct {
	db     *gorm.DB
	secret []byte
}

func NewService(db *gorm.DB, secret []byte) *Service {
	return &Service{db: db, secret: secret}
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (s *Service) IssueAccess(u *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:    u.ID,
		Role:      u.Role,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
		Issuer:    "supabase-manager",
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *Service) ParseAccess(token string) (*Claims, error) {
	claims := &Claims{}
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("supabase-manager"))
	if err != nil || !t.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (s *Service) IssueRefresh(userID uint) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(buf)
	rt := models.RefreshToken{UserID: userID, TokenHash: hashToken(raw), ExpiresAt: time.Now().Add(RefreshTTL)}
	if err := s.db.Create(&rt).Error; err != nil {
		return "", err
	}
	return raw, nil
}

// Rotate consumes a refresh token and returns its user. The token cannot be reused.
func (s *Service) Rotate(raw string) (*models.User, error) {
	var rt models.RefreshToken
	if err := s.db.Where("token_hash = ?", hashToken(raw)).First(&rt).Error; err != nil {
		return nil, ErrInvalidToken
	}
	s.db.Delete(&rt)
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	var u models.User
	if err := s.db.First(&u, rt.UserID).Error; err != nil {
		return nil, ErrInvalidToken
	}
	return &u, nil
}

// Peek returns the user of a valid refresh token without consuming it.
func (s *Service) Peek(raw string) (*models.User, error) {
	var rt models.RefreshToken
	if err := s.db.Where("token_hash = ?", hashToken(raw)).First(&rt).Error; err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	var u models.User
	if err := s.db.First(&u, rt.UserID).Error; err != nil {
		return nil, ErrInvalidToken
	}
	return &u, nil
}

func (s *Service) Revoke(raw string) {
	s.db.Where("token_hash = ?", hashToken(raw)).Delete(&models.RefreshToken{})
}

func (s *Service) RevokeAllForUser(userID uint) {
	s.db.Where("user_id = ?", userID).Delete(&models.RefreshToken{})
}
