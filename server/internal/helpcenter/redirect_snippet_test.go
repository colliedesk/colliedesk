package helpcenter_test

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
)

const snippetAddress = "https://acme.help.example.net"

func importedRedirects() helpcenter.LegacyRedirects {
	return helpcenter.LegacyRedirects{
		Articles:   map[string]string{"7201": "/a/12-pay-by-card"},
		Folders:    map[string]string{"21": "/c/billing/cards"},
		Categories: map[string]string{"11": "/c/billing"},
	}
}

// scriptOf cuts the snippet's JavaScript out of its <script> element.
func scriptOf(t *testing.T, snippet string) string {
	t.Helper()
	open, closing := strings.Index(snippet, "<script>"), strings.LastIndex(snippet, "</script>")
	if open < 0 || closing < open || strings.Count(snippet, "</script>") != 1 {
		t.Fatalf("snippet has no single <script> element:\n%s", snippet)
	}
	return snippet[open+len("<script>") : closing]
}

// visits runs the snippet in Node with a stub location for each old address and returns where it sent
// the visitor ("" when it left the page alone).
func visits(t *testing.T, snippet string, addresses []string) map[string]string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	input, _ := json.Marshal(addresses)
	program := `const script = process.argv[1], visits = JSON.parse(process.argv[2]), out = {};
for (const visit of visits) {
  const [pathname, search = ''] = visit.split('?');
  let sent = '';
  new Function('location', script)({ pathname, search: search ? '?' + search : '', replace(to) { sent = to; } });
  out[visit] = sent;
}
console.log(JSON.stringify(out));`
	output, err := exec.Command(node, "-e", program, scriptOf(t, snippet), string(input)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, output)
	}
	var sent map[string]string
	if err := json.Unmarshal(output, &sent); err != nil {
		t.Fatalf("node answered %s", output)
	}
	return sent
}

// Each kind of old address goes to its new page; an item not in the import goes to the help center's own
// old-address handler by its id; the pages that are not help pages stay.
func TestSnippetSendsEveryOldAddressToItsNewPage(t *testing.T) {
	snippet, mapped := helpcenter.FreshdeskHeadSnippet(snippetAddress, importedRedirects())
	if !mapped {
		t.Fatal("a small import was not mapped")
	}
	want := map[string]string{
		"/support/solutions/articles/7201-pay-by-card":      snippetAddress + "/a/12-pay-by-card",
		"/de/support/solutions/articles/7201":               snippetAddress + "/a/12-pay-by-card",
		"/pt-BR/support/solutions/articles/7201-x/":         snippetAddress + "/a/12-pay-by-card",
		"/support/solutions/folders/21-cards":               snippetAddress + "/c/billing/cards",
		"/support/solutions/11-billing":                     snippetAddress + "/c/billing",
		"/support/solutions/articles/9999-not-imported":     snippetAddress + "/support/solutions/articles/9999",
		"/support/solutions/folders/9998":                   snippetAddress + "/support/solutions/folders/9998",
		"/support/solutions/9997":                           snippetAddress + "/support/solutions/9997",
		"/support/home":                                     snippetAddress + "/",
		"/support":                                          snippetAddress + "/",
		"/support/solutions":                                snippetAddress + "/",
		"/en/support/home":                                  snippetAddress + "/",
		"/support/search?term=reset%20password":             snippetAddress + "/support/search?term=reset%20password",
		"/support/tickets/new":                              "",
		"/support/login":                                    "",
		"/support/tickets/7201":                             "",
		"/":                                                 "",
		"/other/support/solutions/articles/7201":            "",
		"//evil.example/support/solutions/articles/7201":    "",
		"/support/solutions/articles/7201/../../../evil":    "",
		"/support/solutions/articles/0123":                  "",
		"/support/solutions/articles/7201-x%0d%0aLocation:": snippetAddress + "/a/12-pay-by-card",
	}
	addresses := make([]string, 0, len(want))
	for address := range want {
		addresses = append(addresses, address)
	}
	got := visits(t, snippet, addresses)
	for address, expected := range want {
		if got[address] != expected {
			t.Errorf("%s -> %q, want %q", address, got[address], expected)
		}
		if got[address] != "" && !strings.HasPrefix(got[address], snippetAddress+"/") {
			t.Errorf("%s -> %q leaves the help center", address, got[address])
		}
	}
}

// What the snippet embeds is JSON-escaped, so no title, slug or address can close the script element.
func TestSnippetEscapesWhatItEmbeds(t *testing.T) {
	hostile := helpcenter.LegacyRedirects{Articles: map[string]string{"1": `/a/1-</script><script>alert(1)</script>"';`}}
	snippet, _ := helpcenter.FreshdeskHeadSnippet(`https://x.example/"</script>`, hostile)
	if strings.Count(snippet, "</script>") != 1 || strings.Count(snippet, "<script>") != 1 {
		t.Fatalf("a value closed or opened the script element:\n%s", snippet)
	}
	if strings.Contains(snippet, `alert(1)</script>`) {
		t.Fatalf("a value reached the page unescaped:\n%s", snippet)
	}
}

// A knowledge base too large to embed is not mapped: the snippet still redirects every old address by its
// id to the help center, which holds the whole mapping.
func TestSnippetOfALargeKnowledgeBaseDropsTheMapAndStillRedirects(t *testing.T) {
	articles := map[string]string{}
	for i := 0; i < 2000; i++ {
		articles[strconv.Itoa(100000+i)] = "/a/" + strconv.Itoa(i+1) + "-a-title-that-takes-room-in-the-snippet"
	}
	snippet, mapped := helpcenter.FreshdeskHeadSnippet(snippetAddress, helpcenter.LegacyRedirects{Articles: articles})
	if mapped || len(snippet) > 8*1024 {
		t.Fatalf("mapped=%t, snippet is %d bytes", mapped, len(snippet))
	}
	got := visits(t, snippet, []string{"/support/solutions/articles/7201-x"})
	if got["/support/solutions/articles/7201-x"] != snippetAddress+"/support/solutions/articles/7201" {
		t.Fatalf("unmapped snippet sent the visitor to %q", got["/support/solutions/articles/7201-x"])
	}
}
