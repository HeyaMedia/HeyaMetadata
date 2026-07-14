package images

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
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
	}{"discogs": {"discogs", "i.discogs.com", true}, "discogs subdomain": {"discogs", "cdn.i.discogs.com", true}, "tvmaze": {"tvmaze", "static.tvmaze.com", true}, "anidb": {"anidb", "cdn-eu.anidb.net", true}, "cover art archive": {"coverartarchive", "coverartarchive.org", true}, "cover art archive redirect": {"coverartarchive", "archive.org", true}, "cover art archive redirect subdomain": {"coverartarchive", "ia800.us.archive.org", true}, "openlibrary": {"openlibrary", "covers.openlibrary.org", true}, "openlibrary cdn": {"openlibrary", "archive.org", true}, "wrong provider": {"deezer", "i.discogs.com", false}, "wrong archive provider": {"deezer", "archive.org", false}, "arbitrary": {"wikidata", "example.com", false}, "commons": {"wikidata", "upload.wikimedia.org", true}}
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

func TestVariantWidthsAreClassAwareAndNeverUpscale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		class       string
		sourceWidth int
		want        string
	}{
		{"poster", 3000, "[256 512 1024]"},
		{"backdrop", 1500, "[480 960 1500]"},
		{"cover", 400, "[256 400]"},
		{"profile", 120, "[120]"},
	}
	for _, test := range tests {
		if got := fmt.Sprint(variantWidths(test.class, test.sourceWidth)); got != test.want {
			t.Errorf("%s/%d: got %s want %s", test.class, test.sourceWidth, got, test.want)
		}
	}
}

func TestBuildVariantsProducesBothServingFormats(t *testing.T) {
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
	width, height, variants, err := buildVariants(original.Bytes(), "poster")
	if err != nil {
		t.Fatal(err)
	}
	if width != 96 || height != 144 || len(variants) != 2 {
		t.Fatalf("dimensions/variants: %dx%d, %d", width, height, len(variants))
	}
	if variants[0].Format != "webp" || variants[1].Format != "avif" {
		t.Fatalf("formats: %s, %s", variants[0].Format, variants[1].Format)
	}
	for _, variant := range variants {
		if variant.Width != 96 || variant.Height != 144 || len(variant.Body) == 0 || variant.Checksum == "" {
			t.Fatalf("invalid %s variant: %+v", variant.Format, variant)
		}
	}
}

func TestUsableProcessOutputReplacesClosedDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed-output")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	output := usableProcessOutput(file)
	if output == nil {
		t.Fatal("expected a usable output descriptor")
	}
	if output == file {
		t.Fatal("closed descriptor was not replaced")
	}
	t.Cleanup(func() { _ = output.Close() })
	if _, err := output.Stat(); err != nil {
		t.Fatalf("replacement descriptor is not usable: %v", err)
	}
}
