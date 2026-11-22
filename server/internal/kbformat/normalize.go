package kbformat

import (
	"bytes"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gfm "github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// parsing reads a body the way the help center does, so a tag is found where the page finds one and
// code keeps its text.
var parsing = goldmark.New(goldmark.WithExtensions(gfm.Table, gfm.Strikethrough, gfm.TaskList, gfm.Footnote)).Parser()

type replacement struct {
	start, stop int
	with        string
}

// Normalize rewrites each formatting tag of body to its canonical spelling: lowercase, double-quoted,
// only the attributes and values its element allows, in one order. Everything else keeps its bytes,
// so a canonical body comes back unchanged. The web editor writes the same spelling.
func Normalize(body string) string {
	source := []byte(body)
	var edits []replacement
	_ = ast.Walk(parsing.Parse(text.NewReader(source)), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.RawHTML:
			if node.Segments.Len() == 0 {
				break
			}
			start, stop := node.Segments.At(0).Start, node.Segments.At(node.Segments.Len()-1).Stop
			if tag, ok := ParseTag(string(source[start:stop])); ok {
				edits = append(edits, replacement{start, stop, tag.String()})
			}
		case *ast.HTMLBlock:
			for i := range node.Lines().Len() {
				edits = append(edits, blockLine(source, node.Lines().At(i))...)
			}
			if node.HasClosure() {
				edits = append(edits, blockLine(source, node.ClosureLine)...)
			}
			edits = append(edits, figureApart(source, node)...)
		}
		return ast.WalkContinue, nil
	})
	// From the end back, and at one place a tag's rewrite before the blank line put ahead of it.
	slices.SortStableFunc(edits, func(a, b replacement) int {
		if a.start != b.start {
			return b.start - a.start
		}
		return b.stop - a.stop
	})
	for _, edit := range edits {
		source = slices.Concat(source[:edit.start], []byte(edit.with), source[edit.stop:])
	}
	return string(source)
}

// blockLine rewrites a line of an HTML block that holds only formatting tags.
func blockLine(source []byte, line text.Segment) []replacement {
	raw := source[line.Start:line.Stop]
	content := bytes.TrimRight(raw, "\r\n")
	tags, ok := ParseTags(string(content))
	if !ok {
		return nil
	}
	var canonical strings.Builder
	for _, tag := range tags {
		canonical.WriteString(tag.String())
	}
	if canonical.String() == string(content) {
		return nil
	}
	return []replacement{{line.Start, line.Start + len(content), canonical.String()}}
}
