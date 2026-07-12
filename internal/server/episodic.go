package server

import (
	"context"
	"net/http"

	animeservice "github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type episodicEntityInput struct {
	ID string `path:"id" format:"uuid"`
}

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
			var value string
			if runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND provider='tvmaze' AND namespace='show' AND state='accepted'`, input.ID).Scan(&value) == nil {
				_, _ = jobs.InsertTVShow(ctx, runtime, client, jobs.TVShowIngestArgs{TVMazeID: value, Reason: "stale_read"}, jobs.PriorityStaleRead)
			}
			document.Freshness.State = "stale"
		}
		return &entityOutput{Body: document}, nil
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
			var value string
			if runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND provider='anidb' AND namespace='anime' AND state='accepted'`, input.ID).Scan(&value) == nil {
				_, _ = jobs.InsertAnime(ctx, runtime, client, jobs.AnimeIngestArgs{AniDBID: value, Reason: "stale_read"}, jobs.PriorityStaleRead)
			}
			document.Freshness.State = "stale"
		}
		return &entityOutput{Body: document}, nil
	})
}
