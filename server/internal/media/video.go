package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
)

// The videos the knowledge base takes are the ones every browser plays as they are, so nothing is
// converted: MP4 (and QuickTime .mov, the same format) with H.264 or AV1 video and AAC sound, and WebM
// with VP8, VP9 or AV1 video and Opus or Vorbis sound. The server reads the file's boxes or elements
// itself to judge it (http.DetectContentType names neither .mov nor an MP4 branded isom alone).

// Video is what InspectVideo read. Width and Height are the size shown: a track matrix that turns the
// picture a quarter swaps them. DurationMS is nil when the file does not say (a fragmented MP4 or a
// WebM recorded by a browser).
type Video struct {
	ContentType   string
	Width, Height int
	DurationMS    *int64
	// Codecs are the sample entry types (avc1, mp4a) or the WebM codec ids (V_VP9, A_OPUS).
	Codecs []string
}

var (
	// ErrNotVideo means the bytes are not an MP4 or WebM file the server reads.
	ErrNotVideo = errors.New("send an MP4 (H.264 or AV1 with AAC sound) or a WebM (VP8, VP9 or AV1 with Opus or Vorbis) video")
	// ErrHEVC means an H.265 (HEVC) video, the default of iPhones: browsers play it only on some devices.
	ErrHEVC = errors.New("this video is H.265 (HEVC), which many browsers cannot play; export it as H.264 (on an iPhone: Settings, Camera, Formats, Most Compatible) and upload that")
	// ErrUnsupportedCodec means a codec browsers do not all play; Codec names it.
	ErrUnsupportedCodec = errors.New("this video's codec is not one every browser plays")
	// ErrVideoDamaged means the structure ended early, contradicts itself or goes past the limits.
	ErrVideoDamaged = errors.New("the video file is damaged or too complex to check")
	// ErrVideoUnsafeTrack means a track of data (location, subtitles) the server cannot blank safely.
	ErrVideoUnsafeTrack = errors.New("the video carries a data track (location or text) that cannot be removed from this file; export it again without it")
)

// UnsupportedCodecError names the codec InspectVideo refused.
type UnsupportedCodecError struct{ Codec string }

func (e *UnsupportedCodecError) Error() string {
	return fmt.Sprintf("%s (%s); export it as H.264 MP4 or VP9 WebM", ErrUnsupportedCodec.Error(), e.Codec)
}
func (e *UnsupportedCodecError) Unwrap() error { return ErrUnsupportedCodec }

const (
	maxBoxDepth = 8
	maxBoxes    = 10000
	// maxSamples bounds the sample tables read: ten minutes at 60 frames a second is 36,000.
	maxSamples = 2_000_000
)

// mp4Brands are the ftyp brands of files browsers play as MP4: ISO base media, MP4, the H.264 and AV1
// brands, M4V and QuickTime (a screen recording of macOS).
var mp4Brands = map[string]bool{"isom": true, "iso2": true, "iso4": true, "iso5": true, "iso6": true, "mp41": true, "mp42": true,
	"avc1": true, "av01": true, "M4V ": true, "qt  ": true}

var (
	mp4Video  = map[string]bool{"avc1": true, "avc3": true, "av01": true}
	mp4Audio  = map[string]bool{"mp4a": true}
	mp4HEVC   = map[string]bool{"hvc1": true, "hev1": true, "dvh1": true, "dvhe": true}
	webmCodec = map[string]bool{"V_VP8": true, "V_VP9": true, "V_AV1": true, "A_OPUS": true, "A_VORBIS": true}
)

// InspectVideo judges a whole file of size bytes.
func InspectVideo(file io.ReaderAt, size int64) (Video, error) {
	head := make([]byte, 12)
	if size < 12 {
		return Video{}, ErrNotVideo
	}
	if _, err := file.ReadAt(head, 0); err != nil {
		return Video{}, ErrNotVideo
	}
	switch {
	case binary.BigEndian.Uint32(head) == 0x1A45DFA3:
		info, _, err := walkWebM(file, size)
		return info, err
	case string(head[4:8]) == "ftyp":
		walk, err := walkMP4(file, size)
		return walk.video, err
	}
	return Video{}, ErrNotVideo
}

// NeutraliseVideo removes, in place and without changing the file's size or any offset, what a camera
// or phone leaves in a video besides the picture and sound: MP4 udta, meta and uuid (XMP) boxes become
// free boxes with zeroed content; a track of data (GPS, subtitles, chapters) has its samples
// overwritten with zeros and its trak box becomes free; WebM Tags, Attachments and the title become
// Void with zeroed content. Players skip
// free and Void. Call it on a file InspectVideo accepted.
func NeutraliseVideo(file interface {
	io.ReaderAt
	io.WriterAt
}, size int64) error {
	head := make([]byte, 8)
	if _, err := file.ReadAt(head, 0); err != nil {
		return ErrNotVideo
	}
	if binary.BigEndian.Uint32(head) == 0x1A45DFA3 {
		_, voids, err := walkWebM(file, size)
		if err != nil {
			return err
		}
		for _, element := range voids {
			if err := voidEBML(file, element); err != nil {
				return err
			}
		}
		return nil
	}
	walk, err := walkMP4(file, size)
	if err != nil {
		return err
	}
	for _, b := range walk.freed {
		if _, err := file.WriteAt([]byte("free"), b.start+4); err != nil {
			return err
		}
		if err := zeroRange(file, b.body, b.end); err != nil {
			return err
		}
	}
	for _, blank := range walk.blanked {
		if err := zeroRange(file, blank[0], blank[1]); err != nil {
			return err
		}
	}
	return nil
}

func zeroRange(file io.WriterAt, start, end int64) error {
	zeros := make([]byte, min(end-start, 1<<20))
	for at := start; at < end; at += int64(len(zeros)) {
		n := min(int64(len(zeros)), end-at)
		if _, err := file.WriteAt(zeros[:n], at); err != nil {
			return err
		}
	}
	return nil
}

type box struct {
	kind             string
	start, body, end int64 // header start, payload start, end
}

type mp4Track struct {
	handler, codec string
	width, height  int
	turned         bool
	trak           box // freed when the track is one of data
	external       bool
	compact        bool // stz2
	sizes          []uint32
	sampleSize     uint32
	sampleCount    uint32
	chunks         []uint64
	perChunk       [][2]uint32 // first chunk, samples per chunk
	haveSizes      bool
	haveChunks     bool
	haveSampleToCh bool
	id             uint32
}

type mp4Walk struct {
	video     Video
	freed     []box      // boxes to turn into free boxes with their content zeroed
	blanked   [][2]int64 // sample ranges to zero
	mdat      [][2]int64
	fragments bool
}

type mp4Reader struct {
	file  io.ReaderAt
	boxes int
}

func (r *mp4Reader) read(at int64, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := r.file.ReadAt(buf, at); err != nil {
		return nil, ErrVideoDamaged
	}
	return buf, nil
}

// children lists the boxes between start and end.
func (r *mp4Reader) children(start, end int64) ([]box, error) {
	var boxes []box
	for at := start; at < end; {
		if end-at < 8 {
			if end-at == 0 {
				break
			}
			return nil, ErrVideoDamaged
		}
		r.boxes++
		if r.boxes > maxBoxes {
			return nil, ErrVideoDamaged
		}
		head, err := r.read(at, 8)
		if err != nil {
			return nil, err
		}
		size, body := int64(binary.BigEndian.Uint32(head)), at+8
		switch size {
		case 0:
			size = end - at
		case 1:
			large, err := r.read(at+8, 8)
			if err != nil {
				return nil, err
			}
			if big := binary.BigEndian.Uint64(large); big > math.MaxInt64 {
				return nil, ErrVideoDamaged
			} else {
				size = int64(big)
			}
			body = at + 16
		}
		if size < body-at || at+size > end {
			return nil, ErrVideoDamaged
		}
		boxes = append(boxes, box{kind: string(head[4:8]), start: at, body: body, end: at + size})
		at += size
	}
	return boxes, nil
}

// metadataBoxes are the boxes a camera or editor writes its tags, location and XMP in.
var metadataBoxes = map[string]bool{"udta": true, "meta": true, "uuid": true}

func walkMP4(file io.ReaderAt, size int64) (mp4Walk, error) {
	r := &mp4Reader{file: file}
	var walk mp4Walk
	top, err := r.children(0, size)
	if err != nil {
		return walk, err
	}
	if len(top) == 0 || top[0].kind != "ftyp" {
		return walk, ErrNotVideo
	}
	ftyp, err := r.read(top[0].body, int(min(top[0].end-top[0].body, 4096)))
	if err != nil || len(ftyp) < 8 {
		return walk, ErrNotVideo
	}
	branded := mp4Brands[string(ftyp[:4])]
	for at := 8; at+4 <= len(ftyp); at += 4 {
		branded = branded || mp4Brands[string(ftyp[at:at+4])]
	}
	if !branded {
		return walk, ErrNotVideo
	}
	walk.video.ContentType = "video/mp4"
	var tracks []*mp4Track
	var timescale, duration uint64
	moovs := 0
	for _, b := range top {
		switch {
		case metadataBoxes[b.kind]:
			walk.freed = append(walk.freed, b)
		case b.kind == "mdat":
			walk.mdat = append(walk.mdat, [2]int64{b.body, b.end})
		case b.kind == "moof":
			walk.fragments = true
		case b.kind == "moov":
			moovs++
			inner, err := r.children(b.body, b.end)
			if err != nil {
				return walk, err
			}
			for _, child := range inner {
				switch {
				case metadataBoxes[child.kind]:
					walk.freed = append(walk.freed, child)
				case child.kind == "mvex":
					walk.fragments = true
				case child.kind == "mvhd":
					payload, err := r.read(child.body, int(min(child.end-child.body, 40)))
					if err != nil || len(payload) < 20 {
						return walk, ErrVideoDamaged
					}
					if payload[0] == 1 {
						if len(payload) < 32 {
							return walk, ErrVideoDamaged
						}
						timescale, duration = uint64(binary.BigEndian.Uint32(payload[20:])), binary.BigEndian.Uint64(payload[24:])
					} else {
						timescale, duration = uint64(binary.BigEndian.Uint32(payload[12:])), uint64(binary.BigEndian.Uint32(payload[16:]))
					}
				case child.kind == "trak":
					track, err := r.track(child, &walk, 2)
					if err != nil {
						return walk, err
					}
					tracks = append(tracks, track)
				}
			}
		}
	}
	if moovs != 1 {
		return walk, ErrVideoDamaged
	}
	if timescale > 0 && duration > 0 && duration < math.MaxInt64/1000 {
		ms := int64(duration * 1000 / timescale)
		walk.video.DurationMS = &ms
	}
	return walk, r.judge(&walk, tracks, size)
}

func (r *mp4Reader) track(trak box, walk *mp4Walk, depth int) (*mp4Track, error) {
	track := &mp4Track{trak: trak}
	var visit func(b box, depth int) error
	visit = func(b box, depth int) error {
		if depth > maxBoxDepth {
			return ErrVideoDamaged
		}
		inner, err := r.children(b.body, b.end)
		if err != nil {
			return err
		}
		for _, child := range inner {
			switch child.kind {
			case "udta", "meta", "uuid":
				if b.kind == "trak" {
					walk.freed = append(walk.freed, child)
				}
			case "mdia", "minf", "stbl", "dinf", "edts":
				if err := visit(child, depth+1); err != nil {
					return err
				}
			case "tkhd":
				if err := r.tkhd(child, track); err != nil {
					return err
				}
			case "hdlr":
				// QuickTime puts a data handler (alis, url) under minf too; the media handler is mdia's.
				if b.kind != "mdia" {
					continue
				}
				payload, err := r.read(child.body, 12)
				if err != nil {
					return err
				}
				track.handler = string(payload[8:12])
			case "dref":
				if err := r.dref(child, track); err != nil {
					return err
				}
			case "stsd":
				payload, err := r.read(child.body, 16)
				if err != nil {
					return err
				}
				if binary.BigEndian.Uint32(payload[4:]) > 0 {
					track.codec = string(payload[12:16])
				}
			case "stsz":
				if err := r.stsz(child, track); err != nil {
					return err
				}
			case "stz2":
				track.compact = true
			case "stsc":
				if err := r.stsc(child, track); err != nil {
					return err
				}
			case "stco", "co64":
				if err := r.chunkOffsets(child, track); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return track, visit(trak, depth)
}

func (r *mp4Reader) tkhd(b box, track *mp4Track) error {
	payload, err := r.read(b.body, int(min(b.end-b.body, 96)))
	if err != nil {
		return err
	}
	// version 0: flags, creation, modification, id, reserved, duration (4 bytes each); version 1 has
	// 8-byte times and duration. Then 8 reserved, layer, group, volume, reserved, the matrix and size.
	idAt, matrixAt := 12, 40
	if payload[0] == 1 {
		idAt, matrixAt = 20, 52
	}
	if len(payload) < matrixAt+44 {
		return ErrVideoDamaged
	}
	track.id = binary.BigEndian.Uint32(payload[idAt:])
	a, b2 := int32(binary.BigEndian.Uint32(payload[matrixAt:])), int32(binary.BigEndian.Uint32(payload[matrixAt+4:]))
	d := int32(binary.BigEndian.Uint32(payload[matrixAt+16:]))
	track.turned = a == 0 && d == 0 && b2 != 0
	track.width = int(binary.BigEndian.Uint32(payload[matrixAt+36:]) >> 16)
	track.height = int(binary.BigEndian.Uint32(payload[matrixAt+40:]) >> 16)
	return nil
}

func (r *mp4Reader) dref(b box, track *mp4Track) error {
	entries, err := r.children(b.body+8, b.end)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		flags, err := r.read(entry.body, 4)
		if err != nil {
			return err
		}
		// Flag 1 means the data is in this file; anything else points at another file.
		if flags[3]&1 == 0 {
			track.external = true
		}
	}
	return nil
}

func (r *mp4Reader) stsz(b box, track *mp4Track) error {
	head, err := r.read(b.body, 12)
	if err != nil {
		return err
	}
	track.sampleSize, track.sampleCount, track.haveSizes = binary.BigEndian.Uint32(head[4:]), binary.BigEndian.Uint32(head[8:]), true
	if track.sampleCount > maxSamples {
		return ErrVideoDamaged
	}
	if track.sampleSize != 0 {
		return nil
	}
	if b.body+12+int64(track.sampleCount)*4 > b.end {
		return ErrVideoDamaged
	}
	table, err := r.read(b.body+12, int(track.sampleCount)*4)
	if err != nil {
		return err
	}
	track.sizes = make([]uint32, track.sampleCount)
	for i := range track.sizes {
		track.sizes[i] = binary.BigEndian.Uint32(table[i*4:])
	}
	return nil
}

func (r *mp4Reader) stsc(b box, track *mp4Track) error {
	head, err := r.read(b.body, 8)
	if err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(head[4:])
	if count > maxSamples || b.body+8+int64(count)*12 > b.end {
		return ErrVideoDamaged
	}
	table, err := r.read(b.body+8, int(count)*12)
	if err != nil {
		return err
	}
	track.haveSampleToCh = true
	for i := range int(count) {
		track.perChunk = append(track.perChunk, [2]uint32{binary.BigEndian.Uint32(table[i*12:]), binary.BigEndian.Uint32(table[i*12+4:])})
	}
	return nil
}

func (r *mp4Reader) chunkOffsets(b box, track *mp4Track) error {
	head, err := r.read(b.body, 8)
	if err != nil {
		return err
	}
	count, width := binary.BigEndian.Uint32(head[4:]), int64(4)
	if b.kind == "co64" {
		width = 8
	}
	if count > maxSamples || b.body+8+int64(count)*width > b.end {
		return ErrVideoDamaged
	}
	table, err := r.read(b.body+8, int(int64(count)*width))
	if err != nil {
		return err
	}
	track.haveChunks = true
	for i := range int(count) {
		if width == 8 {
			track.chunks = append(track.chunks, binary.BigEndian.Uint64(table[i*8:]))
		} else {
			track.chunks = append(track.chunks, uint64(binary.BigEndian.Uint32(table[i*4:])))
		}
	}
	return nil
}

// samples returns the byte ranges of a track's samples, from its chunk offsets, samples per chunk and
// sample sizes.
func (t *mp4Track) samples() ([][2]int64, error) {
	if !t.haveSizes || !t.haveChunks || !t.haveSampleToCh || t.compact {
		return nil, ErrVideoUnsafeTrack
	}
	var ranges [][2]int64
	sample := uint32(0)
	for i, offset := range t.chunks {
		chunk := uint32(i + 1)
		perChunk := uint32(0)
		for _, entry := range t.perChunk {
			if entry[0] <= chunk {
				perChunk = entry[1]
			}
		}
		at := int64(offset)
		for range perChunk {
			if sample >= t.sampleCount {
				return ranges, nil
			}
			size := t.sampleSize
			if size == 0 {
				size = t.sizes[sample]
			}
			ranges = append(ranges, [2]int64{at, at + int64(size)})
			at += int64(size)
			sample++
			if len(ranges) > maxSamples {
				return nil, ErrVideoDamaged
			}
		}
	}
	return ranges, nil
}

// judge checks the tracks: one video track in a codec browsers play, sound only in AAC, and every
// other track one whose samples can be blanked (a whole file, unfragmented, its samples inside mdat and
// apart from the picture and sound).
func (r *mp4Reader) judge(walk *mp4Walk, tracks []*mp4Track, size int64) error {
	var av [][2]int64
	var data []*mp4Track
	video := false
	for _, track := range tracks {
		if track.external {
			return ErrVideoUnsafeTrack
		}
		switch track.handler {
		case "vide":
			switch {
			case mp4HEVC[track.codec]:
				return ErrHEVC
			case !mp4Video[track.codec]:
				return &UnsupportedCodecError{Codec: track.codec}
			}
			if !video {
				video = true
				walk.video.Width, walk.video.Height = track.width, track.height
				if track.turned {
					walk.video.Width, walk.video.Height = track.height, track.width
				}
			}
		case "soun":
			if !mp4Audio[track.codec] {
				return &UnsupportedCodecError{Codec: track.codec}
			}
		case "tmcd":
		default:
			data = append(data, track)
			continue
		}
		if !slices.Contains(walk.video.Codecs, track.codec) && track.codec != "" && track.handler != "tmcd" {
			walk.video.Codecs = append(walk.video.Codecs, track.codec)
		}
		// The picture and sound samples, which a blanked data sample may never touch.
		if ranges, err := track.samples(); err == nil {
			av = append(av, ranges...)
		}
	}
	if !video || walk.video.Width < 1 || walk.video.Height < 1 {
		return ErrNotVideo
	}
	if len(data) == 0 {
		return nil
	}
	if walk.fragments {
		return ErrVideoUnsafeTrack
	}
	sort.Slice(av, func(i, j int) bool { return av[i][0] < av[j][0] })
	for _, track := range data {
		ranges, err := track.samples()
		if err != nil {
			return err
		}
		for _, sample := range ranges {
			if sample[1] <= sample[0] {
				continue
			}
			if !inside(walk.mdat, sample) || overlaps(av, sample) || sample[1] > size {
				return ErrVideoUnsafeTrack
			}
			walk.blanked = append(walk.blanked, sample)
		}
		walk.freed = append(walk.freed, track.trak)
	}
	return nil
}

func inside(spans [][2]int64, r [2]int64) bool {
	for _, span := range spans {
		if r[0] >= span[0] && r[1] <= span[1] {
			return true
		}
	}
	return false
}

// overlaps reports whether r overlaps a range of sorted.
func overlaps(sorted [][2]int64, r [2]int64) bool {
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i][1] > r[0] })
	return i < len(sorted) && sorted[i][0] < r[1]
}
