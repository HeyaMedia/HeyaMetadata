package server

import (
	"context"
	"errors"
	"net/http"

	animeservice "github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type episodicEntityInput struct {
	ID                string `path:"id" format:"uuid"`
	Language          string `query:"language" doc:"Preferred BCP 47 presentation language"`
	FallbackLanguages string `query:"fallback_languages" doc:"Comma-separated ordered presentation language fallbacks"`
	AcceptLanguage    string `header:"Accept-Language" doc:"Presentation preferences used after explicit query preferences"`
	Country           string `query:"country" minLength:"2" maxLength:"2" doc:"Optional ISO 3166-1 alpha-2 presentation region"`
}

type episodicResourceInput struct {
	ID string `path:"id" format:"uuid"`
}
type seasonResourceOutput struct{ Body episodic.SeasonResource }
type episodeResourceOutput struct{ Body episodic.EpisodeResource }

func registerEpisodic(api huma.API, runtime *platform.Runtime) {
	var tv *tvshows.Service
	var anime *animeservice.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		tv = tvshows.NewService(runtime)
		anime = animeservice.NewService(runtime)
		client, _ = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
	}
	huma.Register(api, huma.Operation{OperationID: "tv-show-detail", Method: http.MethodGet, Path: "/api/v2/tv/shows/{id}", Summary: "Get a canonical conventional TV show", Tags: []string{"TV"}}, func(ctx context.Context, input *episodicEntityInput) (*entityOutput, error) {
		if tv == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		document, fresh, err := tv.Detail(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("TV show not found")
		}
		if !fresh && client != nil {
			provider, value, rootErr := preferredEpisodicRoot(ctx, runtime, input.ID, "tv_show")
			if rootErr == nil {
				_, _ = jobs.InsertTVShow(ctx, runtime, client, jobs.TVShowIngestArgs{Provider: provider, ProviderID: value, Reason: "stale_read"}, jobs.PriorityStaleRead)
			}
			document.Freshness.State = "stale"
		}
		return presentEntity(ctx, runtime, input.ID, "tv_show", document, localeFromEpisodic(input))
	})
	huma.Register(api, huma.Operation{OperationID: "anime-detail", Method: http.MethodGet, Path: "/api/v2/anime/{id}", Summary: "Get a canonical Anime entity", Tags: []string{"Anime"}}, func(ctx context.Context, input *episodicEntityInput) (*entityOutput, error) {
		if anime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		document, fresh, err := anime.Detail(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("Anime not found")
		}
		if !fresh && client != nil {
			provider, value, rootErr := preferredEpisodicRoot(ctx, runtime, input.ID, "anime")
			if rootErr == nil {
				_, _ = jobs.InsertAnime(ctx, runtime, client, jobs.AnimeIngestArgs{Provider: provider, ProviderID: value, Reason: "stale_read"}, jobs.PriorityStaleRead)
			}
			document.Freshness.State = "stale"
		}
		return presentEntity(ctx, runtime, input.ID, "anime", document, localeFromEpisodic(input))
	})
	huma.Register(api, huma.Operation{OperationID: "season-detail", Method: http.MethodGet, Path: "/api/v2/seasons/{id}", Summary: "Get a canonical season resource", Tags: []string{"TV", "Anime"}}, func(ctx context.Context, input *episodicResourceInput) (*seasonResourceOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		resource, err := episodic.SeasonDetail(ctx, runtime, input.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error404NotFound("season not found")
		}
		if err != nil {
			return nil, err
		}
		return &seasonResourceOutput{Body: resource}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "episode-detail", Method: http.MethodGet, Path: "/api/v2/episodes/{id}", Summary: "Get a canonical episode resource", Tags: []string{"TV", "Anime"}}, func(ctx context.Context, input *episodicResourceInput) (*episodeResourceOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		resource, err := episodic.EpisodeDetail(ctx, runtime, input.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error404NotFound("episode not found")
		}
		if err != nil {
			return nil, err
		}
		return &episodeResourceOutput{Body: resource}, nil
	})
}

func preferredEpisodicRoot(ctx context.Context, runtime *platform.Runtime, entityID, kind string) (string, string, error) {
	var provider, value string
	err := runtime.DB.QueryRow(ctx, `
		SELECT provider, normalized_value
		FROM external_id_claims
		WHERE entity_id=$1 AND entity_kind=$2 AND state='accepted'
		  AND normalized_value ~ '^[1-9][0-9]*$'
		  AND (
			(provider='tmdb' AND namespace='tv') OR
			($2='tv_show' AND provider='tvmaze' AND namespace='show') OR
			($2='anime' AND provider IN ('tvmaze','anidb') AND namespace=CASE provider WHEN 'tvmaze' THEN 'show' ELSE 'anime' END)
		  )
		ORDER BY CASE provider WHEN 'tmdb' THEN 1 WHEN 'tvmaze' THEN 2 WHEN 'anidb' THEN 3 ELSE 4 END
		LIMIT 1`, entityID, kind).Scan(&provider, &value)
	return provider, value, err
}
