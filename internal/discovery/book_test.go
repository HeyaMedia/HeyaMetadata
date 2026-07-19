package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type stubOpenLibraryBookSearcher struct {
	broad          providers.Payload
	broadErr       error
	structured     providers.Payload
	structuredErr  error
	structuredCall int
	structuredName string
}

func (s *stubOpenLibraryBookSearcher) Search(context.Context, string, int) (providers.Payload, error) {
	return s.broad, s.broadErr
}

func (s *stubOpenLibraryBookSearcher) SearchByTitleAuthor(_ context.Context, _ string, author string, _ int) (providers.Payload, error) {
	s.structuredCall++
	s.structuredName = author
	return s.structured, s.structuredErr
}

func openLibrarySearchPayload(t *testing.T, status int, docs []openLibraryBookDoc) providers.Payload {
	t.Helper()
	body, err := json.Marshal(openLibrarySearch{Docs: docs})
	if err != nil {
		t.Fatal(err)
	}
	return providers.Payload{StatusCode: status, Body: body}
}

func TestPublicationSubjectClassificationIsConservative(t *testing.T) {
	t.Parallel()
	if !publicationSubjectsMatch(KindMangaVolume, []string{"Japanese Manga", "Graphic novels"}) {
		t.Fatal("manga subjects were not recognized")
	}
	if publicationSubjectsMatch(KindComicVolume, []string{"Japanese Manga", "Comic books"}) {
		t.Fatal("manga leaked into conventional comics")
	}
	if !publicationSubjectsMatch(KindComicVolume, []string{"Superhero comic books", "Sequential art"}) {
		t.Fatal("comic subjects were not recognized")
	}
	if publicationSubjectsMatch(KindMangaVolume, []string{"Fantasy fiction"}) || publicationSubjectsMatch(KindComicVolume, []string{"Fantasy fiction"}) {
		t.Fatal("ordinary book was guessed to be a sequential publication")
	}
}

func TestAuthorQualifiedBookResultsLeadWhenBroadSearchIsCrowdedOut(t *testing.T) {
	t.Parallel()
	broad := make([]openLibraryBookDoc, 60)
	for i := range broad {
		broad[i] = openLibraryBookDoc{
			Key:        "/works/OL" + strconv.Itoa(10000+i) + "W",
			Title:      "Home",
			AuthorName: []string{"Someone Else"},
		}
	}
	request := NormalizeRequest(Request{Kind: KindBookWork, Query: "Home", Hints: Hints{Authors: []string{"Toni Morrison"}}})
	qualified := []openLibraryBookDoc{{Key: "/works/OL6656W", Title: "Home", AuthorName: []string{"Toni Morrison"}}}
	client := &stubOpenLibraryBookSearcher{
		broad:      openLibrarySearchPayload(t, http.StatusOK, broad),
		structured: openLibrarySearchPayload(t, http.StatusOK, qualified),
	}
	merged, warnings, err := searchOpenLibraryBookDocs(t.Context(), client, request, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || client.structuredCall != 1 || client.structuredName != "Toni Morrison" {
		t.Fatalf("fallback call=%d author=%q warnings=%v", client.structuredCall, client.structuredName, warnings)
	}
	if len(merged) != 61 || merged[0].Key != "/works/OL6656W" {
		t.Fatalf("qualified result was not promoted ahead of bounded broad results: %#v", merged[:1])
	}
	if !bookSearchHasAllAuthors(merged, bookHintAuthors(request)) {
		t.Fatal("qualified result did not restore complete author evidence")
	}
}

func TestMergeOpenLibraryBookDocsDeduplicatesQualifiedFallback(t *testing.T) {
	t.Parallel()
	qualified := openLibraryBookDoc{Key: "/works/ol6656w", Title: "Home", AuthorName: []string{"Toni Morrison"}}
	broad := openLibraryBookDoc{Key: "OL6656W", Title: "Home", AuthorKey: []string{"OL123A"}, Subject: []string{"Fiction"}, EditionCount: 18}
	merged := mergeOpenLibraryBookDocs([]openLibraryBookDoc{qualified}, []openLibraryBookDoc{broad})
	if len(merged) != 1 {
		t.Fatalf("duplicate work survived fallback merge: %#v", merged)
	}
	if merged[0].Key != "/works/OL6656W" || len(merged[0].AuthorKey) != 1 || len(merged[0].Subject) != 1 || merged[0].EditionCount != 18 {
		t.Fatalf("dedupe discarded broad result evidence: %#v", merged[0])
	}
}

func TestInvalidBroadDocCannotSuppressStructuredAuthorFallback(t *testing.T) {
	t.Parallel()
	docs := []openLibraryBookDoc{{Key: "not-a-work", Title: "Home", AuthorName: []string{"Toni Morrison"}}}
	if bookSearchHasAllAuthors(docs, []string{"Toni Morrison"}) {
		t.Fatal("an unusable broad result suppressed the structured fallback")
	}
}

func TestStructuredBookSearchFailurePreservesBroadCandidates(t *testing.T) {
	t.Parallel()
	broad := []openLibraryBookDoc{{Key: "/works/OL1W", Title: "Home", AuthorName: []string{"Someone Else"}}}
	request := NormalizeRequest(Request{Kind: KindBookWork, Query: "Home", Hints: Hints{Authors: []string{"Toni Morrison"}}})
	tests := []struct {
		name       string
		payload    providers.Payload
		searchErr  error
		wantDetail string
	}{
		{name: "transport", searchErr: errors.New("upstream unavailable"), wantDetail: "upstream unavailable"},
		{name: "rate limited", payload: providers.Payload{StatusCode: http.StatusTooManyRequests}, wantDetail: "HTTP 429"},
		{name: "malformed response", payload: providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"docs":`)}, wantDetail: "decode Open Library structured"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &stubOpenLibraryBookSearcher{
				broad:         openLibrarySearchPayload(t, http.StatusOK, broad),
				structured:    test.payload,
				structuredErr: test.searchErr,
			}
			docs, warnings, err := searchOpenLibraryBookDocs(t.Context(), client, request, 20)
			if err != nil {
				t.Fatalf("supplemental failure discarded broad search: %v", err)
			}
			if len(docs) != 1 || docs[0].Key != "/works/OL1W" || len(warnings) != 1 || !strings.Contains(warnings[0], test.wantDetail) {
				t.Fatalf("docs=%+v warnings=%v", docs, warnings)
			}
		})
	}
}

func TestBookScoringPrefersAuthorOverAudiobookEditionYear(t *testing.T) {
	t.Parallel()
	request := NormalizeRequest(Request{Kind: KindBookWork, Query: "Home", Hints: Hints{Authors: []string{"Toni Morrison"}, Year: 2023}})
	correct := Candidate{ProviderScore: 100, Display: Display{Title: "Home", Authors: []string{"Toni Morrison"}, Year: 2012}}
	wrongYear := Candidate{ProviderScore: 97, Display: Display{Title: "Home", Authors: []string{"Someone Else"}, Year: 2023}}
	scoreBook(request, &correct)
	scoreBook(request, &wrongYear)
	if correct.Confidence < .85 || correct.Confidence-wrongYear.Confidence < .12 || correct.Match != "strong" {
		t.Fatalf("edition year outweighed work author: correct=%+v wrong=%+v", correct, wrongYear)
	}
}

func TestBookRecommendationKeepsHiddenRunnerUpAmbiguous(t *testing.T) {
	t.Parallel()
	candidates := []Candidate{{Confidence: .9, Identity: ExternalID{Value: "one"}}, {Confidence: .89, Identity: ExternalID{Value: "two"}}}
	recommended, shown := presentCandidates(candidates, 1)
	if recommended != "ambiguous" || len(shown) != 1 {
		t.Fatalf("recommendation=%q candidates=%+v", recommended, shown)
	}
}
