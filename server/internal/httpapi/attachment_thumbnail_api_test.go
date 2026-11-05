package httpapi_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// photoJPEG is a JPEG photo of width x height pixels.
func photoJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// The timeline shows a picture as a thumbnail the server made from it: a few kilobytes instead of the
// original. It is made on the first request, kept, and served with the download route's safety.
func TestAttachmentThumbnailIsASmallPictureMadeOnceAndKept(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Thumbs Alpha", "admin@thumbs-alpha.example.com")
	other := createSession(t, api, service, "Thumbs Bravo", "admin@thumbs-bravo.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Toàn", "email": "toan@thumbs.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "[picture]"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "photo"})
	original := photoJPEG(t, 1200, 900)
	attachmentID := seedAttachment(t, pool, storage, token, api, messageID, "image-1", string(original))
	keyOf := func() *string {
		var key *string
		if err := pool.QueryRow(t.Context(), `SELECT thumb_storage_key FROM attachments WHERE id=$1`, attachmentID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	if keyOf() != nil {
		t.Fatal("a thumbnail must not exist before anyone asks for it")
	}

	foreign := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, other, nil)
	if foreign.Code != http.StatusNotFound || keyOf() != nil {
		t.Fatalf("another workspace: status=%d, thumbnail key %v", foreign.Code, keyOf())
	}

	first := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, token, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("thumbnail status=%d body=%s", first.Code, first.Body.String())
	}
	h := first.Header()
	if h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") ||
		h.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("thumbnail headers: %v", h)
	}
	thumb, err := jpeg.Decode(bytes.NewReader(first.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if bounds := thumb.Bounds(); bounds.Dx() != 320 || bounds.Dy() != 240 {
		t.Fatalf("thumbnail is %dx%d, want 320x240", bounds.Dx(), bounds.Dy())
	}
	if first.Body.Len() >= len(original)/4 {
		t.Fatalf("thumbnail is %d bytes for an original of %d", first.Body.Len(), len(original))
	}
	key := keyOf()
	if key == nil || *key == "" {
		t.Fatalf("the thumbnail must be kept, key = %v", key)
	}
	second := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, token, nil)
	if second.Code != http.StatusOK || !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) || *keyOf() != *key {
		t.Fatalf("second request: status=%d, same bytes %v", second.Code, bytes.Equal(second.Body.Bytes(), first.Body.Bytes()))
	}

	// Content-addressed objects are shared: removing an object some row may hold keeps a thumbnail that
	// an attachment still shows.
	if err := store.New(pool).WithObjects(storage).DiscardObject(t.Context(), workspaceID(t, api, token), *key); err != nil {
		t.Fatal(err)
	}
	if again := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, token, nil); again.Code != http.StatusOK {
		t.Fatalf("the kept thumbnail was deleted: status=%d", again.Code)
	}
}

// A file that is no picture the server can read (an SVG, a PDF, broken bytes named as a photo) has no
// thumbnail: the answer says so, the web app shows the original or the download row, and the server does
// not try again on every view.
func TestAttachmentWithoutAReadablePictureHasNoThumbnail(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Thumbs None", "admin@thumbs-none.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "N", "email": "n@thumbs.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Files"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "files"})
	// A picture over the decoder's limits (more than 8,192 pixels on a side) is a picture with no thumbnail too.
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewGray(image.Rect(0, 0, 9000, 1))); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"logo.svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"terms.pdf": "%PDF-1.4 not a picture",
		"photo.jpg": "\xff\xd8\xff\xe0 broken jpeg",
		"pano.png":  wide.String(),
	} {
		id := seedAttachment(t, pool, storage, token, api, messageID, name, content)
		response := request(t, api, http.MethodGet, "/api/v1/attachments/"+id+"/thumbnail", nil, token, nil)
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "no_thumbnail") || strings.Contains(response.Body.String(), "alert(1)") {
			t.Errorf("%s: status=%d body=%s", name, response.Code, response.Body.String())
		}
		var key *string
		if err := pool.QueryRow(t.Context(), `SELECT thumb_storage_key FROM attachments WHERE id=$1`, id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key == nil || *key != "" {
			t.Errorf("%s: the server must remember there is no thumbnail, key = %v", name, key)
		}
	}
}

// An import that fetches a lost or changed file again points the attachment at the new bytes: its thumbnail
// is made again from them, and the old one is handed back to be removed. The same bytes keep their thumbnail.
func TestReplacedAttachmentBytesDropTheirThumbnail(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Thumbs Replace", "admin@thumbs-replace.example.com")
	workspace := workspaceID(t, api, token)
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "R", "email": "r@thumbs.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Re-import"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "photo"})
	attachmentID := seedAttachment(t, pool, storage, token, api, messageID, "photo.jpg", string(photoJPEG(t, 1200, 900)))
	thumbnail := func() image.Image {
		response := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("thumbnail status=%d", response.Code)
		}
		picture, err := jpeg.Decode(bytes.NewReader(response.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		return picture
	}
	thumbKey := func() *string {
		var key *string
		if err := pool.QueryRow(t.Context(), `SELECT thumb_storage_key FROM attachments WHERE id=$1`, attachmentID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	replace := func(data []byte) string {
		object, err := storage.Put(t.Context(), workspace, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(t.Context()) //nolint:errcheck
		replaced, stale, err := store.ReplaceAttachmentBytes(t.Context(), tx, workspace, attachmentID,
			store.NewAttachment{MessageID: messageID, Filename: "photo.jpg", ContentType: "image/jpeg", Object: object})
		if err != nil || !replaced {
			t.Fatalf("replace = %v, %v", replaced, err)
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		return stale
	}
	if bounds := thumbnail().Bounds(); bounds.Dx() != 320 || bounds.Dy() != 240 {
		t.Fatalf("first thumbnail %v", bounds)
	}
	old := thumbKey()

	// The same bytes again: nothing to make again.
	if stale := replace(photoJPEG(t, 1200, 900)); stale != "" || thumbKey() == nil || *thumbKey() != *old {
		t.Fatalf("the same bytes must keep their thumbnail: stale %q, key %v", stale, thumbKey())
	}
	// Other bytes (a portrait photo): the old thumbnail is handed back, and a new one is made from them.
	if stale := replace(photoJPEG(t, 600, 1200)); stale != *old || thumbKey() != nil {
		t.Fatalf("new bytes: stale %q (want %q), key %v (want none yet)", stale, *old, thumbKey())
	}
	if bounds := thumbnail().Bounds(); bounds.Dx() != 160 || bounds.Dy() != 320 {
		t.Fatalf("the thumbnail must be made from the new bytes, got %v", bounds)
	}
}

// A view that read the old bytes while an import put new ones in their place must not leave the old picture's
// thumbnail on the row: the thumbnail is recorded only for the bytes it was made from, and is removed otherwise.
func TestAThumbnailOfReplacedBytesIsNotRecorded(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Thumbs Race", "admin@thumbs-race.example.com")
	workspace := workspaceID(t, api, token)
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "R", "email": "race@thumbs.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Race"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "photo"})
	attachmentID := seedAttachment(t, pool, storage, token, api, messageID, "photo.jpg", string(photoJPEG(t, 1200, 900)))
	data := store.New(pool).WithObjects(storage)
	// The view reads the row and makes a thumbnail of the old bytes...
	_, oldKey, _, err := data.AttachmentThumbnail(t.Context(), workspace, attachmentID)
	if err != nil {
		t.Fatal(err)
	}
	thumb := []byte("thumbnail of the old bytes")
	thumbObject, err := storage.Put(t.Context(), workspace, bytes.NewReader(thumb), int64(len(thumb)))
	if err != nil {
		t.Fatal(err)
	}
	// ...the import puts new bytes in place...
	fresh := photoJPEG(t, 600, 1200)
	object, err := storage.Put(t.Context(), workspace, bytes.NewReader(fresh), int64(len(fresh)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReplaceAttachmentBytes(t.Context(), tx, workspace, attachmentID,
		store.NewAttachment{MessageID: messageID, Filename: "photo.jpg", ContentType: "image/jpeg", Object: object}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	// ...and the view records what it made: refused, and its file removed. A "no thumbnail" for the old bytes
	// is refused the same way.
	if err := data.SetAttachmentThumbnail(t.Context(), workspace, attachmentID, oldKey, thumbObject.Key); err != nil {
		t.Fatal(err)
	}
	if err := data.SetAttachmentThumbnail(t.Context(), workspace, attachmentID, oldKey, ""); err != nil {
		t.Fatal(err)
	}
	var key *string
	if err := pool.QueryRow(t.Context(), `SELECT thumb_storage_key FROM attachments WHERE id=$1`, attachmentID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != nil {
		t.Fatalf("a thumbnail of the old bytes was recorded on the new ones: %q", *key)
	}
	if _, err := storage.Open(t.Context(), thumbObject.Key); err == nil {
		t.Fatal("the refused thumbnail's file must be removed")
	}
	response := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID+"/thumbnail", nil, token, nil)
	picture, err := jpeg.Decode(bytes.NewReader(response.Body.Bytes()))
	if response.Code != http.StatusOK || err != nil || picture.Bounds().Dx() != 160 || picture.Bounds().Dy() != 320 {
		t.Fatalf("the next view must make the thumbnail of the new bytes: status=%d err=%v", response.Code, err)
	}
}
