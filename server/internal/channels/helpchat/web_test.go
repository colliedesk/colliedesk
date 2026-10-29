package helpchat_test

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
)

// The chat's files stay small (docs/internals/performance.md, Web bundle budget): chat.js at most 25 KB,
// widget.js 6 KB and chat.css 8 KB, gzip. chat.js writes the page only through textContent and
// elements, never HTML strings (Trusted Types), and chat.css names no color of its own.
func TestTheChatFilesStayInTheirBudget(t *testing.T) {
	for name, budget := range map[string]int{"chat.js": 25 << 10, "widget.js": 6 << 10, "chat.css": 8 << 10} {
		file, ok := helpchat.Asset(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		var zipped bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&zipped, gzip.BestCompression)
		_, _ = writer.Write(file.Body)
		_ = writer.Close()
		t.Logf("%s: %d bytes gzip (budget %d)", name, zipped.Len(), budget)
		if zipped.Len() > budget {
			t.Errorf("%s is %d bytes gzip, over %d", name, zipped.Len(), budget)
		}
	}
	script, _ := helpchat.Asset("chat.js")
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if bytes.Contains(script.Body, []byte(banned)) {
			t.Errorf("chat.js uses %s", banned)
		}
	}
	style, _ := helpchat.Asset("chat.css")
	for _, line := range strings.Split(string(style.Body), "\n") {
		code, _, _ := strings.Cut(line, "/*")
		if strings.Contains(code, "#") && !strings.Contains(code, "#fff") {
			t.Errorf("chat.css names a color of its own: %s", strings.TrimSpace(line))
		}
	}
}
