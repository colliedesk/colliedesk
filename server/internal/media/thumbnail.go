package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/sync/semaphore"
)

const (
	// ThumbnailSide is the longest side of a thumbnail: twice the library's card, for sharp screens.
	ThumbnailSide = 320
	// decodeBudget bounds the memory every thumbnail being made at once may take to decode its image.
	decodeBudget = 128 << 20
)

// ErrNoThumbnail means the image would take more memory to decode than the thumbnail budget, or it
// is a kind the decoder does not read (an animated WebP). The image is stored all the same; the
// library shows the original, scaled by the browser.
var ErrNoThumbnail = errors.New("no thumbnail for this image")

// decoding is shared by every upload and the backfill, weighted by the bytes a decode takes, so a
// 16-bit 40-megapixel PNG (about 305 MB decoded) never runs next to others on a small server.
var decoding = semaphore.NewWeighted(decodeBudget)

// DecodeCost estimates the memory decoding the whole image takes.
func DecodeCost(info Image) int64 {
	return int64(info.Width) * int64(info.Height) * int64(info.bytesPerPixel)
}

// Thumbnail makes a thumbnail of an image InspectImage read, with its longest side at most
// ThumbnailSide pixels, turned the way the EXIF orientation says: a JPEG, or a PNG when the image has
// transparency. It returns the bytes and their type.
func Thumbnail(ctx context.Context, info Image, data []byte) ([]byte, string, error) {
	cost := DecodeCost(info)
	if cost > decodeBudget {
		return nil, "", ErrNoThumbnail
	}
	if err := decoding.Acquire(ctx, cost); err != nil {
		return nil, "", err
	}
	defer decoding.Release(cost)
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrNoThumbnail
	}
	// Scale first, then turn: turning the small picture costs nothing.
	bounds := source.Bounds()
	stored := image.Pt(bounds.Dx(), bounds.Dy())
	width, height := fit(stored.X, stored.Y, ThumbnailSide)
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	if stored.X > 2*width && stored.Y > 2*height {
		// Two passes: a fast one to twice the size, then a sharp one; as good as CatmullRom alone and
		// several times faster on a large photo.
		middle := image.NewNRGBA(image.Rect(0, 0, 2*width, 2*height))
		draw.ApproxBiLinear.Scale(middle, middle.Bounds(), source, bounds, draw.Src, nil)
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), middle, middle.Bounds(), draw.Src, nil)
	} else {
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)
	}
	turned := orient(scaled, info.Orientation)
	var out bytes.Buffer
	if turned.Opaque() {
		err = jpeg.Encode(&out, turned, &jpeg.Options{Quality: 82})
		return out.Bytes(), "image/jpeg", err
	}
	err = png.Encode(&out, turned)
	return out.Bytes(), "image/png", err
}

// fit scales width × height down to fit a side × side square, never up.
func fit(width, height, side int) (int, int) {
	if width <= side && height <= side {
		return width, height
	}
	if width >= height {
		return side, max(1, height*side/width)
	}
	return max(1, width*side/height), side
}

// orient turns and mirrors a picture the way EXIF orientation 1–8 says it was taken.
func orient(source *image.NRGBA, orientation int) *image.NRGBA {
	if orientation < 2 || orientation > 8 {
		return source
	}
	width, height := source.Rect.Dx(), source.Rect.Dy()
	outWidth, outHeight := width, height
	if orientation >= 5 {
		outWidth, outHeight = height, width
	}
	out := image.NewNRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := range height {
		for x := range width {
			var tx, ty int
			switch orientation {
			case 2: // mirrored
				tx, ty = width-1-x, y
			case 3: // turned half
				tx, ty = width-1-x, height-1-y
			case 4: // mirrored upside down
				tx, ty = x, height-1-y
			case 5: // mirrored along the top-left diagonal
				tx, ty = y, x
			case 6: // turned a quarter clockwise to show
				tx, ty = height-1-y, x
			case 7: // mirrored along the other diagonal
				tx, ty = height-1-y, width-1-x
			case 8: // turned a quarter counterclockwise to show
				tx, ty = y, width-1-x
			}
			copy(out.Pix[out.PixOffset(tx, ty):out.PixOffset(tx, ty)+4], source.Pix[source.PixOffset(x, y):source.PixOffset(x, y)+4])
		}
	}
	return out
}
