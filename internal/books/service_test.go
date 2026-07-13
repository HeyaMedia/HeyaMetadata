package books

import "testing"

func TestBookIdentityNormalization(t *testing.T) {
	if got := normalizeClaim("openlibrary", "/works/ol27482w"); got != "OL27482W" {
		t.Fatalf("work key: %s", got)
	}
	if got := normalizeClaim("isbn", "978-0-261-10221-7"); got != "9780261102217" {
		t.Fatalf("ISBN: %s", got)
	}
	if got := isbnNamespace("0-261-10221-4"); got != "isbn10" {
		t.Fatalf("namespace: %s", got)
	}
}
func TestDescriptionAcceptsOpenLibraryShapes(t *testing.T) {
	if got := description(map[string]any{"value": "hello"}); got != "hello" {
		t.Fatalf("description: %q", got)
	}
	if got := description("plain"); got != "plain" {
		t.Fatalf("plain: %q", got)
	}
}
func TestGoogleVolumeRequiresExactISBN(t *testing.T) {
	values := googleVolumes{Items: []googleVolume{{}}}
	values.Items[0].VolumeInfo.IndustryIdentifiers = []struct{ Type, Identifier string }{{Type: "ISBN_13", Identifier: "9780261102217"}}
	if _, ok := exactGoogleVolume(values, "978-0-261-10221-7"); !ok {
		t.Fatal("exact ISBN was not selected")
	}
	if _, ok := exactGoogleVolume(values, "9780000000000"); ok {
		t.Fatal("non-matching ISBN was selected")
	}
}

func TestGoogleVolumePrefersRicherExactISBNRecord(t *testing.T) {
	t.Parallel()
	values := googleVolumes{Items: []googleVolume{{ID: "empty"}, {ID: "rated"}}}
	for index := range values.Items {
		values.Items[index].VolumeInfo.IndustryIdentifiers = []struct{ Type, Identifier string }{{Type: "ISBN_13", Identifier: "9780261102217"}}
	}
	values.Items[1].VolumeInfo.AverageRating = 4.5
	values.Items[1].VolumeInfo.RatingsCount = 3
	selected, ok := exactGoogleVolume(values, "978-0-261-10221-7")
	if !ok || selected.ID != "rated" {
		t.Fatalf("selected %#v", selected)
	}
}

func TestSequentialPublicationKindsShareBookInfrastructureWithoutSharingIdentityKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{KindBook, KindMangaVolume, KindComicVolume} {
		if !ValidWorkKind(kind) || EditionKind(kind) == "" || Medium(kind) == "" {
			t.Fatalf("incomplete publication kind %q", kind)
		}
	}
	if ValidWorkKind("anime") {
		t.Fatal("accepted unrelated kind")
	}
	if EditionKind(KindMangaVolume) != "manga_edition" || EditionKind(KindComicVolume) != "comic_edition" {
		t.Fatalf("edition kinds: %s/%s", EditionKind(KindMangaVolume), EditionKind(KindComicVolume))
	}
}

func TestGoogleEditionCandidatesPrioritizeSelectedEditionWithinBound(t *testing.T) {
	t.Parallel()
	editions := []olEdition{{Key: "/books/OL1M"}, {Key: "/books/OL2M"}, {Key: "/books/OL3M"}}
	selected := googleEditionCandidates(editions, "OL3M", 2)
	if len(selected) != 2 || trimKey(selected[0].Key) != "OL3M" || trimKey(selected[1].Key) != "OL1M" {
		t.Fatalf("unexpected candidates: %#v", selected)
	}
}

func TestEditionWorkMembershipUsesCanonicalOpenLibraryKeys(t *testing.T) {
	t.Parallel()
	var edition olEdition
	edition.Works = append(edition.Works, struct {
		Key string `json:"key"`
	}{Key: "/works/ol27482w"})
	if !editionBelongsToWork(edition, "OL27482W") {
		t.Fatal("edition work relationship was not recognized")
	}
	if editionBelongsToWork(edition, "OL1W") {
		t.Fatal("edition matched an unrelated work")
	}
}

func TestSeriesMembershipsPreserveDecimalPositionsAndUnnumberedNames(t *testing.T) {
	values := seriesMemberships([]string{"The Expanse (Book 1)", "The Expanse, Vol. 1.5", "The 100", "The Expanse #1", "The Expanse -- bk. 1"}, "edition", "obs")
	if len(values) != 3 {
		t.Fatalf("memberships: %+v", values)
	}
	if values[0].Name != "The 100" || values[0].Position != "" {
		t.Fatalf("unnumbered series: %+v", values[0])
	}
	if values[1].Name != "The Expanse" || values[1].Position != "1" || values[1].ObservationID != "obs" {
		t.Fatalf("numbered series: %+v", values[1])
	}
	if values[2].Position != "1.5" {
		t.Fatalf("decimal position: %+v", values[2])
	}
}
