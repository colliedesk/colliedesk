package freshdesk

import (
	"encoding/json"
	"os"
	"testing"
)

// htmlMarkdownCases is the fixture the editor's paste reads too (web/src/features/kb/editor/paste-html.test.ts):
// an article imported from Freshdesk and the same HTML pasted into the editor become the same Markdown.
type htmlMarkdownCases struct {
	Cases []struct {
		Name     string `json:"name"`
		HTML     string `json:"html"`
		Markdown string `json:"markdown"`
	} `json:"cases"`
}

func TestArticleMarkdownMatchesTheSharedPasteCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/html-markdown-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture htmlMarkdownCases
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) < 20 {
		t.Fatalf("only %d cases were read", len(fixture.Cases))
	}
	for _, c := range fixture.Cases {
		got, _, err := articleMarkdown(c.HTML)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if got != c.Markdown {
			t.Errorf("%s:\n got %q\nwant %q", c.Name, got, c.Markdown)
		}
	}
}
