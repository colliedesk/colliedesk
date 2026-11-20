package helpcenter

import (
	"bytes"
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// calloutTypes are GitHub's alert types, the only markers that make a quote a callout.
var calloutTypes = []string{"note", "tip", "important", "warning", "caution"}

// calloutAttribute marks a top-level quote whose first line was a known [!TYPE] marker; the marker
// itself is taken out of the tree, so excerpts and the page read only the words after it.
var calloutAttribute = []byte("data-callout")

// callouts turns GitHub's alert syntax into callouts: a quote at the top of the article whose first
// line is only `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]` or `[!CAUTION]`, in any case. The
// editor preview (web/src/kb/callouts.ts) applies the same rule.
type callouts struct{}

func (callouts) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		quote, ok := node.(*ast.Blockquote)
		if !ok {
			continue
		}
		paragraph, ok := quote.FirstChild().(*ast.Paragraph)
		if !ok {
			continue
		}
		kind, line := calloutMarker(paragraph, source)
		if kind == "" {
			continue
		}
		for _, inline := range line {
			paragraph.RemoveChild(paragraph, inline)
		}
		if paragraph.FirstChild() == nil {
			quote.RemoveChild(quote, paragraph)
		}
		quote.SetAttribute(calloutAttribute, []byte(kind))
	}
}

// calloutMarker reads the paragraph's first line: the type it names and the inline nodes it is made
// of, or "" when it is anything but a known marker alone.
func calloutMarker(paragraph *ast.Paragraph, source []byte) (string, []ast.Node) {
	var words bytes.Buffer
	var line []ast.Node
	for inline := paragraph.FirstChild(); inline != nil; inline = inline.NextSibling() {
		piece, ok := inline.(*ast.Text)
		if !ok {
			return "", nil
		}
		words.Write(piece.Segment.Value(source))
		line = append(line, inline)
		if piece.SoftLineBreak() || piece.HardLineBreak() {
			break
		}
	}
	marker := strings.ToLower(strings.TrimSpace(words.String()))
	kind, ok := strings.CutPrefix(marker, "[!")
	if !ok {
		return "", nil
	}
	kind, ok = strings.CutSuffix(kind, "]")
	for _, known := range calloutTypes {
		if ok && kind == known {
			return kind, line
		}
	}
	return "", nil
}

// calloutLabel names a type in the page's words.
func calloutLabel(words uiText, kind string) string {
	switch kind {
	case "note":
		return words.Note
	case "tip":
		return words.Tip
	case "important":
		return words.Important
	case "warning":
		return words.Warning
	}
	return words.Caution
}

// calloutHTML renders every quote: a callout as an aside a screen reader announces as a note, with
// its type named first (no icon in the markup: the stylesheet draws it), any other quote as before.
type calloutHTML struct{}

func (calloutHTML) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(ast.KindBlockquote, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		value, isCallout := node.AttributeString(string(calloutAttribute))
		kind, _ := value.([]byte)
		switch {
		case isCallout && entering:
			_, _ = writer.WriteString(`<aside class="callout callout-` + string(kind) + `" role="note">` + "\n")
			_, _ = writer.WriteString(`<p class="callout-label">` + html.EscapeString(calloutLabel(textOf(writer), string(kind))) + "</p>\n")
		case isCallout:
			_, _ = writer.WriteString("</aside>\n")
		case entering:
			_, _ = writer.WriteString("<blockquote")
			if node.Attributes() != nil {
				goldhtml.RenderAttributes(writer, node, goldhtml.BlockquoteAttributeFilter)
			}
			_, _ = writer.WriteString(">\n")
		default:
			_, _ = writer.WriteString("</blockquote>\n")
		}
		return ast.WalkContinue, nil
	})
}
