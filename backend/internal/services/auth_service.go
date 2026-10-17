package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const SessionTTL = 30 * 24 * time.Hour

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrPasswordTooLong    = errors.New("password must be at most 72 bytes")
	ErrUnauthenticated    = errors.New("authentication required")
)

// dummyHash keeps login timing similar whether or not the email exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("lifeos-timing-equalizer"), bcrypt.DefaultCost)

type Session struct {
	Token     string
	ExpiresAt time.Time
}

type AuthService struct {
	Users    repository.UserRepository
	Sessions repository.SessionRepository
	// OnRegister runs after an account is created, so modules can seed
	// per-user defaults (e.g. task lists) without auth depending on them.
	OnRegister []func(ctx context.Context, user models.User) error
}

func (s AuthService) Register(ctx context.Context, name, email, password string) (models.User, error) {
	if len(password) > 72 {
		return models.User{}, ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return models.User{}, err
	}
	user, err := s.Users.Create(ctx, strings.TrimSpace(name), normalizeEmail(email), string(hash))
	if errors.Is(err, repository.ErrDuplicate) {
		return models.User{}, ErrEmailTaken
	}
	if err != nil {
		return models.User{}, err
	}
	for _, hook := range s.OnRegister {
		if err := hook(ctx, user); err != nil {
			return user, err
		}
	}
	return user, nil
}

func (s AuthService) Login(ctx context.Context, email, password, userAgent, ip string) (models.User, Session, error) {
	user, err := s.Users.FindByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, repository.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return models.User{}, Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return models.User{}, Session{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return models.User{}, Session{}, ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return models.User{}, Session{}, err
	}
	session := Session{Token: token, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := s.Sessions.Create(ctx, user.ID, hashToken(token), truncate(userAgent, 255), ip, session.ExpiresAt); err != nil {
		return models.User{}, Session{}, err
	}
	return user, session, nil
}

func (s AuthService) Authenticate(ctx context.Context, token string) (models.User, error) {
	user, err := s.Sessions.FindUserByTokenHash(ctx, hashToken(token))
	if errors.Is(err, repository.ErrNotFound) {
		return models.User{}, ErrUnauthenticated
	}
	return user, err
}

func (s AuthService) Logout(ctx context.Context, token string) error {
	return s.Sessions.DeleteByTokenHash(ctx, hashToken(token))
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Only the SHA-256 of a session token is stored, so a database leak does not
// expose usable tokens.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
