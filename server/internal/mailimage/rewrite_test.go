package mailimage_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

const workspace = "11111111-1111-4111-8111-111111111111"

func signer() *mailimage.Signer {
	return mailimage.NewSigner([]byte("test-key-of-at-least-32-bytes-long!!"))
}

// proxied extracts the original URL a rewritten src stands for, and checks that its signature holds.
func proxied(t *testing.T, s *mailimage.Signer, src string) string {
	t.Helper()
	const prefix = "/api/v1/mail-image?"
	if !strings.HasPrefix(src, prefix) {
		t.Fatalf("src %q is not a proxy path", src)
	}
	query, err := url.ParseQuery(strings.TrimPrefix(src, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(workspace, query.Get("u"), query.Get("s")) {
		t.Fatalf("signature of %q does not verify", src)
	}
	return query.Get("u")
}

func TestRewriteSendsRemoteImagesThroughTheProxy(t *testing.T) {
	s := signer()
	out := s.Rewrite(workspace, `<p>Hi</p><img alt="logo" src="https://cdn.example.com/logo.png?v=1&amp;w=2" width="120"><IMG SRC='http://news.example.org/banner.gif'><img src="//static.example.net/a.jpg">`)
	srcs := regexpAll(t, out, `(?i)src="([^"]*)"`)
	if len(srcs) != 3 {
		t.Fatalf("got %d src attributes in %q", len(srcs), out)
	}
	want := []string{"https://cdn.example.com/logo.png?v=1&w=2", "http://news.example.org/banner.gif", "https://static.example.net/a.jpg"}
	for i, src := range srcs {
		if got := proxied(t, s, htmlUnescape(src)); got != want[i] {
			t.Errorf("image %d proxies %q, want %q", i, got, want[i])
		}
	}
	if !strings.Contains(out, `alt="logo"`) || !strings.Contains(out, `width="120"`) {
		t.Errorf("other attributes were lost: %q", out)
	}
	if strings.Contains(out, "cdn.example.com/logo.png?v=1&amp;w=2\"") {
		t.Errorf("the remote address is still a src: %q", out)
	}
}

func TestRewriteLeavesLocalSourcesAlone(t *testing.T) {
	in := `<img src="data:image/png;base64,iVBORw0KGgo="><img src="cid:logo@x"><img src="/relative.png"><img src="javascript:alert(1)"><img src="ftp://host/a.png">`
	if out := signer().Rewrite(workspace, in); out != in {
		t.Errorf("local sources changed:\n got %q\nwant %q", out, in)
	}
}

func TestRewriteStripsSrcsetSoNothingLoadsAroundTheProxy(t *testing.T) {
	out := signer().Rewrite(workspace, `<img src="https://a.example/1.png" srcset="https://a.example/2.png 2x, https://a.example/3.png 3x" alt="x"><img srcset='https://a.example/4.png 1x' src="data:image/gif;base64,R0lGOD">`)
	if strings.Contains(strings.ToLower(out), "srcset") || strings.Contains(out, "a.example/2.png") || strings.Contains(out, "a.example/4.png") {
		t.Errorf("srcset survived: %q", out)
	}
	if !strings.Contains(out, `alt="x"`) || !strings.Contains(out, "data:image/gif;base64,R0lGOD") {
		t.Errorf("rewrite dropped more than srcset: %q", out)
	}
}

func TestSignatureBindsTheWorkspaceAndTheExactURL(t *testing.T) {
	s := signer()
	sig := s.Sign(workspace, "https://a.example/x.png")
	if !s.Valid(workspace, "https://a.example/x.png", sig) {
		t.Fatal("a fresh signature does not verify")
	}
	for name, ok := range map[string]bool{
		"other url":       s.Valid(workspace, "https://a.example/y.png", sig),
		"other workspace": s.Valid("22222222-2222-4222-8222-222222222222", "https://a.example/x.png", sig),
		"empty":           s.Valid(workspace, "https://a.example/x.png", ""),
		"not hex":         s.Valid(workspace, "https://a.example/x.png", "zz"),
		"other key":       mailimage.NewSigner([]byte("another-key-of-at-least-32-bytes!!!")).Valid(workspace, "https://a.example/x.png", sig),
	} {
		if ok {
			t.Errorf("signature accepted for %s", name)
		}
	}
}
