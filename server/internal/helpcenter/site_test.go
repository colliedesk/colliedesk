package helpcenter

import (
	"io/fs"
	"strings"
	"testing"
)

// The help center is the customer's own site (#125): no template or style may name the product, and
// no page may carry a script or inline style.
func TestTemplatesCarryNoBrandingScriptOrInlineStyle(t *testing.T) {
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		text := strings.ToLower(string(data))
		for _, banned := range []string{"colliedesk", "hc labs", "powered by", "<script", "style=", "<style", "@import", "url("} {
			if strings.Contains(text, banned) {
				t.Errorf("%s holds %q", path, banned)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	parsePages()
}

func TestAddresses(t *testing.T) {
	cases := []struct{ origin, domain, slug, want string }{
		{"https://desk.example.com/", "", "acme", "https://desk.example.com/help/acme"},
		{"https://app.example.com", "help.example.com", "acme", "https://acme.help.example.com"},
		{"http://127.0.0.1:8080", "help.test", "acme", "http://acme.help.test:8080"},
		// A slug that cannot be a host name stays on the path address.
		{"https://app.example.com", "help.example.com", "www", "https://app.example.com/help/www"},
	}
	for _, c := range cases {
		if got := URL(c.origin, c.domain, c.slug); got != c.want {
			t.Errorf("URL(%q, %q, %q) = %q, want %q", c.origin, c.domain, c.slug, got, c.want)
		}
	}
	if got := ArticleURL("https://d.example", "", "acme", 12, "réinitialiser-mot-de-passe"); got != "https://d.example/help/acme/a/12-r%C3%A9initialiser-mot-de-passe" {
		t.Errorf("ArticleURL = %q", got)
	}
	for host, want := range map[string]string{"acme.help.example.com": "acme", "ACME.help.example.com:443": "acme", "acme.help.example.com.": "acme",
		"app.help.example.com": "", "a.b.help.example.com": "", "help.example.com": "", "acme.example.com": "", "xn--abc.help.example.com": ""} {
		if got, _ := hostSlug(host, "help.example.com", "desk.example.com"); got != want {
			t.Errorf("hostSlug(%q) = %q, want %q", host, got, want)
		}
	}
	if _, ok := hostSlug("desk.example.com", "example.com", "desk.example.com"); ok {
		t.Error("the server's own host was taken for a help center")
	}
	for value, ok := range map[string]bool{"": true, "help.example.com": true, " Help.Example.com. ": true, "localhost": false, "a b.com": false, "-x.com": false} {
		if _, err := DomainFromEnv(func(string) string { return value }); (err == nil) != ok {
			t.Errorf("DomainFromEnv(%q) error = %v", value, err)
		}
	}
}

// Every link a visitor taps on a phone is at least 44 px tall (2.75rem).
func TestTouchTargetsAreAtLeast44Pixels(t *testing.T) {
	style, err := files.ReadFile("static/site.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{".brand {", ".crumbs a {", ".card-tab a {", ".card-list a {", ".chips a {", ".card-more {", ".index-rail a {", ".result-title {", ".foot a {", ".pager a {", ".meta a {"} {
		start := strings.Index(string(style), selector)
		if start < 0 {
			t.Errorf("no rule %s", selector)
			continue
		}
		rule := string(style[start:])
		rule = rule[:strings.Index(rule, "}")]
		if !strings.Contains(rule, "min-height: 2.75rem") {
			t.Errorf("%s} has no min-height: 2.75rem", rule)
		}
	}
}
