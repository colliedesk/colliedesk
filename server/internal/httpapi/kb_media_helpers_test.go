package httpapi_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// pngImage is a real 2 × 2 PNG with no metadata, its color set by n: the upload reads it, strips
// nothing and stores it byte for byte, and a different n gives different bytes.
func pngImage(n byte) []byte {
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range picture.Pix {
		picture.Pix[i] = n
	}
	picture.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, picture); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// kbPNG is the knowledge base tests' image.
var kbPNG = pngImage(0)
