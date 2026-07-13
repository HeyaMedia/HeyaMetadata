package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestLocalUserAndSessionRoundTrip(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres and Redis stack")
	}
	if err := config.LoadEnvFiles(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	if _, err := migrations.MigrateApp(ctx, database); err != nil {
		t.Fatal(err)
	}

	service := New(database, redisClient)
	username := "auth_test_" + time.Now().UTC().Format("20060102150405.000000000")
	username = strings.ReplaceAll(username, ".", "_")
	password := "integration-password"

	user, token, err := service.Register(ctx, username, password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = redisClient.Del(cleanupCtx, sessionKey(token)).Err()
		_, _ = database.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, user.ID)
	})
	if user.Role != "user" || user.Username != username {
		t.Fatalf("unexpected registered user: %+v", user)
	}
	var storedHash string
	if err := database.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, user.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == password || !strings.HasPrefix(storedHash, "$argon2id$") {
		t.Fatalf("password was not stored as Argon2id: %q", storedHash)
	}
	ttl, err := redisClient.TTL(ctx, sessionKey(token)).Result()
	if err != nil || ttl <= SessionTTL-time.Minute || ttl > SessionTTL {
		t.Fatalf("session TTL: got %s err=%v", ttl, err)
	}
	current, err := service.CurrentUser(ctx, token)
	if err != nil || current.ID != user.ID {
		t.Fatalf("resolve current user: user=%+v err=%v", current, err)
	}
	if _, _, err := service.Login(ctx, username, "wrong-password"); err != ErrInvalidCredential {
		t.Fatalf("wrong password: got %v, want %v", err, ErrInvalidCredential)
	}
	if _, _, err := service.Register(ctx, strings.ToUpper(username), password); err != ErrUsernameTaken {
		t.Fatalf("case-insensitive duplicate: got %v, want %v", err, ErrUsernameTaken)
	}

	createdKey, err := service.CreateAPIKey(ctx, user.ID, "  Integration Server  ")
	if err != nil {
		t.Fatal(err)
	}
	if createdKey.Name != "Integration Server" || !validAPIKey(createdKey.Key) || !strings.HasPrefix(createdKey.Key, createdKey.Prefix) {
		t.Fatalf("unexpected created API key: %+v", createdKey)
	}
	var storedDigest []byte
	if err := database.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id=$1`, createdKey.ID).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256([]byte(createdKey.Key))
	if !bytes.Equal(storedDigest, wantDigest[:]) {
		t.Fatal("stored API key digest does not match the generated key")
	}
	principal, err := service.AuthenticateAPIKey(ctx, createdKey.Key)
	if err != nil || principal.User.ID != user.ID || principal.KeyID != createdKey.ID {
		t.Fatalf("authenticate API key: principal=%+v err=%v", principal, err)
	}
	keys, err := service.ListAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != createdKey.ID || keys[0].LastUsedAt == nil {
		t.Fatalf("unexpected API key listing: %+v", keys)
	}
	if err := service.RevokeAPIKey(ctx, "00000000-0000-0000-0000-000000000000", createdKey.ID); err != ErrAPIKeyNotFound {
		t.Fatalf("cross-user API key revocation: got %v, want %v", err, ErrAPIKeyNotFound)
	}
	if _, err := service.AuthenticateAPIKey(ctx, createdKey.Key); err != nil {
		t.Fatalf("cross-user revocation affected the key: %v", err)
	}
	if err := service.RevokeAPIKey(ctx, user.ID, createdKey.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAPIKey(ctx, user.ID, createdKey.ID); err != nil {
		t.Fatalf("repeated API key revocation was not idempotent: %v", err)
	}
	if _, err := service.AuthenticateAPIKey(ctx, createdKey.Key); err != ErrInvalidAPIKey {
		t.Fatalf("revoked API key: got %v, want %v", err, ErrInvalidAPIKey)
	}
	keys, err = service.ListAPIKeys(ctx, user.ID)
	if err != nil || len(keys) != 0 {
		t.Fatalf("revoked key remained listable: keys=%+v err=%v", keys, err)
	}
	if err := service.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CurrentUser(ctx, token); err != ErrUnauthenticated {
		t.Fatalf("logged-out session: got %v, want %v", err, ErrUnauthenticated)
	}
}
