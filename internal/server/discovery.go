package server

import (
	"context"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type discoveryCreateInput struct {
	Prefer            string `header:"Prefer" doc:"respond-async or wait=N (maximum 5 seconds)"`
	TMDBAPIKey        string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey        string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey        string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey      string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey       string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey     string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey      string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
	GoogleBooksAPIKey string `header:"X-Heya-Google-Books-API-Key" doc:"Optional request-scoped Google Books API key; never persisted"`
	MALClientID       string `header:"X-Heya-MAL-Client-ID" doc:"Optional request-scoped MyAnimeList client ID; never persisted"`
	Body              discovery.Request
}
type dedicatedDiscoveryRequest struct {
	Query       string                 `json:"query,omitempty"`
	Identifiers []discovery.Identifier `json:"identifiers,omitempty" maxItems:"50"`
	Limit       int                    `json:"limit,omitempty"`
	Hints       discovery.Hints        `json:"hints,omitempty"`
}
type dedicatedDiscoveryInput struct {
	Prefer            string `header:"Prefer" doc:"respond-async or wait=N (maximum 5 seconds)"`
	TMDBAPIKey        string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey        string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey        string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey      string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey       string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey     string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey      string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
	GoogleBooksAPIKey string `header:"X-Heya-Google-Books-API-Key" doc:"Optional request-scoped Google Books API key; never persisted"`
	MALClientID       string `header:"X-Heya-MAL-Client-ID" doc:"Optional request-scoped MyAnimeList client ID; never persisted"`
	Body              dedicatedDiscoveryRequest
}

type discoveryCredentialHeaders struct {
	TMDBAPIKey        string
	OMDBAPIKey        string
	TVDBAPIKey        string
	FanartAPIKey      string
	AppleAPIKey       string
	DiscogsAPIKey     string
	LastFMAPIKey      string
	GoogleBooksAPIKey string
	MALClientID       string
}

func discoveryCredentials(input interface {
	discoveryCredentialHeaders() discoveryCredentialHeaders
}) discoveryCredentialHeaders {
	return input.discoveryCredentialHeaders()
}

func (input *discoveryCreateInput) discoveryCredentialHeaders() discoveryCredentialHeaders {
	return discoveryCredentialHeaders{input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey, input.GoogleBooksAPIKey, input.MALClientID}
}

func (input *dedicatedDiscoveryInput) discoveryCredentialHeaders() discoveryCredentialHeaders {
	return discoveryCredentialHeaders{input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey, input.GoogleBooksAPIKey, input.MALClientID}
}

type discoveryGetInput struct {
	ID string `path:"id" format:"uuid"`
}
type discoveryResource struct {
	ID        string            `json:"id" format:"uuid"`
	State     string            `json:"state" enum:"queued,working,completed,failed"`
	Result    *discovery.Result `json:"result,omitempty"`
	Job       *jobResource      `json:"job,omitempty"`
	Error     string            `json:"error,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}
type discoveryOutput struct {
	Status     int
	RetryAfter string `header:"Retry-After"`
	Body       discoveryResource
}

func registerDiscovery(api huma.API, runtime *platform.Runtime) {
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		var err error
		client, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}
	create := func(ctx context.Context, request discovery.Request, prefer string, credentials discoveryCredentialHeaders) (*discoveryOutput, error) {
		if runtime == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		request = discovery.NormalizeRequest(request)
		if request.Kind != discovery.KindArtist && request.Kind != discovery.KindMovie && request.Kind != discovery.KindReleaseGroup && request.Kind != discovery.KindRecording && request.Kind != discovery.KindMusicalWork && request.Kind != discovery.KindTVShow && request.Kind != discovery.KindAnime && request.Kind != discovery.KindBookWork && request.Kind != discovery.KindManga && request.Kind != discovery.KindMangaVolume && request.Kind != discovery.KindComicVolume {
			return nil, huma.Error400BadRequest("unsupported discovery kind")
		}
		if request.Query == "" && len(request.Identifiers) == 0 {
			return nil, huma.Error400BadRequest("discovery requires a query or at least one identifier")
		}
		run, err := discovery.EnsureRun(ctx, runtime, request)
		if err != nil {
			return nil, err
		}
		if run.State == "completed" && run.Result != nil {
			return discoveryRunOutput(run), nil
		}
		if run.State == "queued" && len(request.Identifiers) > 0 {
			result, handled, resolveErr := discovery.NewService(runtime).ResolveKnownIdentifiers(ctx, request)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if handled {
				if result.Kind == discovery.KindArtist && result.EntityID != "" {
					releaseEvidence := jobs.ArtistCatalogReleaseEvidence(request)
					if len(releaseEvidence) > 0 {
						mbid, lookupErr := jobs.AcceptedMusicBrainzArtistID(ctx, runtime, result.EntityID)
						if lookupErr != nil {
							return nil, lookupErr
						}
						if enqueueErr := jobs.InsertArtistCatalog(ctx, client, result.EntityID, mbid, releaseEvidence...); enqueueErr != nil {
							return nil, enqueueErr
						}
					}
				}
				if completeErr := discovery.Complete(ctx, runtime, run.RequestHash, result); completeErr != nil {
					return nil, completeErr
				}
				current, getErr := discovery.GetRun(ctx, runtime, run.ID)
				if getErr != nil {
					return nil, getErr
				}
				return &discoveryOutput{Status: http.StatusOK, Body: discoveryRunResource(current)}, nil
			}
		}
		if run.State == "queued" {
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, credentials.TMDBAPIKey, credentials.OMDBAPIKey, credentials.TVDBAPIKey, credentials.FanartAPIKey, credentials.AppleAPIKey, credentials.DiscogsAPIKey, credentials.LastFMAPIKey, credentials.GoogleBooksAPIKey, credentials.MALClientID)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertDiscovery(ctx, runtime, client, run, credentialRef)
			if insertErr != nil {
				_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, credentialRef)
				return nil, insertErr
			}
			run.RiverJobID = inserted.Job.ID
		}
		wait := 1200 * time.Millisecond
		if preferredWait(prefer) > 0 {
			wait = preferredWait(prefer)
		}
		if prefer == "respond-async" {
			wait = 0
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-timer.C:
					goto accepted
				case <-ticker.C:
					current, getErr := discovery.GetRun(ctx, runtime, run.ID)
					if getErr == nil && (current.State == "completed" || current.State == "failed") {
						return discoveryRunOutput(current), nil
					}
				}
			}
		}
	accepted:
		return &discoveryOutput{Status: http.StatusAccepted, RetryAfter: "1", Body: discoveryRunResource(run)}, nil
	}
	discoveryResponses := func() map[string]*huma.Response {
		return map[string]*huma.Response{
			"202": acceptedJSONResponse("#/components/schemas/DiscoveryResource"),
			"503": retryableJSONResponse("#/components/schemas/DiscoveryResource"),
		}
	}
	huma.Register(api, huma.Operation{OperationID: "create-discovery", Method: http.MethodPost, Path: "/api/v2/discoveries", Summary: "Identify an entity from local facts and opaque external evidence", Description: "Resolves known identifiers locally, internally crosswalks fresh identifiers into the canonical pipeline, or searches upstream by descriptive hints. Provider routing is never returned to the caller.", Tags: []string{"Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *discoveryCreateInput) (*discoveryOutput, error) {
		return create(ctx, input.Body, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "discover-tv-show", Method: http.MethodPost, Path: "/api/v2/tv/discoveries", Summary: "Discover conventional television shows", Tags: []string{"TV", "Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindTVShow, Query: input.Body.Query, Identifiers: input.Body.Identifiers, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "discover-anime", Method: http.MethodPost, Path: "/api/v2/anime/discoveries", Summary: "Discover anime identities", Tags: []string{"Anime", "Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindAnime, Query: input.Body.Query, Identifiers: input.Body.Identifiers, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "discover-manga", Method: http.MethodPost, Path: "/api/v2/manga/discoveries", Summary: "Discover manga publications", Tags: []string{"Manga", "Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindManga, Query: input.Body.Query, Identifiers: input.Body.Identifiers, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "discover-comic", Method: http.MethodPost, Path: "/api/v2/comics/discoveries", Summary: "Discover comic publications", Tags: []string{"Comics", "Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindComicVolume, Query: input.Body.Query, Identifiers: input.Body.Identifiers, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "discover-manga-volume", Method: http.MethodPost, Path: "/api/v2/manga/volumes/discoveries", Summary: "Discover physical manga volumes", Tags: []string{"Manga", "Discovery"}, DefaultStatus: http.StatusOK, Responses: discoveryResponses()}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindMangaVolume, Query: input.Body.Query, Identifiers: input.Body.Identifiers, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, discoveryCredentials(input))
	})
	huma.Register(api, huma.Operation{OperationID: "get-discovery", Method: http.MethodGet, Path: "/api/v2/discoveries/{id}", Summary: "Get smart-discovery status and candidates", Tags: []string{"Discovery"}, Responses: map[string]*huma.Response{"503": retryableJSONResponse("#/components/schemas/DiscoveryResource")}}, func(ctx context.Context, input *discoveryGetInput) (*discoveryOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		run, err := discovery.GetRun(ctx, runtime, input.ID)
		if err == pgx.ErrNoRows {
			return nil, huma.Error404NotFound("discovery not found")
		}
		if err != nil {
			return nil, err
		}
		return discoveryRunOutput(run), nil
	})
}

func discoveryRunOutput(run discovery.Run) *discoveryOutput {
	status := http.StatusOK
	retryAfter := ""
	if run.State == "failed" {
		status = http.StatusServiceUnavailable
		retryAfter = "5"
	}
	return &discoveryOutput{Status: status, RetryAfter: retryAfter, Body: discoveryRunResource(run)}
}

func discoveryRunResource(run discovery.Run) discoveryResource {
	resource := discoveryResource{ID: run.ID, State: run.State, Result: run.Result, Error: run.Error, ExpiresAt: run.ExpiresAt}
	if run.RiverJobID > 0 {
		resource.Job = &jobResource{ID: run.RiverJobID, Kind: jobs.DiscoverySearchKind, State: run.State}
	}
	return resource
}
