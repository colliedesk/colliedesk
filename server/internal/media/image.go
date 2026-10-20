// Package media reads the images (and later the videos) of the knowledge base: the type from the
// bytes, the size in pixels, the metadata a phone or camera leaves in a file, and a small thumbnail
// for the library. It never re-encodes an original: removing metadata drops whole segments or chunks,
// so the pixels, colors and animation stay as they were.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // registers the GIF decoder for DecodeConfig and Decode
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp" // registers the WebP decoder
)

const (
	// MaxImageSide is the most pixels an image may have on either side.
	MaxImageSide = 8192
	// MaxImagePixels is the most pixels an image may hold in all (40 MP), checked from the header
	// before anything is decoded, so a small file that would unpack into gigabytes is refused.
	MaxImagePixels = 40_000_000
)

// ImageTypes are the image types the knowledge base stores, as http.DetectContentType names them.
// SVG is never one: it can carry script.
var ImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Image is what InspectImage read. Width and Height are the size a browser shows: an EXIF orientation
// that turns the picture a quarter swaps them.
type Image struct {
	ContentType   string
	Width, Height int
	// Orientation is the EXIF orientation of a JPEG (1–8); 1 for every other image.
	Orientation int
	// bytesPerPixel estimates what decoding the whole image takes, for the thumbnail budget.
	bytesPerPixel int
}

var (
	// ErrNotImage means the bytes are not a PNG, JPEG, GIF or WebP image a decoder can read.
	ErrNotImage = errors.New("send the raw bytes of a PNG, JPEG, GIF or WebP image")
	// ErrImageTooLarge means the image has more pixels than the knowledge base takes.
	ErrImageTooLarge = fmt.Errorf("an image holds at most %d pixels on a side and %d megapixels", MaxImageSide, MaxImagePixels/1_000_000)
)

// InspectImage judges the type from the bytes and requires the decoder of that type to read the
// header too, so a file made to look like two types at once is refused. It checks the size limits
// without decoding the pixels.
func InspectImage(data []byte) (Image, error) {
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	if len(data) == 0 || !ImageTypes[contentType] {
		return Image{}, ErrNotImage
	}
	info := Image{ContentType: contentType, Orientation: 1, bytesPerPixel: 4}
	if contentType == "image/webp" {
		// The WebP decoder reads no animated or extended file; the container names the canvas.
		width, height, err := webpSize(data)
		if err != nil {
			return Image{}, ErrNotImage
		}
		info.Width, info.Height = width, height
	} else {
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || "image/"+format != contentType {
			return Image{}, ErrNotImage
		}
		info.Width, info.Height, info.bytesPerPixel = config.Width, config.Height, bytesPerPixel(config.ColorModel)
	}
	if info.Width < 1 || info.Height < 1 {
		return Image{}, ErrNotImage
	}
	if info.Width > MaxImageSide || info.Height > MaxImageSide || int64(info.Width)*int64(info.Height) > MaxImagePixels {
		return Image{}, ErrImageTooLarge
	}
	if contentType == "image/jpeg" {
		if components := jpegProgressive(data); components > 0 {
			// A progressive JPEG keeps a 4-byte coefficient for every sample of every component until
			// its last scan, on top of the picture: measured 515 MB for 6000 × 6000 against 103 MB baseline.
			info.bytesPerPixel = 3 + 4*components
		}
		info.Orientation = jpegOrientation(data)
		if info.Orientation >= 5 {
			info.Width, info.Height = info.Height, info.Width
		}
	}
	return info, nil
}

// bytesPerPixel is what Go's decoder holds per pixel for the color model a header names: 16-bit
// images take 8 bytes, a JPEG's YCbCr planes up to 3.
func bytesPerPixel(model color.Model) int {
	switch model {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.YCbCrModel:
		return 3
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := model.(color.Palette); ok {
		return 1
	}
	return 4
}
