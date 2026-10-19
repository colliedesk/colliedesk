package mailimage_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// proxiedURLs returns the original addresses of every proxy path found in text, in order. The proxy path may
// sit in HTML (entity-escaped) or in CSS text.
func proxiedURLs(t *testing.T, text string) []string {
	t.Helper()
	var found []string
	for _, match := range regexp.MustCompile(`/api/v1/mail-image\?[^"')\s<]*`).FindAllString(htmlUnescape(text), -1) {
		found = append(found, proxied(t, signer(), match))
	}
	return found
}

func TestRewriteSendsCSSBackgroundImagesThroughTheProxy(t *testing.T) {
	cases := map[string]struct{ html, want string }{
		"unquoted in a style attribute":     {`<td style="background:url(https://a.example/logo.png) no-repeat">x</td>`, "https://a.example/logo.png"},
		"double quoted with a query":        {`<td style='background-image: url("https://a.example/l.png?x=1&y=2")'>x</td>`, "https://a.example/l.png?x=1&y=2"},
		"single quoted, spaces, upper case": {`<td style="BACKGROUND: URL( 'https://a.example/l.png' )">x</td>`, "https://a.example/l.png"},
		"protocol relative in a style tag":  {`<style>.logo{background-image:url(//cdn.example/x.png)}</style>`, "https://cdn.example/x.png"},
		"list-style-image":                  {`<ul style="list-style-image:url(https://a.example/dot.gif)"><li>x</li></ul>`, "https://a.example/dot.gif"},
		"border-image":                      {`<div style="border-image:url(https://a.example/b.png) 30 round">x</div>`, "https://a.example/b.png"},
		"content":                           {`<style>.q:before{content:url(https://a.example/q.png)}</style>`, "https://a.example/q.png"},
		"css escape for a space":            {`<div style="background:url(https://a.example/a\ b.png)">x</div>`, "https://a.example/a%20b.png"},
		"css hex escape":                    {`<div style="background:url('https://a.example/\41 .png')">x</div>`, "https://a.example/A.png"},
		"inside image-set":                  {`<div style='background-image:image-set("https://a.example/1x.png" 1x, "https://a.example/2x.png" 2x)'>x</div>`, "https://a.example/1x.png"},
		"on an img tag too":                 {`<img src="data:image/gif;base64,R0lGOD" style="background:url(https://a.example/bg.png)">`, "https://a.example/bg.png"},
		"after a comment":                   {`<style>/* url(https://comment.example/no.png) */ .a{background:url(https://a.example/yes.png)}</style>`, "https://a.example/yes.png"},
	}
	for name, tc := range cases {
		out := signer().Rewrite(workspace, tc.html)
		got := proxiedURLs(t, out)
		if len(got) == 0 || got[0] != tc.want {
			t.Errorf("%s: proxied %v, want first %q in %q", name, got, tc.want, out)
		}
		if strings.Contains(out, "a.example/") && !strings.Contains(out, "%2F") && name != "inside image-set" {
			t.Errorf("%s: a remote address is still written in the CSS: %q", name, out)
		}
		for _, remote := range []string{"https://a.example", "//cdn.example", "http://a.example"} {
			if strings.Contains(htmlUnescape(out), "url("+remote) || strings.Contains(htmlUnescape(out), "(\""+remote) || strings.Contains(htmlUnescape(out), "('"+remote) {
				t.Errorf("%s: still loads %s directly: %q", name, remote, out)
			}
		}
	}
}

func TestRewriteImageSetRewritesEveryCandidate(t *testing.T) {
	out := signer().Rewrite(workspace, `<div style='background-image:image-set("https://a.example/1x.png" 1x, "https://a.example/2x.png" 2x)'>x</div>`)
	got := proxiedURLs(t, out)
	if len(got) != 2 || got[0] != "https://a.example/1x.png" || got[1] != "https://a.example/2x.png" || strings.Contains(out, `"https://a.example`) {
		t.Fatalf("proxied %v in %q", got, out)
	}
}

func TestRewriteLeavesLocalAndQuotedLookalikesInCSSAlone(t *testing.T) {
	in := `<style>.a{background:url(data:image/png;base64,iVBORw0KGgo=)}.b{background:url(/relative.png)}.c{background:url(cid:logo@x)}` +
		`.d{content:"url(https://a.example/in-a-string.png)"}.e{content:'url(https://a.example/also.png)'}` +
		`/* url(https://a.example/in-a-comment.png) */.f{color:red}.g::after{content:"\"; url(https://a.example/x.png)"}</style>`
	if out := signer().Rewrite(workspace, in); out != in {
		t.Errorf("CSS that loads nothing remote changed:\n got %q\nwant %q", out, in)
	}
}

func TestRewriteDropsRemoteImportsAndKeepsTheRules(t *testing.T) {
	in := `<style>@import url(https://evil.example/a.css);@import "https://evil.example/b.css" screen;@IMPORT 'https://evil.example/c.css';.keep{color:red}</style>`
	out := signer().Rewrite(workspace, in)
	if strings.Contains(strings.ToLower(out), "@import") || strings.Contains(out, "evil.example") {
		t.Errorf("an @import survived: %q", out)
	}
	if !strings.Contains(out, ".keep{color:red}") {
		t.Errorf("the rules after the imports were lost: %q", out)
	}
}

func TestRewriteCountsCSSImagesInTheFiftyPerMessageLimit(t *testing.T) {
	var in strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&in, `<img src="https://x.example/i%d.png">`, i)
	}
	in.WriteString(`<style>`)
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&in, `.c%d{background:url(https://x.example/c%d.png)}`, i, i)
	}
	in.WriteString(`</style>`)
	out := signer().Rewrite(workspace, in.String())
	if got := proxiedURLs(t, out); len(got) != 50 {
		t.Fatalf("%d distinct images proxied, want 50 across img tags and CSS", len(got))
	}
	if strings.Contains(out, "https://x.example") {
		t.Errorf("an image past the limit keeps its remote address: %q", out)
	}
}
