package kbformat

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Extension reads a text's formatting HTML into nodes (Transformer) and renders them with the help
// center's classes: kb-color-<name>, kb-align-<align>, kb-indent-<n> and kb-bg-<name>, and colspan
// and rowspan as attributes. Formatting HTML it cannot read stays raw HTML, for the page's own rules.
var Extension goldmark.Extender = extension{}

type extension struct{}

// transformerPriority runs the transformer after the help center's link rewrite (100) and before it
// hides raw HTML's content (200).
const transformerPriority = 150

func (extension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(Transformer, transformerPriority)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(inlineHTML{}, 100), util.Prioritized(tableHTML{}, 100)))
}

// Transformer turns the formatting HTML of a parsed text into Inline, Table, Row and Cell nodes and
// classes on wrapped paragraphs and headings.
var Transformer parser.ASTTransformer = transformer{}

type transformer struct{}

func (transformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.FirstChild() == nil {
			return ast.WalkContinue, nil
		}
		if node.FirstChild().Type() == ast.TypeInline {
			pairInline(node, source)
		} else {
			buildTables(node, source)
			unwrapBlocks(node, source)
		}
		return ast.WalkContinue, nil
	})
}
