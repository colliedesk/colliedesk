package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A video goes up in parts, comes out without the title and location a camera wrote, and seeks: a
// range answers 206 with exactly those bytes.
func TestKBVideoUploadsInPartsAndServesRanges(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video", "admin@kb-video.example.com")
	data := mediaVideo(t, "quicktime.mov")
	video := uploadKBVideo(t, api, admin, data, 3000, map[string]any{"name": "Screen recording", "alt": "Settings tour", "keep": true})
	if video.Kind != "video" || video.ContentType != "video/mp4" || video.Width == nil || *video.Width != 64 || video.DurationMS == nil || !video.Kept ||
		video.URL != "/api/v1/kb/media/"+video.ID || video.Name != "Screen recording" || video.Size != int64(len(data)) {
		t.Fatalf("video = %+v", video)
	}
	whole := kbCall(t, api, http.MethodGet, video.URL, nil, admin, http.StatusOK, nil)
	if bytes.Contains(whole.Body.Bytes(), []byte("21.0285")) || whole.Body.Len() != len(data) || whole.Header().Get("Accept-Ranges") != "bytes" ||
		whole.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" || whole.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("whole video: %d bytes, headers %v, still holds the location: %v", whole.Body.Len(), whole.Header(), bytes.Contains(whole.Body.Bytes(), []byte("21.0285")))
	}
	ranged := newRequest(t, http.MethodGet, video.URL, nil, admin, nil)
	ranged.Header.Set("Range", "bytes=100-199")
	part := serve(api, ranged)
	if part.Code != http.StatusPartialContent || part.Header().Get("Content-Range") != "bytes 100-199/"+strconv.Itoa(len(data)) || !bytes.Equal(part.Body.Bytes(), whole.Body.Bytes()[100:200]) {
		t.Fatalf("range: %d %v", part.Code, part.Header())
	}
	cached := newRequest(t, http.MethodGet, video.URL, nil, admin, nil)
	cached.Header.Set("If-None-Match", whole.Header().Get("ETag"))
	if response := serve(api, cached); response.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match status=%d", response.Code)
	}
	download := kbCall(t, api, http.MethodGet, video.URL+"?download=1", nil, admin, http.StatusOK, nil)
	if got := download.Header().Get("Content-Disposition"); got != `attachment; filename="Screen recording.mp4"` {
		t.Fatalf("download disposition = %q", got)
	}
	// The same bytes again are the same video.
	if again := uploadKBVideo(t, api, admin, data, 8<<20, nil); again.ID != video.ID {
		t.Fatalf("same video gave %s, want %s", again.ID, video.ID)
	}
}

// A part that does not start where the upload stands is refused with the offset; the upload goes on
// from there. Too large, too many and incomplete are refused before anything is stored.
func TestKBVideoUploadRefusesWhatDoesNotFit(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.KBVideoMaxBytes = 1 << 20 })
	admin := createSession(t, api, service, "KB Video Limits", "admin@kb-video-limits.example.com")
	data := mediaVideo(t, "h264.mp4")
	upload := startKBUpload(t, api, admin, len(data), nil)
	if response := serve(api, kbPartRequest(upload.ID, admin, data, 0, 999, len(data))); response.Code != http.StatusOK {
		t.Fatalf("first part status=%d body=%s", response.Code, response.Body.String())
	}
	wrong := serve(api, kbPartRequest(upload.ID, admin, data, 2000, 2999, len(data)))
	if wrong.Code != http.StatusConflict || wrong.Header().Get("Upload-Offset") != "1000" || errorCode(t, wrong) != "upload_offset_mismatch" {
		t.Fatalf("wrong offset: %d %v %s", wrong.Code, wrong.Header(), wrong.Body.String())
	}
	var status kbUploadView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media-uploads/"+upload.ID, nil, admin, http.StatusOK, &status)
	if status.Offset != 1000 {
		t.Fatalf("status = %+v", status)
	}
	early := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+upload.ID+"/complete", nil, admin, http.StatusConflict, nil)
	if errorCode(t, early) != "upload_incomplete" {
		t.Fatalf("early complete = %s", early.Body.String())
	}
	if response := serve(api, kbPartRequest(upload.ID, admin, data, 1000, len(data)-1, len(data))); response.Code != http.StatusOK {
		t.Fatalf("rest status=%d body=%s", response.Code, response.Body.String())
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+upload.ID+"/complete", nil, admin, http.StatusCreated, nil)

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"size": 2 << 20}, admin, http.StatusRequestEntityTooLarge, nil)
	var open []string
	for range 3 {
		open = append(open, startKBUpload(t, api, admin, 100, nil).ID)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"size": 100}, admin, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth upload status=%d", response.Code)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media-uploads/"+open[0], nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media-uploads/"+open[0], nil, admin, http.StatusNotFound, nil)
	startKBUpload(t, api, admin, 100, nil)
}

// An H.265 video, the iPhone's default, is refused at completion with what to do, and its parts go.
func TestKBVideoUploadRefusesHEVCAndThrowsItAway(t *testing.T) {
	api, service, _, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video HEVC", "admin@kb-video-hevc.example.com")
	data := mediaVideo(t, "hevc.mp4")
	upload := startKBUpload(t, api, admin, len(data), nil)
	if response := serve(api, kbPartRequest(upload.ID, admin, data, 0, len(data)-1, len(data))); response.Code != http.StatusOK {
		t.Fatalf("part status=%d", response.Code)
	}
	refused := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+upload.ID+"/complete", nil, admin, http.StatusBadRequest, nil)
	if errorCode(t, refused) != "unsupported_codec" || !strings.Contains(refused.Body.String(), "H.264") {
		t.Fatalf("HEVC = %s", refused.Body.String())
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media-uploads/"+upload.ID, nil, admin, http.StatusNotFound, nil)
	staged, err := storage.(interface{ StagingFile(string) (string, error) }).StagingFile(upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the refused video's parts are still on disk: %v", err)
	}
}

// Sending parts streams them to disk: an 8 MiB part never sits in memory whole.
func TestKBVideoPartsStreamToDisk(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video Stream", "admin@kb-video-stream.example.com")
	size := 3 * (8 << 20)
	data := make([]byte, size)
	upload := startKBUpload(t, api, admin, size, nil)
	for first := 0; first < size; first += 8 << 20 {
		req := kbPartRequest(upload.ID, admin, data, first, first+(8<<20)-1, size)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		response := serve(api, req)
		runtime.ReadMemStats(&after)
		if response.Code != http.StatusOK {
			t.Fatalf("part status=%d body=%s", response.Code, response.Body.String())
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
			t.Fatalf("an 8 MiB part allocated %d bytes", allocated)
		}
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media-uploads/"+upload.ID, nil, admin, http.StatusNoContent, nil)
}

// A poster is an image of the video's, under an address of its own version.
func TestKBVideoPosterHasAVersionedAddress(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video Poster", "admin@kb-video-poster.example.com")
	video := uploadKBVideo(t, api, admin, mediaVideo(t, "vp9.webm"), 8<<20, nil)
	if video.PosterURL != nil || video.ContentType != "video/webm" {
		t.Fatalf("new video = %+v", video)
	}
	var first, second kbMediaView
	response := serve(api, kbPosterRequest(video.ID, admin, pngImage(110)))
	decode(t, response, &first)
	if response.Code != http.StatusOK || first.PosterURL == nil || first.ThumbnailURL == nil {
		t.Fatalf("poster status=%d %+v", response.Code, first)
	}
	shown := kbCall(t, api, http.MethodGet, *first.PosterURL, nil, admin, http.StatusOK, nil)
	if shown.Header().Get("Content-Type") != "image/png" || !bytes.Equal(shown.Body.Bytes(), pngImage(110)) {
		t.Fatalf("poster served as %s", shown.Header().Get("Content-Type"))
	}
	decode(t, serve(api, kbPosterRequest(video.ID, admin, pngImage(111))), &second)
	if second.PosterURL == nil || *second.PosterURL == *first.PosterURL {
		t.Fatal("a new poster kept the old address")
	}
	kbCall(t, api, http.MethodGet, *first.PosterURL, nil, admin, http.StatusNotFound, nil)
	var counted int64
	if err := pool.QueryRow(context.Background(), `SELECT counted_storage_bytes FROM workspaces w JOIN kb_media m ON m.workspace_id=w.id WHERE m.id=$1`, video.ID).Scan(&counted); err != nil ||
		counted != video.Size+int64(len(pngImage(111))) {
		t.Fatalf("counted = %d (%v), want the video and its poster", counted, err)
	}
	image := uploadKBMedia(t, api, admin, pngImage(112), nil)
	if response := serve(api, kbPosterRequest(image.ID, admin, pngImage(113))); response.Code != http.StatusBadRequest {
		t.Fatalf("poster on an image status=%d", response.Code)
	}
}

// Another workspace's video, poster and uploads answer as if they did not exist.
func TestKBVideoNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Video Alpha", "admin@kb-video-alpha.example.com")
	beta := createSession(t, api, service, "KB Video Beta", "admin@kb-video-beta.example.com")
	video := uploadKBVideo(t, api, alpha, mediaVideo(t, "h264.mp4"), 8<<20, nil)
	var posted kbMediaView
	decode(t, serve(api, kbPosterRequest(video.ID, alpha, pngImage(114))), &posted)
	upload := startKBUpload(t, api, alpha, 100, nil)
	for _, path := range []string{video.URL, *posted.PosterURL, "/api/v1/kb/media-uploads/" + upload.ID} {
		kbCall(t, api, http.MethodGet, path, nil, beta, http.StatusNotFound, nil)
	}
	if response := serve(api, kbPartRequest(upload.ID, beta, make([]byte, 100), 0, 99, 100)); response.Code != http.StatusNotFound {
		t.Fatalf("beta part status=%d", response.Code)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+upload.ID+"/complete", nil, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media-uploads/"+upload.ID, nil, beta, http.StatusNotFound, nil)
	if response := serve(api, kbPosterRequest(video.ID, beta, pngImage(115))); response.Code != http.StatusNotFound {
		t.Fatalf("beta poster status=%d", response.Code)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media-uploads/"+upload.ID, nil, alpha, http.StatusOK, nil)
}

// The export carries each video and its poster as files, checked against their checksums.
func TestKBVideoExportCarriesTheVideoAndItsPoster(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video Export", "admin@kb-video-export.example.com")
	data := mediaVideo(t, "vp9.webm")
	video := uploadKBVideo(t, api, admin, data, 8<<20, map[string]any{"name": "Tour"})
	if response := serve(api, kbPosterRequest(video.ID, admin, pngImage(120))); response.Code != http.StatusOK {
		t.Fatalf("poster status=%d", response.Code)
	}
	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	videos := ndjsonLines(t, entries["kb-videos.ndjson"])
	if len(videos) != 1 || videos[0]["name"] != "Tour" || videos[0]["path"] != "kb-videos/"+video.ID+".webm" || videos[0]["poster_path"] != "kb-videos/"+video.ID+".poster.png" {
		t.Fatalf("kb-videos.ndjson = %v", videos)
	}
	if !bytes.Equal(entries["kb-videos/"+video.ID+".poster.png"], pngImage(120)) || len(entries["kb-videos/"+video.ID+".webm"]) != len(data) {
		t.Fatal("the video or its poster is not in the export")
	}
	if images := ndjsonLines(t, entries["kb-images.ndjson"]); len(images) != 0 {
		t.Fatalf("a video is in kb-images.ndjson: %v", images)
	}
}

// An upload left unfinished goes with the daily purge after 24 hours, its parts with it.
func TestKBVideoUploadExpiresWithTheDailyPurge(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Video Expiry", "admin@kb-video-expiry.example.com")
	upload := startKBUpload(t, api, admin, 100, nil)
	if response := serve(api, kbPartRequest(upload.ID, admin, make([]byte, 100), 0, 49, 100)); response.Code != http.StatusOK {
		t.Fatalf("part status=%d", response.Code)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media_uploads SET expires_at=now()-interval '1 minute' WHERE id=$1`, upload.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx); err != nil {
		t.Fatal(err)
	}
	staged, _ := storage.(interface{ StagingFile(string) (string, error) }).StagingFile(upload.ID)
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the expired upload's parts are still on disk: %v", err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_media_uploads WHERE id=$1`, upload.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("upload rows left = %d (%v)", left, err)
	}
}
