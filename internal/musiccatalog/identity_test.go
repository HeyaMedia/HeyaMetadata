package musiccatalog

import "testing"

func TestIdentityCatalogOverlapRequiresCompatibleReleaseEvidence(t *testing.T) {
	left := []IdentityRelease{
		{Provider: "apple", Namespace: "album", ID: "1", Title: "Freaks Out", Date: "2022-07-01", Kind: "single"},
		{Provider: "apple", Namespace: "album", ID: "2", Title: "After Dark - Single", Date: "2023-02-01", Kind: "single"},
	}
	right := []IdentityRelease{
		{Provider: "discogs", Namespace: "master", ID: "10", Title: "Freaks Out", Date: "2022", Kind: "single"},
		{Provider: "deezer", Namespace: "album", ID: "20", Title: "After Dark", Date: "2023-02-01", Kind: "single"},
		{Provider: "deezer", Namespace: "album", ID: "30", Title: "Freaks Out", Date: "2021-01-01", Kind: "single"},
	}
	if got := IdentityCatalogOverlap(left, right); got != 2 {
		t.Fatalf("overlap=%d", got)
	}
}

func TestAppleIdentityCatalogKeepsDirectAndCollaborativeCollections(t *testing.T) {
	body := []byte(`{"resultCount":5,"results":[{"wrapperType":"artist","artistId":591024034,"artistName":"Yoshiko"},{"wrapperType":"collection","collectionId":1630125755,"collectionName":"Freaks Out - Single","collectionType":"Album","artistId":591024034,"artistName":"Yoshiko","releaseDate":"2022-07-01T07:00:00Z","trackCount":1},{"wrapperType":"collection","collectionId":2,"collectionName":"Together - Single","collectionType":"Album","artistId":99,"artistName":"Yoshiko & Alee","releaseDate":"2023-01-01","trackCount":1},{"wrapperType":"collection","collectionId":3,"collectionName":"Namesake","artistId":99,"artistName":"Someone Else"},{"wrapperType":"track","collectionId":1630125755,"artistId":591024034}]}`)
	items, err := AppleIdentityCatalog(body, "591024034")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "1630125755" || items[0].Title != "Freaks Out" || items[0].Kind != "single" || items[1].ID != "2" {
		t.Fatalf("items=%+v", items)
	}
}

func TestDeezerIdentityCatalogTrustsArtistScopedEndpoint(t *testing.T) {
	body := []byte(`{"total":2,"data":[{"id":10,"title":"Own","release_date":"2024-01-01","record_type":"single","artist":{"id":7}},{"id":11,"title":"Other","artist":{"id":8}}]}`)
	items, total, err := DeezerIdentityCatalog(body, "7")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 || items[0].ID != "10" || items[1].ID != "11" {
		t.Fatalf("total=%d items=%+v", total, items)
	}
}
