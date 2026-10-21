package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// The files in testdata were made with Pillow: a 32 × 16 picture, red on the left and blue on the
// right, carrying the camera make SECRETCAM, a GPS position, XMP and comments with SECRET in them.
func testImage(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStripMetadataRemovesWhatCamerasLeaveAndKeepsThePicture(t *testing.T) {
	for _, test := range []struct {
		file, contentType string
		width, height     int
	}{
		{"photo-gps.jpg", "image/jpeg", 16, 32}, // orientation 6: shown turned a quarter
		{"plain.jpg", "image/jpeg", 32, 16},
		{"progressive.jpg", "image/jpeg", 32, 16}, // orientation 3: turned half, same size
		{"text.png", "image/png", 32, 16},
		{"comment.gif", "image/gif", 16, 16},
		{"exif.webp", "image/webp", 32, 16},
		{"animated.webp", "image/webp", 32, 16},
		{"lossless.webp", "image/webp", 32, 16},
	} {
		t.Run(test.file, func(t *testing.T) {
			data := testImage(t, test.file)
			info, err := InspectImage(data)
			if err != nil {
				t.Fatal(err)
			}
			if info.ContentType != test.contentType || info.Width != test.width || info.Height != test.height {
				t.Fatalf("inspect = %s %d×%d, want %s %d×%d", info.ContentType, info.Width, info.Height, test.contentType, test.width, test.height)
			}
			stripped, err := StripMetadata(info, data)
			if err != nil {
				t.Fatal(err)
			}
			// SECRETCAM sits in the EXIF block with the GPS position, so the position went with it.
			if bytes.Contains(stripped, []byte("SECRET")) {
				t.Errorf("metadata is still in the stripped file: %q", stripped)
			}
			again, err := InspectImage(stripped)
			if err != nil || again.Width != info.Width || again.Height != info.Height || again.Orientation != info.Orientation {
				t.Fatalf("stripped inspect = %+v (%v), want %+v", again, err, info)
			}
			if test.file != "animated.webp" {
				if _, _, err := image.Decode(bytes.NewReader(stripped)); err != nil {
					t.Fatalf("stripped file does not decode: %v", err)
				}
			}
		})
	}
}

// motionPhoto is plain.jpg the way a phone writes a motion photo: a Multi-Picture index in APP2 and
// a video (with a location box) after the end of the image.
func motionPhoto(t testing.TB) []byte {
	data := testImage(t, "plain.jpg")
	mpf := append([]byte{0xFF, 0xE2, 0, 12}, []byte("MPF\x00SECRET")...)
	out := append(append(append([]byte{}, data[:2]...), mpf...), data[2:]...)
	return append(out, []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00udta\xa9xyz+21.0285+105.8542/SECRET")...)
}

func TestStripMetadataDropsWhatFollowsTheEndOfAJPEG(t *testing.T) {
	data := motionPhoto(t)
	info, err := InspectImage(data)
	if err != nil {
		t.Fatal(err)
	}
	stripped, err := StripMetadata(info, data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stripped, []byte("SECRET")) || bytes.Contains(stripped, []byte("ftyp")) || !bytes.HasSuffix(stripped, []byte{0xFF, 0xD9}) {
		t.Fatalf("stripped motion photo still holds its index or video: %q", stripped[len(stripped)-40:])
	}
	if _, _, err := image.Decode(bytes.NewReader(stripped)); err != nil {
		t.Fatalf("stripped motion photo does not decode: %v", err)
	}
	plain := testImage(t, "plain.jpg")
	if again, err := StripMetadata(info, plain); err != nil || !bytes.Equal(again, plain) {
		t.Fatalf("a JPEG with nothing to remove changed: %d → %d bytes (%v)", len(plain), len(again), err)
	}
}

// A chunk the stripper does not know, such as a C2PA manifest, goes too: PNG keeps only what draws.
func TestStripMetadataDropsUnknownPNGChunks(t *testing.T) {
	data := pngHeader(1, 1, 8, 6)
	chunk := append([]byte("caBX"), []byte("SECRET manifest")...)
	unknown := binary.BigEndian.AppendUint32(nil, uint32(len(chunk)-4))
	unknown = append(append(unknown, chunk...), binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(chunk))...)
	at := len(data) - 12 // before IEND
	data = append(append(append([]byte{}, data[:at]...), unknown...), data[at:]...)
	stripped, err := stripPNG(data)
	if err != nil || bytes.Contains(stripped, []byte("SECRET")) || !bytes.Equal(stripped, pngHeader(1, 1, 8, 6)) {
		t.Fatalf("stripped = %q (%v)", stripped, err)
	}
}

func TestStripMetadataOfAWebPClearsItsFlags(t *testing.T) {
	data := testImage(t, "exif.webp")
	info, _ := InspectImage(data)
	stripped, err := StripMetadata(info, data)
	if err != nil {
		t.Fatal(err)
	}
	if size := binary.LittleEndian.Uint32(stripped[4:]); int(size) != len(stripped)-8 {
		t.Fatalf("RIFF size %d, file %d", size, len(stripped))
	}
	err = webpChunks(stripped, func(fourCC string, payload, _ []byte) {
		if fourCC == "VP8X" && payload[0]&0x0C != 0 {
			t.Errorf("VP8X still flags EXIF or XMP: %08b", payload[0])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailIsTurnedAsTheOrientationSays(t *testing.T) {
	data := testImage(t, "photo-gps.jpg")
	info, _ := InspectImage(data)
	thumb, contentType, err := Thumbnail(context.Background(), info, data)
	if err != nil || contentType != "image/jpeg" {
		t.Fatalf("thumbnail = %s (%v)", contentType, err)
	}
	picture, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatal(err)
	}
	if bounds := picture.Bounds(); bounds.Dx() != 16 || bounds.Dy() != 32 {
		t.Fatalf("thumbnail %v, want 16×32", bounds)
	}
	// Turned a quarter clockwise, the red left half is on top.
	top, bottom := picture.At(8, 4), picture.At(8, 28)
	if r, _, b, _ := top.RGBA(); r < 0xC000 || b > 0x4000 {
		t.Errorf("top is %v, want red", top)
	}
	if r, _, b, _ := bottom.RGBA(); r > 0x4000 || b < 0xC000 {
		t.Errorf("bottom is %v, want blue", bottom)
	}
}

func TestThumbnailKeepsTransparencyAndFitsTheSide(t *testing.T) {
	data := testImage(t, "text.png")
	info, _ := InspectImage(data)
	if _, contentType, err := Thumbnail(context.Background(), info, data); err != nil || contentType != "image/png" {
		t.Fatalf("transparent thumbnail = %s (%v), want PNG", contentType, err)
	}
	if w, h := fit(4000, 1000, 320); w != 320 || h != 80 {
		t.Fatalf("fit = %d×%d", w, h)
	}
	if w, h := fit(100, 3000, 320); w != 10 || h != 320 {
		t.Fatalf("fit = %d×%d", w, h)
	}
}

// pngHeader is a PNG whose header names the size and bit depth; its pixels are missing, so only
// the header can be read.
func pngHeader(width, height uint32, depth, colorType byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8], ihdr[9] = depth, colorType
	chunk := append([]byte("IHDR"), ihdr...)
	out := append([]byte{}, pngSignature...)
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
	return append(out, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xAE, 0x42, 0x60, 0x82)
}

func TestInspectImageRefusesTooManyPixelsAndFilesOfTwoTypes(t *testing.T) {
	if _, err := InspectImage(pngHeader(MaxImageSide+1, 10, 8, 6)); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("wide image = %v", err)
	}
	if _, err := InspectImage(pngHeader(8000, 6000, 8, 6)); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("48 MP image = %v", err)
	}
	// PNG's signature with a damaged header: the sniffer says PNG, the decoder cannot read it.
	broken := pngHeader(10, 10, 8, 6)
	broken[20] ^= 0xFF
	if _, err := InspectImage(broken); !errors.Is(err, ErrNotImage) {
		t.Fatalf("damaged header = %v", err)
	}
	if _, err := InspectImage([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>")); !errors.Is(err, ErrNotImage) {
		t.Fatalf("SVG = %v", err)
	}
}

// A 16-bit 40-megapixel PNG decodes into about 305 MB, over the budget: it gets no thumbnail and
// is never decoded.
func TestThumbnailSkipsAnImageOverTheDecodeBudget(t *testing.T) {
	info, err := InspectImage(pngHeader(8000, 5000, 16, 6))
	if err != nil {
		t.Fatal(err)
	}
	if cost := DecodeCost(info); cost != 8000*5000*8 {
		t.Fatalf("cost = %d", cost)
	}
	if _, _, err := Thumbnail(context.Background(), info, nil); !errors.Is(err, ErrNoThumbnail) {
		t.Fatalf("thumbnail = %v, want ErrNoThumbnail", err)
	}
}

// A progressive JPEG decodes into its coefficients as well as its picture (515 MB for a 6000 × 6000
// one against 103 MB baseline), so its cost counts 4 more bytes per sample of each component.
func TestDecodeCostCountsTheCoefficientsOfAProgressiveJPEG(t *testing.T) {
	baseline, _ := InspectImage(testImage(t, "plain.jpg"))
	progressive, _ := InspectImage(testImage(t, "progressive.jpg"))
	if got := DecodeCost(baseline); got != 32*16*3 {
		t.Errorf("baseline cost = %d", got)
	}
	if got := DecodeCost(progressive); got != 32*16*15 {
		t.Errorf("progressive cost = %d, want 15 bytes a pixel", got)
	}
}

func FuzzImageMetadataStrip(f *testing.F) {
	for _, name := range []string{"photo-gps.jpg", "plain.jpg", "progressive.jpg", "text.png", "comment.gif", "exif.webp", "animated.webp", "lossless.webp"} {
		f.Add(testImage(f, name))
	}
	f.Add(motionPhoto(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := InspectImage(data)
		if err != nil {
			return
		}
		stripped, err := StripMetadata(info, data)
		if err != nil {
			return
		}
		again, err := InspectImage(stripped)
		if err != nil || again.Width != info.Width || again.Height != info.Height {
			t.Fatalf("stripped %+v (%v), was %+v", again, err, info)
		}
	})
}
