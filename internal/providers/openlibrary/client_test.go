package openlibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

func TestStructuredSearchUsesTitleAndAuthorFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/search.json" {
			t.Errorf("unexpected path: %s", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("title") != "Home" || query.Get("author") != "Toni Morrison" || query.Get("q") != "" {
			t.Errorf("unexpected structured query: %s", request.URL.RawQuery)
		}
		if request.Header.Get("User-Agent") != "HeyaMetadata/test (test@example.com)" {
			t.Errorf("missing user agent: %q", request.Header.Get("User-Agent"))
		}
		_, _ = writer.Write([]byte(`{"docs":[]}`))
	}))
	defer server.Close()

	client := New(config.OpenLibraryConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	payload, err := client.SearchByTitleAuthor(context.Background(), "Home", "Toni Morrison", 60)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ProviderRecordID != "author=Toni+Morrison&title=Home" || strings.ContainsRune(payload.ProviderRecordID, '\x00') || !strings.Contains(payload.RequestKey, "author=Toni+Morrison") || !strings.Contains(payload.RequestKey, "title=Home") {
		t.Fatalf("unexpected request identity: %+v", payload)
	}
}

func TestStructuredSearchRecordIdentityEscapesDatabaseUnsafeInput(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("title") != "A\x00B" || request.URL.Query().Get("author") != "D/E & F" {
			t.Errorf("structured values did not round trip: %q", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"docs":[]}`))
	}))
	defer server.Close()

	client := New(config.OpenLibraryConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	payload, err := client.SearchByTitleAuthor(context.Background(), "A\x00B", "D/E & F", 20)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(payload.ProviderRecordID, '\x00') || strings.ContainsRune(payload.RequestKey, '\x00') || !strings.Contains(payload.ProviderRecordID, "%00") {
		t.Fatalf("provider observation identity is not PostgreSQL-safe: %+v", payload)
	}
}

func TestBroadSearchRecordIdentityEscapesDatabaseUnsafeInput(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"docs":[]}`))
	}))
	defer server.Close()

	client := New(config.OpenLibraryConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	payload, err := client.Search(context.Background(), "A\x00B", 20)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(payload.ProviderRecordID, '\x00') || !strings.Contains(payload.ProviderRecordID, "%00") {
		t.Fatalf("provider observation identity is not PostgreSQL-safe: %+v", payload)
	}
}

func TestCanonicalKeyAcceptsProviderShapesAndRejectsForeignURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "OL6656W", want: "OL6656W", ok: true},
		{input: "/works/ol6656w", want: "OL6656W", ok: true},
		{input: "https://openlibrary.org/books/OL123M?mode=all", want: "OL123M", ok: true},
		{input: "/authors/OL42A", want: "OL42A", ok: true},
		{input: "https://example.com/works/OL6656W", ok: false},
		{input: "/works/not-a-key", ok: false},
		{input: "/editions/OL123M", ok: false},
	}
	for _, test := range tests {
		got, ok := CanonicalKey(test.input)
		if got != test.want || ok != test.ok {
			t.Errorf("CanonicalKey(%q) = %q, %t; want %q, %t", test.input, got, ok, test.want, test.ok)
		}
	}
}
