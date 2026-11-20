package helpcenter

import (
	"reflect"
	"strings"
	"testing"
)

// TestAnArticleWithThreeOrMoreHeadingsListsItsContents: each H2 and H3 of the text is an item that
// links to the heading's anchor; fewer than three give no list.
func TestAnArticleWithThreeOrMoreHeadingsListsItsContents(t *testing.T) {
	page, err := renderArticlePage("Intro\n\n## Sign in\n\ntext\n\n## Reset *your* password\n\n### By email\n\n## Sign in\n\n#### Deep\n\n> ## Quoted\n\n## Delete the account", "/help/acme", "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []contentsItem{
		{ID: "sign-in", Text: "Sign in", Level: 2},
		{ID: "reset-your-password", Text: "Reset your password", Level: 2},
		{ID: "by-email", Text: "By email", Level: 3},
		{ID: "sign-in-1", Text: "Sign in", Level: 2},
		{ID: "delete-the-account", Text: "Delete the account", Level: 2},
	}
	if !reflect.DeepEqual(page.contents, want) {
		t.Errorf("contents %+v, want %+v", page.contents, want)
	}
	for _, anchor := range []string{`<h2 id="sign-in">`, `<h2 id="reset-your-password">`, `<h3 id="by-email">`, `<h2 id="sign-in-1">`, `<h2 id="delete-the-account">`} {
		if !strings.Contains(string(page.body), anchor) {
			t.Errorf("body has no %s:\n%s", anchor, page.body)
		}
	}
	if page.size() <= len(page.body) {
		t.Errorf("size %d leaves out the contents", page.size())
	}

	four, err := renderArticlePage("## One\n\n## Two\n\n## Three\n\n## Four", "/help/acme", "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(four.contents) != 4 {
		t.Errorf("4 headings gave %+v", four.contents)
	}
	two, err := renderArticlePage("## One\n\ntext\n\n### Two\n\n#### Three", "/help/acme", "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	if two.contents != nil {
		t.Errorf("2 headings gave %+v", two.contents)
	}
}

// TestALongContentsListStopsAtFiftyItems: the list shows twice on a page, so an article of
// thousands of headings would multiply the page; it lists its first 50.
func TestALongContentsListStopsAtFiftyItems(t *testing.T) {
	page, err := renderArticlePage(strings.Repeat("a\n---\n", 300), "/help/acme", "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.contents) != 50 {
		t.Fatalf("300 headings gave %d items, want 50", len(page.contents))
	}
	if first, last := page.contents[0], page.contents[49]; first.ID != "a" || last.ID != "a-49" {
		t.Errorf("items run from %q to %q, want a to a-49", first.ID, last.ID)
	}
}
