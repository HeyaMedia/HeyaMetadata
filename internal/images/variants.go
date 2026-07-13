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
	"sort"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

const (
	TransformVersion = "v1"
	MaxSourcePixels  = 60_000_000
)

type Variant struct {
	Format, MediaType, Checksum, ObjectKey string
	Width, Height                          int
	ByteSize                               int64
	Body                                   []byte
}

func variantWidths(class string, sourceWidth int) []int {
	var requested []int
	switch class {
	case "backdrop", "still", "banner":
		requested = []int{480, 960, 1920}
	case "poster", "cover", "profile", "logo":
		requested = []int{256, 512, 1024}
	default:
		requested = []int{512, 1024}
	}
	if sourceWidth < 1 {
		return nil
	}
	widths := make([]int, 0, len(requested))
	for _, width := range requested {
		if width <= sourceWidth {
			widths = append(widths, width)
		}
	}
	if len(widths) == 0 || widths[len(widths)-1] < sourceWidth && sourceWidth < requested[len(requested)-1] {
		widths = append(widths, sourceWidth)
	}
	sort.Ints(widths)
	return widths
}

func buildVariants(body []byte, class string) (int, int, []Variant, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("inspect image for variants: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxSourcePixels {
		return 0, 0, nil, fmt.Errorf("image dimensions %dx%d exceed the safety limit", config.Width, config.Height)
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("decode image for variants: %w", err)
	}
	bounds := source.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return 0, 0, nil, fmt.Errorf("image has invalid dimensions")
	}
	variants := make([]Variant, 0, 6)
	for _, width := range variantWidths(class, bounds.Dx()) {
		height := max(1, int(float64(bounds.Dy())*float64(width)/float64(bounds.Dx())+0.5))
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(resized, resized.Bounds(), source, bounds, draw.Over, nil)
		for _, format := range []string{"webp", "avif"} {
			var encoded bytes.Buffer
			switch format {
			case "webp":
				err = webp.Encode(&encoded, resized, webp.Options{Quality: 82, Method: 4})
			case "avif":
				err = avif.Encode(&encoded, resized, avif.Options{Quality: 62, QualityAlpha: 70, Speed: 8})
			}
			if err != nil {
				return 0, 0, nil, fmt.Errorf("encode %s variant at %dpx: %w", format, width, err)
			}
			digest := sha256.Sum256(encoded.Bytes())
			variants = append(variants, Variant{
				Format: format, MediaType: "image/" + format,
				Width: width, Height: height, ByteSize: int64(encoded.Len()),
				Checksum: hex.EncodeToString(digest[:]),
				Body:     append([]byte(nil), encoded.Bytes()...),
			})
		}
	}
	return bounds.Dx(), bounds.Dy(), variants, nil
}
