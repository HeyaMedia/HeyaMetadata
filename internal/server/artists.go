package server

import (
	"context"
	"net/http"

	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
)

type artistTopTracksInput struct {
	ID     string `path:"id" format:"uuid"`
	Offset int    `query:"offset" minimum:"0" default:"0"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50" doc:"Maximum ranked provider tracks returned"`
}

type artistTopTracksOutput struct {
	Body artists.TopTracksPage
}

func registerArtists(api huma.API, runtime *platform.Runtime) {
	huma.Register(api, huma.Operation{
		OperationID: "artist-top-tracks",
		Method:      http.MethodGet,
		Path:        "/api/v2/entities/{id}/top-tracks",
		Summary:     "Read ranked provider top tracks for an artist",
		Description: "Returns the complete persisted canonical ranking snapshot, ordered by provider and rank. Collection is capped at the provider's top 100 tracks; sources reports the upstream total and whether that snapshot was truncated. Existing MusicBrainz recording claims are linked at read time, while unresolved tracks remain provider evidence and never create recording entities.",
		Tags:        []string{"Music"},
	}, func(ctx context.Context, input *artistTopTracksInput) (*artistTopTracksOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		page, err := artists.NewService(runtime).TopTracks(ctx, input.ID, input.Offset, input.Limit)
		if err == artists.ErrNotFound {
			return nil, huma.Error404NotFound("artist not found")
		}
		if err != nil {
			return nil, err
		}
		return &artistTopTracksOutput{Body: page}, nil
	})
}
