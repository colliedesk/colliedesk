package freshdesk

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Markdown has no place for these; the converter drops them, so the import names them in a warning.
// The original HTML stays in source_html and in the export.
var droppedElements = map[atom.Atom]bool{
	atom.Iframe: true, atom.Video: true, atom.Audio: true, atom.Embed: true, atom.Object: true, atom.Form: true,
	atom.Input: true, atom.Button: true, atom.Select: true, atom.Textarea: true, atom.Script: true, atom.Style: true,
	atom.Svg: true, atom.Canvas: true, atom.Noscript: true, atom.Math: true,
}

var markdownConverter = converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(),
	table.NewTablePlugin(table.WithSpanCellBehavior(table.SpanBehaviorMirror)), strikethrough.NewStrikethroughPlugin()))

// articleMarkdown turns an article's HTML into Markdown. It returns what Markdown could not hold
// (element names, "table cell spanning rows or columns", "link with an unsafe address"); a link whose
// address is not http, https, mailto, relative or a fragment keeps only its text.
func articleMarkdown(markup string) (string, []string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(markup), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return "", nil, err
	}
	root := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	var dropped []string
	note := func(what string) {
		if !slices.Contains(dropped, what) {
			dropped = append(dropped, what)
		}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.ElementNode && droppedElements[child.DataAtom] {
				note(child.Data)
				node.RemoveChild(child)
				child = next
				continue
			}
			if child.Type == html.ElementNode {
				switch child.DataAtom {
				case atom.Td, atom.Th:
					for _, attribute := range child.Attr {
						if (attribute.Key == "colspan" || attribute.Key == "rowspan") && strings.TrimSpace(attribute.Val) != "1" {
							note("table cell spanning rows or columns")
						}
					}
					if holdsBlock(child) {
						// A Markdown table cell is one line: the converter shows such a table as plain text.
						note("table with lines or lists inside a cell (shown as text, not as a table)")
					}
				case atom.Sub, atom.Sup:
					note("subscript or superscript (shown as plain text)")
				case atom.A:
					child.Attr = safeLink(child.Attr, note)
				case atom.Details:
					note("collapsible section (shown open)")
				}
				walk(child)
				if child.DataAtom == atom.Summary || child.DataAtom == atom.A && !hasAttribute(child, "href") {
					// A summary becomes its own paragraph; an anchor without an address keeps only its text.
					unwrap(node, child, child.DataAtom == atom.Summary)
				}
			}
			child = next
		}
	}
	walk(root)
	out, err := markdownConverter.ConvertNode(root)
	if err != nil {
		return "", nil, err
	}
	markdown := strings.TrimSpace(string(out))
	if htmlTag.MatchString(markdown) {
		// Raw HTML in Markdown is never rendered (the help center and the editor's preview skip it).
		note("HTML the converter could not turn into Markdown")
	}
	return markdown, dropped, nil
}

// htmlTag finds a tag the converter left in the Markdown as raw HTML.
var htmlTag = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9-]*(\s[^<>]*)?/?>`)

// holdsBlock tells whether a table cell holds a line break or block content.
func holdsBlock(cell *html.Node) bool {
	for node := cell.FirstChild; node != nil; node = node.NextSibling {
		if node.Type != html.ElementNode {
			continue
		}
		switch node.DataAtom {
		case atom.Br, atom.P, atom.Div, atom.Ul, atom.Ol, atom.Table, atom.Pre, atom.Blockquote, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Hr:
			return true
		}
		if holdsBlock(node) {
			return true
		}
	}
	return false
}

// safeLink drops an href whose scheme is not http, https or mailto (javascript:, data:, ...). A
// relative address or a fragment stays: it names a page of the old portal, which the original keeps.
func safeLink(attributes []html.Attribute, note func(string)) []html.Attribute {
	kept := attributes[:0]
	for _, attribute := range attributes {
		if attribute.Key == "href" {
			address := strings.TrimSpace(attribute.Val)
			if _, ok := linkAddress(address); !ok && schemePattern.MatchString(strings.Map(dropSpace, address)) {
				note("link with an unsafe address")
				continue
			}
		}
		kept = append(kept, attribute)
	}
	return kept
}

// dropSpace removes what a browser ignores inside a scheme, as linkAddress does.
func dropSpace(r rune) rune {
	if unicode.IsSpace(r) || unicode.IsControl(r) {
		return -1
	}
	return r
}

func hasAttribute(node *html.Node, key string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return true
		}
	}
	return false
}

// unwrap replaces child by its children, inside a paragraph when asParagraph is set.
func unwrap(parent, child *html.Node, asParagraph bool) {
	target := parent
	if asParagraph {
		target = &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
		parent.InsertBefore(target, child)
	}
	for grandchild := child.FirstChild; grandchild != nil; grandchild = child.FirstChild {
		child.RemoveChild(grandchild)
		if asParagraph {
			target.AppendChild(grandchild)
		} else {
			parent.InsertBefore(grandchild, child)
		}
	}
	parent.RemoveChild(child)
}
