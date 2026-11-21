package kbformat

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// figureApart puts a blank line before and after each <figure> and <figcaption> tag of an HTML block
// that starts with one. An HTML block runs to the next blank line, so without them the image and the
// caption under the tag would be read as HTML and shown as text. Only blocks at the top of the body are
// changed: a blank line inside a list item or a quote would need its prefix. Code is not an HTML block,
// so a figure shown as code keeps its lines.
func figureApart(source []byte, block *ast.HTMLBlock) []replacement {
	if _, top := block.Parent().(*ast.Document); !top {
		return nil
	}
	lines := block.Lines()
	if lines.Len() == 0 || !figureLine(source, lines.At(0)) {
		return nil
	}
	var edits []replacement
	if start := lines.At(0).Start; !afterBlankLine(source, start) {
		edits = append(edits, replacement{start, start, "\n"})
	}
	for i := 1; i < lines.Len(); i++ {
		if figureLine(source, lines.At(i)) || figureLine(source, lines.At(i-1)) {
			start := lines.At(i).Start
			edits = append(edits, replacement{start, start, "\n"})
		}
	}
	return edits
}

// figureLine reports whether a line holds one <figure> or <figcaption> tag and nothing else.
func figureLine(source []byte, line text.Segment) bool {
	tag, ok := ParseTag(string(bytes.TrimSpace(source[line.Start:line.Stop])))
	return ok && tag.IsFigure()
}

// afterBlankLine reports whether the line starting at start is the body's first or follows a blank line.
func afterBlankLine(source []byte, start int) bool {
	if start == 0 {
		return true
	}
	before := bytes.TrimSuffix(bytes.TrimSuffix(source[:start], []byte("\n")), []byte("\r"))
	return len(bytes.TrimSpace(before[bytes.LastIndexByte(before, '\n')+1:])) == 0
}
