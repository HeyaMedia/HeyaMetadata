package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const seoShell = `<!doctype html><html><head><meta charset="utf-8"><meta property="og:type" content="website"></head><body><main>observatory</main></body></html>`

func TestSEORenderStaticRoute(t *testing.T) {
	renderer := &seoRenderer{runtime: nil, siteURL: "https://heya.media"}
	out, ok := renderer.render(context.Background(), []byte(seoShell), "/movies")
	if !ok {
		t.Fatal("expected static route metadata to be injected")
	}
	html := string(out)
	for _, want := range []string{
		"<title>Movies · Heya</title>",
		`<link rel="canonical" href="https://heya.media/movies">`,
		`<meta property="og:site_name" content="Heya">`,
		`<meta property="og:type" content="website">`,
		`<meta name="twitter:card" content="summary">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in:\n%s", want, html)
		}
	}
	// The static og:type placeholder must be replaced, not duplicated.
	if strings.Count(html, `property="og:type"`) != 1 {
		t.Fatalf("expected exactly one og:type tag, got:\n%s", html)
	}
	if !strings.HasSuffix(strings.TrimSpace(html), "</html>") {
		t.Fatalf("shell structure not preserved:\n%s", html)
	}
}

func TestSEORenderPrivateRouteNoindex(t *testing.T) {
	renderer := &seoRenderer{siteURL: "https://heya.media"}
	out, ok := renderer.render(context.Background(), []byte(seoShell), "/account")
	if !ok {
		t.Fatal("expected private route metadata to be injected")
	}
	if !strings.Contains(string(out), `<meta name="robots" content="noindex, nofollow">`) {
		t.Fatalf("expected noindex on private route:\n%s", out)
	}
}

func TestSEORenderNilRuntimeEntityFallsBackToShell(t *testing.T) {
	renderer := &seoRenderer{runtime: nil, siteURL: "https://heya.media"}
	// A UUID-terminated entity path requires the runtime; with none it must not
	// inject and must leave the shell untouched.
	if _, ok := renderer.render(context.Background(), []byte(seoShell), "/movies/11111111-1111-1111-1111-111111111111"); ok {
		t.Fatal("expected nil-runtime entity route to serve the unmodified shell")
	}
}

func TestSEORenderNonUUIDListingRouteNotInjected(t *testing.T) {
	renderer := &seoRenderer{siteURL: "https://heya.media"}
	if _, ok := renderer.render(context.Background(), []byte(seoShell), "/artists/not-a-uuid"); ok {
		t.Fatal("expected non-UUID listing route to be treated as static/no-op")
	}
}

func TestSEORenderWithoutHeadMarkerFallsBack(t *testing.T) {
	renderer := &seoRenderer{siteURL: "https://heya.media"}
	if _, ok := renderer.render(context.Background(), []byte("<main>observatory</main>"), "/"); ok {
		t.Fatal("expected shells without </head> to be served unmodified")
	}
}

func TestSEOEntityMetaBuild(t *testing.T) {
	summary := seoSummary{title: `Blade & Runner "2049"`, description: "A blade runner unearths a secret.", imageID: "img-1", kind: "movie", canonicalID: "abc"}
	// Exercise block rendering directly through metaFor's downstream helpers.
	meta := seoMeta{
		title:       summary.title + " · Heya",
		description: truncateDescription(summary.description),
		canonical:   "https://heya.media/movies/abc",
		ogType:      ogTypeOr(summary.kind, "website"),
		imageURL:    "https://heya.media/api/v2/images/img-1/variants/webp/1200",
	}
	meta.jsonLD = buildJSONLD(summary.kind, summary.title, meta.description, meta.canonical, meta.imageURL)
	block := meta.block()

	if !strings.Contains(block, "Blade &amp; Runner &#34;2049&#34; · Heya") {
		t.Fatalf("expected escaped title in block:\n%s", block)
	}
	if !strings.Contains(block, `<meta property="og:type" content="video.movie">`) {
		t.Fatalf("expected movie og:type:\n%s", block)
	}
	if !strings.Contains(block, `<meta name="twitter:card" content="summary_large_image">`) {
		t.Fatalf("expected large image twitter card:\n%s", block)
	}
	if !strings.Contains(block, `<meta property="og:image" content="https://heya.media/api/v2/images/img-1/variants/webp/1200">`) {
		t.Fatalf("expected og:image:\n%s", block)
	}

	start := strings.Index(block, `<script type="application/ld+json">`)
	if start < 0 {
		t.Fatalf("expected JSON-LD script:\n%s", block)
	}
	start += len(`<script type="application/ld+json">`)
	end := strings.Index(block[start:], "</script>")
	var node map[string]any
	if err := json.Unmarshal([]byte(block[start:start+end]), &node); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v\n%s", err, block)
	}
	if node["@type"] != "Movie" {
		t.Fatalf("expected schema.org Movie type, got %v", node["@type"])
	}
	if node["@context"] != "https://schema.org" {
		t.Fatalf("expected schema.org context, got %v", node["@context"])
	}
}

func TestTruncateDescription(t *testing.T) {
	if got := truncateDescription("  hello   world  "); got != "hello world" {
		t.Fatalf("whitespace collapse failed: %q", got)
	}
	long := strings.Repeat("a", 400)
	got := truncateDescription(long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 301 {
		t.Fatalf("expected 300 runes plus ellipsis, got %d", len([]rune(got)))
	}
}
