package helpcenter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicPagesMakeNoExternalCall: a visitor's page, sitemap, llms.txt or Markdown reads only the
// database and the server's own storage, so no other service's latency or outage reaches it. Only
// the admin's domain check (domain-check.go) asks DNS.
func TestPublicPagesMakeNoExternalCall(t *testing.T) {
	outbound := []string{"http.Client", "http.DefaultClient", "http.Get(", "http.Head(", "http.Post", "http.NewRequest", "http.DefaultTransport",
		"net.Dial", "LookupTXT", "net.Resolver", "net.Lookup", "net.DefaultResolver", "smtp."}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") || source == "domain-check.go" {
			continue
		}
		text, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range outbound {
			if strings.Contains(string(text), call) {
				t.Errorf("%s uses %s: the public help center must not call another service", source, call)
			}
		}
	}
}
