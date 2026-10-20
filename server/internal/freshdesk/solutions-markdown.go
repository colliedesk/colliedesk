package freshdesk

import (
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
				case atom.A:
					child.Attr = safeLink(child.Attr, note)
				}
				walk(child)
			}
			child = next
		}
	}
	walk(root)
	out, err := markdownConverter.ConvertNode(root)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(string(out)), dropped, nil
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
