package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

const (
	// v2 variants are generated on demand at a bounded requested width instead
	// of precomputing every size and format before an image can be served.
	TransformVersion = "v2"
	MaxSourcePixels  = 60_000_000
	VariantFormat    = "webp"
)

var variantWidthBuckets = [...]int{320, 640, 1280, 1920, 2560, 3840}

type Variant struct {
	Format, MediaType, Checksum, ObjectKey string
	Width, Height                          int
	ByteSize                               int64
	Body                                   []byte
}

// CanonicalVariantWidth bounds the number of durable derivatives an
// unauthenticated client can create while still serving an image at or above
// the requested display size whenever the source is large enough.
func CanonicalVariantWidth(requested int) int {
	for _, width := range variantWidthBuckets {
		if requested <= width {
			return width
		}
	}
	return variantWidthBuckets[len(variantWidthBuckets)-1]
}

func inspectImage(body []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("inspect image: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxSourcePixels {
		return 0, 0, fmt.Errorf("image dimensions %dx%d exceed the safety limit", config.Width, config.Height)
	}
	return config.Width, config.Height, nil
}

func buildVariant(body []byte, requestedWidth int) (Variant, error) {
	if requestedWidth < 1 {
		return Variant{}, fmt.Errorf("image variant width must be positive")
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return Variant{}, fmt.Errorf("decode image for variant: %w", err)
	}
	bounds := source.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return Variant{}, fmt.Errorf("image has invalid dimensions")
	}
	width := min(requestedWidth, bounds.Dx())
	height := max(1, int(float64(bounds.Dy())*float64(width)/float64(bounds.Dx())+0.5))
	resized := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(resized, resized.Bounds(), source, bounds, draw.Over, nil)
	var encoded bytes.Buffer
	if err := webp.Encode(&encoded, resized, webp.Options{Quality: 82, Method: 1}); err != nil {
		return Variant{}, fmt.Errorf("encode WebP variant at %dpx: %w", width, err)
	}
	digest := sha256.Sum256(encoded.Bytes())
	return Variant{
		Format: VariantFormat, MediaType: "image/webp",
		Width: width, Height: height, ByteSize: int64(encoded.Len()),
		Checksum: hex.EncodeToString(digest[:]),
		Body:     append([]byte(nil), encoded.Bytes()...),
	}, nil
}
