package helpcenter

import (
	"strings"
	"testing"
)

// TestCodeWithALanguageIsHighlightedWithClassesOnly: a fenced block in a known language gets token
// classes the site's style sheet colours; nothing inline, so the page's CSP stays as it is.
func TestCodeWithALanguageIsHighlightedWithClassesOnly(t *testing.T) {
	got, err := renderArticle("```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```", "/help/acme")
	if err != nil {
		t.Fatal(err)
	}
	page := string(got)
	for _, want := range []string{`<pre><code class="language-go">`, `<span class="kd">func</span>`, `<span class="s">`, "</code></pre>\n"} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered\n%s\nhas no %s", page, want)
		}
	}
	for _, bad := range []string{"style", "<script", "tabindex", "<div", "<pre class"} {
		if strings.Contains(page, bad) {
			t.Errorf("rendered\n%s\nholds %q", page, bad)
		}
	}
}

// TestCodeThatCannotBeHighlightedStaysPlainText: no language, a language the help center does not
// know, or a block too long to colour quickly is escaped text, as before.
func TestCodeThatCannotBeHighlightedStaysPlainText(t *testing.T) {
	long := strings.Repeat("echo <b>x</b>\n", 3000)
	cases := map[string]string{
		"```\n<b>x</b>\n```":           "<pre><code>&lt;b&gt;x&lt;/b&gt;\n</code></pre>\n",
		"```nosuchlang\n<b>x</b>\n```": "<pre><code class=\"language-nosuchlang\">&lt;b&gt;x&lt;/b&gt;\n</code></pre>\n",
		"```bash\n" + long + "```":     "<pre><code class=\"language-bash\">" + strings.Repeat("echo &lt;b&gt;x&lt;/b&gt;\n", 3000) + "</code></pre>\n",
		"    indented <b>code</b>\n":   "<pre><code>indented &lt;b&gt;code&lt;/b&gt;\n</code></pre>\n",
	}
	for body, want := range cases {
		got, err := renderArticle(body, "/help/acme")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%.40q rendered\n%.300s\nwant\n%.300s", body, got, want)
		}
	}
}

// TestHighlightedHTMLCodeIsShownNotRun: markup inside a code block is text, highlighted or not.
func TestHighlightedHTMLCodeIsShownNotRun(t *testing.T) {
	got, err := renderArticle("```html\n<script>alert(1)</script>\n<img src=x onerror=alert(1)>\n```", "/help/acme")
	if err != nil {
		t.Fatal(err)
	}
	page := string(got)
	if strings.Contains(page, "<script") || strings.Contains(page, "<img") || !strings.Contains(page, "&lt;") {
		t.Errorf("rendered\n%s", page)
	}
}
