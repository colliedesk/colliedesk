package mcpserver_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// An agent stores an image it found on the web by its address, in one call, and gets the url to
// link; a private address is refused with a reason, and a read token cannot import.
func TestAgentImportsARemoteImageIntoTheMediaLibrary(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(kbPNG)
	}))
	t.Cleanup(host.Close)
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.MailImages = mailimage.NewFetcher(mailimage.Options{AllowAddresses: map[string]bool{host.Listener.Addr().String(): true}})
	})
	w := e.newWorkspace("KB Import Tools Desk", "admin@kb-import-tools.example.com")
	session := e.connect(w.send, "")

	imported := e.ok(session, "colliedesk_kb_image_import", map[string]any{"url": host.URL + "/chart.png", "alt": "Monthly chart"})
	id := at(t, imported, "image", "id").(string)
	if at(t, imported, "image", "url") != "/api/v1/kb/images/"+id || at(t, imported, "image", "content_type") != "image/png" {
		t.Fatalf("import = %v", imported)
	}
	again := e.ok(session, "colliedesk_kb_image_import", map[string]any{"url": host.URL + "/chart.png"})
	if at(t, again, "image", "id") != id {
		t.Fatalf("the same image gave another id: %v", again)
	}
	if message := e.fails(session, "colliedesk_kb_image_import", map[string]any{"url": "http://169.254.169.254/latest/meta-data"}); !strings.Contains(message, "private or internal address") {
		t.Fatalf("metadata address: %s", message)
	}
	e.refused(e.connect(w.read, ""), "colliedesk_kb_image_import", map[string]any{"url": host.URL + "/chart.png"})
}
