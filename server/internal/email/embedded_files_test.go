package email_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func part(contentType, extra string, data []byte) string {
	return "Content-Type: " + contentType + "\nContent-Transfer-Encoding: base64\n" + extra + "\n" + base64.StdEncoding.EncodeToString(data) + "\n"
}

// Only a part whose bytes went into the HTML as a data: URI is Embedded. The name of a file decides nothing.
func TestOnlyFilesEmbeddedInTheHTMLAreMarkedEmbedded(t *testing.T) {
	small := []byte("\x89PNG\r\n\x1a\nsmall-logo")
	big := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 6<<20))
	raw := "From: Nora <nora@sample.example>\nTo: support@desk.example\nSubject: Logos\nMessage-ID: <emb1@sample.example>\nMIME-Version: 1.0\n" +
		"Content-Type: multipart/related; boundary=\"r\"\n\n" +
		"--r\nContent-Type: text/html; charset=utf-8\n\n<p><img src=\"cid:small@x\"><img src=\"cid:big@x\"></p>\n" +
		"--r\n" + part("image/png", "Content-ID: <small@x>\nContent-Disposition: inline; filename=\"small.png\"\n", small) +
		"--r\n" + part("image/png", "Content-ID: <big@x>\nContent-Disposition: inline; filename=\"big.png\"\n", big) +
		"--r\n" + part("image/png", "Content-ID: <unused@x>\nContent-Disposition: inline; filename=\"unused.png\"\n", small) +
		"--r\n" + part("image/png", "Content-Disposition: attachment; filename=\"inline-image-0123456789abcdef.png\"\n", small) +
		"--r--\n"
	parsed, err := email.Parse(crlf(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	embedded := map[string]bool{}
	for _, file := range parsed.Files {
		embedded[file.Filename] = file.Embedded
	}
	want := map[string]bool{"small.png": true, "big.png": false, "unused.png": false, "inline-image-0123456789abcdef.png": false}
	if len(embedded) != len(want) {
		t.Fatalf("files = %v", embedded)
	}
	for name, marked := range want {
		if embedded[name] != marked {
			t.Errorf("%s: Embedded=%v, want %v", name, embedded[name], marked)
		}
	}
	if !strings.Contains(parsed.BodyHTML, "data:image/png;base64,") || strings.Contains(parsed.BodyHTML, "cid:small") {
		t.Errorf("the small image was not embedded: %q", parsed.BodyHTML)
	}
}
