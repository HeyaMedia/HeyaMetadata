package images

import (
	"net/url"
	"testing"
)

func TestValidateSourceURLRejectsProxyAndLocalTargets(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://i.discogs.com/a.jpg", "https://user:pass@i.discogs.com/a.jpg", "https://127.0.0.1/a.jpg", "https://localhost/a.jpg"} {
		parsed, _ := url.Parse(raw)
		if err := validateSourceURL(parsed, false); err == nil {
			t.Errorf("accepted unsafe URL %s", raw)
		}
	}
}
func TestProviderImageHostsAreExplicit(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		provider, host string
		want           bool
	}{"discogs": {"discogs", "i.discogs.com", true}, "discogs subdomain": {"discogs", "cdn.i.discogs.com", true}, "wrong provider": {"deezer", "i.discogs.com", false}, "arbitrary": {"wikidata", "example.com", false}, "commons": {"wikidata", "upload.wikimedia.org", true}}
	for name, test := range tests {
		if got := providerHostAllowed(test.provider, test.host); got != test.want {
			t.Errorf("%s: got %v want %v", name, got, test.want)
		}
	}
}
func TestSupportedImageTypes(t *testing.T) {
	t.Parallel()
	if got := normalizedImageType("image/webp; charset=binary"); got != "image/webp" {
		t.Fatalf("type: %q", got)
	}
	if got := normalizedImageType("text/html"); got != "" {
		t.Fatalf("accepted %q", got)
	}
}
