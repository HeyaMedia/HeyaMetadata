package movies

import (
	"errors"
	"reflect"
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

func TestNonNilStringsForSearchProjection(t *testing.T) {
	t.Parallel()
	if got := nonNilStrings(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil values: got %#v, want a non-nil empty slice", got)
	}
	want := []string{"en"}
	if got := nonNilStrings(want); !reflect.DeepEqual(got, want) {
		t.Fatalf("values: got %#v, want %#v", got, want)
	}
}
