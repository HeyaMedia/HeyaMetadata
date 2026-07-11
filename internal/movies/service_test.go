package movies

import (
	"errors"
	"testing"
)

func TestProviderFailureClass(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"omdb returned HTTP 401": "authentication",
		"Movie not found!":       "not_found",
		"HTTP 429":               "rate_limited",
		"invalid JSON":           "provider_error",
	}
	for message, want := range tests {
		if got := providerFailureClass(errors.New(message)); got != want {
			t.Fatalf("%q: got %q, want %q", message, got, want)
		}
	}
}
