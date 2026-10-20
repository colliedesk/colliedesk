package media

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// errMalformed means a file's structure ended early or contradicts itself; the upload is refused
// rather than stored with metadata the stripper could not reach.
var errMalformed = errors.New("the image file is damaged")

// StripMetadata returns the image without the metadata a camera, phone or editor leaves in it
// (location, camera, dates, comments, XMP, IPTC), removing whole segments or chunks so the pixels
// are untouched:
//
//   - JPEG: APP1 (EXIF, XMP), APP3–APP13, APP15 and comments go; APP0 (JFIF), APP2 (ICC color
//     profile) and APP14 (Adobe color transform) stay. An orientation other than 1 is written back
//     as a minimal EXIF segment holding only it, so a phone photo is not shown sideways.
//   - PNG: only the chunks that draw the picture stay (pngKept); text, EXIF, time and the rest go.
//   - WebP: only the picture, transparency, animation and ICC chunks stay; EXIF and XMP go, with their
//     VP8X flags and the RIFF size fixed.
//   - GIF: comments and application extensions other than the loop count go; frames stay.
func StripMetadata(info Image, data []byte) ([]byte, error) {
	switch info.ContentType {
	case "image/jpeg":
		return stripJPEG(data, info.Orientation)
	case "image/png":
		return stripPNG(data)
	case "image/webp":
		return stripWebP(data)
	case "image/gif":
		return stripGIF(data)
	}
	return nil, ErrNotImage
}

// jpegSegments calls visit with each marker segment before the scan (its marker and payload,
// without the length bytes) and returns the offset of the start-of-scan marker.
func jpegSegments(data []byte, visit func(marker byte, payload []byte)) (int, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, errMalformed
	}
	at := 2
	for {
		if at+4 > len(data) || data[at] != 0xFF {
			return 0, errMalformed
		}
		marker := data[at+1]
		if marker == 0xFF { // fill byte
			at++
			continue
		}
		if marker == 0xDA {
			return at, nil
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) { // segments without a length
			at += 2
			continue
		}
		length := int(binary.BigEndian.Uint16(data[at+2:]))
		if length < 2 || at+2+length > len(data) {
			return 0, errMalformed
		}
		visit(marker, data[at+4:at+2+length])
		at += 2 + length
	}
}

func stripJPEG(data []byte, orientation int) ([]byte, error) {
	out := make([]byte, 0, len(data))
	out = append(out, 0xFF, 0xD8)
	// The orientation goes right after JFIF, or first, where readers look for EXIF.
	pending, first := orientation != 1, true
	scan, err := jpegSegments(data, func(marker byte, payload []byte) {
		if pending && first && marker != 0xE0 {
			out, pending = append(out, orientationSegment(orientation)...), false
		}
		first = false
		if jpegMetadata(marker, payload) {
			return
		}
		out = append(out, 0xFF, marker, byte((len(payload)+2)>>8), byte(len(payload)+2))
		out = append(out, payload...)
		if pending && marker == 0xE0 {
			out, pending = append(out, orientationSegment(orientation)...), false
		}
	})
	if err != nil {
		return nil, err
	}
	if pending {
		out = append(out, orientationSegment(orientation)...)
	}
	if err := jpegEnd(data, scan, func(segment []byte) {
		if !jpegMetadata(segment[1], segment[4:]) {
			out = append(out, segment...)
		}
	}, func(scanData []byte) { out = append(out, scanData...) }); err != nil {
		return nil, err
	}
	// Nothing after the end of the image stays: phones put a motion photo's video (with its own
	// location), more pictures with their own EXIF, or their maker's trailer there.
	return append(out, 0xFF, 0xD9), nil
}

// jpegMetadata is true for a segment StripMetadata removes: comments, APP1 (EXIF, XMP), APP3–APP13,
// APP15, and an APP2 holding the Multi-Picture index of images after the end (an ICC profile stays).
func jpegMetadata(marker byte, payload []byte) bool {
	switch {
	case marker == 0xFE:
		return true
	case marker == 0xE2:
		return bytes.HasPrefix(payload, []byte("MPF\x00"))
	case marker >= 0xE1 && marker <= 0xEF:
		return marker != 0xEE
	}
	return false
}

// jpegEnd walks a JPEG from its first start of scan to its end-of-image marker: segment gets each
// marker segment between scans (a progressive image has tables and scans there), scan the
// entropy-coded bytes. It stops at the end-of-image marker.
func jpegEnd(data []byte, at int, segment func([]byte), scan func([]byte)) error {
	for {
		if at+2 > len(data) || data[at] != 0xFF {
			return errMalformed
		}
		marker := data[at+1]
		switch {
		case marker == 0xFF: // fill byte
			at++
			continue
		case marker == 0xD9:
			return nil
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			scan(data[at : at+2])
			at += 2
			continue
		}
		if at+4 > len(data) {
			return errMalformed
		}
		length := int(binary.BigEndian.Uint16(data[at+2:]))
		if length < 2 || at+2+length > len(data) {
			return errMalformed
		}
		segment(data[at : at+2+length])
		at += 2 + length
		if marker != 0xDA {
			continue
		}
		// Entropy-coded data runs to the next marker: FF 00 is a stuffed byte and FF D0–D7 a restart.
		start := at
		for {
			if at+1 >= len(data) {
				return errMalformed
			}
			if data[at] == 0xFF && data[at+1] != 0x00 && (data[at+1] < 0xD0 || data[at+1] > 0xD7) {
				break
			}
			if data[at] == 0xFF {
				at += 2
			} else {
				at++
			}
		}
		scan(data[start:at])
	}
}

// orientationSegment is an APP1 EXIF segment holding only the orientation tag.
func orientationSegment(orientation int) []byte {
	segment := []byte{0xFF, 0xE1, 0, 34, 'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42, 0, 0, 0, 8, // big-endian TIFF header, IFD0 at 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(orientation), 0, 0, // Orientation, SHORT, 1 value
		0, 0, 0, 0} // no next IFD
	return segment
}

// jpegOrientation reads the EXIF orientation of a JPEG; 1 when it has none or it is unreadable.
func jpegOrientation(data []byte) int {
	orientation := 1
	found := false
	_, _ = jpegSegments(data, func(marker byte, payload []byte) {
		if found || marker != 0xE1 || !bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			return
		}
		found = true
		if value := exifOrientation(payload[6:]); value >= 1 && value <= 8 {
			orientation = value
		}
	})
	return orientation
}

// jpegProgressive returns the number of components of a progressive JPEG (SOF2, SOF6, SOF10 or
// SOF14), and 0 for any other.
func jpegProgressive(data []byte) int {
	components := 0
	_, _ = jpegSegments(data, func(marker byte, payload []byte) {
		if (marker == 0xC2 || marker == 0xC6 || marker == 0xCA || marker == 0xCE) && len(payload) >= 6 {
			components = int(payload[5])
		}
	})
	return components
}

// exifOrientation reads tag 0x0112 of IFD0 in a TIFF structure.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[ifd:]))
	for i := range count {
		entry := ifd + 2 + 12*i
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:]) == 0x0112 && order.Uint16(tiff[entry+2:]) == 3 {
			return int(order.Uint16(tiff[entry+8:]))
		}
	}
	return 0
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// pngKept are the chunks a PNG keeps: what draws the picture (with its color space, transparency,
// background, density and animation). Every other chunk goes: text, EXIF, time, C2PA manifests
// (caBX) and whatever an editor adds next.
var pngKept = map[string]bool{"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true, "tRNS": true, "gAMA": true, "cHRM": true,
	"sRGB": true, "iCCP": true, "sBIT": true, "bKGD": true, "pHYs": true, "cICP": true, "mDCv": true, "cLLi": true,
	"acTL": true, "fcTL": true, "fdAT": true}

func stripPNG(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, errMalformed
	}
	out := make([]byte, 0, len(data))
	out = append(out, pngSignature...)
	at := len(pngSignature)
	for {
		if at+12 > len(data) {
			return nil, errMalformed
		}
		length := int64(binary.BigEndian.Uint32(data[at:]))
		end := int64(at) + 12 + length
		if length > 0x7FFFFFFF || end > int64(len(data)) {
			return nil, errMalformed
		}
		kind := string(data[at+4 : at+8])
		if pngKept[kind] {
			out = append(out, data[at:end]...)
		}
		at = int(end)
		if kind == "IEND" {
			return out, nil
		}
	}
}

// webpChunks calls visit with each chunk of a WebP file: its FourCC, its payload and the chunk
// bytes with header and padding.
func webpChunks(data []byte, visit func(fourCC string, payload, chunk []byte)) error {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return errMalformed
	}
	size := int64(binary.LittleEndian.Uint32(data[4:]))
	if size+8 > int64(len(data)) || size < 4 {
		return errMalformed
	}
	body := data[:size+8]
	at := 12
	for at < len(body) {
		if at+8 > len(body) {
			return errMalformed
		}
		length := int64(binary.LittleEndian.Uint32(body[at+4:]))
		end := int64(at) + 8 + length
		if end > int64(len(body)) {
			return errMalformed
		}
		padded := end + length%2
		if padded > int64(len(body)) {
			padded = end
		}
		visit(string(body[at:at+4]), body[at+8:end], body[at:padded])
		at = int(padded)
	}
	return nil
}

// webpSize reads the canvas of a WebP image from VP8X, or the frame size of a simple VP8 or VP8L one.
func webpSize(data []byte) (int, int, error) {
	width, height := 0, 0
	err := webpChunks(data, func(fourCC string, payload, _ []byte) {
		if width > 0 {
			return
		}
		switch {
		case fourCC == "VP8X" && len(payload) >= 10:
			width, height = 1+(int(payload[4])|int(payload[5])<<8|int(payload[6])<<16), 1+(int(payload[7])|int(payload[8])<<8|int(payload[9])<<16)
		case fourCC == "VP8 " && len(payload) >= 10 && payload[3] == 0x9D && payload[4] == 0x01 && payload[5] == 0x2A:
			width, height = int(binary.LittleEndian.Uint16(payload[6:]))&0x3FFF, int(binary.LittleEndian.Uint16(payload[8:]))&0x3FFF
		case fourCC == "VP8L" && len(payload) >= 5 && payload[0] == 0x2F:
			bits := binary.LittleEndian.Uint32(payload[1:])
			width, height = int(bits&0x3FFF)+1, int(bits>>14&0x3FFF)+1
		}
	})
	if err != nil || width == 0 || height == 0 {
		return 0, 0, errMalformed
	}
	return width, height, nil
}

// webpKept are the chunks a WebP keeps: the picture, its transparency, animation and color profile.
var webpKept = map[string]bool{"VP8 ": true, "VP8L": true, "VP8X": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true}

func stripWebP(data []byte) ([]byte, error) {
	out := make([]byte, 12, len(data))
	copy(out, data[:12])
	err := webpChunks(data, func(fourCC string, _, chunk []byte) {
		switch {
		case !webpKept[fourCC]:
			return
		case fourCC == "VP8X":
			start := len(out)
			out = append(out, chunk...)
			if len(chunk) > 8 {
				out[start+8] &^= 0x08 | 0x04 // the EXIF and XMP flags
			}
			return
		}
		out = append(out, chunk...)
	})
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out, nil
}

func stripGIF(data []byte) ([]byte, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return nil, errMalformed
	}
	at := 13
	if data[10]&0x80 != 0 {
		at += 3 << (int(data[10]&0x07) + 1)
	}
	if at > len(data) {
		return nil, errMalformed
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:at]...)
	// subBlocks returns the end of the data sub-blocks starting at offset.
	subBlocks := func(offset int) (int, error) {
		for {
			if offset >= len(data) {
				return 0, errMalformed
			}
			size := int(data[offset])
			offset++
			if size == 0 {
				return offset, nil
			}
			offset += size
		}
	}
	for {
		if at >= len(data) {
			return nil, errMalformed
		}
		switch data[at] {
		case 0x3B: // trailer
			return append(out, 0x3B), nil
		case 0x21: // extension
			if at+2 > len(data) {
				return nil, errMalformed
			}
			label := data[at+1]
			end, err := subBlocks(at + 2)
			if err != nil {
				return nil, err
			}
			keep := label == 0xF9 || label == 0x01
			if label == 0xFF && at+3+11 <= len(data) && data[at+2] == 11 {
				name := string(data[at+3 : at+3+11])
				keep = name == "NETSCAPE2.0" || name == "ANIMEXTS1.0"
			}
			if keep {
				out = append(out, data[at:end]...)
			}
			at = end
		case 0x2C: // image descriptor
			start := at
			if at+10 > len(data) {
				return nil, errMalformed
			}
			flags := data[at+9]
			at += 10
			if flags&0x80 != 0 {
				at += 3 << (int(flags&0x07) + 1)
			}
			at++ // LZW minimum code size
			end, err := subBlocks(at)
			if err != nil {
				return nil, err
			}
			out = append(out, data[start:end]...)
			at = end
		default:
			return nil, errMalformed
		}
	}
}
