package helpcenter

import (
	"strings"
	"testing"
)

// TestCalloutsRenderWithTheirLabelAndANoteRole: GitHub's alert syntax renders as a labelled box a
// screen reader announces as a note, one per type.
func TestCalloutsRenderWithTheirLabelAndANoteRole(t *testing.T) {
	cases := map[string]string{
		"> [!NOTE]\n> Exports run at night.": `<aside class="callout callout-note" role="note">
<p class="callout-label">Note</p>
<p>Exports run at night.</p>
</aside>
`,
		"> [!TIP]\n> Press **/** to search.": `<aside class="callout callout-tip" role="note">
<p class="callout-label">Tip</p>
<p>Press <strong>/</strong> to search.</p>
</aside>
`,
		"> [!IMPORTANT]\n> Sign in as an admin.": `<aside class="callout callout-important" role="note">
<p class="callout-label">Important</p>
<p>Sign in as an admin.</p>
</aside>
`,
		"> [!WARNING]\n> Deleting a workspace cannot be undone.": `<aside class="callout callout-warning" role="note">
<p class="callout-label">Warning</p>
<p>Deleting a workspace cannot be undone.</p>
</aside>
`,
		"> [!CAUTION]\n> This removes every reply.\n>\n> - Export first\n> - Then delete": `<aside class="callout callout-caution" role="note">
<p class="callout-label">Caution</p>
<p>This removes every reply.</p>
<ul>
<li>Export first</li>
<li>Then delete</li>
</ul>
</aside>
`,
		// GitHub reads the type in any case.
		"> [!note]\n> Lower case.": `<aside class="callout callout-note" role="note">
<p class="callout-label">Note</p>
<p>Lower case.</p>
</aside>
`,
	}
	for body, want := range cases {
		got, err := renderArticle(body, "/help/acme")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%q rendered\n%s\nwant\n%s", body, got, want)
		}
	}
}

// TestOtherQuotesStayQuotes: only a known type alone on the first line makes a callout.
func TestOtherQuotesStayQuotes(t *testing.T) {
	cases := map[string]string{
		"> [!DANGER]\n> Unknown type.":      "<blockquote>\n<p>[!DANGER]\nUnknown type.</p>\n</blockquote>\n",
		"> [!NOTE] same line\n> text.":      "<blockquote>\n<p>[!NOTE] same line\ntext.</p>\n</blockquote>\n",
		"> Just a quote.":                   "<blockquote>\n<p>Just a quote.</p>\n</blockquote>\n",
		"Text [!NOTE] in a paragraph.":      "<p>Text [!NOTE] in a paragraph.</p>\n",
		"> - [!NOTE]\n>   inside a list item": "<blockquote>\n<ul>\n<li>[!NOTE]\ninside a list item</li>\n</ul>\n</blockquote>\n",
	}
	for body, want := range cases {
		got, err := renderArticle(body, "/help/acme")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%q rendered\n%s\nwant\n%s", body, got, want)
		}
	}
}

// TestCalloutLabelsAreInThePagesLanguage: a French page names the type in French.
func TestCalloutLabelsAreInThePagesLanguage(t *testing.T) {
	page, err := renderArticlePage("> [!WARNING]\n> Irréversible.", "/help/acme", "fr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<p class="callout-label">Avertissement</p>`; !strings.Contains(string(page.body), want) {
		t.Fatalf("rendered %s, want %s", page.body, want)
	}
}

// TestCalloutMarkerStaysOutOfTheDescription: the meta description and excerpts read the words, not
// the marker.
func TestCalloutMarkerStaysOutOfTheDescription(t *testing.T) {
	if got := excerpt("> [!WARNING]\n> Deleting a workspace cannot be undone.", 160); got != "Deleting a workspace cannot be undone." {
		t.Fatalf("excerpt = %q", got)
	}
}
