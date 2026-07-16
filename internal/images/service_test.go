package images

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
	}{"discogs": {"discogs", "i.discogs.com", true}, "discogs subdomain": {"discogs", "cdn.i.discogs.com", true}, "tvmaze": {"tvmaze", "static.tvmaze.com", true}, "anidb": {"anidb", "cdn-eu.anidb.net", true}, "cover art archive": {"coverartarchive", "coverartarchive.org", true}, "cover art archive redirect": {"coverartarchive", "archive.org", true}, "cover art archive redirect subdomain": {"coverartarchive", "ia800.us.archive.org", true}, "openlibrary": {"openlibrary", "covers.openlibrary.org", true}, "openlibrary cdn": {"openlibrary", "archive.org", true}, "wrong provider": {"deezer", "i.discogs.com", false}, "wrong archive provider": {"deezer", "archive.org", false}, "arbitrary": {"wikidata", "example.com", false}, "commons": {"wikidata", "upload.wikimedia.org", true}, "audiodb cdn": {"audiodb", "r2.theaudiodb.com", true}, "bandcamp cdn": {"bandcamp", "f4.bcbits.com", true}, "tidal cdn": {"tidal", "resources.tidal.com", true}, "tidal wrong host": {"tidal", "tidal.com", false}}
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

func TestCanonicalVariantWidthsBoundDurableDerivatives(t *testing.T) {
	t.Parallel()
	for requested, want := range map[int]int{64: 320, 320: 320, 321: 640, 640: 640, 1200: 1280, 1280: 1280, 3840: 3840} {
		if got := CanonicalVariantWidth(requested); got != want {
			t.Errorf("requested %d: got %d want %d", requested, got, want)
		}
	}
}

func TestBuildVariantProducesRequestedServingFormatAndWidth(t *testing.T) {
	t.Parallel()
	source := image.NewNRGBA(image.Rect(0, 0, 96, 144))
	for y := range 144 {
		for x := range 96 {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 2), G: uint8(y), B: 120, A: 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	width, height, err := inspectImage(original.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if width != 96 || height != 144 {
		t.Fatalf("dimensions: %dx%d", width, height)
	}
	variant, err := buildVariant(original.Bytes(), 64)
	if err != nil {
		t.Fatal(err)
	}
	if variant.Format != "webp" || variant.MediaType != "image/webp" {
		t.Fatalf("format: %+v", variant)
	}
	if variant.Width != 64 || variant.Height != 96 || len(variant.Body) == 0 || variant.Checksum == "" {
		t.Fatalf("invalid WebP variant: %+v", variant)
	}

	variant, err = buildVariant(original.Bytes(), 640)
	if err != nil {
		t.Fatal(err)
	}
	if variant.Width != 96 || variant.Height != 144 || len(variant.Body) == 0 || variant.Checksum == "" {
		t.Fatalf("variant was upscaled: %+v", variant)
	}
}
