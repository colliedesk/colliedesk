package freshdesk

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// calloutKinds maps the types of Freshdesk's callout card to the help center's callouts (GitHub's
// alert markers). A type missing here comes in as a plain quote.
var calloutKinds = map[string]string{"note": "NOTE", "info": "NOTE", "tip": "TIP", "idea": "TIP", "warning": "WARNING"}

// freshdeskCallout tells whether node is a callout card, which Freshdesk writes as
// <pre class="fd-callout fd-callout--TYPE">, and names its marker ("" for an unknown type).
func freshdeskCallout(node *html.Node) (string, bool) {
	if node.Type != html.ElementNode || node.DataAtom != atom.Pre {
		return "", false
	}
	for _, attribute := range node.Attr {
		if attribute.Key != "class" {
			continue
		}
		classes := strings.Fields(attribute.Val)
		if !slices.Contains(classes, "fd-callout") {
			return "", false
		}
		for _, class := range classes {
			if kind, ok := strings.CutPrefix(class, "fd-callout--"); ok {
				return calloutKinds[strings.ToLower(kind)], true
			}
		}
		return "", true
	}
	return "", false
}

// calloutQuote turns a callout card into a quote: a paragraph holding the marker word, when the type
// has one, and a paragraph of the card's text, whose line breaks stay line breaks.
func calloutQuote(card *html.Node, marker string) *html.Node {
	quote := &html.Node{Type: html.ElementNode, Data: "blockquote", DataAtom: atom.Blockquote}
	if marker != "" {
		first := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
		first.AppendChild(&html.Node{Type: html.TextNode, Data: marker})
		quote.AppendChild(first)
	}
	text := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
	for child := card.FirstChild; child != nil; child = card.FirstChild {
		card.RemoveChild(child)
		if child.Type != html.TextNode {
			text.AppendChild(child)
			continue
		}
		for i, line := range strings.Split(strings.Trim(child.Data, "\n"), "\n") {
			if i > 0 {
				text.AppendChild(&html.Node{Type: html.ElementNode, Data: "br", DataAtom: atom.Br})
			}
			text.AppendChild(&html.Node{Type: html.TextNode, Data: line})
		}
	}
	quote.AppendChild(text)
	return quote
}

// placeCalloutMarker puts [!TYPE] where the converter wrote the marker word: on the quote's first
// line, with the card's text right under it, as GitHub writes an alert.
func placeCalloutMarker(markdown, marker, kind string) string {
	for _, blank := range []string{"\n> \n", "\n>\n"} {
		if strings.Contains(markdown, marker+blank) {
			return strings.Replace(markdown, marker+blank, "[!"+kind+"]\n", 1)
		}
	}
	return strings.Replace(markdown, marker, "[!"+kind+"]", 1)
}
