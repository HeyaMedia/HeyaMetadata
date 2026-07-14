package resourceid

import (
	"testing"

	"github.com/google/uuid"
)

func TestForIsStableAndNamespaced(t *testing.T) {
	first := For("movie_collection", "tmdb:10")
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("resource ID is not a UUID: %v", err)
	}
	if first != For("movie_collection", "tmdb:10") {
		t.Fatal("resource ID is not stable")
	}
	if first == For("movie_collection", "tmdb:11") || first == For("season", "tmdb:10") {
		t.Fatal("resource identities are not isolated")
	}
}
