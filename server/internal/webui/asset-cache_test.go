package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// A build's files under assets/ are named by their content hash, so a browser may keep them for good; the
// page that names them must be asked for again, or a deploy would leave browsers on the old names.
func TestHashedAssetsAreImmutableAndThePageIsRevalidated(t *testing.T) {
	build := fstest.MapFS{
		"index.html":              {Data: []byte(`<div id="root"></div><script src="/assets/index-abc123.js"></script>`)},
		"assets/index-abc123.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc123.css": {Data: []byte("body{}")},
		"favicon.svg":             {Data: []byte("<svg/>")},
	}
	api := http.NotFoundHandler()
	handler := appHandler(api, http.NotFoundHandler(), build)

	for _, path := range []string{"/assets/index-abc123.js", "/assets/index-abc123.css"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("%s: Cache-Control %q, want public, max-age=31536000, immutable", path, got)
		}
	}

	for _, path := range []string{"/", "/conversations/123"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control %q, want no-cache", path, got)
		}
	}

	// A file an old page still names after a deploy must fail as a file, not arrive as the page: a cache would
	// otherwise keep the page under a script's name.
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/assets/index-old999.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing asset: status %d, want 404", missing.Code)
	}
	if got := missing.Header().Get("Cache-Control"); got == "public, max-age=31536000, immutable" {
		t.Errorf("missing asset: Cache-Control %q must not be immutable", got)
	}
}
