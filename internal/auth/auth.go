// Package auth implements local user identity and opaque browser sessions.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	SessionCookieName = "__Host-heya_session"
	SessionTTL        = 30 * 24 * time.Hour

	minimumPasswordBytes = 10
	maximumPasswordBytes = 128
	sessionTokenBytes    = 32
	sessionKeyPrefix     = "heya:metadata:v1:auth:session:"
)

var (
	ErrInvalidUsername   = errors.New("username must be 3-64 characters, start and end with a letter or number, and contain only letters, numbers, dots, hyphens, or underscores")
	ErrInvalidPassword   = fmt.Errorf("password must be between %d and %d bytes", minimumPasswordBytes, maximumPasswordBytes)
	ErrUsernameTaken     = errors.New("username is already taken")
	ErrInvalidCredential = errors.New("invalid username or password")
	ErrUnauthenticated   = errors.New("authentication required")

	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{1,62}[A-Za-z0-9]$`)
)

type User struct {
	ID        string    `json:"id" format:"uuid"`
	Username  string    `json:"username"`
	Role      string    `json:"role" enum:"user,moderator,trusted,admin"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	database *pgxpool.Pool
	sessions sessionStore
}

type sessionStore interface {
	Set(context.Context, string, string, time.Duration) error
	Get(context.Context, string) (string, error)
	Delete(context.Context, string) error
}

type redisSessionStore struct {
	client *redis.Client
}

func (store redisSessionStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return store.client.Set(ctx, key, value, ttl).Err()
}

func (store redisSessionStore) Get(ctx context.Context, key string) (string, error) {
	return store.client.Get(ctx, key).Result()
}

func (store redisSessionStore) Delete(ctx context.Context, key string) error {
	return store.client.Del(ctx, key).Err()
}

func New(database *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{database: database, sessions: redisSessionStore{client: redisClient}}
}

// Register durably creates a local user and issues a session. The Postgres
// transaction is not committed until Redis accepts the session, preventing a
// failed registration from leaving behind an account the caller did not see.
func (service *Service) Register(ctx context.Context, username, password string) (User, string, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return User{}, "", err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, "", err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return User{}, "", err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return User{}, "", fmt.Errorf("begin user registration: %w", err)
	}
	defer tx.Rollback(ctx)

	var user User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, password_hash)
		VALUES ($1, $2)
		RETURNING id::text, username, role, created_at`, normalized, passwordHash,
	).Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt)
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return User{}, "", ErrUsernameTaken
		}
		return User{}, "", fmt.Errorf("insert local user: %w", err)
	}

	token, err := service.createSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = service.sessions.Delete(context.WithoutCancel(ctx), sessionKey(token))
		return User{}, "", fmt.Errorf("commit user registration: %w", err)
	}
	return user, token, nil
}

func (service *Service) Login(ctx context.Context, username, password string) (User, string, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil || len(password) > maximumPasswordBytes {
		consumeDummyPassword(password)
		return User{}, "", ErrInvalidCredential
	}

	var user User
	var passwordHash string
	err = service.database.QueryRow(ctx, `
		SELECT id::text, username, role, created_at, password_hash
		FROM users
		WHERE username = $1`, normalized,
	).Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		consumeDummyPassword(password)
		return User{}, "", ErrInvalidCredential
	}
	if err != nil {
		return User{}, "", fmt.Errorf("look up local user: %w", err)
	}

	valid, err := verifyPassword(password, passwordHash)
	if err != nil {
		return User{}, "", fmt.Errorf("verify local user password: %w", err)
	}
	if !valid {
		return User{}, "", ErrInvalidCredential
	}
	token, err := service.createSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func (service *Service) CurrentUser(ctx context.Context, token string) (User, error) {
	if !validSessionToken(token) {
		return User{}, ErrUnauthenticated
	}
	userID, err := service.sessions.Get(ctx, sessionKey(token))
	if errors.Is(err, redis.Nil) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("read Redis auth session: %w", err)
	}

	var user User
	err = service.database.QueryRow(ctx, `
		SELECT id::text, username, role, created_at
		FROM users
		WHERE id = $1`, userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = service.sessions.Delete(context.WithoutCancel(ctx), sessionKey(token))
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("load session user: %w", err)
	}
	return user, nil
}

// Logout is idempotent for absent and malformed cookies.
func (service *Service) Logout(ctx context.Context, token string) error {
	if !validSessionToken(token) {
		return nil
	}
	if err := service.sessions.Delete(ctx, sessionKey(token)); err != nil {
		return fmt.Errorf("delete Redis auth session: %w", err)
	}
	return nil
}

func NormalizeUsername(username string) (string, error) {
	if !usernamePattern.MatchString(username) {
		return "", ErrInvalidUsername
	}
	return strings.ToLower(username), nil
}

func ValidatePassword(password string) error {
	if len(password) < minimumPasswordBytes || len(password) > maximumPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}

func (service *Service) createSession(ctx context.Context, userID string) (string, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", err
	}
	if err := service.sessions.Set(ctx, sessionKey(token), userID, SessionTTL); err != nil {
		return "", fmt.Errorf("store Redis auth session: %w", err)
	}
	return token, nil
}

func newSessionToken() (string, error) {
	random := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate session identifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func validSessionToken(token string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(decoded) == sessionTokenBytes && base64.RawURLEncoding.EncodeToString(decoded) == token
}

func sessionKey(token string) string {
	return sessionKeyPrefix + token
}
