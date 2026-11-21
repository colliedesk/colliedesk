package kbformat

import (
	"slices"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// KindTable, KindRow and KindCell are the kinds of a formatted table's nodes.
var (
	KindTable = ast.NewNodeKind("KBTable")
	KindRow   = ast.NewNodeKind("KBTableRow")
	KindCell  = ast.NewNodeKind("KBTableCell")
)

// Table is a table written as HTML because a cell is merged or coloured; its children are Rows.
type Table struct{ ast.BaseBlock }

// Row is a <tr>; its children are Cells.
type Row struct{ ast.BaseBlock }

// Cell is a <th> or <td>; its children are the blocks written inside it.
type Cell struct {
	ast.BaseBlock
	Tag Tag
}

func (n *Table) Kind() ast.NodeKind { return KindTable }
func (n *Row) Kind() ast.NodeKind   { return KindRow }
func (n *Cell) Kind() ast.NodeKind  { return KindCell }

func (n *Table) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }
func (n *Row) Dump(source []byte, level int)   { ast.DumpHelper(n, source, level, nil, nil) }
func (n *Cell) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Tag": n.Tag.String()}, nil)
}

// buildTables replaces each run of parent's children that spells a table with a Table node:
// HTML blocks of table, row and cell tags, with the blocks of each cell between its tags:
//
//	<table>
//	<tr>
//	<th colspan="2">
//
//	Text
//
//	</th>
//	</tr>
//	</table>
//
// A run that breaks the shape (text outside a cell, another tag, no </table>) is left as it was.
func buildTables(parent ast.Node, source []byte) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if tags, ok := tagsOf(child, source); ok && !tags[0].Close && tags[0].Name == "table" {
			if table, last := readTable(child, source); table != nil {
				stop := last.NextSibling()
				for node := child.NextSibling(); node != stop; {
					next := node.NextSibling()
					parent.RemoveChild(parent, node)
					node = next
				}
				parent.ReplaceChild(parent, child, table)
				child = table
			}
		}
	}
}

// readTable reads the table starting at first; it returns the table and the last node it took. It
// moves no node until the whole table has been read.
func readTable(first ast.Node, source []byte) (*Table, ast.Node) {
	type cellOf struct {
		tag    Tag
		blocks []ast.Node
	}
	var rows [][]*cellOf
	var row []*cellOf
	var cell *cellOf
	inRow := false
	for node := first; node != nil; node = node.NextSibling() {
		tags, ok := tagsOf(node, source)
		// A block with no table tag in it is the cell's content, a paragraph's wrapper too.
		if !ok || !slices.ContainsFunc(tags, Tag.IsTable) {
			if cell == nil {
				return nil, nil
			}
			cell.blocks = append(cell.blocks, node)
			continue
		}
		for i, tag := range tags {
			cellTag := tag.Name == "th" || tag.Name == "td"
			switch {
			case tag.Name == "table" && !tag.Close && node == first && i == 0:
			case tag.Name == "tr" && !tag.Close && !inRow && cell == nil:
				inRow, row = true, nil
			case cellTag && !tag.Close && inRow && cell == nil:
				cell = &cellOf{tag: tag}
			case cellTag && tag.Close && cell != nil && cell.tag.Name == tag.Name:
				row, cell = append(row, cell), nil
			case tag.Name == "tr" && tag.Close && inRow && cell == nil && len(row) > 0:
				rows, inRow = append(rows, row), false
			case tag.Name == "table" && tag.Close && !inRow && len(rows) > 0 && i == len(tags)-1:
				table := &Table{}
				for _, cells := range rows {
					tr := &Row{}
					for _, c := range cells {
						td := &Cell{Tag: c.tag}
						for _, block := range c.blocks {
							td.AppendChild(td, block)
						}
						tr.AppendChild(tr, td)
					}
					table.AppendChild(table, tr)
				}
				return table, node
			default:
				return nil, nil
			}
		}
	}
	return nil, nil
}

type tableHTML struct{}

func (tableHTML) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(KindTable, func(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = writer.WriteString("<table>\n<tbody>\n")
		} else {
			_, _ = writer.WriteString("</tbody>\n</table>\n")
		}
		return ast.WalkContinue, nil
	})
	registerer.Register(KindRow, func(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = writer.WriteString("<tr>\n")
		} else {
			_, _ = writer.WriteString("</tr>\n")
		}
		return ast.WalkContinue, nil
	})
	registerer.Register(KindCell, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		tag := node.(*Cell).Tag
		if !entering {
			_, _ = writer.WriteString("</" + tag.Name + ">\n")
			return ast.WalkContinue, nil
		}
		_, _ = writer.WriteString("<" + tag.Name)
		for _, key := range []string{"colspan", "rowspan"} {
			if value := tag.Attr(key); value != "" {
				_, _ = writer.WriteString(" " + key + `="` + value + `"`)
			}
		}
		if class := blockClass(tag); class != "" {
			_, _ = writer.WriteString(` class="` + class + `"`)
		}
		_, _ = writer.WriteString(">")
		return ast.WalkContinue, nil
	})
}
