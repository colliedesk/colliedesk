package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The files in testdata/video were made with ffmpeg: one second of a 64 × 48 test picture with a tone,
// carrying the title SECRETTITLE, the location +21.0285+105.8542 (QuickTime) or a subtitle track that
// says SECRETLOC.
func testVideo(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "video", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writable is a file in memory for NeutraliseVideo.
type writable struct{ data []byte }

func (w *writable) ReadAt(p []byte, at int64) (int, error) {
	if at >= int64(len(w.data)) {
		return 0, os.ErrInvalid
	}
	n := copy(p, w.data[at:])
	if n < len(p) {
		return n, os.ErrInvalid
	}
	return n, nil
}

func (w *writable) WriteAt(p []byte, at int64) (int, error) {
	if at+int64(len(p)) > int64(len(w.data)) {
		return 0, os.ErrInvalid
	}
	return copy(w.data[at:], p), nil
}

// decodes plays the whole file with ffmpeg when the machine has it.
func decodes(t *testing.T, data []byte) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Log("ffmpeg is not installed: the decode check is skipped")
		return
	}
	path := filepath.Join(t.TempDir(), "video")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(ffmpeg, "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("the neutralised file does not decode: %v %s", err, out)
	}
}

func TestInspectVideoReadsBrowserFormatsAndNeutraliseRemovesWhatCamerasLeave(t *testing.T) {
	for _, test := range []struct {
		file, contentType string
		width, height     int
		codecs            []string
		secret            string
		duration          bool
	}{
		{"h264.mp4", "video/mp4", 64, 48, []string{"avc1", "mp4a"}, "SECRETTITLE", true},
		{"quicktime.mov", "video/mp4", 64, 48, []string{"avc1", "mp4a"}, "21.0285", true},
		{"vp9.webm", "video/webm", 64, 48, []string{"V_VP9", "A_OPUS"}, "SECRETTITLE", true},
		{"datatrack.mp4", "video/mp4", 64, 48, []string{"avc1"}, "SECRETLOC", true},
		{"fragmented.mp4", "video/mp4", 64, 48, []string{"avc1"}, "", false},
	} {
		t.Run(test.file, func(t *testing.T) {
			data := testVideo(t, test.file)
			file := &writable{data: append([]byte{}, data...)}
			info, err := InspectVideo(file, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if info.ContentType != test.contentType || info.Width != test.width || info.Height != test.height || !slicesEqual(info.Codecs, test.codecs) {
				t.Fatalf("inspect = %+v", info)
			}
			if (info.DurationMS != nil) != test.duration || (info.DurationMS != nil && (*info.DurationMS < 900 || *info.DurationMS > 1200)) {
				t.Fatalf("duration = %v", info.DurationMS)
			}
			if test.secret != "" && !bytes.Contains(data, []byte(test.secret)) {
				t.Fatalf("the sample does not hold %q", test.secret)
			}
			if err := NeutraliseVideo(file, int64(len(data))); err != nil {
				t.Fatal(err)
			}
			if len(file.data) != len(data) {
				t.Fatal("the size changed")
			}
			if test.secret != "" && bytes.Contains(file.data, []byte(test.secret)) {
				t.Fatalf("%q is still in the file", test.secret)
			}
			if again, err := InspectVideo(file, int64(len(file.data))); err != nil || again.Width != info.Width {
				t.Fatalf("neutralised inspect = %+v (%v)", again, err)
			}
			decodes(t, file.data)
		})
	}
}

func TestInspectVideoRefusesWhatBrowsersDoNotAllPlay(t *testing.T) {
	data := testVideo(t, "hevc.mp4")
	if _, err := InspectVideo(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrHEVC) {
		t.Fatalf("HEVC = %v", err)
	}
	for _, junk := range [][]byte{[]byte("not a video at all"), testImage(t, "plain.jpg"), testVideo(t, "h264.mp4")[:200]} {
		if _, err := InspectVideo(bytes.NewReader(junk), int64(len(junk))); err == nil {
			t.Fatalf("%q was taken as a video", junk[:12])
		}
	}
}

func TestInspectVideoSwapsTheSizeOfAQuarterTurnedVideo(t *testing.T) {
	data := testVideo(t, "rotated.mp4")
	info, err := InspectVideo(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 48 || info.Height != 64 {
		t.Fatalf("size = %d×%d, want 48×64", info.Width, info.Height)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func FuzzVideoWalk(f *testing.F) {
	for _, name := range []string{"h264.mp4", "quicktime.mov", "vp9.webm", "datatrack.mp4", "fragmented.mp4", "rotated.mp4", "hevc.mp4"} {
		f.Add(testVideo(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file := &writable{data: append([]byte{}, data...)}
		if _, err := InspectVideo(file, int64(len(data))); err != nil {
			return
		}
		if err := NeutraliseVideo(file, int64(len(data))); err != nil {
			return
		}
		if len(file.data) != len(data) {
			t.Fatal("the size changed")
		}
	})
}

func mp4Box(kind string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out, uint32(8+len(payload)))
	copy(out[4:], kind)
	return append(out, payload...)
}

// Every box a player does not need goes, whatever its name: a dashcam's gps box, and what a free or
// skip box still holds, at the top level as anywhere else.
func TestNeutraliseVideoKeepsOnlyTheBoxesAPlayerNeeds(t *testing.T) {
	secret := []byte("GPS:10.7769N,106.7009E-SECRET")
	data := testVideo(t, "h264.mp4")
	for _, kind := range []string{"gps ", "free", "skip", "wide", "XYZW"} {
		data = append(data, mp4Box(kind, secret)...)
	}
	file := &writable{data: data}
	if _, err := InspectVideo(file, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if err := NeutraliseVideo(file, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(file.data, secret); n > 0 {
		t.Fatalf("the secret is still in the file %d times", n)
	}
	decodes(t, file.data)
}

// Reading the sample tables takes one pass: 200,000 chunks of one sample each, each with its own
// entry, took minutes when every chunk read every entry.
func TestVideoSampleTablesReadInLinearTime(t *testing.T) {
	const n = 200_000
	track := &mp4Track{haveSizes: true, haveChunks: true, haveSampleToCh: true, sampleSize: 1, sampleCount: n}
	for i := range n {
		track.chunks = append(track.chunks, uint64(i*2))
		track.perChunk = append(track.perChunk, [2]uint32{uint32(i + 1), 1})
	}
	started := time.Now()
	ranges, err := track.samples()
	if err != nil || len(ranges) != n {
		t.Fatalf("samples = %d (%v)", len(ranges), err)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("%d chunks took %s", n, took)
	}
}

func ebml(id []byte, payload []byte) []byte {
	return append(append(append([]byte{}, id...), 0x80|byte(len(payload))), payload...)
}

// A picture wider than MaxVideoSide is refused, as the library stores at most 65,535 pixels a side.
func TestInspectVideoRefusesAPictureTooWide(t *testing.T) {
	header := ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte("webm")))
	video := ebml([]byte{0xE0}, append(ebml([]byte{0xB0}, []byte{0x01, 0x11, 0x70}), ebml([]byte{0xBA}, []byte{0x64})...)) // 70,000 × 100
	entry := ebml([]byte{0xAE}, append(append(ebml([]byte{0x83}, []byte{1}), ebml([]byte{0x86}, []byte("V_VP9"))...), video...))
	segment := append([]byte{0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, ebml([]byte{0x16, 0x54, 0xAE, 0x6B}, entry)...)
	data := append(header, segment...)
	if _, err := InspectVideo(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrVideoTooLarge) {
		t.Fatalf("70,000 pixels wide = %v", err)
	}
}
