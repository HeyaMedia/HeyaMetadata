package bandcamp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

const bandPage = `<html><head>
<meta property="og:image" content="https://f4.bcbits.com/img/0039319826_23.jpg">
<meta property="og:description"  content="Psychedelic rock band from Melbourne.">
</head><body>
<div data-band="{&quot;id&quot;:2632533392,&quot;name&quot;:&quot;King Gizzard &amp; The Lizard Wizard&quot;,&quot;subdomain&quot;:&quot;kinggizzard&quot;,&quot;https_url&quot;:&quot;https://kinggizzard.bandcamp.com&quot;}"></div>
</body></html>`

// homepageFallback mirrors Bandcamp's response for unknown subdomains: a 200
// with a featured band's data-band that has no subdomain field.
const homepageFallback = `<html><body><div data-band="{&quot;id&quot;:1350796274,&quot;name&quot;:&quot;Some Featured Band&quot;}"></div></body></html>`

func testConfig(pageURL string) config.BandcampConfig {
	return config.BandcampConfig{BaseURL: "https://bandcamp.com", PageURLTemplate: pageURL, RequestsPerSecond: 100}
}

func TestCollectFetchesBandPage(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/kinggizzard" {
			t.Errorf("unexpected Bandcamp request path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(bandPage))
	}))
	defer server.Close()
	client := New(testConfig(server.URL + "/{subdomain}"))
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "bandcamp", Namespace: "artist", Value: "KingGizzard"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].RequestKey != "artist/kinggizzard" || payloads[0].ProviderRecordID != "kinggizzard" {
		t.Fatalf("payload identity: %+v", payloads)
	}
}

func TestCollectRejectsInvalidIdentifiers(t *testing.T) {
	t.Parallel()
	client := New(testConfig("https://{subdomain}.example.invalid"))
	for _, identifier := range []providers.Identifier{
		{Provider: "musicbrainz", Namespace: "artist", Value: "kinggizzard"},
		{Provider: "bandcamp", Namespace: "album", Value: "kinggizzard"},
		{Provider: "bandcamp", Namespace: "artist", Value: "bad subdomain!"},
		{Provider: "bandcamp", Namespace: "artist", Value: ""},
	} {
		if _, err := client.Collect(context.Background(), identifier); err == nil {
			t.Fatalf("expected identifier rejection: %+v", identifier)
		}
	}
}

func TestClassifyMarksPagesWithoutBandMetadataNonReusable(t *testing.T) {
	t.Parallel()
	interstitial := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<html>checking your browser</html>`)}
	classify(&interstitial)
	if interstitial.ReuseDurationOverride == nil || *interstitial.ReuseDurationOverride != 0 {
		t.Fatalf("interstitial classification: %+v", interstitial.ReuseDurationOverride)
	}
	valid := providers.Payload{StatusCode: http.StatusOK, Body: []byte(bandPage)}
	classify(&valid)
	if valid.ReuseDurationOverride != nil {
		t.Fatalf("valid classification should use the policy default: %+v", valid.ReuseDurationOverride)
	}
}

func TestNormalizeArtist(t *testing.T) {
	t.Parallel()
	record, err := NormalizeArtist([]byte(bandPage), "kinggizzard", "obs-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "bandcamp" || record.ProviderRecord.Value != "kinggizzard" {
		t.Fatalf("provider record: %+v", record.ProviderRecord)
	}
	if len(record.Names) != 1 || record.Names[0].Value != "King Gizzard & The Lizard Wizard" {
		t.Fatalf("names: %+v", record.Names)
	}
	if len(record.IdentityCandidates) != 2 || record.IdentityCandidates[0].NormalizedValue != "kinggizzard" || record.IdentityCandidates[1].NormalizedValue != "2632533392" {
		t.Fatalf("identity candidates: %+v", record.IdentityCandidates)
	}
	if len(record.Links) != 1 || record.Links[0].URL != "https://kinggizzard.bandcamp.com" {
		t.Fatalf("links: %+v", record.Links)
	}
	if len(record.Images) != 1 || !strings.HasPrefix(record.Images[0].SourceURL, "https://f4.bcbits.com/") || record.Images[0].Class != "profile" {
		t.Fatalf("images: %+v", record.Images)
	}
	if len(record.Biographies) != 1 || record.Biographies[0].Value != "Psychedelic rock band from Melbourne." {
		t.Fatalf("biographies: %+v", record.Biographies)
	}
}

func TestNormalizeArtistRejectsHomepageFallback(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeArtist([]byte(homepageFallback), "metallica", "obs-1", time.Now()); err == nil {
		t.Fatal("expected the homepage fallback to be rejected")
	}
	if _, err := NormalizeArtist([]byte(bandPage), "someoneelse", "obs-1", time.Now()); err == nil {
		t.Fatal("expected a mismatched subdomain to be rejected")
	}
	if _, err := NormalizeArtist([]byte(`<html></html>`), "kinggizzard", "obs-1", time.Now()); err == nil {
		t.Fatal("expected a page without band metadata to be rejected")
	}
}
