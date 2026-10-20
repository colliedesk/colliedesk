package httpapi_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type kbUploadView struct {
	ID       string  `json:"id"`
	Size     int64   `json:"size"`
	Offset   int64   `json:"offset"`
	PartSize int64   `json:"part_size"`
	MediaID  *string `json:"media_id"`
}

func mediaVideo(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../media/testdata/video/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// startKBUpload starts an upload of size bytes.
func startKBUpload(t *testing.T, api http.Handler, token string, size int, extra map[string]any) kbUploadView {
	t.Helper()
	body := map[string]any{"size": size}
	for key, value := range extra {
		body[key] = value
	}
	var upload kbUploadView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads", body, token, http.StatusCreated, &upload)
	return upload
}

// kbPartRequest is a part of an upload: the bytes of data from first on, with their Content-Range.
func kbPartRequest(id, token string, data []byte, first, last, size int) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kb/media-uploads/"+id, bytes.NewReader(data[first:last+1]))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, size))
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// uploadKBVideo uploads data in parts of partSize and completes the upload.
func uploadKBVideo(t *testing.T, api http.Handler, token string, data []byte, partSize int, extra map[string]any) kbMediaView {
	t.Helper()
	upload := startKBUpload(t, api, token, len(data), extra)
	for first := 0; first < len(data); first += partSize {
		last := min(first+partSize, len(data)) - 1
		req := kbPartRequest(upload.ID, token, data, first, last, len(data))
		validateContractRequest(t, req)
		response := serve(api, req)
		if response.Code != http.StatusOK {
			t.Fatalf("part %d-%d status=%d body=%s", first, last, response.Code, response.Body.String())
		}
		validateContractResponse(t, kbPartRequest(upload.ID, token, data, first, last, len(data)), response)
	}
	var media kbMediaView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+upload.ID+"/complete", nil, token, http.StatusCreated, &media)
	return media
}

// kbPosterRequest sets a video's poster to the image data.
func kbPosterRequest(id, token string, data []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kb/media/"+id+"/poster", bytes.NewReader(data))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
