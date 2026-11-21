import type { Node as PMNode } from '@tiptap/pm/model';
import { tagString } from '../format-tags';
import { html, phrasing } from './to-markdown-inline';
import type { MD } from './to-markdown-inline';

const rows = (node: PMNode) => {
  const out: PMNode[][] = [];
  node.forEach((row) => {
    const cells: PMNode[] = [];
    row.forEach((cell) => cells.push(cell));
    out.push(cells);
  });
  return out;
};

// plainCell reports whether a Markdown table can hold the cell: one paragraph of text, no line break, no
// span, background, or alignment of its own on the paragraph.
function plainCell(cell: PMNode) {
  const { colspan, rowspan, bg } = cell.attrs as { colspan: number; rowspan: number; bg: string | null };
  if (colspan !== 1 || rowspan !== 1 || bg || cell.childCount !== 1) return false;
  const paragraph = cell.firstChild!;
  if (paragraph.type.name !== 'paragraph' || paragraph.attrs.align || paragraph.attrs.indent) return false;
  let breaks = false;
  paragraph.forEach((child) => { breaks ||= child.type.name === 'hardBreak'; });
  return !breaks;
}

// gfm writes the table as Markdown when it can say all of it: a header row over plain cells, each
// column aligned one way.
function gfm(grid: PMNode[][]): MD | undefined {
  const [head, ...body] = grid;
  if (!head || head.some((cell) => cell.type.name !== 'tableHeader')) return undefined;
  if (body.some((row) => row.some((cell) => cell.type.name !== 'tableCell'))) return undefined;
  if (grid.some((row) => row.length !== head.length || !row.every(plainCell))) return undefined;
  const align = head.map((cell) => (cell.attrs.align as string | null) ?? null);
  if (grid.some((row) => row.some((cell, column) => ((cell.attrs.align as string | null) ?? null) !== align[column]))) return undefined;
  return {
    type: 'table',
    align,
    children: grid.map((row) => ({ type: 'tableRow', children: row.map((cell) => ({ type: 'tableCell', children: phrasing(cell.firstChild!) })) })),
  };
}

function cellTag(cell: PMNode) {
  const { colspan, rowspan, align, bg } = cell.attrs as { colspan: number; rowspan: number; align: string | null; bg: string | null };
  return tagString({
    name: cell.type.name === 'tableHeader' ? 'th' : 'td',
    close: false,
    attrs: {
      colspan: colspan > 1 ? String(colspan) : undefined,
      rowspan: rowspan > 1 ? String(rowspan) : undefined,
      'data-align': align ?? undefined,
      'data-bg': bg ?? undefined,
    },
  });
}

// table writes a table as Markdown, or else as the HTML the server keeps: the tags as HTML blocks, each
// cell's blocks between its tags.
export function table(node: PMNode, flow: (parent: PMNode) => MD[]): MD[] {
  const grid = rows(node);
  const markdown = gfm(grid);
  if (markdown) return [markdown];
  const out: MD[] = [];
  let tags = ['<table>'];
  const flush = () => {
    if (tags.length > 0) out.push(html(tags.join('\n')));
    tags = [];
  };
  for (const row of grid) {
    tags.push('<tr>');
    for (const cell of row) {
      tags.push(cellTag(cell));
      const blocks = flow(cell);
      if (blocks.length > 0) {
        flush();
        out.push(...blocks);
      }
      tags.push(cell.type.name === 'tableHeader' ? '</th>' : '</td>');
    }
    tags.push('</tr>');
  }
  tags.push('</table>');
  flush();
  return out;
}
