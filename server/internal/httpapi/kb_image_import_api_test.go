package httpapi_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

const kbImageImportPath = "/api/v1/kb/images/import"

// importDesk is an API with file storage whose image fetcher may dial only the given options' hosts.
func importDesk(t *testing.T, options mailimage.Options) (http.Handler, func(name, email string) string) {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(options)
	})
	return api, func(name, email string) string { return createSession(t, api, service, name, email) }
}

// An image pasted by its web address is fetched by the server and stored in the media library, as an
// upload of the same bytes would be.
func TestKBImageImportStoresARemoteImageInTheMediaLibrary(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", kbPNG))
	api, session := importDesk(t, allowing(host))
	admin := session("KB Import", "admin@kb-import.example.com")

	var image kbMediaView
	kbCall(t, api, http.MethodPost, kbImageImportPath, map[string]any{"url": host.URL + "/diagram.png", "name": "Diagram", "alt": "The flow"},
		admin, http.StatusCreated, &image)
	if image.ContentType != "image/png" || image.Size != int64(len(kbPNG)) || image.URL != "/api/v1/kb/images/"+image.ID {
		t.Fatalf("imported = %+v", image)
	}
	served := request(t, api, http.MethodGet, image.URL, nil, admin, nil)
	if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), kbPNG) {
		t.Fatalf("served status=%d len=%d", served.Code, served.Body.Len())
	}
	page := kbMediaList(t, api, admin, "")
	if len(page.Items) != 1 || page.Items[0].ID != image.ID || page.Items[0].Name != "Diagram" || page.Items[0].Alt != "The flow" {
		t.Fatalf("media library = %+v", page.Items)
	}
	// The same image again is the one already stored.
	var again kbMediaView
	kbCall(t, api, http.MethodPost, kbImageImportPath, map[string]any{"url": host.URL + "/diagram.png"}, admin, http.StatusCreated, &again)
	if again.ID != image.ID {
		t.Fatalf("the same bytes gave a second image %s", again.ID)
	}
}

// The server never fetches from its own network: a loopback or private address, a redirect to one,
// another port or another scheme is refused before anything is stored.
func TestKBImageImportRefusesPrivateAddressesRedirectsAndOtherSchemes(t *testing.T) {
	internal := newImageHost(t, serveBytes("image/png", kbPNG)) // loopback, not allowed
	redirector := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/secret.png", http.StatusFound)
	})
	page := newImageHost(t, serveBytes("text/html", []byte("<html><script>alert(1)</script></html>")))
	broken := newImageHost(t, serveBytes("image/png", tinyPNG)) // PNG magic, no image behind it
	api, session := importDesk(t, allowing(redirector, page, broken))
	admin := session("KB Import Guard", "admin@kb-import-guard.example.com")

	for _, target := range []struct {
		url, code string
		status    int
	}{
		{internal.URL + "/secret.png", "address_refused", http.StatusForbidden},
		{redirector.URL + "/go", "address_refused", http.StatusForbidden},
		{"http://169.254.169.254/latest/meta-data", "address_refused", http.StatusForbidden},
		{page.URL + "/page.html", "not_an_image", http.StatusUnsupportedMediaType},
		{broken.URL + "/broken.png", "not_an_image", http.StatusUnsupportedMediaType},
		{"file:///etc/passwd", "invalid_request", http.StatusBadRequest},
		{"data:image/png;base64,iVBORw0KGgo=", "invalid_request", http.StatusBadRequest},
		{"/api/v1/kb/images/x", "invalid_request", http.StatusBadRequest},
	} {
		response := request(t, api, http.MethodPost, kbImageImportPath, map[string]any{"url": target.url}, admin, nil)
		if response.Code != target.status || errorCode(t, response) != target.code {
			t.Errorf("%s: status=%d body=%s, want %d %s", target.url, response.Code, response.Body.String(), target.status, target.code)
		}
	}
	if internal.hits.Load() != 0 {
		t.Fatalf("the private host was called %d times", internal.hits.Load())
	}
	if items := kbMediaList(t, api, admin, "").Items; len(items) != 0 {
		t.Fatalf("a refused import stored %+v", items)
	}
}

// An import is the importing workspace's own: the same remote image in another workspace is a
// separate item, and neither sees the other's.
func TestKBImageImportIsPerWorkspace(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", kbPNG))
	api, session := importDesk(t, allowing(host))
	alpha := session("KB Import Alpha", "admin@kb-import-alpha.example.com")
	beta := session("KB Import Beta", "admin@kb-import-beta.example.com")

	var first, second kbMediaView
	kbCall(t, api, http.MethodPost, kbImageImportPath, map[string]any{"url": host.URL + "/shared.png"}, alpha, http.StatusCreated, &first)
	kbCall(t, api, http.MethodPost, kbImageImportPath, map[string]any{"url": host.URL + "/shared.png"}, beta, http.StatusCreated, &second)
	if first.ID == second.ID {
		t.Fatal("beta got alpha's image for the same remote image")
	}
	if response := request(t, api, http.MethodGet, first.URL, nil, beta, nil); response.Code != http.StatusNotFound {
		t.Fatalf("beta read alpha's image: status=%d", response.Code)
	}
	if items := kbMediaList(t, api, beta, "").Items; len(items) != 1 || items[0].ID != second.ID || strings.Contains(items[0].URL, first.ID) {
		t.Fatalf("beta library = %+v", items)
	}
}

// A paste retried by a slow network names itself with the same Idempotency-Key: the repeat answers the
// first import (200) without fetching the image again, and the key with another address is refused.
func TestKBImageImportWithARepeatedKeyFetchesOnce(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", kbPNG))
	api, session := importDesk(t, allowing(host))
	admin := session("KB Import Key", "admin@kb-import-key.example.com")
	body := map[string]any{"url": host.URL + "/once.png"}

	first := postWithKey(t, api, admin, kbImageImportPath, "paste-1", body)
	repeat := postWithKey(t, api, admin, kbImageImportPath, "paste-1", body)
	if first.Code != http.StatusCreated || repeat.Code != http.StatusOK {
		t.Fatalf("first=%d repeat=%d body=%s, want 201 then 200", first.Code, repeat.Code, repeat.Body.String())
	}
	var made, again kbMediaView
	decode(t, first, &made)
	decode(t, repeat, &again)
	if again.ID != made.ID || host.hits.Load() != 1 {
		t.Fatalf("repeat id=%s (want %s), image fetched %d times (want 1)", again.ID, made.ID, host.hits.Load())
	}
	other := postWithKey(t, api, admin, kbImageImportPath, "paste-1", map[string]any{"url": host.URL + "/other.png"})
	if other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), "idempotency_key_reused") {
		t.Fatalf("other address with the same key: status=%d body=%s, want 422 idempotency_key_reused", other.Code, other.Body.String())
	}
}
