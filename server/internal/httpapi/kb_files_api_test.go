package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// uploadKBFile uploads data as a file named name in 8 MiB parts and completes the upload.
func uploadKBFile(t *testing.T, api http.Handler, token, name string, data []byte) kbMediaView {
	t.Helper()
	return uploadKBVideo(t, api, token, data, 8<<20, map[string]any{"kind": "file", "name": name})
}

// startAndSendKBFile starts a file upload and sends all of data, without completing it.
func startAndSendKBFile(t *testing.T, api http.Handler, token, name string, data []byte) string {
	t.Helper()
	upload := startKBUpload(t, api, token, len(data), map[string]any{"kind": "file", "name": name})
	if response := serve(api, kbPartRequest(upload.ID, token, data, 0, len(data)-1, len(data))); response.Code != http.StatusOK {
		t.Fatalf("part status=%d body=%s", response.Code, response.Body.String())
	}
	return upload.ID
}

// A 20 MB PDF goes up in parts as a file of the library; its text is read for search.
func TestKBFileUploadsAPDFInParts(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Files", "admin@kb-files.example.com")
	data := pdfsample.PDF("Fill in the warranty form", 20<<20)
	file := uploadKBFile(t, api, admin, "Warranty form.pdf", data)
	if file.Kind != "file" || file.ContentType != "application/pdf" || file.Size != int64(len(data)) || file.Name != "Warranty form.pdf" ||
		file.URL != "/api/v1/kb/media/"+file.ID || file.Width != nil || file.DurationMS != nil {
		t.Fatalf("file = %+v", file)
	}
	var text string
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(text,'') FROM kb_media WHERE id=$1`, file.ID).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "warranty form") {
		t.Fatalf("stored text = %q, want it to hold the PDF's words", text)
	}
	csv := []byte("sku,name\nA-1,Kettle\n")
	if sheet := uploadKBFile(t, api, admin, "parts.csv", csv); sheet.ContentType != "text/csv" || sheet.Kind != "file" {
		t.Fatalf("csv = %+v", sheet)
	}
}

// A file over 25 MB, a type the list does not allow and bytes that are not what the name says are
// refused with what is allowed, and nothing is stored.
func TestKBFileUploadRefusesWhatIsNotAllowed(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB File Limits", "admin@kb-file-limits.example.com")
	large := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"kind": "file", "name": "big.pdf", "size": 26 << 20},
		admin, http.StatusRequestEntityTooLarge, nil)
	if !strings.Contains(large.Body.String(), "25 MiB") {
		t.Fatalf("26 MB = %s", large.Body.String())
	}
	html := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"kind": "file", "name": "page.html", "size": 100},
		admin, http.StatusBadRequest, nil)
	if errorCode(t, html) != "unsupported_file_type" || !strings.Contains(html.Body.String(), "PDF") || !strings.Contains(html.Body.String(), "tar.gz") {
		t.Fatalf("html = %s", html.Body.String())
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"kind": "file", "size": 100}, admin, http.StatusBadRequest, nil)

	exe := append([]byte("MZ\x90\x00\x03\x00\x00\x00"), make([]byte, 2000)...)
	id := startAndSendKBFile(t, api, admin, "setup.pdf", exe)
	refused := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-uploads/"+id+"/complete", nil, admin, http.StatusBadRequest, nil)
	if errorCode(t, refused) != "unsupported_file_type" || !strings.Contains(refused.Body.String(), ".docx") {
		t.Fatalf("exe renamed .pdf = %s", refused.Body.String())
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media-uploads/"+id, nil, admin, http.StatusNotFound, nil)
	var stored int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_media WHERE name='setup.pdf'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("%d files stored, want none", stored)
	}
	// An upload is of a video or a file; images go up whole (the contract refuses the kind too).
	image := serve(api, newRequest(t, http.MethodPost, "/api/v1/kb/media-uploads", map[string]any{"kind": "image", "size": 100}, admin, nil))
	if image.Code != http.StatusBadRequest || !strings.Contains(image.Body.String(), "/api/v1/kb/images") {
		t.Fatalf("kind image status=%d body=%s, want 400 naming the image upload", image.Code, image.Body.String())
	}
}
