package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cc-web/backend/internal/store"
	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPasswordPolicy     = errors.New("password must contain 12 to 128 bytes")
	ErrSessionInvalid     = errors.New("session invalid")
	ErrCurrentPassword    = errors.New("current password invalid")
	ErrLoginInput         = errors.New("username or password exceeds allowed size")
)

const SessionLifetime = 12 * time.Hour

var (
	dummyHashOnce  sync.Once
	dummyHash      string
	loginHashSlots = make(chan struct{}, 2)
)

type Repository interface {
	HasAuthUser(context.Context) (bool, error)
	CreateAuthUser(context.Context, string, string) error
	AuthUserByUsername(context.Context, string) (store.AuthUser, error)
	AuthUserByID(context.Context, string) (store.AuthUser, error)
	CreateAuthSession(context.Context, string, string, string, time.Time) error
	AuthSession(context.Context, string, time.Time) (store.AuthSession, error)
	RevokeAuthSession(context.Context, string) error
	ChangeAuthPassword(context.Context, string, string) error
}

type Service struct {
	repo Repository
	now  func() time.Time
}

type Session struct {
	Token              string
	CSRFToken          string
	UserID             string
	Username           string
	MustChangePassword bool
	ExpiresAt          time.Time
}

func New(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

func ValidatePassword(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return ErrPasswordPolicy
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const memory = 64 * 1024
	const iterations = 3
	const parallelism = 1
	const keyLength = 32
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	var memory, iterations, parallelism uint32
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var saltText, keyText string
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || memory < 8*1024 || memory > 256*1024 || iterations == 0 || iterations > 10 || parallelism == 0 || parallelism > 8 {
		return false
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallelism) {
		return false
	}
	saltText, keyText = parts[4], parts[5]
	salt, err := base64.RawStdEncoding.DecodeString(saltText)
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(keyText)
	if err != nil || len(key) < 16 || len(key) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(key)))
	return subtle.ConstantTimeCompare(key, actual) == 1
}

func (s *Service) Bootstrap(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return errors.New("bootstrap username must contain 1 to 64 characters")
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hasUser, err := s.repo.HasAuthUser(ctx)
	if err != nil {
		return err
	}
	if hasUser {
		return errors.New("administrator already initialized")
	}
	return s.CreateInitialAdmin(ctx, username, password)
}

func (s *Service) CreateInitialAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return errors.New("bootstrap username must contain 1 to 64 characters")
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.repo.CreateAuthUser(ctx, username, hash); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return errors.New("administrator already initialized")
		}
		return err
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	if len(username) > 64 || len(password) > 128 {
		return Session{}, ErrLoginInput
	}
	select {
	case loginHashSlots <- struct{}{}:
		defer func() { <-loginHashSlots }()
	case <-ctx.Done():
		return Session{}, ctx.Err()
	}
	user, err := s.repo.AuthUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		dummyHashOnce.Do(func() { dummyHash, _ = HashPassword("cc-web-dummy-password-never-used") })
		_ = VerifyPassword(dummyHash, password)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if !VerifyPassword(user.PasswordHash, password) {
		return Session{}, ErrInvalidCredentials
	}
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	expires := s.now().Add(SessionLifetime)
	if err := s.repo.CreateAuthSession(ctx, user.ID, tokenHash(token), csrf, expires); err != nil {
		return Session{}, err
	}
	return Session{Token: token, CSRFToken: csrf, UserID: user.ID, Username: user.Username, MustChangePassword: user.MustChangePassword, ExpiresAt: expires}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (store.AuthSession, error) {
	if token == "" {
		return store.AuthSession{}, ErrSessionInvalid
	}
	session, err := s.repo.AuthSession(ctx, tokenHash(token), s.now())
	if errors.Is(err, store.ErrNotFound) {
		return store.AuthSession{}, ErrSessionInvalid
	}
	return session, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repo.RevokeAuthSession(ctx, tokenHash(token))
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	user, err := s.repo.AuthUserByID(ctx, userID)
	if err != nil || !VerifyPassword(user.PasswordHash, currentPassword) {
		return ErrCurrentPassword
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.repo.ChangeAuthPassword(ctx, userID, hash)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
