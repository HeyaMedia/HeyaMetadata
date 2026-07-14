package thexem

import (
	"net/http"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestNormalizeAnimeSplitsFlattenedTVDBSeason(t *testing.T) {
	t.Parallel()
	payloads := []providers.Payload{{
		StatusCode: http.StatusOK, ObservationID: "mapping-observation", ObservedAt: time.Unix(1, 0),
		Body: []byte(`{"result":"success","data":[
			{"scene":{"season":1,"episode":11,"absolute":11},"anidb":{"season":1,"episode":11,"absolute":11},"tvdb":{"season":1,"episode":11,"absolute":11}},
			{"scene":{"season":1,"episode":12,"absolute":12},"anidb":{"season":2,"episode":1,"absolute":12},"tvdb":{"season":1,"episode":12,"absolute":12}}
		]}`),
	}, {
		StatusCode: http.StatusOK, ObservationID: "names-observation", ObservedAt: time.Unix(1, 0),
		Body: []byte(`{"result":"success","data":{"all":"86: Eighty Six"}}`),
	}}
	record, mapping, err := NormalizeAnime(payloads, "378609")
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Seasons) != 2 || record.Seasons[0].Number != 1 || record.Seasons[1].Number != 2 {
		t.Fatalf("seasons=%+v", record.Seasons)
	}
	if record.Seasons[0].EpisodeCount != 1 || record.Seasons[1].EpisodeCount != 1 {
		t.Fatalf("season episode counts=%+v", record.Seasons)
	}
	if len(record.Episodes) != 2 || record.Episodes[1].Numbers[0].Scheme != "aired" || record.Episodes[1].Numbers[0].Season != 2 || record.Episodes[1].Numbers[0].Number != 1 {
		t.Fatalf("episodes=%+v", record.Episodes)
	}
	if got, ok := mapping.CanonicalAnimeSeason(1, 12); !ok || got != 2 {
		t.Fatalf("canonical season=%d ok=%t", got, ok)
	}
	if len(record.Titles) != 1 || record.Titles[0].Value != "86: Eighty Six" {
		t.Fatalf("titles=%+v", record.Titles)
	}
}

func TestNormalizeAnimeUsesTVDBSeriesAbsoluteNumber(t *testing.T) {
	t.Parallel()
	payloads := []providers.Payload{{
		StatusCode: http.StatusOK, ObservationID: "mapping-observation", ObservedAt: time.Unix(1, 0),
		Body: []byte(`{"result":"success","data":[
			{"scene":{"season":2,"episode":1,"absolute":1},"anidb":{"season":2,"episode":1,"absolute":1},"tvdb":{"season":2,"episode":1,"absolute":29}}
		]}`),
	}}
	record, _, err := NormalizeAnime(payloads, "424536")
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Episodes) != 1 {
		t.Fatalf("episodes=%+v", record.Episodes)
	}
	for _, number := range record.Episodes[0].Numbers {
		if number.Scheme == "absolute" {
			if number.Number != 29 {
				t.Fatalf("absolute=%+v", number)
			}
			return
		}
	}
	t.Fatalf("missing absolute numbering: %+v", record.Episodes[0].Numbers)
}
