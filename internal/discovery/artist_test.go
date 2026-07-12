package discovery

import (
	"encoding/json"
	"testing"
)

func TestArtistHintsPromoteCorrectAmbiguousCandidate(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "ano", Hints: Hints{Country: "JP", Type: "Person", Releases: []ReleaseHint{{Title: "猫猫吐吐", Year: 2023}}}})
	japanese := Candidate{ProviderScore: 100, Display: Display{Name: "ano", Country: "JP", Type: "person"}, MatchedReleases: request.Hints.Releases}
	german := Candidate{ProviderScore: 100, Display: Display{Name: "ANO", Country: "DE", Type: "group"}}
	scoreCandidate(request, &japanese)
	scoreCandidate(request, &german)
	if japanese.Confidence <= german.Confidence || japanese.Match != "strong" {
		t.Fatalf("hints did not separate candidates: jp=%+v de=%+v", japanese, german)
	}
}
func TestRecommendationRequiresMargin(t *testing.T) {
	values := []Candidate{{Confidence: .91}, {Confidence: .86}}
	if got := recommendation(values); got != "ambiguous" {
		t.Fatalf("recommendation: %s", got)
	}
	values[1].Confidence = .70
	if got := recommendation(values); got != "strong_match" {
		t.Fatalf("recommendation: %s", got)
	}
}
func TestNormalizeRequestMakesHintOrderDeterministic(t *testing.T) {
	left := NormalizeRequest(Request{Kind: " ARTIST ", Query: " Ado ", Hints: Hints{Aliases: []string{"アド", "Ado", "アド"}, Releases: []ReleaseHint{{Title: "残夢", Year: 2024}, {Title: "狂言", Year: 2022}}}})
	right := NormalizeRequest(Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"Ado", "アド"}, Releases: []ReleaseHint{{Title: "狂言", Year: 2022}, {Title: "残夢", Year: 2024}}}})
	if left.Kind != right.Kind || left.Query != right.Query || len(left.Hints.Aliases) != 2 || left.Hints.Releases[0].Title != right.Hints.Releases[0].Title {
		t.Fatalf("requests differ: %+v / %+v", left, right)
	}
}
func TestNormalizedTextHandlesPunctuationAndNativeScript(t *testing.T) {
	if normalizedText("Blank & Jones") != "blankjones" || normalizedText("ハク。") != "ハク" {
		t.Fatal("normalization lost meaningful identity text")
	}
}
func TestRequestHashIgnoresHintOrdering(t *testing.T) {
	left := Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"アド", "Ado"}, Releases: []ReleaseHint{{Title: "残夢", Year: 2024}, {Title: "狂言", Year: 2022}}}}
	right := Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"Ado", "アド"}, Releases: []ReleaseHint{{Title: "狂言", Year: 2022}, {Title: "残夢", Year: 2024}}}}
	leftHash, leftJSON, err := RequestHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, rightJSON, err := RequestHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash || !json.Valid(leftJSON) || !json.Valid(rightJSON) {
		t.Fatalf("hashes differ: %s %s", leftHash, rightHash)
	}
}
func TestReleaseHintEvidenceIsDeduplicated(t *testing.T) {
	hint := ReleaseHint{Title: "Monstersound", Year: 2001}
	values := appendUniqueReleaseHint(nil, hint)
	values = appendUniqueReleaseHint(values, hint)
	if len(values) != 1 {
		t.Fatalf("hints: %+v", values)
	}
}

func TestMovieHintsSeparateSameTitleRemakes(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindMovie, Query: "Dune", Hints: Hints{Year: 2021, Language: "en"}})
	current := Candidate{ProviderScore: 100, Display: Display{Title: "Dune", Year: 2021, Language: "en"}}
	older := Candidate{ProviderScore: 96, Display: Display{Title: "Dune", Year: 1984, Language: "en"}}
	scoreMovieCandidate(request, &current)
	scoreMovieCandidate(request, &older)
	if current.Confidence <= older.Confidence || current.Match != "likely" {
		t.Fatalf("movie hints did not separate remakes: current=%+v older=%+v", current, older)
	}
}

func TestReleaseGroupHintsSeparateSameTitleReleases(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Greatest Hits", Hints: Hints{Year: 1980, Type: "album", Artists: []string{"Queen"}, Tracks: []string{"Another One Bites the Dust"}}})
	queen := Candidate{ProviderScore: 100, Display: Display{Title: "Greatest Hits", Year: 1980, Type: "album", Artists: []ArtistDisplay{{ID: "queen", Name: "Queen"}}}, MatchedTracks: request.Hints.Tracks}
	other := Candidate{ProviderScore: 100, Display: Display{Title: "Greatest Hits", Year: 1990, Type: "album", Artists: []ArtistDisplay{{ID: "other", Name: "Other"}}}}
	scoreReleaseGroupCandidate(request, &queen)
	scoreReleaseGroupCandidate(request, &other)
	if queen.Confidence <= other.Confidence || queen.Match != "strong" {
		t.Fatalf("release-group hints did not separate releases: queen=%+v other=%+v", queen, other)
	}
}

func TestNormalizeRequestMakesCrossDomainHintsDeterministic(t *testing.T) {
	left := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Album", Hints: Hints{Language: " EN ", Artists: []string{"Queen", "queen"}, ArtistIDs: []string{"B10BBBFC-CF9E-42E0-BE17-E2C3E1D2600D"}, Tracks: []string{"Two", "One"}}})
	right := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Album", Hints: Hints{Language: "en", Artists: []string{"Queen"}, ArtistIDs: []string{"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"}, Tracks: []string{"One", "Two"}}})
	leftHash, _, err := RequestHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, _, err := RequestHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("normalized cross-domain hints differ: %+v / %+v", left, right)
	}
}
