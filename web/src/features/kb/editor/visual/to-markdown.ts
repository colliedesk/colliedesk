import type { Node as PMNode } from '@tiptap/pm/model';
import { gfmToMarkdown } from 'mdast-util-gfm';
import { toMarkdown } from 'mdast-util-to-markdown';
import type { Join } from 'mdast-util-to-markdown';
import { tagString } from '../format-tags';
import type { FormatTag } from '../format-tags';
import { html, phrasing } from './to-markdown-inline';
import type { MD } from './to-markdown-inline';
import { table } from './to-markdown-table';

// docToMarkdown writes the visual editor's article as Markdown in the one spelling the server keeps
// (server/internal/kbformat), so saving it again changes nothing.
export function docToMarkdown(doc: PMNode): string {
  const root = { type: 'root', children: flow(doc) };
  return toMarkdown(root as Parameters<typeof toMarkdown>[0], {
    // Cells are not padded to the column's width, so changing one cell changes one line.
    extensions: [gfmToMarkdown({ tablePipeAlign: false })],
    bullet: '-',
    rule: '-',
    emphasis: '*',
    fences: true,
    join: [htmlApart],
  });
}

// An HTML block ends at a blank line, so one always stands apart from the blocks around it, even in a
// tight list.
const htmlApart: Join = (left, right) => (left.type === 'html' || right.type === 'html' ? 1 : undefined);

export function flow(parent: PMNode): MD[] {
  const out: MD[] = [];
  parent.forEach((child) => out.push(...block(child)));
  return out;
}

// wrap puts a paragraph or heading in the <p>/<hN> wrapper its alignment and indent need.
function wrap(node: PMNode, name: string, inner: MD): MD[] {
  const { align, indent } = node.attrs as { align: string | null; indent: number };
  if (!align && !indent) return [inner];
  const tag: FormatTag = { name, close: false, attrs: { 'data-align': align ?? undefined, 'data-indent': indent ? String(indent) : undefined } };
  return [html(tagString(tag)), inner, html(`</${name}>`)];
}

function block(node: PMNode): MD[] {
  switch (node.type.name) {
    case 'paragraph':
      return node.childCount === 0 ? [] : wrap(node, 'p', { type: 'paragraph', children: phrasing(node) });
    case 'heading': {
      const depth = node.attrs.level as number;
      return wrap(node, `h${depth}`, { type: 'heading', depth, children: phrasing(node) });
    }
    case 'blockquote':
      return [{ type: 'blockquote', children: flow(node) }];
    case 'callout':
      return [{ type: 'blockquote', children: [html(`[!${(node.attrs.type as string).toUpperCase()}]`), ...flow(node)] }];
    case 'codeBlock':
      return [{ type: 'code', lang: (node.attrs.language as string | null) || null, value: node.textContent }];
    case 'horizontalRule':
      return [{ type: 'thematicBreak' }];
    case 'bulletList': case 'orderedList': case 'taskList':
      return [list(node)];
    case 'table':
      return table(node, flow);
    case 'markdownBlock':
      return [html(node.attrs.source as string)];
  }
  throw new Error(`the visual editor cannot write a ${node.type.name}`);
}

function list(node: PMNode): MD {
  const spread = node.attrs.spread as boolean;
  const children: MD[] = [];
  node.forEach((item) => {
    const checked = item.type.name === 'taskItem' ? (item.attrs.checked as boolean) : null;
    children.push({ type: 'listItem', spread, checked, children: flow(item) });
  });
  const ordered = node.type.name === 'orderedList';
  return { type: 'list', ordered, start: ordered ? (node.attrs.start as number) : null, spread, children };
}
