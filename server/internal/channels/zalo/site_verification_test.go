package zalo_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
)

// A person pastes what Zalo shows: the code, the TXT record, the meta tag, the file's name or address, or
// the whole file. Each gives the same code; anything else is refused.
func TestASiteVerificationCodeIsReadFromWhateverZaloShows(t *testing.T) {
	const code = "OOMiTAgDDsCXlvT2meuJUq6BicIj_Ma_DJKs"
	for _, pasted := range []string{
		code,
		"  " + code + "\n",
		"zalo-platform-site-verification=" + code,
		`<meta property="zalo-platform-site-verification" content="` + code + `" />`,
		`<meta name='zalo-platform-site-verification' content='` + code + `'>`,
		"zalo_verifier" + code + ".html",
		"zalo_verifier" + code,
		" zalo_verifier" + code + ".html ",
		"https://desk.example.com/api/v1/channels/zalo/zalo_verifier" + code,
		"https://desk.example.com/api/v1/channels/zalo/zalo_verifier" + code + ".html",
		`<!DOCTYPE html><html lang="en"><head><meta property="zalo-platform-site-verification" content="` + code +
			`" /></head><body>There Is No Limit To What You Can Accomplish Using Zalo!</body></html>`,
	} {
		got, ok := zalo.SiteVerificationCode(pasted)
		if !ok || got != code {
			t.Errorf("SiteVerificationCode(%q) = %q, %v; want %q", pasted, got, ok, code)
		}
	}
	for _, pasted := range []string{"", "short", "has space inside code", "abc/def/ghi/jkl", "<script>alert(1)</script>",
		"zalo_verifier../../etc.html", string(make([]byte, 200))} {
		if got, ok := zalo.SiteVerificationCode(pasted); ok {
			t.Errorf("SiteVerificationCode(%q) = %q, accepted", pasted, got)
		}
	}
}

// The file Zalo fetches is its own format, named and filled by the code.
func TestTheSiteVerificationFileIsZalosFormat(t *testing.T) {
	const code = "abcDEF012_-xyz789"
	got, ok := zalo.SiteVerificationFileCode("zalo_verifier" + code + ".html")
	if !ok || got != code {
		t.Fatalf("file code = %q, %v", got, ok)
	}
	for _, name := range []string{"zalo_verifier.html", "zalo_verifier" + code + ".htm", "Zalo_verifier" + code + ".html", "zalo_verifier" + code + ".html.bak", "zalo_verifier a.html"} {
		if _, ok := zalo.SiteVerificationFileCode(name); ok {
			t.Errorf("%q was read as a verification file", name)
		}
	}
	want := `<!DOCTYPE html><html lang="en"><head><meta property="zalo-platform-site-verification" content="` + code +
		`" /></head><body>There Is No Limit To What You Can Accomplish Using Zalo!</body></html>`
	if body := string(zalo.SiteVerificationFile(code)); body != want {
		t.Fatalf("file = %s", body)
	}
}
