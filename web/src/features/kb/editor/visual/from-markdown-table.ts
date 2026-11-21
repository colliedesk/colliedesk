import type { JSONContent } from '@tiptap/core';
import type { FormatNode } from '../format-tree';
import { inline, unsupported } from './from-markdown-inline';

interface GFMTable extends FormatNode { align?: (string | null)[] | null }

// gfmTable reads a Markdown table: its first row is the header. Left is a column's alignment already, so
// it reads as none; a row short of cells gets empty ones, as the page shows it.
export function gfmTable(node: GFMTable): JSONContent {
  const rows = node.children ?? [];
  const width = rows[0]?.children?.length ?? 0;
  const align = (column: number) => {
    const value = node.align?.[column];
    return value === 'center' || value === 'right' ? value : null;
  };
  return {
    type: 'table',
    content: rows.map((row, r) => {
      const cells = row.children ?? [];
      if (cells.length > width) return unsupported();
      return {
        type: 'tableRow',
        content: Array.from({ length: width }, (_, column) => ({
          type: r === 0 ? 'tableHeader' : 'tableCell',
          attrs: { align: align(column) },
          content: [{ type: 'paragraph', content: inline(cells[column]?.children ?? []) }],
        })),
      };
    }),
  };
}

const span = (value: string | undefined) => Number(value ?? 1);

// kbTable reads a table written as HTML (merged or coloured cells); one whose cells do not make a grid
// stays as written.
export function kbTable(node: FormatNode, blocks: (nodes: FormatNode[]) => JSONContent[]): JSONContent {
  const rows = (node.children ?? []).map((row) => row.children ?? []);
  if (!isGrid(rows)) return unsupported();
  return {
    type: 'table',
    content: rows.map((cells) => ({
      type: 'tableRow',
      content: cells.map((cell) => {
        const attrs = cell.tag!.attrs;
        const children = cell.children ?? [];
        return {
          type: cell.tag!.name === 'th' ? 'tableHeader' : 'tableCell',
          attrs: { colspan: span(attrs.colspan), rowspan: span(attrs.rowspan), align: attrs['data-align'] ?? null, bg: attrs['data-bg'] ?? null },
          content: children.length > 0 ? blocks(children) : [{ type: 'paragraph' }],
        };
      }),
    })),
  };
}

// isGrid reports whether the cells, with their spans, fill every row to the same width with no overlap.
function isGrid(rows: FormatNode[][]): boolean {
  const taken = rows.map(() => new Set<number>());
  for (const [r, cells] of rows.entries()) {
    let column = 0;
    for (const cell of cells) {
      while (taken[r]!.has(column)) column++;
      const across = span(cell.tag!.attrs.colspan);
      const down = span(cell.tag!.attrs.rowspan);
      if (r + down > rows.length) return false;
      for (let dr = 0; dr < down; dr++) {
        for (let dc = 0; dc < across; dc++) {
          if (taken[r + dr]!.has(column + dc)) return false;
          taken[r + dr]!.add(column + dc);
        }
      }
      column += across;
    }
  }
  const width = taken[0]?.size ?? 0;
  return width > 0 && taken.every((row) => row.size === width && Math.max(...row) === width - 1);
}
