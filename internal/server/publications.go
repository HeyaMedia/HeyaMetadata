package server

import (
	"context"
	"net/http"

	"github.com/HeyaMedia/HeyaMetadata/internal/books"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/manga"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type publicationEntityInput struct {
	ID                string `path:"id" format:"uuid"`
	Language          string `query:"language" doc:"Preferred BCP 47 presentation language"`
	FallbackLanguages string `query:"fallback_languages" doc:"Comma-separated ordered presentation language fallbacks"`
	AcceptLanguage    string `header:"Accept-Language" doc:"Presentation preferences used after explicit query preferences"`
	Country           string `query:"country" minLength:"2" maxLength:"2" doc:"Optional ISO 3166-1 alpha-2 presentation region"`
	GoogleBooksAPIKey string `header:"X-Heya-Google-Books-API-Key" doc:"Optional request-scoped Google Books API key; never persisted"`
	MALClientID       string `header:"X-Heya-MAL-Client-ID" doc:"Optional request-scoped MyAnimeList client ID; never persisted"`
}

func registerPublications(api huma.API, runtime *platform.Runtime) {
	var service *books.Service
	var mangaService *manga.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = books.NewService(runtime)
		mangaService = manga.NewService(runtime)
		client, _ = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
	}
	huma.Register(api, huma.Operation{OperationID: "manga-detail", Method: http.MethodGet, Path: "/api/v2/manga/{id}", Summary: "Get a canonical manga series", Tags: []string{"Manga"}}, func(ctx context.Context, input *publicationEntityInput) (*entityOutput, error) {
		if mangaService == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var actualKind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1 AND deleted_at IS NULL`, input.ID).Scan(&actualKind); err != nil || actualKind != "manga" {
			return nil, huma.Error404NotFound("manga not found")
		}
		document, fresh, err := mangaService.Detail(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("manga not found")
		}
		if !fresh && client != nil {
			if kitsuID, claimErr := mangaService.KitsuID(ctx, input.ID); claimErr == nil {
				credentialRef, _ := storeProviderCredentials(ctx, runtime, "", "", "", "", "", "", "", "", input.MALClientID)
				_, _ = jobs.InsertManga(ctx, runtime, client, jobs.MangaIngestArgs{KitsuMangaID: kitsuID, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
			}
			document.Freshness.State = "stale"
		}
		locale := localeRequest{Language: input.Language, FallbackLanguages: input.FallbackLanguages, AcceptLanguage: input.AcceptLanguage, Country: input.Country}
		return presentEntity(ctx, runtime, input.ID, "manga", document, locale)
	})
	read := func(kind string) func(context.Context, *publicationEntityInput) (*entityOutput, error) {
		return func(ctx context.Context, input *publicationEntityInput) (*entityOutput, error) {
			if service == nil {
				return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
			}
			var actualKind string
			if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1 AND deleted_at IS NULL`, input.ID).Scan(&actualKind); err != nil || actualKind != kind {
				return nil, huma.Error404NotFound("publication not found")
			}
			document, fresh, err := service.Detail(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("publication not found")
			}
			if !fresh && client != nil {
				if workID, claimErr := service.OpenLibraryWorkID(ctx, input.ID); claimErr == nil {
					credentialRef, _ := storeProviderCredentials(ctx, runtime, "", "", "", "", "", "", "", input.GoogleBooksAPIKey)
					_, _ = jobs.InsertBook(ctx, runtime, client, jobs.BookIngestArgs{OpenLibraryWorkID: workID, EntityKind: kind, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
				}
				document.Freshness.State = "stale"
			}
			locale := localeRequest{Language: input.Language, FallbackLanguages: input.FallbackLanguages, AcceptLanguage: input.AcceptLanguage, Country: input.Country}
			return presentEntity(ctx, runtime, input.ID, kind, document, locale)
		}
	}
	huma.Register(api, huma.Operation{OperationID: "manga-volume-detail", Method: http.MethodGet, Path: "/api/v2/manga/volumes/{id}", Summary: "Get a canonical physical manga volume", Tags: []string{"Manga"}}, read(books.KindMangaVolume))
	huma.Register(api, huma.Operation{OperationID: "comic-volume-detail", Method: http.MethodGet, Path: "/api/v2/comics/volumes/{id}", Summary: "Get a canonical comic volume", Tags: []string{"Comics"}}, read(books.KindComicVolume))
}
