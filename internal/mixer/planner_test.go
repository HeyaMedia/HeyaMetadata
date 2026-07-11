package mixer

import (
	"context"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type fakeCollector struct{ capability providers.Capability }

func (c fakeCollector) Capability() providers.Capability { return c.capability }
func (fakeCollector) Collect(context.Context, providers.Identifier) ([]providers.Payload, error) {
	return nil, nil
}

func TestPlannerUnlocksProvidersFromKnownIdentifiers(t *testing.T) {
	t.Parallel()
	tmdb := fakeCollector{providers.Capability{Provider: "tmdb", AcceptedIdentifiers: []providers.Identifier{{Provider: "tmdb", Namespace: "movie"}}, Provides: []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles}}}
	omdb := fakeCollector{providers.Capability{Provider: "omdb", AcceptedIdentifiers: []providers.Identifier{{Provider: "imdb", Namespace: "title"}}, Provides: []providers.Scope{providers.ScopeRatings}}}
	planner := New(tmdb, omdb)
	first := planner.Build([]providers.Identifier{{Provider: "tmdb", Namespace: "movie", Value: "603"}}, []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeRatings})
	if len(first.Steps) != 1 || first.Steps[0].Collector.Capability().Provider != "tmdb" || len(first.Missing) != 1 {
		t.Fatalf("unexpected initial plan: %+v", first)
	}
	second := planner.Build([]providers.Identifier{{Provider: "tmdb", Namespace: "movie", Value: "603"}, {Provider: "imdb", Namespace: "title", Value: "tt0133093"}}, []providers.Scope{providers.ScopeRatings})
	if len(second.Steps) != 1 || second.Steps[0].Collector.Capability().Provider != "omdb" || len(second.Missing) != 0 {
		t.Fatalf("unexpected follow-up plan: %+v", second)
	}
	third := planner.BuildAvailable(
		[]providers.Identifier{{Provider: "tmdb", Namespace: "movie", Value: "603"}, {Provider: "imdb", Namespace: "title", Value: "tt0133093"}},
		[]providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeRatings},
		map[string]bool{"tmdb": true},
	)
	if len(third.Steps) != 1 || third.Steps[0].Collector.Capability().Provider != "omdb" {
		t.Fatalf("completed primary provider hid supplemental evidence: %+v", third)
	}
}
