package auth

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestAPIKeysContain256RandomBitsAndOnlyExposeAPrefix(t *testing.T) {
	t.Parallel()

	first, firstPrefix, firstDigest, err := newAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	second, secondPrefix, secondDigest, err := newAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || firstPrefix == secondPrefix || bytes.Equal(firstDigest[:], secondDigest[:]) {
		t.Fatal("generated API keys were not unique")
	}
	if !validAPIKey(first) || !strings.HasPrefix(first, firstPrefix) || len(firstPrefix) != apiKeyPrefixLength {
		t.Fatalf("invalid API key format: key=%q prefix=%q", first, firstPrefix)
	}
	wantDigest := sha256.Sum256([]byte(first))
	if !bytes.Equal(firstDigest[:], wantDigest[:]) {
		t.Fatal("API key digest does not match its plaintext")
	}
	if validAPIKey(first+"tampered") || validAPIKey("not-a-heya-key") {
		t.Fatal("invalid API key accepted")
	}
}

func TestAPIKeyNameValidation(t *testing.T) {
	t.Parallel()

	name, err := normalizeAPIKeyName("  Living Room Server  ")
	if err != nil || name != "Living Room Server" {
		t.Fatalf("normalize name: name=%q err=%v", name, err)
	}
	for _, invalid := range []string{"", "   ", "line\nbreak", strings.Repeat("a", maximumAPIKeyName+1)} {
		if _, err := normalizeAPIKeyName(invalid); err == nil {
			t.Errorf("expected API key name %q to be rejected", invalid)
		}
	}
}
