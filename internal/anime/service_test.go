package anime

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/animelists"
)

func TestNormalizeTreatsLegacyCachedNotFoundEnvelopeAsNotFound(t *testing.T) {
	t.Parallel()
	_, err := normalize(providers.Payload{
		StatusCode: http.StatusOK,
		Body:       []byte(`<error>Anime not found</error>`),
	})
	var statusError *providers.StatusError
	if !errors.As(err, &statusError) {
		t.Fatalf("error: got %v, want provider status error", err)
	}
	if statusError.Provider != "anidb" || statusError.StatusCode != http.StatusNotFound {
		t.Fatalf("status error: %+v", statusError)
	}
}

func TestNormalizePreservesNamedAniDBNumberingSchemes(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`<anime id="23"><type>TV Series</type><episodecount>1</episodecount><startdate>1998-01-01</startdate><titles><title xml:lang="x-jat" type="main">Cowboy Bebop</title><title xml:lang="ja" type="official">カウボーイビバップ</title></titles><tags><tag weight="600"><name>space</name></tag><tag weight="200"><name>noise</name></tag></tags><episodes><episode id="2"><epno type="2">S1</epno><title xml:lang="en">Special</title></episode><episode id="1"><epno type="1">1</epno><title xml:lang="en">Episode One</title></episode></episodes></anime>`)}
	record, err := normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if record.EpisodeCount != 1 || len(record.Episodes) != 2 || record.Episodes[0].Numbers[0].Scheme != "aired" || record.Episodes[0].Numbers[0].Season != 1 || !record.Episodes[1].IsSpecial || record.Episodes[1].EpisodeType != "special" || record.Episodes[1].Numbers[2].Scheme != "special" || record.Episodes[1].Numbers[2].Number != 1 {
		t.Fatalf("episodes: %+v", record.Episodes)
	}
	if len(record.Seasons) != 2 || record.Seasons[0].Number != 1 || record.Seasons[1].Number != 0 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
	if len(record.Genres) != 1 || record.Genres[0] != "space" {
		t.Fatalf("genres: %+v", record.Genres)
	}
}

func TestNormalizeDoesNotClaimAmbiguousRelatedExternalIDs(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`<anime id="23"><titles><title xml:lang="en" type="main">Cowboy Bebop</title></titles><resources><resource type="2"><externalentity><identifier>1</identifier></externalentity><externalentity><identifier>4037</identifier></externalentity></resource><resource type="44"><externalentity><identifier>30991</identifier><identifier>tv</identifier></externalentity></resource></resources></anime>`)}
	record, err := normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range record.ExternalIDs {
		if id.Provider == "myanimelist" {
			t.Fatalf("ambiguous MAL claim escaped: %+v", id)
		}
	}
	foundTMDB := false
	for _, id := range record.ExternalIDs {
		if id.Provider == "tmdb" && id.Namespace == "tv" && id.Value == "30991" {
			foundTMDB = true
		}
	}
	if !foundTMDB {
		t.Fatalf("unambiguous TMDB ID missing: %+v", record.ExternalIDs)
	}
}

func TestTVDBAnimeMappingPreservesTVDBAndRelativeAniDBNumbers(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), Body: []byte(`{"data":{"id":7,"name":"Split Cour","episodes":[{"id":9,"name":"Return","seasonNumber":1,"number":13}]}}`)}
	record, err := normalizeTVDBAnime(payload, 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Episodes) != 1 || len(record.Episodes[0].Numbers) != 2 || record.Episodes[0].Numbers[0].Scheme != "aired" || record.Episodes[0].Numbers[0].Number != 1 || record.Episodes[0].Numbers[1].Scheme != "tvdb" || record.Episodes[0].Numbers[1].Number != 13 {
		t.Fatalf("numbers: %+v", record.Episodes)
	}
	if len(record.Seasons) != 1 || record.Seasons[0].Number != 1 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
}

func TestNonRootAnimeSeparatesSeriesLevelIdentifiers(t *testing.T) {
	values := []episodic.ExternalID{
		{Provider: "anidb", Namespace: "anime", Value: "10944"},
		{Provider: "myanimelist", Namespace: "anime", Value: "25777"},
		{Provider: "imdb", Namespace: "title", Value: "tt2560140"},
		{Provider: "tmdb", Namespace: "tv", Value: "1429"},
		{Provider: "tvdb", Namespace: "series", Value: "267440"},
	}
	entityIDs, seriesIDs := splitAnimeSeriesExternalIDs(values)
	if len(entityIDs) != 2 || len(seriesIDs) != 3 {
		t.Fatalf("entity=%+v series=%+v", entityIDs, seriesIDs)
	}
	for _, value := range entityIDs {
		if isAnimeSeriesExternalID(value) {
			t.Fatalf("series identity escaped onto a season entity: %+v", value)
		}
	}
}

func TestAnimeListSeriesIdentifiersAreRootScoped(t *testing.T) {
	var entry animelists.Entry
	entry.TVDBID = 267440
	entry.TMDBID.TV = 1429
	entry.IMDbIDs = []string{"tt2560140"}
	values := animeListSeriesExternalIDs(entry)
	if len(values) != 3 {
		t.Fatalf("series IDs: %+v", values)
	}
	for _, value := range values {
		if !isAnimeSeriesExternalID(value) {
			t.Fatalf("unexpected entity-scoped ID: %+v", value)
		}
	}
}

func TestTVDBAnimeSeasonSupplementUsesSeasonIdentity(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), Body: []byte(`{"data":{"id":267440,"name":"Attack on Titan","seasons":[{"id":777,"number":2,"name":"Season 2"}],"episodes":[]}}`)}
	record, err := normalizeTVDBAnime(payload, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if record.Namespace != "season" || record.ProviderID != "777" {
		t.Fatalf("provider identity: %s/%s", record.Namespace, record.ProviderID)
	}
	if len(record.ExternalIDs) != 1 || record.ExternalIDs[0].Namespace != "season" || record.ExternalIDs[0].Value != "777" {
		t.Fatalf("external IDs: %+v", record.ExternalIDs)
	}
}
