package helpcenter

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// contentsMinimum is the fewest headings an article lists as its contents; a shorter one is read
// at a glance.
const contentsMinimum = 3

// contentsMaximum is the most items the list holds. The list shows twice on a page (above the text
// and in the rail), so an article of thousands of headings would otherwise multiply its page.
const contentsMaximum = 50

// contentsItem is one heading of an article's contents: its anchor, its words and its level (2 or 3).
type contentsItem struct {
	ID    string
	Text  string
	Level int
}

// contentsOf lists the H2 and H3 headings of the text itself (not those in a quote or a list), or
// nothing when there are fewer than contentsMinimum. Each item links to the anchor the renderer
// gave its heading. It stops at the first contentsMaximum.
func contentsOf(document ast.Node, source []byte) []contentsItem {
	var items []contentsItem
	for node := document.FirstChild(); node != nil && len(items) < contentsMaximum; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level < 2 || heading.Level > 3 {
			continue
		}
		id, ok := heading.AttributeString("id")
		words := strings.Join(strings.Fields(plainText(heading, source)), " ")
		if anchor, isBytes := id.([]byte); ok && isBytes && len(anchor) > 0 && words != "" {
			items = append(items, contentsItem{ID: string(anchor), Text: words, Level: heading.Level})
		}
	}
	if len(items) < contentsMinimum {
		return nil
	}
	return items
}
