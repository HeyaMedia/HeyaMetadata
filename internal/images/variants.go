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
	"io"

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
	if err := validateImageDimensions(config.Width, config.Height); err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func validateImageDimensions(width, height int) error {
	if width < 1 || height < 1 || int64(width)*int64(height) > MaxSourcePixels {
		return fmt.Errorf("image dimensions %dx%d exceed the safety limit", width, height)
	}
	return nil
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

func buildBoundedWebP(sourceReader io.ReadSeeker, maxWidth, maxHeight int) (Variant, error) {
	if maxWidth < 1 || maxHeight < 1 {
		return Variant{}, fmt.Errorf("image bounds must be positive")
	}
	config, _, err := image.DecodeConfig(sourceReader)
	if err != nil {
		return Variant{}, fmt.Errorf("inspect image: %w", err)
	}
	if err := validateImageDimensions(config.Width, config.Height); err != nil {
		return Variant{}, err
	}
	return encodeBoundedWebP(sourceReader, config.Width, config.Height, maxWidth, maxHeight)
}

func buildStoredWebP(sourceReader io.ReadSeeker) (Variant, error) {
	config, _, err := image.DecodeConfig(sourceReader)
	if err != nil {
		return Variant{}, fmt.Errorf("inspect image: %w", err)
	}
	if err := validateImageDimensions(config.Width, config.Height); err != nil {
		return Variant{}, err
	}
	maxWidth, maxHeight := oversizedImageBounds(config.Width, config.Height)
	return encodeBoundedWebP(sourceReader, config.Width, config.Height, maxWidth, maxHeight)
}

func oversizedImageBounds(width, height int) (int, int) {
	difference := width - height
	if difference < 0 {
		difference = -difference
	}
	// Treat artwork within five percent of 1:1 as square. Provider scans are
	// occasionally off by a pixel or carry a narrow scanner border.
	if width > 0 && height > 0 && difference*20 <= max(width, height) {
		return OversizedSquareEdge, OversizedSquareEdge
	}
	if width > height {
		return OversizedLandscapeWidth, OversizedLandscapeHeight
	}
	return OversizedPortraitWidth, OversizedPortraitHeight
}

func encodeBoundedWebP(sourceReader io.ReadSeeker, sourceWidth, sourceHeight, maxWidth, maxHeight int) (Variant, error) {
	if _, err := sourceReader.Seek(0, io.SeekStart); err != nil {
		return Variant{}, fmt.Errorf("rewind image after inspection: %w", err)
	}
	source, _, err := image.Decode(sourceReader)
	if err != nil {
		return Variant{}, fmt.Errorf("decode image: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width != sourceWidth || height != sourceHeight {
		return Variant{}, fmt.Errorf("decoded image dimensions %dx%d differ from header %dx%d", width, height, sourceWidth, sourceHeight)
	}
	output := source
	if width > maxWidth || height > maxHeight {
		scale := min(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
		width = max(1, int(float64(width)*scale+0.5))
		height = max(1, int(float64(height)*scale+0.5))
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(resized, resized.Bounds(), source, bounds, draw.Over, nil)
		output = resized
	}
	var encoded bytes.Buffer
	if err := webp.Encode(&encoded, output, webp.Options{Quality: 88, Method: 1}); err != nil {
		return Variant{}, fmt.Errorf("encode bounded WebP image: %w", err)
	}
	digest := sha256.Sum256(encoded.Bytes())
	return Variant{
		Format: VariantFormat, MediaType: "image/webp",
		Width: width, Height: height, ByteSize: int64(encoded.Len()),
		Checksum: hex.EncodeToString(digest[:]),
		Body:     append([]byte(nil), encoded.Bytes()...),
	}, nil
}
