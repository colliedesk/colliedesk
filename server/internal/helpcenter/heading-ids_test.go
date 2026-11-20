package helpcenter

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAHeadingNamedLikeAPartOfThePageGetsAnotherID: the page's own elements (the main text, the
// search box, the chat) keep their ids, so a heading of the same name links to itself, not to them.
func TestAHeadingNamedLikeAPartOfThePageGetsAnotherID(t *testing.T) {
	page, err := renderArticlePage("## Main\n\n## Q\n\n## Chat title\n\n## Main", "/help/acme", "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<h2 id="main-1">Main</h2>
<h2 id="q-1">Q</h2>
<h2 id="chat-title-1">Chat title</h2>
<h2 id="main-2">Main</h2>
`
	if string(page.body) != want {
		t.Errorf("body\n%s\nwant\n%s", page.body, want)
	}
}

// TestEveryIDOfThePageTemplatesIsReserved: an id added to a template must be reserved for headings
// too, or a heading of that name gets the same id.
func TestEveryIDOfThePageTemplatesIsReserved(t *testing.T) {
	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("templates: %v %v", files, err)
	}
	literal := regexp.MustCompile(`\bid="([^"{}]+)"`)
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range literal.FindAllStringSubmatch(string(text), -1) {
			if !strings.Contains(" "+strings.Join(reservedIDs, " ")+" ", " "+match[1]+" ") {
				t.Errorf("%s: id %q is not in reservedIDs", file, match[1])
			}
		}
	}
}
