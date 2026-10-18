package mailimage_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// Magic numbers written by hand: PNG signature, GIF89a header, JPEG SOI, RIFF/WEBP container.
var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	gifBytes  = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00")
	jpegBytes = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	webpBytes = []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	svgBytes  = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><script>alert(1)</script></svg>`)
)

// serve starts a local image host the fetcher is allowed to dial and counts its requests.
func serve(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32, *mailimage.Fetcher) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, mailimage.NewFetcher(mailimage.Options{AllowAddresses: map[string]bool{srv.Listener.Addr().String(): true}})
}

func TestFetchServesRasterImagesAndFixesTheirType(t *testing.T) {
	for name, body := range map[string][]byte{"image/png": pngBytes, "image/gif": gifBytes, "image/jpeg": jpegBytes, "image/webp": webpBytes} {
		srv, _, fetcher := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html") // the source lies; the bytes decide
			w.Write(body)
		})
		image, err := fetcher.Fetch(context.Background(), srv.URL+"/a")
		if err != nil || image.ContentType != name || len(image.Data) != len(body) {
			t.Errorf("%s: got %q, %d bytes, %v", name, image.ContentType, len(image.Data), err)
		}
	}
}

func TestFetchRefusesWhatIsNotARasterImage(t *testing.T) {
	cases := map[string][]byte{"svg": svgBytes, "html": []byte("<html><body>hi</body></html>"), "empty": nil, "text": []byte("just text")}
	for name, body := range cases {
		srv, _, fetcher := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png") // declared as an image, bytes say otherwise
			w.Write(body)
		})
		if _, err := fetcher.Fetch(context.Background(), srv.URL+"/a"); !errors.Is(err, mailimage.ErrNotImage) {
			t.Errorf("%s: got %v, want ErrNotImage", name, err)
		}
	}
}

func TestFetchRefusesAnImageOverTheSizeCap(t *testing.T) {
	srv, _, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(pngBytes)
		w.Write(make([]byte, 2048))
	})
	fetcher := mailimage.NewFetcher(mailimage.Options{AllowAddresses: map[string]bool{srv.Listener.Addr().String(): true}, MaxBytes: 1024})
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/big.png"); !errors.Is(err, mailimage.ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

func TestFetchSendsNothingTheViewerOwns(t *testing.T) {
	var seen http.Header
	srv, _, fetcher := serve(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Write(pngBytes)
	})
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/a.png"); err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"Cookie", "Authorization", "Referer", "Origin", "X-Forwarded-For"} {
		if seen.Get(header) != "" {
			t.Errorf("%s was forwarded: %q", header, seen.Get(header))
		}
	}
	if agent := seen.Get("User-Agent"); agent == "" || strings.Contains(agent, "Go-http-client") || strings.Contains(strings.ToLower(agent), "colliedesk") {
		t.Errorf("User-Agent %q is not neutral", agent)
	}
}

func TestFetchRefusesBlockedAddressesWithoutConnecting(t *testing.T) {
	var hits atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.Write(pngBytes) }))
	defer local.Close()
	port := strings.TrimPrefix(local.URL, "http://127.0.0.1:")
	fetcher := mailimage.NewFetcher(mailimage.Options{})
	targets := []string{
		local.URL + "/a.png",                      // loopback literal
		"http://localhost:" + port + "/a.png",     // a name that resolves to loopback
		"http://[::1]:" + port + "/a.png",         // IPv6 loopback
		"http://169.254.169.254/latest/meta-data", // cloud metadata
		"http://[fd00:ec2::254]/latest/meta-data", // AWS IPv6 metadata
		"http://10.0.0.1/a.png", "http://192.168.1.1/a.png", "http://172.16.0.1/a.png",
		"http://100.64.0.1/a.png", "http://0.0.0.0/a.png", "http://[::]/a.png", "http://224.0.0.1/a.png",
		"http://[::ffff:127.0.0.1]:" + port + "/a.png", "http://[fe80::1]/a.png",
	}
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := fetcher.Fetch(ctx, target)
		cancel()
		if !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Errorf("%s: got %v, want a blocked address", target, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("the loopback server received %d requests", hits.Load())
	}
}

func TestFetchChecksEveryRedirectHop(t *testing.T) {
	var hits atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.Write(pngBytes) }))
	defer private.Close()
	srv, _, fetcher := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secret.png", http.StatusFound)
	})
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/a.png"); !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("redirect to loopback: got %v, want a blocked address", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("the redirect target received %d requests", hits.Load())
	}
}

func TestFetchFollowsAtMostThreeRedirects(t *testing.T) {
	for redirects, wantOK := range map[int]bool{3: true, 4: false} {
		var srv *httptest.Server
		srv, _, fetcher := serve(t, func(w http.ResponseWriter, r *http.Request) {
			var step int
			fmt.Sscanf(r.URL.Path, "/hop/%d", &step)
			if step < redirects {
				http.Redirect(w, r, fmt.Sprintf("%s/hop/%d", srv.URL, step+1), http.StatusFound)
				return
			}
			w.Write(pngBytes)
		})
		_, err := fetcher.Fetch(context.Background(), srv.URL+"/hop/0")
		if (err == nil) != wantOK {
			t.Errorf("%d redirects: error %v, want success=%v", redirects, err, wantOK)
		}
	}
}

func TestFetchDropsRefererOnRedirectAndRefusesOtherSchemes(t *testing.T) {
	var referer string
	var srv *httptest.Server
	srv, _, fetcher := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, srv.URL+"/end.png", http.StatusFound)
			return
		}
		referer = r.Header.Get("Referer")
		w.Write(pngBytes)
	})
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/start"); err != nil || referer != "" {
		t.Fatalf("redirect: err %v, Referer %q", err, referer)
	}
	for _, target := range []string{"file:///etc/passwd", "ftp://example.com/a.png", "gopher://example.com/", "/relative.png", "https://"} {
		if _, err := fetcher.Fetch(context.Background(), target); err == nil {
			t.Errorf("%s was fetched", target)
		}
	}
}

func TestFetchStopsWhenTheSourceIsTooSlow(t *testing.T) {
	srv, _, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	fetcher := mailimage.NewFetcher(mailimage.Options{AllowAddresses: map[string]bool{srv.Listener.Addr().String(): true}, Timeout: 200 * time.Millisecond})
	started := time.Now()
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/slow.png"); err == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(started))
	}
}
