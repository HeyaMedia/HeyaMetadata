package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	apiKeyMarker       = "heya_v2_"
	apiKeyRandomBytes  = 32
	apiKeyPrefixLength = len(apiKeyMarker) + 12
	maximumAPIKeyName  = 64
)

var (
	ErrInvalidAPIKeyName = errors.New("API key name must be between 1 and 64 characters and contain no control characters")
	ErrAPIKeyNotFound    = errors.New("API key not found")
	ErrInvalidAPIKey     = errors.New("invalid API key")
)

// APIKey is the safe, listable metadata for a user API key. It deliberately
// contains neither the plaintext key nor its stored digest.
type APIKey struct {
	ID         string     `json:"id" format:"uuid"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// CreatedAPIKey includes the plaintext secret exactly once, in the creation
// response. It cannot be recovered from Postgres afterward.
type CreatedAPIKey struct {
	ID         string     `json:"id" format:"uuid"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Key        string     `json:"key" readOnly:"true" doc:"Plaintext API key; shown only in this creation response"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type APIKeyPrincipal struct {
	User   User
	KeyID  string
	Scopes []string
}

func (service *Service) CreateAPIKey(ctx context.Context, userID, name string) (CreatedAPIKey, error) {
	name, err := normalizeAPIKeyName(name)
	if err != nil {
		return CreatedAPIKey{}, err
	}

	// A prefix or digest collision is extraordinarily unlikely. Retrying keeps
	// that invariant explicit instead of surfacing a database conflict.
	for range 3 {
		plaintext, prefix, digest, err := newAPIKey()
		if err != nil {
			return CreatedAPIKey{}, err
		}
		var created CreatedAPIKey
		err = service.database.QueryRow(ctx, `
			INSERT INTO api_keys (user_id, name, key_prefix, key_hash)
			VALUES ($1, $2, $3, $4)
			RETURNING id::text, name, key_prefix, scopes, created_at, last_used_at, expires_at`,
			userID, name, prefix, digest[:],
		).Scan(
			&created.ID,
			&created.Name,
			&created.Prefix,
			&created.Scopes,
			&created.CreatedAt,
			&created.LastUsedAt,
			&created.ExpiresAt,
		)
		if err == nil {
			created.Key = plaintext
			return created, nil
		}
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
			return CreatedAPIKey{}, fmt.Errorf("insert user API key: %w", err)
		}
	}
	return CreatedAPIKey{}, fmt.Errorf("generate a unique user API key")
}

func (service *Service) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := service.database.Query(ctx, `
		SELECT id::text, name, key_prefix, scopes, created_at, last_used_at, expires_at
		FROM api_keys
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user API keys: %w", err)
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.Scopes, &key.CreatedAt, &key.LastUsedAt, &key.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan user API key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user API keys: %w", err)
	}
	return keys, nil
}

// RevokeAPIKey retains the row for audit/history while making the secret
// unusable immediately. Repeating a revoke for the same owned key is safe.
func (service *Service) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	result, err := service.database.Exec(ctx, `
		UPDATE api_keys
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return fmt.Errorf("revoke user API key: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// AuthenticateAPIKey resolves a bearer secret to its user and scopes. A
// successful use updates last_used_at; revoked and expired keys fail closed.
func (service *Service) AuthenticateAPIKey(ctx context.Context, plaintext string) (APIKeyPrincipal, error) {
	if !validAPIKey(plaintext) {
		return APIKeyPrincipal{}, ErrInvalidAPIKey
	}
	digest := sha256.Sum256([]byte(plaintext))
	var principal APIKeyPrincipal
	err := service.database.QueryRow(ctx, `
		WITH matched AS (
			UPDATE api_keys
			SET last_used_at = now()
			WHERE key_hash = $1
			  AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > now())
			RETURNING id, user_id, scopes
		)
		SELECT users.id::text, users.username, users.role, users.created_at,
		       matched.id::text, matched.scopes
		FROM matched
		JOIN users ON users.id = matched.user_id`, digest[:],
	).Scan(
		&principal.User.ID,
		&principal.User.Username,
		&principal.User.Role,
		&principal.User.CreatedAt,
		&principal.KeyID,
		&principal.Scopes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKeyPrincipal{}, ErrInvalidAPIKey
	}
	if err != nil {
		return APIKeyPrincipal{}, fmt.Errorf("authenticate user API key: %w", err)
	}
	return principal, nil
}

func normalizeAPIKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > maximumAPIKeyName {
		return "", ErrInvalidAPIKeyName
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", ErrInvalidAPIKeyName
		}
	}
	return name, nil
}

func newAPIKey() (plaintext string, prefix string, digest [sha256.Size]byte, err error) {
	random := make([]byte, apiKeyRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", "", digest, fmt.Errorf("generate API key: %w", err)
	}
	plaintext = apiKeyMarker + base64.RawURLEncoding.EncodeToString(random)
	prefix = plaintext[:apiKeyPrefixLength]
	digest = sha256.Sum256([]byte(plaintext))
	return plaintext, prefix, digest, nil
}

func validAPIKey(plaintext string) bool {
	if !strings.HasPrefix(plaintext, apiKeyMarker) {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(plaintext, apiKeyMarker))
	return err == nil && len(decoded) == apiKeyRandomBytes && apiKeyMarker+base64.RawURLEncoding.EncodeToString(decoded) == plaintext
}
