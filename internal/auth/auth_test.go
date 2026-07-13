package auth

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashesUseSaltedArgon2id(t *testing.T) {
	password := "correct horse battery staple"
	first, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reused a salt")
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$") || strings.Contains(first, password) {
		t.Fatalf("unexpected password hash encoding: %q", first)
	}
	valid, err := verifyPassword(password, first)
	if err != nil || !valid {
		t.Fatalf("correct password did not verify: valid=%t err=%v", valid, err)
	}
	valid, err = verifyPassword("incorrect password", first)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("incorrect password verified")
	}
}

func TestUsernameNormalizationAndValidation(t *testing.T) {
	t.Parallel()

	normalized, err := NormalizeUsername("Alice.Example")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "alice.example" {
		t.Fatalf("normalized username: got %q", normalized)
	}
	for _, invalid := range []string{"ab", " leading", "trailing ", "-alice", "alice-", "ålice", strings.Repeat("a", 65)} {
		if _, err := NormalizeUsername(invalid); err == nil {
			t.Errorf("expected username %q to be rejected", invalid)
		}
	}
}

func TestPasswordLengthValidation(t *testing.T) {
	t.Parallel()

	if err := ValidatePassword(strings.Repeat("a", minimumPasswordBytes)); err != nil {
		t.Fatalf("minimum password rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", minimumPasswordBytes-1)); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", maximumPasswordBytes+1)); err == nil {
		t.Fatal("oversized password accepted")
	}
}

func TestSessionTokensContain256RandomBits(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 100 {
		token, err := newSessionToken()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || len(decoded) != sessionTokenBytes {
			t.Fatalf("session token is not %d bytes: len=%d err=%v", sessionTokenBytes, len(decoded), err)
		}
		if seen[token] {
			t.Fatal("duplicate session token")
		}
		seen[token] = true
	}
}

func TestSessionCreationUsesNamespacedKeyAndTTL(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{}
	service := &Service{sessions: store}
	token, err := service.createSession(context.Background(), "6f4fa2e7-974b-4e8c-aeff-348ac48a036c")
	if err != nil {
		t.Fatal(err)
	}
	if store.key != sessionKeyPrefix+token || store.value != "6f4fa2e7-974b-4e8c-aeff-348ac48a036c" {
		t.Fatalf("unexpected session mapping: key=%q value=%q", store.key, store.value)
	}
	if store.ttl != SessionTTL {
		t.Fatalf("session TTL: got %s, want %s", store.ttl, SessionTTL)
	}
}

type recordingSessionStore struct {
	key   string
	value string
	ttl   time.Duration
}

func (store *recordingSessionStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	store.key, store.value, store.ttl = key, value, ttl
	return nil
}

func (*recordingSessionStore) Get(context.Context, string) (string, error) { return "", nil }
func (*recordingSessionStore) Delete(context.Context, string) error        { return nil }
