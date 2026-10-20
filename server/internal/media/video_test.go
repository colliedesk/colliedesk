package media

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
