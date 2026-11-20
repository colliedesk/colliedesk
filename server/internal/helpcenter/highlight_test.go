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

// TestALanguageIsFoundByItsNameOrAliasOnly: the language after the fence is looked up by a name or
// an alias of the languages the help center colours, in any case. It is never matched as a file name
// or guessed (chroma's fallback for an unknown name scans every lexer: 3 to 15 ms a block), and a
// language whose lexer can stall on one token is not on the list.
func TestALanguageIsFoundByItsNameOrAliasOnly(t *testing.T) {
	for _, body := range []string{"```py\nx = 1\n```", "```GO\nfunc main() {}\n```", "```shell\necho hi\n```", "```yml\na: 1\n```"} {
		got, err := renderArticle(body, "/help/acme")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), `<span class="`) {
			t.Errorf("%q is not coloured:\n%s", body, got)
		}
	}
	plain := map[string]string{
		"```main.go\nfunc main() {}\n```":   "<pre><code class=\"language-main.go\">func main() {}\n</code></pre>\n",
		"```cfengine3\nbundle agent x\n```": "<pre><code class=\"language-cfengine3\">bundle agent x\n</code></pre>\n",
		"```markdown\n# a\n```":             "<pre><code class=\"language-markdown\"># a\n</code></pre>\n",
	}
	for body, want := range plain {
		got, err := renderArticle(body, "/help/acme")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%q rendered\n%s\nwant\n%s", body, got, want)
		}
	}
}

// TestAnArticleColoursAtMostItsLimitOfCode: an article colours code blocks while their total stays
// within 10,000 characters; a block that would pass it stays plain, and a later one that fits is
// still coloured. Highlighting multiplies a block's HTML by up to 18, so this bounds the page too.
func TestAnArticleColoursAtMostItsLimitOfCode(t *testing.T) {
	block := strings.Repeat("x := 1\n", 571) // 3,997 characters
	body := "```go\n" + block + "```\n\n```go\n" + block + "```\n\n```go\n" + block + "```\n\n```go\nx := 2\n```\n"
	got, err := renderArticle(body, "/help/acme")
	if err != nil {
		t.Fatal(err)
	}
	page := string(got)
	if coloured := strings.Count(page, `<pre><code class="language-go"><span`); coloured != 3 {
		t.Errorf("%d blocks coloured, want 3 (the first two and the last)", coloured)
	}
	if !strings.Contains(page, "<pre><code class=\"language-go\">"+block+"</code></pre>\n") {
		t.Error("the third block, past the limit, is not plain text")
	}
}

// TestCodeLexedPastTheArticlesTimeStaysPlain: some lexers take seconds on a few kilobytes of crafted
// code (8,000 newlines in C, colons in YAML), so an article's lexing has a time limit; once it is
// spent the block being coloured and every later block show plain.
func TestCodeLexedPastTheArticlesTimeStaysPlain(t *testing.T) {
	saved := highlightTime
	highlightTime = 0
	t.Cleanup(func() { highlightTime = saved })
	got, err := renderArticle("```go\nfunc main() {}\n```\n\n```bash\necho hi\n```", "/help/acme")
	if err != nil {
		t.Fatal(err)
	}
	want := "<pre><code class=\"language-go\">func main() {}\n</code></pre>\n<pre><code class=\"language-bash\">echo hi\n</code></pre>\n"
	if string(got) != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}
}

// TestEveryCodeLanguageIsAChromaLexer: a name on the list that chroma does not have under exactly
// that name would silently colour nothing (or the wrong language).
func TestEveryCodeLanguageIsAChromaLexer(t *testing.T) {
	for _, name := range codeLanguages {
		if codeLexers()[strings.ToLower(name)] == nil {
			t.Errorf("%q is not a chroma lexer name", name)
		}
	}
}
