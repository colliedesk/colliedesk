package media

import (
	"encoding/binary"
	"io"
	"math"
	"slices"
)

// EBML element ids WebM uses.
const (
	ebmlHeader    = 0x1A45DFA3
	ebmlDocType   = 0x4282
	ebmlSegment   = 0x18538067
	ebmlInfo      = 0x1549A966
	ebmlScale     = 0x2AD7B1
	ebmlDuration  = 0x4489
	ebmlTitle     = 0x7BA9
	ebmlTracks    = 0x1654AE6B
	ebmlEntry     = 0xAE
	ebmlTrackType = 0x83
	ebmlCodecID   = 0x86
	ebmlVideo     = 0xE0
	ebmlWidth     = 0xB0
	ebmlHeight    = 0xBA
	ebmlTags      = 0x1254C367
	ebmlAttached  = 0x1941A469
	ebmlCluster   = 0x1F43B675
	ebmlVoid      = 0xEC
)

type ebmlElement struct {
	id               uint32
	start, body, end int64
	unknownSize      bool
}

type ebmlReader struct {
	file     io.ReaderAt
	size     int64
	elements int
}

// vint reads an EBML variable-length integer at at: its value, its length, and whether every value
// bit is set (an unknown size).
func (r *ebmlReader) vint(at int64, keepMarker bool) (uint64, int, bool, error) {
	first := make([]byte, 1)
	if _, err := r.file.ReadAt(first, at); err != nil {
		return 0, 0, false, ErrVideoDamaged
	}
	length := 1
	for mask := byte(0x80); length <= 8 && first[0]&mask == 0; mask >>= 1 {
		length++
	}
	if length > 8 {
		return 0, 0, false, ErrVideoDamaged
	}
	raw := make([]byte, length)
	if _, err := r.file.ReadAt(raw, at); err != nil {
		return 0, 0, false, ErrVideoDamaged
	}
	value := uint64(raw[0])
	if !keepMarker {
		value &= uint64(0xFF >> length)
	}
	all := value == uint64(0xFF>>length)
	for _, b := range raw[1:] {
		value = value<<8 | uint64(b)
		all = all && b == 0xFF
	}
	return value, length, all && !keepMarker, nil
}

func (r *ebmlReader) element(at, end int64) (ebmlElement, error) {
	r.elements++
	if r.elements > maxBoxes {
		return ebmlElement{}, ErrVideoDamaged
	}
	id, idLength, _, err := r.vint(at, true)
	if err != nil || idLength > 4 {
		return ebmlElement{}, ErrVideoDamaged
	}
	size, sizeLength, unknown, err := r.vint(at+int64(idLength), false)
	if err != nil {
		return ebmlElement{}, err
	}
	body := at + int64(idLength+sizeLength)
	element := ebmlElement{id: uint32(id), start: at, body: body, unknownSize: unknown}
	if unknown {
		element.end = end
	} else {
		if size > uint64(math.MaxInt64) || body+int64(size) > end {
			return ebmlElement{}, ErrVideoDamaged
		}
		element.end = body + int64(size)
	}
	return element, nil
}

func (r *ebmlReader) bytes(e ebmlElement, limit int64) ([]byte, error) {
	if e.end-e.body > limit {
		return nil, ErrVideoDamaged
	}
	buf := make([]byte, e.end-e.body)
	if _, err := r.file.ReadAt(buf, e.body); err != nil {
		return nil, ErrVideoDamaged
	}
	return buf, nil
}

func (r *ebmlReader) uint(e ebmlElement) (uint64, error) {
	raw, err := r.bytes(e, 8)
	if err != nil {
		return 0, err
	}
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return value, nil
}

// children calls visit with each element between start and end.
func (r *ebmlReader) children(start, end int64, visit func(ebmlElement) (bool, error)) error {
	for at := start; at < end; {
		e, err := r.element(at, end)
		if err != nil {
			return err
		}
		more, err := visit(e)
		if err != nil || !more {
			return err
		}
		at = e.end
	}
	return nil
}

// walkWebM reads a WebM file and returns what InspectVideo reports and the Tags and Attachments
// elements NeutraliseVideo turns into Void. It stops at a cluster of unknown size (a recording
// streamed by a browser), since what follows cannot be found without reading every block; browsers
// write no tags there.
func walkWebM(file io.ReaderAt, size int64) (Video, []ebmlElement, error) {
	r := &ebmlReader{file: file, size: size}
	info := Video{ContentType: "video/webm"}
	header, err := r.element(0, size)
	if err != nil || header.id != ebmlHeader {
		return Video{}, nil, ErrNotVideo
	}
	docType := ""
	if err := r.children(header.body, header.end, func(e ebmlElement) (bool, error) {
		if e.id == ebmlDocType {
			raw, err := r.bytes(e, 32)
			docType = string(raw)
			return true, err
		}
		return true, nil
	}); err != nil {
		return Video{}, nil, err
	}
	if docType != "webm" {
		return Video{}, nil, ErrNotVideo
	}
	segment, err := r.element(header.end, size)
	if err != nil || segment.id != ebmlSegment {
		return Video{}, nil, ErrVideoDamaged
	}
	var voids []ebmlElement
	scale, duration := uint64(1_000_000), -1.0
	video := false
	err = r.children(segment.body, segment.end, func(e ebmlElement) (bool, error) {
		switch e.id {
		case ebmlInfo:
			return true, r.children(e.body, e.end, func(child ebmlElement) (bool, error) {
				switch child.id {
				case ebmlTitle:
					voids = append(voids, child)
				case ebmlScale:
					value, err := r.uint(child)
					scale = value
					return true, err
				case ebmlDuration:
					raw, err := r.bytes(child, 8)
					if err != nil {
						return false, err
					}
					switch len(raw) {
					case 4:
						duration = float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))
					case 8:
						duration = math.Float64frombits(binary.BigEndian.Uint64(raw))
					}
				}
				return true, nil
			})
		case ebmlTracks:
			return true, r.children(e.body, e.end, func(entry ebmlElement) (bool, error) {
				if entry.id != ebmlEntry {
					return true, nil
				}
				var kind uint64
				var codec string
				var width, height uint64
				if err := r.children(entry.body, entry.end, func(child ebmlElement) (bool, error) {
					var err error
					switch child.id {
					case ebmlTrackType:
						kind, err = r.uint(child)
					case ebmlCodecID:
						var raw []byte
						raw, err = r.bytes(child, 64)
						codec = string(raw)
					case ebmlVideo:
						err = r.children(child.body, child.end, func(dim ebmlElement) (bool, error) {
							var err error
							switch dim.id {
							case ebmlWidth:
								width, err = r.uint(dim)
							case ebmlHeight:
								height, err = r.uint(dim)
							}
							return true, err
						})
					}
					return true, err
				}); err != nil {
					return false, err
				}
				switch {
				case kind == 1 && codec == "V_MPEGH/ISO/HEVC":
					return false, ErrHEVC
				case (kind == 1 || kind == 2) && !webmCodec[codec]:
					return false, &UnsupportedCodecError{Codec: codec}
				case kind != 1 && kind != 2:
					// Subtitles or data live in blocks that cannot be blanked without reading them all.
					return false, ErrVideoUnsafeTrack
				}
				if kind == 1 && !video {
					video = true
					info.Width, info.Height = int(min(width, math.MaxInt32)), int(min(height, math.MaxInt32))
				}
				if !slices.Contains(info.Codecs, codec) {
					info.Codecs = append(info.Codecs, codec)
				}
				return true, nil
			})
		case ebmlTags, ebmlAttached:
			if e.unknownSize {
				return false, ErrVideoDamaged
			}
			voids = append(voids, e)
		case ebmlCluster:
			if e.unknownSize {
				return false, nil
			}
		}
		return !e.unknownSize, nil
	})
	if err != nil {
		return Video{}, nil, err
	}
	if !video || info.Width < 1 || info.Height < 1 {
		return Video{}, nil, ErrNotVideo
	}
	if duration > 0 && scale > 0 {
		if ms := duration * float64(scale) / 1e6; ms < math.MaxInt64/2 {
			value := int64(ms)
			info.DurationMS = &value
		}
	}
	return info, voids, nil
}

// voidEBML turns an element into a Void element of exactly the same length with zeroed content.
func voidEBML(file io.WriterAt, e ebmlElement) error {
	total := e.end - e.start
	if total < 2 {
		return ErrVideoDamaged
	}
	// One id byte, then the size as an 8-byte vint when it fits, else the shortest length that does.
	sizeLength := int64(8)
	if total < 9 {
		sizeLength = total - 1
	}
	content := total - 1 - sizeLength
	header := make([]byte, 1+sizeLength)
	header[0] = ebmlVoid
	value := uint64(content)
	for i := sizeLength; i >= 1; i-- {
		header[i] = byte(value)
		value >>= 8
	}
	header[1] |= byte(0x80 >> (sizeLength - 1))
	if _, err := file.WriteAt(header, e.start); err != nil {
		return err
	}
	return zeroRange(file, e.start+1+sizeLength, e.end)
}
