package heyametadata_test

import (
	"errors"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/sdk/go/heyametadata"
)

func TestDecodeCanonicalDocument(t *testing.T) {
	tests := []struct {
		kind string
		want heyametadata.CanonicalKind
	}{
		{"movie", heyametadata.CanonicalKindMovie},
		{"tv_show", heyametadata.CanonicalKindTVShow},
		{"anime", heyametadata.CanonicalKindAnime},
		{"artist", heyametadata.CanonicalKindArtist},
		{"release_group", heyametadata.CanonicalKindReleaseGroup},
		{"release", heyametadata.CanonicalKindRelease},
		{"recording", heyametadata.CanonicalKindRecording},
		{"musical_work", heyametadata.CanonicalKindMusicalWork},
		{"book_work", heyametadata.CanonicalKindBookWork},
		{"book_edition", heyametadata.CanonicalKindBookEdition},
		{"author", heyametadata.CanonicalKindAuthor},
		{"manga", heyametadata.CanonicalKindManga},
		{"manga_volume", heyametadata.CanonicalKindMangaVolume},
		{"comic_volume", heyametadata.CanonicalKindComicVolume},
		{"person", heyametadata.CanonicalKindPerson},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			document, err := heyametadata.DecodeCanonicalDocument([]byte(`{"schema_version":1,"projection_version":2,"id":"00000000-0000-4000-8000-000000000001","kind":"` + test.kind + `","slug":"fixture","display":{},"data":{},"freshness":{}}`))
			if err != nil {
				t.Fatal(err)
			}
			if document.DocumentKind() != test.want {
				t.Fatalf("kind=%q, want %q", document.DocumentKind(), test.want)
			}
		})
	}
}

func TestDecodeCanonicalDocumentRejectsUnknownKind(t *testing.T) {
	_, err := heyametadata.DecodeCanonicalDocument([]byte(`{"kind":"future_kind"}`))
	if !errors.Is(err, heyametadata.ErrUnsupportedCanonicalKind) {
		t.Fatalf("err=%v", err)
	}
}
