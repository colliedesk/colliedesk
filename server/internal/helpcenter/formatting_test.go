package helpcenter

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The formatting subset of an article (underline, highlight, colour, alignment, indent and tables
// with merged or coloured cells) renders with the help center's own classes, never a style attribute.
func TestArticleFormattingRendersWithHelpCenterClasses(t *testing.T) {
	body := "<p data-indent=\"2\" data-align=\"center\">\n\n" +
		"Say <u>under **b**</u> <mark>hi</mark> <span style=\"color:red\" data-color=\"danger\">red</span> <span data-color=\"pink\">plain</span>\n\n" +
		"</p>\n\n" +
		"<h2 data-align=\"right\">\n\n## Title\n\n</h2>\n\n" +
		"<table>\n<tr>\n<th colspan=\"2\" data-align=\"center\" data-bg=\"accent\" style=\"background:red\">\n\nHead\n\n</th>\n</tr>\n" +
		"<tr>\n<td data-bg=\"success\">\n\na\n\n</td>\n<td>\n\nb\n\nc\n\n</td>\n</tr>\n</table>\n"
	out, err := renderArticle(body, "")
	if err != nil {
		t.Fatal(err)
	}
	want := `<p class="kb-align-center kb-indent-2">Say <u>under <strong>b</strong></u> <mark>hi</mark> <span class="kb-color-danger">red</span> plain</p>
<h2 id="title" class="kb-align-right">Title</h2>
<table>
<tbody>
<tr>
<th colspan="2" class="kb-align-center kb-bg-accent"><p>Head</p>
</th>
</tr>
<tr>
<td class="kb-bg-success"><p>a</p>
</td>
<td><p>b</p>
<p>c</p>
</td>
</tr>
</tbody>
</table>
`
	if string(out) != want {
		t.Fatalf("rendered\n%s\nwant\n%s", out, want)
	}
}

// An inline formatting tag without its partner, or across another element's edge, is left out with
// its text kept, so it can never wrap the rest of the page.
func TestArticleFormattingLeavesOutUnpairedTags(t *testing.T) {
	out, err := renderArticle("one <u>two **three</u> four** five </mark>", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "<p>one two <strong>three four</strong> five </p>\n"; string(out) != want {
		t.Fatalf("rendered %q, want %q", out, want)
	}
}

// The page's policy stays exactly what it was before articles could hold formatting: colours and
// alignment come from the style sheet's classes, so no inline style is ever allowed.
func TestPagePolicyAllowsNoInlineStyle(t *testing.T) {
	want := "default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if contentSecurityPolicy != want {
		t.Fatalf("policy %q, want %q", contentSecurityPolicy, want)
	}
}

// Every class the renderer writes has a rule in the style sheet, in light and in dark.
func TestStyleSheetStylesEveryFormattingClass(t *testing.T) {
	css, err := os.ReadFile("static/site.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(css)
	dark := sheet[strings.Index(sheet, "@media (prefers-color-scheme: dark)"):]
	for _, class := range []string{"kb-align-center", "kb-align-right", "kb-indent-1", "kb-indent-4",
		"kb-color-accent", "kb-color-success", "kb-color-warning", "kb-color-danger", "kb-color-muted",
		"kb-bg-accent", "kb-bg-success", "kb-bg-warning", "kb-bg-danger", "kb-bg-muted"} {
		if !regexp.MustCompile(`\.` + class + `\b`).MatchString(sheet) {
			t.Errorf("site.css has no rule for .%s", class)
		}
	}
	for _, token := range []string{"--kb-success", "--kb-warning", "--kb-danger"} {
		if !strings.Contains(dark, token+":") {
			t.Errorf("the dark theme does not set %s", token)
		}
	}
}
