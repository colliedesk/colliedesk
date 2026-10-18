package mailimage_test

import (
	"fmt"
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

func TestRewriteReadsTagsLikeABrowserDoes(t *testing.T) {
	s := signer()
	in := `<img alt="a>b" src="https://x.example/1.png"><img data-x=" src='https://evil.example/n.png'" src="https://x.example/2.png">` +
		`<!-- <img src="https://x.example/in-comment.png"> --><style>.a::after{content:"<img src='https://x.example/css.png'>"}</style>`
	out := s.Rewrite(workspace, in)
	var originals []string
	for _, src := range regexpAll(t, out, `(?i)\ssrc="(/api/v1/mail-image[^"]*)"`) {
		originals = append(originals, proxied(t, s, htmlUnescape(src)))
	}
	if len(originals) != 2 || originals[0] != "https://x.example/1.png" || originals[1] != "https://x.example/2.png" {
		t.Fatalf("proxied %v in %q", originals, out)
	}
	if !strings.Contains(out, `alt="a>b"`) && !strings.Contains(out, `alt="a&gt;b"`) {
		t.Errorf("the alt text was damaged: %q", out)
	}
	for _, text := range []string{"https://x.example/in-comment.png", "https://x.example/css.png", "evil.example"} {
		if strings.Contains(out, "mail-image?u="+url.QueryEscape(text)) {
			t.Errorf("%s was proxied from the wrong place: %q", text, out)
		}
	}
	if !strings.Contains(out, "data-x=") || !strings.Contains(out, "evil.example/n.png") {
		t.Errorf("another attribute's value was rewritten or lost: %q", out)
	}
}

func TestRewriteProxiesAtMostFiftyDistinctImagesPerMessage(t *testing.T) {
	s := signer()
	var in strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&in, `<img src="https://x.example/%d.png">`, i%55) // 55 distinct URLs, five of them twice
	}
	out := s.Rewrite(workspace, in.String())
	distinct := map[string]bool{}
	for _, src := range regexpAll(t, out, `(?i)\ssrc="(/api/v1/mail-image[^"]*)"`) {
		distinct[proxied(t, s, htmlUnescape(src))] = true
	}
	if len(distinct) != mailimage.MaxImagesPerMessage || mailimage.MaxImagesPerMessage != 50 {
		t.Fatalf("%d distinct images proxied, want 50", len(distinct))
	}
	if strings.Contains(out, "x.example/54.png") {
		t.Errorf("an image over the limit keeps its remote address: the viewer must show nothing for it")
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
