package kbformat

import (
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// KindFigure and KindCaption are the kinds of an image's figure and of its caption.
var (
	KindFigure  = ast.NewNodeKind("KBFigure")
	KindCaption = ast.NewNodeKind("KBFigureCaption")
)

// Figure is an image in a <figure>: its children are the image, or the link around it, and an
// optional Caption.
type Figure struct {
	ast.BaseBlock
	Tag Tag
}

// Caption is a figure's <figcaption>; its children are the caption's inline text.
type Caption struct{ ast.BaseBlock }

func (n *Figure) Kind() ast.NodeKind  { return KindFigure }
func (n *Caption) Kind() ast.NodeKind { return KindCaption }

func (n *Figure) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Tag": n.Tag.String()}, nil)
}
func (n *Caption) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

// buildFigures replaces each run of parent's children that spells an image's figure with a Figure
// node: an opening <figure> block, a paragraph holding only an image (or a link holding only an
// image), an optional caption, and </figure>:
//
//	<figure data-width="480" data-align="center" data-border>
//
//	[![Alt](/api/v1/kb/images/ID)](https://example.com)
//
//	<figcaption>
//
//	Caption
//
//	</figcaption>
//
//	</figure>
//
// A run that breaks the shape is left as it was, so its content still shows without the figure.
func buildFigures(parent ast.Node, source []byte) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		opening, ok := figureTag(child, source, "figure", false)
		if !ok {
			continue
		}
		picture := child.NextSibling()
		shown := pictureOf(picture)
		if shown == nil {
			continue
		}
		last := picture.NextSibling()
		var caption ast.Node
		if _, ok := figureTag(last, source, "figcaption", false); ok {
			caption = last.NextSibling()
			if caption == nil || caption.Kind() != ast.KindParagraph {
				continue
			}
			if _, ok := figureTag(caption.NextSibling(), source, "figcaption", true); !ok {
				continue
			}
			last = caption.NextSibling().NextSibling()
		}
		if _, ok := figureTag(last, source, "figure", true); !ok {
			continue
		}
		figure := &Figure{Tag: opening}
		if width := opening.Attr("data-width"); within(width, 1, rules.Figure.Pixels) {
			imageOf(shown).SetAttributeString("width", []byte(width))
		}
		figure.AppendChild(figure, shown)
		if caption != nil {
			text := &Caption{}
			for inline := caption.FirstChild(); inline != nil; {
				next := inline.NextSibling()
				text.AppendChild(text, inline)
				inline = next
			}
			figure.AppendChild(figure, text)
		}
		stop := last.NextSibling()
		for node := child.NextSibling(); node != stop; {
			next := node.NextSibling()
			parent.RemoveChild(parent, node)
			node = next
		}
		parent.ReplaceChild(parent, child, figure)
		child = figure
	}
}

// figureTag reads node as the one tag <name>, or </name> when closing.
func figureTag(node ast.Node, source []byte, name string, closing bool) (Tag, bool) {
	if node == nil {
		return Tag{}, false
	}
	tags, ok := tagsOf(node, source)
	if !ok || len(tags) != 1 || tags[0].Name != name || tags[0].Close != closing {
		return Tag{}, false
	}
	return tags[0], true
}

// pictureOf is what a figure's paragraph shows when it holds only an image, or only a link around
// an image; nil for anything else.
func pictureOf(node ast.Node) ast.Node {
	if node == nil || node.Kind() != ast.KindParagraph || node.ChildCount() != 1 {
		return nil
	}
	shown := node.FirstChild()
	if imageOf(shown) == nil {
		return nil
	}
	return shown
}

func imageOf(node ast.Node) *ast.Image {
	if link, ok := node.(*ast.Link); ok && link.ChildCount() == 1 {
		node = link.FirstChild()
	}
	image, _ := node.(*ast.Image)
	return image
}

// figureClass is the help center's classes for a figure: its named width, its alignment and its
// border. A width in pixels is the image's own width attribute instead.
func figureClass(tag Tag) string {
	classes := []string{"kb-figure"}
	if width := tag.Attr("data-width"); slices.Contains(rules.Figure.Widths, width) {
		classes = append(classes, "kb-figure-"+width)
	}
	if align := tag.Attr("data-align"); align != "" {
		classes = append(classes, "kb-figure-"+align)
	}
	if tag.Has("data-border") {
		classes = append(classes, "kb-figure-border")
	}
	return strings.Join(classes, " ")
}

type figureHTML struct{}

func (figureHTML) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(KindFigure, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = writer.WriteString(`<figure class="` + figureClass(node.(*Figure).Tag) + `">`)
		} else {
			_, _ = writer.WriteString("</figure>\n")
		}
		return ast.WalkContinue, nil
	})
	registerer.Register(KindCaption, func(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = writer.WriteString("<figcaption>")
		} else {
			_, _ = writer.WriteString("</figcaption>")
		}
		return ast.WalkContinue, nil
	})
}
