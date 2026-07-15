package artists

import (
	"testing"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

func TestArtistSlugPreservesNonLatinNames(t *testing.T) {
	if got := artistSlug("ハク。"); got != "ハク" {
		t.Fatalf("artist slug: %q", got)
	}
}

func TestWikidataArtistEvidenceIsDescriptiveAndPrimaryNameScoped(t *testing.T) {
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord: artistdomain.ProviderRecord{Provider: "wikidata", Namespace: "entity", Value: "Q74123"},
		Names:          []artistdomain.Name{{Value: "Da Hool", Language: "en", Primary: true}},
	}
	if !wikidataArtistRecordMatches(record, "Da Hool") {
		t.Fatal("exact primary artist label should retain scoped Wikidata descriptions")
	}
	if wikidataArtistRecordMatches(record, "DJ Hooligan") {
		t.Fatal("a shared Wikidata item must not leak across distinct stage-name artists")
	}
	spine := artistdomain.NormalizedRecordV1{IdentityCandidates: []artistdomain.IdentityCandidate{
		{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: "08e6bef1-633e-41d8-8201-a65e1ac8ec64", Confidence: 1},
		{Provider: "wikidata", Namespace: "entity", NormalizedValue: "Q74123", Confidence: 1},
	}}
	candidates := authoritativeArtistCandidates(spine)
	if len(candidates) != 1 || candidates[0].Provider != "musicbrainz" {
		t.Fatalf("authoritative candidates=%+v", candidates)
	}
}
