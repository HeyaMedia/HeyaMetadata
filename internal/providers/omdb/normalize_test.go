package omdb

import (
	"testing"
	"time"
)

func TestNormalizePreservesPlotRuntimeAndIndependentRatingScales(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"Title":"The Matrix","Year":"1999","Released":"31 Mar 1999","Runtime":"136 min",
		"Plot":"A computer hacker learns the truth.","imdbID":"tt0133093","Type":"movie",
		"imdbRating":"8.7","imdbVotes":"2,154,321","Metascore":"73",
		"Ratings":[
			{"Source":"Internet Movie Database","Value":"8.7/10"},
			{"Source":"Rotten Tomatoes","Value":"83%"},
			{"Source":"Metacritic","Value":"73/100"}
		],"Response":"True"
	}`)
	record, err := Normalize(body, "00000000-0000-0000-0000-000000000001", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "omdb" || record.IdentityCandidates[0].NormalizedValue != "tt0133093" {
		t.Fatalf("identity: %+v", record)
	}
	if record.Measurements.RuntimeMinutes == nil || *record.Measurements.RuntimeMinutes != 136 {
		t.Fatalf("runtime: %+v", record.Measurements)
	}
	if len(record.Descriptions) != 1 || len(record.Ratings) != 3 {
		t.Fatalf("plot/ratings: %+v / %+v", record.Descriptions, record.Ratings)
	}
	wantScales := map[string]float64{"imdb": 10, "rotten_tomatoes": 100, "metacritic": 100}
	for _, rating := range record.Ratings {
		if rating.ScaleMax != wantScales[rating.System] {
			t.Fatalf("rating scale was flattened: %+v", rating)
		}
	}
}

func TestNormalizeRejectsLogicalError(t *testing.T) {
	t.Parallel()
	if _, err := Normalize([]byte(`{"Response":"False","Error":"Movie not found!"}`), "id", time.Now()); err == nil {
		t.Fatal("expected OMDb logical error")
	}
}
