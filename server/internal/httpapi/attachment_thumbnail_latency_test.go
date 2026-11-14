package httpapi_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"sort"
	"testing"
	"time"
)

// Budgets for GET /api/v1/attachments/{id}/thumbnail (docs/internals/performance.md "Attachment thumbnail
// budget"). Making one decodes the whole photo once; a kept one is read from storage.
const (
	attachmentThumbnailMakeBudget = 400 * time.Millisecond
	attachmentThumbnailReadBudget = 20 * time.Millisecond
)

// phonePhotoJPEG is a 12-megapixel (4000x3000) JPEG with detail, the size of a phone photo (about 3 MB).
func phonePhotoJPEG(t *testing.T, seed uint32) []byte {
	t.Helper()
	picture := image.NewYCbCr(image.Rect(0, 0, 4000, 3000), image.YCbCrSubsampleRatio420)
	for index := range picture.Y {
		seed = seed*1664525 + 1013904223
		picture.Y[index] = byte(index%4000/16) + byte(seed>>29)
	}
	for index := range picture.Cb {
		picture.Cb[index], picture.Cr[index] = byte(index%2000/8), byte(index/2000%256)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestAttachmentThumbnailLatencyBudget makes the thumbnails of six 12-megapixel phone photos (the first
// view of each), then reads a kept one 50 times (every later view of the timeline).
func TestAttachmentThumbnailLatencyBudget(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Thumb Budget", "admin@thumb-budget.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Budget", "email": "budget@thumb.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Photos"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "photos"})
	ids := make([]string, 6)
	for index := range ids {
		photo := phonePhotoJPEG(t, uint32(index+1))
		if index == 0 {
			t.Logf("photo: %d bytes", len(photo))
		}
		ids[index] = seedAttachment(t, pool, storage, token, api, messageID, "photo.jpg", string(photo))
	}
	timed := func(id string) time.Duration {
		started := time.Now()
		response := request(t, api, http.MethodGet, "/api/v1/attachments/"+id+"/thumbnail", nil, token, nil)
		elapsed := time.Since(started)
		if response.Code != http.StatusOK {
			t.Fatalf("thumbnail status=%d body=%s", response.Code, response.Body.String())
		}
		return elapsed
	}
	made := make([]time.Duration, len(ids))
	for index, id := range ids {
		made[index] = timed(id)
	}
	sort.Slice(made, func(i, j int) bool { return made[i] < made[j] })
	slowest := made[len(made)-1]
	t.Logf("make: median %s, slowest %s", made[len(made)/2], slowest)
	if slowest > attachmentThumbnailMakeBudget {
		t.Fatalf("making a thumbnail took %s, over %s", slowest, attachmentThumbnailMakeBudget)
	}
	reads := make([]time.Duration, 50)
	for index := range reads {
		reads[index] = timed(ids[0])
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
	p95 := reads[len(reads)*95/100]
	t.Logf("read kept: p95 %s", p95)
	if p95 > attachmentThumbnailReadBudget {
		t.Fatalf("reading a kept thumbnail p95 %s, over %s", p95, attachmentThumbnailReadBudget)
	}
}
