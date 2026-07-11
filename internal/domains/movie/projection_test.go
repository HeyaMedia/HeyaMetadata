package movie

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCombineEnrichesWithoutErasingProviderData(t *testing.T) {
	t.Parallel()
	runtime := 120
	tmdb := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "tmdb", PrimaryObservationID: "tmdb-observation"}, Titles: []LocalizedText{{Value: "The Matrix", Type: "display"}}, Classification: Classification{Genres: []string{"Action"}, Countries: []string{"US"}}, Lifecycle: Lifecycle{NormalizedStatus: "released", ReleaseEvents: []ReleaseEvent{{Country: "US", Type: "theatrical", Date: "1999-03-31"}}}, Measurements: Measurements{RuntimeMinutes: &runtime}, Images: []Image{{ProviderImageID: "/poster.jpg", SourceURL: "https://image.tmdb.org/poster.jpg", Class: "poster"}}}
	omdb := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "omdb", PrimaryObservationID: "omdb-observation"}, Descriptions: []LocalizedText{{Value: "A computer hacker learns the truth.", Language: "en", Type: "overview"}}, Ratings: []Rating{{System: "rotten_tomatoes", Value: 83, ScaleMin: 0, ScaleMax: 100, RawValue: "83%"}}}
	projection := Combine("entity", "the-matrix-1999", 1, []RecordInput{{ID: "tmdb-record", Record: tmdb}, {ID: "omdb-record", Record: omdb}}, map[string]string{"tmdb:poster:/poster.jpg": "opaque-image"}, time.Unix(100, 0).UTC())
	if projection.Detail.Display.Title != "The Matrix" || len(projection.Detail.Data.Classification.Genres) != 1 || len(projection.Detail.Data.Ratings) != 1 || len(projection.Detail.Data.Overviews) != 1 {
		t.Fatalf("providers were not combined: %+v", projection.Detail)
	}
	if projection.Detail.Display.ImageID != "opaque-image" {
		t.Fatalf("image ID: %q", projection.Detail.Display.ImageID)
	}
	body, _ := json.Marshal(projection.Detail)
	if strings.Contains(string(body), "image.tmdb.org") {
		t.Fatal("public projection leaked an upstream image URL")
	}
}
