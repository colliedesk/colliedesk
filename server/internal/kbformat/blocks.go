package kbformat

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// htmlOf is the raw HTML of an HTML block, its lines joined.
func htmlOf(block *ast.HTMLBlock, source []byte) string {
	var raw bytes.Buffer
	lines := block.Lines()
	for i := range lines.Len() {
		segment := lines.At(i)
		raw.Write(segment.Value(source))
	}
	if block.HasClosure() {
		raw.Write(block.ClosureLine.Value(source))
	}
	return raw.String()
}

// tagsOf reads an HTML block as a run of formatting tags.
func tagsOf(node ast.Node, source []byte) ([]Tag, bool) {
	block, ok := node.(*ast.HTMLBlock)
	if !ok {
		return nil, false
	}
	return ParseTags(htmlOf(block, source))
}

// unwrapBlocks finds, among parent's children, each paragraph or heading between an opening <p> or
// <hN> block and its closing block, gives it the wrapper's classes and removes the wrapper:
//
//	<p data-align="center" data-indent="2">
//
//	Text
//
//	</p>
func unwrapBlocks(parent ast.Node, source []byte) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		opening, ok := tagsOf(child, source)
		if !ok || len(opening) != 1 || opening[0].Close || !opening[0].IsBlock() {
			continue
		}
		inner := child.NextSibling()
		if inner == nil || !wrappable(inner, opening[0].Name) {
			continue
		}
		closing, ok := tagsOf(inner.NextSibling(), source)
		if !ok || len(closing) != 1 || !closing[0].Close || closing[0].Name != opening[0].Name {
			continue
		}
		if class := blockClass(opening[0]); class != "" {
			inner.SetAttributeString("class", []byte(class))
		}
		parent.RemoveChild(parent, inner.NextSibling())
		parent.RemoveChild(parent, child)
		child = inner
	}
}

// wrappable reports whether node is what a wrapper of element name holds: a paragraph in <p>, a
// heading of the same level in <hN>.
func wrappable(node ast.Node, name string) bool {
	if name == "p" {
		return node.Kind() == ast.KindParagraph
	}
	heading, ok := node.(*ast.Heading)
	return ok && "h"+strconv.Itoa(heading.Level) == name
}

// blockClass is the help center's classes for a wrapper's or a cell's attributes.
func blockClass(tag Tag) string {
	var classes []string
	if align := tag.Attr("data-align"); align != "" {
		classes = append(classes, "kb-align-"+align)
	}
	if indent := tag.Attr("data-indent"); indent != "" {
		classes = append(classes, "kb-indent-"+indent)
	}
	if bg := tag.Attr("data-bg"); bg != "" {
		classes = append(classes, "kb-bg-"+bg)
	}
	return strings.Join(classes, " ")
}
