package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	animeservice "github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/books"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/HeyaMedia/HeyaMetadata/internal/releases"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type entityInput struct {
	ID                string `path:"id" format:"uuid"`
	TMDBAPIKey        string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey        string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey        string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey      string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey       string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey     string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey      string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
	GoogleBooksAPIKey string `header:"X-Heya-Google-Books-API-Key" doc:"Optional request-scoped Google Books API key; never persisted"`
}
type entityOutput struct{ Body any }
type entityMetadataInput struct {
	ID     string `path:"id" format:"uuid"`
	Offset int    `query:"offset" minimum:"0" default:"0"`
	Limit  int    `query:"limit" minimum:"1" maximum:"250" default:"100"`
}
type entityMetadataOutput struct {
	Body struct {
		Results any `json:"results"`
		Total   int `json:"total"`
		Offset  int `json:"offset"`
		Limit   int `json:"limit"`
	}
}

type resolutionInput struct {
	Prefer            string `header:"Prefer"`
	TMDBAPIKey        string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey        string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey        string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey      string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey       string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey     string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey      string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
	GoogleBooksAPIKey string `header:"X-Heya-Google-Books-API-Key" doc:"Optional request-scoped Google Books API key; never persisted"`
	Body              struct {
		Kind      string `json:"kind" enum:"movie,artist,release_group,release,recording,tv_show,anime,book_work,book_edition,author"`
		Provider  string `json:"provider" example:"tmdb"`
		Namespace string `json:"namespace" example:"movie"`
		Value     string `json:"value" example:"603"`
	}
}
type resolutionBody struct {
	State    string       `json:"state" enum:"completed,accepted"`
	Entity   any          `json:"entity,omitempty"`
	EntityID string       `json:"entity_id,omitempty"`
	Job      *jobResource `json:"job,omitempty"`
}
type resolutionOutput struct {
	Status int
	Body   resolutionBody
}

type refreshOutput struct {
	Status int
	Body   jobResource
}

type jobInput struct {
	ID int64 `path:"id" minimum:"1"`
}
type jobResource struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	EntityID string `json:"entity_id,omitempty"`
	Error    string `json:"error,omitempty"`
}
type jobOutput struct{ Body jobResource }

type searchInput struct {
	Query    string `query:"q" minLength:"1"`
	Kind     string `query:"kind" enum:"movie,artist,release_group,release,recording,tv_show,anime,book_work,book_edition,author" doc:"Optional canonical domain filter"`
	Limit    int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Year     int    `query:"year" minimum:"1800" maximum:"2200"`
	Genre    string `query:"genre"`
	Country  string `query:"country"`
	Language string `query:"language"`
	Status   string `query:"status"`
}
type searchOutput struct {
	Body struct {
		Results []json.RawMessage `json:"results"`
	}
}

type changesInput struct {
	After int64 `query:"after" minimum:"0"`
	Limit int   `query:"limit" minimum:"1" maximum:"500" default:"100"`
}
type changeEntry struct {
	Sequence          int64    `json:"sequence"`
	EntityID          string   `json:"entity_id"`
	EntityKind        string   `json:"entity_kind"`
	Slug              string   `json:"slug"`
	ChangeType        string   `json:"change_type"`
	ChangedScopes     []string `json:"changed_scopes"`
	ProjectionVersion int64    `json:"projection_version"`
	CreatedAt         string   `json:"created_at"`
}
type changesOutput struct {
	Body struct {
		Entries    []changeEntry `json:"entries"`
		NextCursor int64         `json:"next_cursor"`
	}
}

func registerMovies(api huma.API, runtime *platform.Runtime) {
	var service *movies.Service
	var artistService *artists.Service
	var releaseGroupService *releasegroups.Service
	var releaseService *releases.Service
	var recordingService *recordings.Service
	var tvService *tvshows.Service
	var animeService *animeservice.Service
	var bookService *books.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = movies.NewService(runtime)
		artistService = artists.NewService(runtime)
		releaseGroupService = releasegroups.NewService(runtime)
		releaseService = releases.NewService(runtime)
		recordingService = recordings.NewService(runtime)
		tvService = tvshows.NewService(runtime)
		animeService = animeservice.NewService(runtime)
		bookService = books.NewService(runtime)
		var err error
		client, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}

	huma.Register(api, huma.Operation{OperationID: "entity-detail", Method: http.MethodGet, Path: "/api/v2/entities/{id}", Summary: "Get a canonical entity", Tags: []string{"Entities"}}, func(ctx context.Context, input *entityInput) (*entityOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var kind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1 AND deleted_at IS NULL`, input.ID).Scan(&kind); err != nil {
			return nil, huma.Error404NotFound("entity not found")
		}
		_ = accessstats.Track(ctx, runtime.Redis, input.ID)
		if kind == "artist" {
			document, fresh, err := artistService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			if !fresh {
				if mbid, claimErr := artistService.MusicBrainzID(ctx, input.ID); claimErr == nil {
					if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
						_, _ = jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
					}
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "release_group" {
			document, fresh, err := releaseGroupService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			if !fresh {
				if mbid, claimErr := releaseGroupService.MusicBrainzID(ctx, input.ID); claimErr == nil {
					if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
						_, _ = jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
					}
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "release" {
			document, _, err := releaseService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "recording" {
			document, fresh, err := recordingService.Detail(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("recording not found")
			}
			if !fresh {
				if mbid, claimErr := recordingService.MusicBrainzID(ctx, input.ID); claimErr == nil {
					_, _ = jobs.InsertRecording(ctx, runtime, client, jobs.RecordingIngestArgs{MusicBrainzID: mbid, Reason: "stale_read"}, jobs.PriorityStaleRead)
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "tv_show" {
			document, _, err := tvService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "anime" {
			document, _, err := animeService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "book_work" || kind == "book_edition" {
			document, fresh, err := bookService.Detail(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("book entity not found")
			}
			if !fresh && kind == "book_work" {
				if workID, claimErr := bookService.OpenLibraryWorkID(ctx, input.ID); claimErr == nil {
					credentialRef, _ := storeProviderCredentials(ctx, runtime, "", "", "", "", "", "", "", input.GoogleBooksAPIKey)
					_, _ = jobs.InsertBook(ctx, runtime, client, jobs.BookIngestArgs{OpenLibraryWorkID: workID, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "author" {
			var body []byte
			if err := runtime.DB.QueryRow(ctx, `SELECT document FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, input.ID).Scan(&body); err != nil {
				return nil, huma.Error404NotFound("author not found")
			}
			return &entityOutput{Body: json.RawMessage(body)}, nil
		}
		document, fresh, err := service.Detail(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity not found")
		}
		if err != nil {
			return nil, err
		}
		if !fresh {
			if tmdbID, claimErr := service.TMDBID(ctx, input.ID); claimErr == nil {
				if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
					_, _ = jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
				}
			}
			document.Freshness.State = "stale"
		}
		return &entityOutput{Body: document}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "entity-credits", Method: http.MethodGet, Path: "/api/v2/entities/{id}/credits", Summary: "Get canonical cast and crew credits", Tags: []string{"Entities", "Credits"}}, func(ctx context.Context, input *entityMetadataInput) (*entityMetadataOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var exists bool
		if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND kind IN('movie','tv_show','anime') AND deleted_at IS NULL)`, input.ID).Scan(&exists); err != nil || !exists {
			return nil, huma.Error404NotFound("entity not found")
		}
		offset, limit := metadataPage(input.Offset, input.Limit)
		return creditProjectionPage(ctx, runtime, input.ID, offset, limit)
	})
	huma.Register(api, huma.Operation{OperationID: "entity-ratings", Method: http.MethodGet, Path: "/api/v2/entities/{id}/ratings", Summary: "Get provider-native ratings without scale coercion", Tags: []string{"Entities", "Ratings"}}, func(ctx context.Context, input *entityMetadataInput) (*entityMetadataOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var exists bool
		if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND kind IN('movie','tv_show','anime') AND deleted_at IS NULL)`, input.ID).Scan(&exists); err != nil || !exists {
			return nil, huma.Error404NotFound("entity not found")
		}
		offset, limit := metadataPage(input.Offset, input.Limit)
		return ratingProjectionPage(ctx, runtime, input.ID, offset, limit)
	})

	huma.Register(api, huma.Operation{OperationID: "resolve-entity", Method: http.MethodPost, Path: "/api/v2/resolutions", Summary: "Resolve or ingest an external entity ID", Tags: []string{"Entities"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *resolutionInput) (*resolutionOutput, error) {
		if service == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		if input.Body.Kind == "artist" {
			entityID, err := artistService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := artistService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != artists.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "artist") {
				return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.ArtistIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "release_group" {
			entityID, err := releaseGroupService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := releaseGroupService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != releasegroups.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "release_group") {
				return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseGroupIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "release" {
			entityID, err := releaseService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := releaseService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != releases.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "release") {
				return nil, huma.Error404NotFound("external ID is not known and no release collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertRelease(ctx, runtime, client, jobs.ReleaseIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "recording" {
			entityID, err := recordingService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := recordingService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != recordings.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "recording") {
				return nil, huma.Error404NotFound("external ID is not known and no recording collector accepts it")
			}
			inserted, insertErr := jobs.InsertRecording(ctx, runtime, client, jobs.RecordingIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.RecordingIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "tv_show" {
			entityID, err := tvService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := tvService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != pgx.ErrNoRows {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "tvmaze") || !strings.EqualFold(input.Body.Namespace, "show") {
				return nil, huma.Error404NotFound("external ID is not known and no TV collector accepts it")
			}
			if value, parseErr := strconv.ParseInt(input.Body.Value, 10, 64); parseErr != nil || value < 1 {
				return nil, huma.Error400BadRequest("invalid TVMaze show ID")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertTVShow(ctx, runtime, client, jobs.TVShowIngestArgs{TVMazeID: input.Body.Value, CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.TVShowIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "anime" {
			entityID, err := animeService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := animeService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != pgx.ErrNoRows {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "anidb") || !strings.EqualFold(input.Body.Namespace, "anime") {
				return nil, huma.Error404NotFound("external ID is not known and no Anime collector accepts it")
			}
			if value, parseErr := strconv.ParseInt(input.Body.Value, 10, 64); parseErr != nil || value < 1 {
				return nil, huma.Error400BadRequest("invalid AniDB AID")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertAnime(ctx, runtime, client, jobs.AnimeIngestArgs{AniDBID: input.Body.Value, CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.AnimeIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "book_work" || input.Body.Kind == "book_edition" || input.Body.Kind == "author" {
			entityID, err := bookService.Resolve(ctx, input.Body.Kind, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				if input.Body.Kind == "author" {
					var body []byte
					if e := runtime.DB.QueryRow(ctx, `SELECT document FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, entityID).Scan(&body); e != nil {
						return nil, e
					}
					return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: json.RawMessage(body)}}, nil
				}
				document, _, detailErr := bookService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != pgx.ErrNoRows {
				return nil, err
			}
			if input.Body.Kind != "book_work" || !strings.EqualFold(input.Body.Provider, "openlibrary") || !strings.EqualFold(input.Body.Namespace, "work") {
				return nil, huma.Error404NotFound("external book ID is not known and no collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, "", "", "", "", "", "", "", input.GoogleBooksAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertBook(ctx, runtime, client, jobs.BookIngestArgs{OpenLibraryWorkID: strings.ToUpper(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.BookIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind != "movie" {
			return nil, huma.Error400BadRequest("kind must be movie, artist, release_group, release, recording, tv_show, or anime")
		}
		entityID, err := service.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
		if err == nil {
			document, _, detailErr := service.Detail(ctx, entityID)
			if detailErr != nil {
				return nil, detailErr
			}
			return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
		}
		if err != movies.ErrNotFound {
			return nil, err
		}
		if !strings.EqualFold(input.Body.Provider, "tmdb") || !strings.EqualFold(input.Body.Namespace, "movie") {
			return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
		}
		tmdbID, parseErr := strconv.ParseInt(input.Body.Value, 10, 64)
		if parseErr != nil || tmdbID < 1 {
			return nil, huma.Error400BadRequest("invalid TMDB movie ID")
		}
		credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
		if credentialErr != nil {
			return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
		}
		inserted, insertErr := jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
		if insertErr != nil {
			return nil, insertErr
		}
		if wait := preferredWait(input.Prefer); wait > 0 {
			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				if entityID, resolveErr := service.Resolve(ctx, "tmdb", "movie", input.Body.Value); resolveErr == nil {
					document, _, detailErr := service.Detail(ctx, entityID)
					if detailErr != nil {
						return nil, detailErr
					}
					return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-deadline.C:
					goto accepted
				case <-ticker.C:
				}
			}
		}
	accepted:
		return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.MovieIngestKind, State: string(inserted.Job.State)}}}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "refresh-entity", Method: http.MethodPost, Path: "/api/v2/entities/{id}/refreshes", Summary: "Refresh a canonical entity", Tags: []string{"Entities"}, DefaultStatus: http.StatusAccepted}, func(ctx context.Context, input *entityInput) (*refreshOutput, error) {
		if service == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var kind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1`, input.ID).Scan(&kind); err != nil {
			return nil, huma.Error404NotFound("entity not found")
		}
		if kind == "artist" {
			mbid, err := artistService.MusicBrainzID(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("entity has no MusicBrainz artist claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.ArtistIngestKind, State: string(inserted.Job.State)}}, nil
		}
		if kind == "release_group" {
			mbid, err := releaseGroupService.MusicBrainzID(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("entity has no MusicBrainz release-group claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseGroupIngestKind, State: string(inserted.Job.State)}}, nil
		}
		if kind == "release" {
			mbid, err := releaseService.MusicBrainzID(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("entity has no MusicBrainz release claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertRelease(ctx, runtime, client, jobs.ReleaseIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseIngestKind, State: string(inserted.Job.State)}}, nil
		}
		if kind == "recording" {
			return nil, huma.Error404NotFound("recordings are refreshed internally")
		}
		if kind == "tv_show" {
			var value string
			if err := runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND provider='tvmaze' AND namespace='show' AND state='accepted'`, input.ID).Scan(&value); err != nil {
				return nil, huma.Error404NotFound("entity has no TVMaze show claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertTVShow(ctx, runtime, client, jobs.TVShowIngestArgs{TVMazeID: value, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.TVShowIngestKind, State: string(inserted.Job.State)}}, nil
		}
		if kind == "anime" {
			var value string
			if err := runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND provider='anidb' AND namespace='anime' AND state='accepted'`, input.ID).Scan(&value); err != nil {
				return nil, huma.Error404NotFound("entity has no AniDB anime claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertAnime(ctx, runtime, client, jobs.AnimeIngestArgs{AniDBID: value, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.AnimeIngestKind, State: string(inserted.Job.State)}}, nil
		}
		tmdbID, err := service.TMDBID(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity has no TMDB movie claim")
		}
		if err != nil {
			return nil, err
		}
		credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
		if credentialErr != nil {
			return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
		}
		inserted, err := jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
		if err != nil {
			return nil, err
		}
		return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.MovieIngestKind, State: string(inserted.Job.State)}}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "job-status", Method: http.MethodGet, Path: "/api/v2/jobs/{id}", Summary: "Get durable job status", Tags: []string{"Jobs"}}, func(ctx context.Context, input *jobInput) (*jobOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var resource jobResource
		resource.ID = input.ID
		if err := runtime.DB.QueryRow(ctx, `SELECT kind, state FROM river_job WHERE id = $1`, input.ID).Scan(&resource.Kind, &resource.State); err != nil {
			return nil, huma.Error404NotFound("job not found")
		}
		var entityID *string
		var failure *string
		_ = runtime.DB.QueryRow(ctx, `SELECT entity_id, error FROM movie_ingestion_runs WHERE river_job_id = $1`, input.ID).Scan(&entityID, &failure)
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM artist_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM release_group_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM release_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM recording_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM episodic_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM book_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID != nil {
			resource.EntityID = *entityID
		}
		if failure != nil {
			resource.Error = *failure
		}
		return &jobOutput{Body: resource}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "search-entities", Method: http.MethodGet, Path: "/api/v2/search", Summary: "Search canonical entities", Tags: []string{"Search"}}, func(ctx context.Context, input *searchInput) (*searchOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		results, err := searchAllEntities(ctx, runtime, input)
		if err != nil {
			return nil, fmt.Errorf("search entities: %w", err)
		}
		output := &searchOutput{}
		output.Body.Results = results
		return output, nil
	})

	huma.Register(api, huma.Operation{OperationID: "public-changes", Method: http.MethodGet, Path: "/api/v2/changes", Summary: "Read the gap-free public change feed", Tags: []string{"Changes"}}, func(ctx context.Context, input *changesInput) (*changesOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		limit := input.Limit
		if limit < 1 {
			limit = 100
		}
		rows, err := runtime.DB.Query(ctx, `SELECT sequence,entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,created_at FROM change_log WHERE sequence > $1 AND scope='public' ORDER BY sequence LIMIT $2`, input.After, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		output := &changesOutput{}
		output.Body.NextCursor = input.After
		for rows.Next() {
			var entry changeEntry
			var createdAt time.Time
			if err := rows.Scan(&entry.Sequence, &entry.EntityID, &entry.EntityKind, &entry.Slug, &entry.ChangeType, &entry.ChangedScopes, &entry.ProjectionVersion, &createdAt); err != nil {
				return nil, err
			}
			entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
			output.Body.Entries = append(output.Body.Entries, entry)
			output.Body.NextCursor = entry.Sequence
		}
		return output, rows.Err()
	})
}

func searchAllEntities(ctx context.Context, runtime *platform.Runtime, input *searchInput) ([]json.RawMessage, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, nil
	}
	limit := input.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	cacheInput, _ := json.Marshal([]any{strings.ToLower(query), kind, limit, input.Year, strings.ToLower(input.Genre), strings.ToUpper(input.Country), strings.ToLower(input.Language), strings.ToLower(input.Status)})
	digest := sha256.Sum256(cacheInput)
	cacheKey := "heya:metadata:v2:search:" + hex.EncodeToString(digest[:])
	if cached, err := runtime.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var result []json.RawMessage
		if json.Unmarshal(cached, &result) == nil {
			return result, nil
		}
	}
	provider := ""
	value := query
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		provider = strings.ToLower(parts[0])
		value = parts[1]
	}
	rows, err := runtime.DB.Query(ctx, `WITH matches AS (
		SELECT entity_id,0 AS tier,1::double precision AS score FROM external_id_claims WHERE state='accepted' AND lower(normalized_value)=lower($1) AND ($2='' OR provider=$2)
		UNION ALL
		SELECT entity_id,CASE WHEN normalized_value=lower(unaccent($3)) THEN 1 WHEN normalized_value LIKE lower(unaccent($3))||'%' THEN 2 ELSE 3 END,similarity(normalized_value,lower(unaccent($3))) FROM search_names WHERE normalized_value=lower(unaccent($3)) OR normalized_value LIKE lower(unaccent($3))||'%' OR similarity(normalized_value,lower(unaccent($3)))>=0.25
	), ranked AS (SELECT entity_id,min(tier) tier,max(score) score FROM matches GROUP BY entity_id)
	SELECT se.summary FROM ranked JOIN search_entities se ON se.entity_id=ranked.entity_id
	WHERE ($5=0 OR se.release_year=$5) AND ($6='' OR EXISTS(SELECT 1 FROM unnest(se.genres) genre WHERE lower(genre)=lower($6))) AND ($7='' OR upper($7)=ANY(se.countries)) AND ($8='' OR lower($8)=ANY(se.languages)) AND ($9='' OR se.status=lower($9)) AND ($10='' OR se.kind=$10)
	ORDER BY ranked.tier,ranked.score DESC,se.popularity DESC NULLS LAST,se.display_title LIMIT $4`, value, provider, query, limit, input.Year, input.Genre, input.Country, input.Language, input.Status, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []json.RawMessage
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(body))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ttl := 2 * time.Minute
	if len(result) == 0 {
		ttl = 15 * time.Second
	}
	if body, err := json.Marshal(result); err == nil {
		_ = runtime.Redis.Set(ctx, cacheKey, body, ttl).Err()
	}
	return result, nil
}

func storeProviderCredentials(ctx context.Context, runtime *platform.Runtime, tmdbAPIKey, omdbAPIKey, tvdbAPIKey, fanartAPIKey, appleAPIKey, discogsAPIKey, lastFMAPIKey string, extra ...string) (string, error) {
	apiKeys := map[string]string{}
	if value := strings.TrimSpace(tmdbAPIKey); value != "" {
		apiKeys["tmdb"] = value
	}
	if value := strings.TrimSpace(omdbAPIKey); value != "" {
		apiKeys["omdb"] = value
	}
	if value := strings.TrimSpace(tvdbAPIKey); value != "" {
		apiKeys["tvdb"] = value
	}
	if value := strings.TrimSpace(fanartAPIKey); value != "" {
		apiKeys["fanart"] = value
	}
	if value := strings.TrimSpace(appleAPIKey); value != "" {
		apiKeys["apple"] = value
	}
	if value := strings.TrimSpace(discogsAPIKey); value != "" {
		apiKeys["discogs"] = value
	}
	if value := strings.TrimSpace(lastFMAPIKey); value != "" {
		apiKeys["lastfm"] = value
	}
	if len(extra) > 0 {
		if value := strings.TrimSpace(extra[0]); value != "" {
			apiKeys["googlebooks"] = value
		}
	}
	if len(apiKeys) == 0 {
		return "", nil
	}
	return providercredentials.Store(ctx, runtime.Redis, providercredentials.Credentials{
		APIKeys: apiKeys,
	})
}

func preferredWait(header string) time.Duration {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "wait=") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimPrefix(part, "wait="))
		if err != nil || seconds < 1 {
			return 0
		}
		if seconds > 5 {
			seconds = 5
		}
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func metadataPage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 250 {
		limit = 100
	}
	return offset, limit
}
func creditProjectionPage(ctx context.Context, runtime *platform.Runtime, entityID string, offset, limit int) (*entityMetadataOutput, error) {
	out := &entityMetadataOutput{}
	out.Body.Offset, out.Body.Limit = offset, limit
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_credit_projections WHERE entity_id=$1`, entityID).Scan(&out.Body.Total); err != nil {
		return nil, err
	}
	rows, err := runtime.DB.Query(ctx, `SELECT jsonb_strip_nulls(jsonb_build_object('provider',provider,'provider_person_id',provider_person_id,'display_name',display_name,'credit_type',credit_type,'character',character_name,'department',department,'job',job,'order',NULLIF(credit_order,0),'profile_image_id',profile_image_id))FROM entity_credit_projections WHERE entity_id=$1 ORDER BY CASE credit_type WHEN 'cast' THEN 0 ELSE 1 END,credit_order,id OFFSET $2 LIMIT $3`, entityID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		items = append(items, json.RawMessage(body))
	}
	out.Body.Results = items
	return out, rows.Err()
}
func ratingProjectionPage(ctx context.Context, runtime *platform.Runtime, entityID string, offset, limit int) (*entityMetadataOutput, error) {
	out := &entityMetadataOutput{}
	out.Body.Offset, out.Body.Limit = offset, limit
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_rating_projections WHERE entity_id=$1`, entityID).Scan(&out.Body.Total); err != nil {
		return nil, err
	}
	rows, err := runtime.DB.Query(ctx, `SELECT jsonb_strip_nulls(jsonb_build_object('system',system,'value',value,'scale_min',scale_min,'scale_max',scale_max,'votes',NULLIF(votes,0)))FROM entity_rating_projections WHERE entity_id=$1 ORDER BY system OFFSET $2 LIMIT $3`, entityID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		items = append(items, json.RawMessage(body))
	}
	out.Body.Results = items
	return out, rows.Err()
}
