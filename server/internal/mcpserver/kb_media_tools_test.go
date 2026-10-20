package mcpserver_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
)

// An agent uploads a video in parts, gives it a poster, finds it in the library, moves an unused image
// to the trash and back, and replaces one image by another; a read token can do none of the writes.
func TestSendTokenUploadsVideosAndManagesTheMediaLibrary(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Media Tools Desk", "admin@kb-media-tools.example.com")
	session := e.connect(w.send, "")
	data, err := os.ReadFile("../media/testdata/video/h264.mp4")
	if err != nil {
		t.Fatal(err)
	}

	started := e.ok(session, "colliedesk_kb_video_upload_start", map[string]any{"size": len(data), "name": "Tour", "keep": true})
	uploadID := at(t, started, "upload_id").(string)
	half := len(data) / 2
	e.ok(session, "colliedesk_kb_video_upload_part", map[string]any{"upload_id": uploadID, "offset": 0, "data_base64": base64.StdEncoding.EncodeToString(data[:half])})
	if message := e.fails(session, "colliedesk_kb_video_upload_part", map[string]any{"upload_id": uploadID, "offset": 0, "data_base64": base64.StdEncoding.EncodeToString(data[half:])}); !strings.Contains(message, "byte "+itoa(half)) {
		t.Fatalf("part at the wrong offset: %s", message)
	}
	status := e.ok(session, "colliedesk_kb_video_upload_status", map[string]any{"upload_id": uploadID})
	if at(t, status, "offset") != float64(half) {
		t.Fatalf("status = %v", status)
	}
	if listed := fmt.Sprint(e.ok(session, "colliedesk_kb_video_upload_list", map[string]any{})); !strings.Contains(listed, uploadID) || !strings.Contains(listed, "Tour") {
		t.Fatalf("uploads in progress = %s", listed)
	}
	e.ok(session, "colliedesk_kb_video_upload_part", map[string]any{"upload_id": uploadID, "offset": half, "data_base64": base64.StdEncoding.EncodeToString(data[half:])})
	done := e.ok(session, "colliedesk_kb_video_upload_complete", map[string]any{"upload_id": uploadID})
	videoID := at(t, done, "media", "id").(string)
	if at(t, done, "media", "kind") != "video" || at(t, done, "media", "url") != "/api/v1/kb/media/"+videoID || at(t, done, "media", "team_text", "name") != "Tour" {
		t.Fatalf("complete = %v", done)
	}
	posted := e.ok(session, "colliedesk_kb_video_poster_set", map[string]any{"media_id": videoID, "data_base64": base64.StdEncoding.EncodeToString(kbPNG)})
	if at(t, posted, "media", "has_poster") != true {
		t.Fatalf("poster = %v", posted)
	}
	cancelled := e.ok(session, "colliedesk_kb_video_upload_start", map[string]any{"size": 10})
	e.ok(session, "colliedesk_kb_video_upload_cancel", map[string]any{"upload_id": at(t, cancelled, "upload_id")})

	listed := e.ok(session, "colliedesk_kb_media_list", map[string]any{"kind": "video"})
	if media := at(t, listed, "media").([]any); len(media) != 1 {
		t.Fatalf("videos = %v", listed)
	}
	image := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(kbPNG), "name": "Logo"})
	imageID := at(t, image, "image", "id").(string)
	trashed := e.ok(session, "colliedesk_kb_media_trash", map[string]any{"media_ids": []string{imageID}})
	if at(t, trashed, "results", 0, "status") != "trashed" {
		t.Fatalf("trash = %v", trashed)
	}
	inTrash := e.ok(session, "colliedesk_kb_media_trash_list", map[string]any{})
	if media := at(t, inTrash, "media").([]any); len(media) != 1 {
		t.Fatalf("trash list = %v", inTrash)
	}
	e.ok(session, "colliedesk_kb_media_undelete", map[string]any{"batch_id": at(t, trashed, "batch_id")})
	other := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(otherPNG()), "name": "New logo"})
	replaced := e.ok(session, "colliedesk_kb_media_replace", map[string]any{"media_id": imageID, "with_media_id": at(t, other, "image", "id")})
	e.ok(session, "colliedesk_kb_media_replace_undo", map[string]any{"replacement_id": at(t, replaced, "replacement_id")})

	reader := e.connect(w.read, "")
	if message := e.fails(reader, "colliedesk_kb_video_upload_start", map[string]any{"size": 10}); !strings.Contains(strings.ToLower(message), "file") {
		t.Fatalf("read token upload: %s", message)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// otherPNG is a 2 × 1 PNG, an image other than kbPNG.
func otherPNG() []byte {
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		panic(err)
	}
	return out.Bytes()
}
