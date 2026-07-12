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
