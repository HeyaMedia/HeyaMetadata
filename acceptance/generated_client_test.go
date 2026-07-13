package acceptance_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/server"
	"github.com/HeyaMedia/HeyaMetadata/sdk/go/heyametadata"
	"github.com/google/uuid"
)

func TestGeneratedClientAgainstServer(t *testing.T) {
	host := httptest.NewServer(server.New("acceptance-test").Handler())
	defer host.Close()
	client, err := heyametadata.NewClientWithResponses(host.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HealthLiveWithResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != 200 || response.JSON200 == nil || response.JSON200.Status != "ok" || response.JSON200.Version != "acceptance-test" {
		t.Fatalf("unexpected generated-client response: status=%d body=%+v", response.StatusCode(), response.JSON200)
	}
}

func TestGeneratedClientBuildsCoreLookupFlow(t *testing.T) {
	host := httptest.NewServer(server.New("acceptance-test").Handler())
	defer host.Close()
	client, err := heyametadata.NewClientWithResponses(host.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	query := "contract probe"
	kind := heyametadata.SearchEntitiesParamsKind("manga")
	search, err := client.SearchEntitiesWithResponse(ctx, &heyametadata.SearchEntitiesParams{Q: &query, Kind: &kind})
	if err != nil {
		t.Fatal(err)
	}
	if search.StatusCode() != 503 {
		t.Fatalf("search compatibility: status=%d", search.StatusCode())
	}
	resolution, err := client.ResolveEntityWithResponse(ctx, nil, heyametadata.ResolutionInputBody{Kind: heyametadata.ResolutionInputBodyKind("manga"), Provider: "kitsu", Namespace: "manga", Value: "35"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.StatusCode() != 503 {
		t.Fatalf("resolution compatibility: status=%d", resolution.StatusCode())
	}
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	entity, err := client.EntityDetailWithResponse(ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entity.StatusCode() != 503 {
		t.Fatalf("entity compatibility: status=%d", entity.StatusCode())
	}
}

func TestGeneratedClientBuildsCrossDomainDiscoveryRequests(t *testing.T) {
	host := httptest.NewServer(server.New("acceptance-test").Handler())
	defer host.Close()
	client, err := heyametadata.NewClientWithResponses(host.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	query := heyametadata.DedicatedDiscoveryRequest{Query: "contract probe"}
	checks := []struct {
		name string
		call func() (int, error)
	}{
		{"movie", func() (int, error) {
			r, e := client.CreateDiscoveryWithResponse(ctx, nil, heyametadata.Request{Kind: "movie", Query: "contract probe"})
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"tv", func() (int, error) {
			r, e := client.DiscoverTvShowWithResponse(ctx, nil, query)
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"anime", func() (int, error) {
			r, e := client.DiscoverAnimeWithResponse(ctx, nil, query)
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"manga", func() (int, error) {
			r, e := client.DiscoverMangaWithResponse(ctx, nil, query)
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"manga volume", func() (int, error) {
			r, e := client.DiscoverMangaVolumeWithResponse(ctx, nil, query)
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"comic volume", func() (int, error) {
			r, e := client.DiscoverComicWithResponse(ctx, nil, query)
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"book", func() (int, error) {
			r, e := client.CreateDiscoveryWithResponse(ctx, nil, heyametadata.Request{Kind: "book_work", Query: "contract probe"})
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
		{"music", func() (int, error) {
			r, e := client.CreateDiscoveryWithResponse(ctx, nil, heyametadata.Request{Kind: "artist", Query: "contract probe"})
			if e != nil {
				return 0, e
			}
			return r.StatusCode(), nil
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			status, err := check.call()
			if err != nil {
				t.Fatal(err)
			}
			// A server without runtime dependencies rejects the request after
			// routing and schema validation. That is exactly the boundary this
			// SDK/server compatibility test needs to reach.
			if status != 503 {
				t.Fatalf("status=%d, want 503", status)
			}
		})
	}
}
