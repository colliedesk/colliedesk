package kbformat

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// KindInline is the kind of an Inline node.
var KindInline = ast.NewNodeKind("KBInline")

// Inline is a <u>, <mark> or <span> of the text with what it wraps as its children.
type Inline struct {
	ast.BaseInline
	Tag Tag
}

func (n *Inline) Kind() ast.NodeKind { return KindInline }

func (n *Inline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Tag": n.Tag.String()}, nil)
}

// pairInline turns each opening inline tag of parent's children and the closing tag that matches it
// at the same level into an Inline node. A tag without its partner at that level (one that crosses
// the edge of a link or an emphasis) stays raw HTML, which the page leaves out.
func pairInline(parent ast.Node, source []byte) {
	type open struct {
		node ast.Node
		tag  Tag
	}
	var stack []open
	for child := parent.FirstChild(); child != nil; {
		next := child.NextSibling()
		raw, ok := child.(*ast.RawHTML)
		if !ok {
			child = next
			continue
		}
		tag, ok := ParseTag(string(raw.Segments.Value(source)))
		switch {
		case !ok || !tag.IsInline():
		case !tag.Close:
			stack = append(stack, open{child, tag})
		case len(stack) > 0 && stack[len(stack)-1].tag.Name == tag.Name:
			opened := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			wrap := &Inline{Tag: opened.tag}
			for inner := opened.node.NextSibling(); inner != child; {
				after := inner.NextSibling()
				wrap.AppendChild(wrap, inner)
				inner = after
			}
			parent.ReplaceChild(parent, opened.node, wrap)
			parent.RemoveChild(parent, child)
		default:
			// A closing tag that crosses an inner opening (<u><mark></u>) leaves both unpaired; one with
			// no opening at this level is left alone.
			for at := len(stack) - 1; at >= 0; at-- {
				if stack[at].tag.Name == tag.Name {
					stack = stack[:at]
					break
				}
			}
		}
		child = next
	}
}

type inlineHTML struct{}

func (inlineHTML) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(KindInline, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		tag := node.(*Inline).Tag
		name := tag.Name
		class := ""
		if name == "span" {
			if color := tag.Attr("data-color"); color != "" {
				class = "kb-color-" + color
			} else {
				// A span with no colour of the palette shows its text only.
				return ast.WalkContinue, nil
			}
		}
		switch {
		case !entering:
			_, _ = writer.WriteString("</" + name + ">")
		case class != "":
			_, _ = writer.WriteString("<" + name + ` class="` + class + `">`)
		default:
			_, _ = writer.WriteString("<" + name + ">")
		}
		return ast.WalkContinue, nil
	})
}
