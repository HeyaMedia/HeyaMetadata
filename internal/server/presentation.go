package server

import (
	"context"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	presentationview "github.com/HeyaMedia/HeyaMetadata/internal/presentation"
)

type localeRequest struct {
	Language, FallbackLanguages, AcceptLanguage, Country string
}

func presentEntity(ctx context.Context, runtime *platform.Runtime, entityID, kind string, document any, locale localeRequest) (*entityOutput, error) {
	preferences := images.LanguagePreferences(locale.Language, locale.FallbackLanguages, locale.AcceptLanguage)
	candidates, err := images.NewService(runtime).Candidates(ctx, entityID, "")
	if err != nil {
		return nil, err
	}
	candidates = images.RankCandidates(candidates, preferences, locale.Country)
	selections := map[string]string{}
	for _, candidate := range candidates {
		if candidate.Selected {
			selections[candidate.Class] = candidate.ID
		}
	}
	body, err := presentationview.Apply(document, kind, preferences, locale.Country, selections)
	if err != nil {
		return nil, err
	}
	return &entityOutput{Vary: "Accept-Language", Body: body}, nil
}

func localeFromEntity(input *entityInput) localeRequest {
	return localeRequest{Language: input.Language, FallbackLanguages: input.FallbackLanguages, AcceptLanguage: input.AcceptLanguage, Country: input.Country}
}

func localeFromEpisodic(input *episodicEntityInput) localeRequest {
	return localeRequest{Language: input.Language, FallbackLanguages: input.FallbackLanguages, AcceptLanguage: input.AcceptLanguage, Country: input.Country}
}
