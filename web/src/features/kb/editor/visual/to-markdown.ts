import type { Node as PMNode } from '@tiptap/pm/model';
import { gfmToMarkdown } from 'mdast-util-gfm';
import { toMarkdown } from 'mdast-util-to-markdown';
import type { Join } from 'mdast-util-to-markdown';
import { tagString } from '../format-tags';
import type { FormatTag } from '../format-tags';
import { figureTag } from './article-figure';
import type { FigureAttrs } from './article-figure';
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
    join: [htmlApart, listApart],
    // Right under a line of text, --- would make that line a heading.
    handlers: { thematicBreak: (node) => (node.data?.underText ? '***' : '---') },
  });
}

// An HTML block ends at a blank line, so one always stands apart from the blocks around it, even in a
// tight list.
const htmlApart: Join = (left, right) => (left.type === 'html' || right.type === 'html' ? 1 : undefined);

// A list can start right under a line of text only from 1 and with an item that is not empty.
const listApart: Join = (left, right) => {
  if (left.type !== 'paragraph' || right.type !== 'list') return undefined;
  const first = right.children[0];
  return (right.ordered && (right.start ?? 1) !== 1) || !first || first.children.length === 0 ? 1 : undefined;
};

export function flow(parent: PMNode): MD[] {
  const out: MD[] = [];
  parent.forEach((child) => {
    for (const node of block(child)) {
      if (node.type === 'thematicBreak' && out.at(-1)?.type === 'paragraph') node.data = { underText: true };
      out.push(node);
    }
  });
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
    case 'articleFigure':
      return figure(node);
  }
  throw new Error(`the visual editor cannot write a ${node.type.name}`);
}

// figure writes an image's <figure> as the server reads it.
function figure(node: PMNode): MD[] {
  const attrs = node.attrs as FigureAttrs;
  const image: MD = { type: 'image', url: attrs.src, alt: attrs.alt ?? '', title: attrs.title };
  const shown: MD = attrs.href ? { type: 'link', url: attrs.href, title: attrs.linkTitle, children: [image] } : image;
  const picture: MD = { type: 'paragraph', children: [shown] };
  const caption = node.childCount > 0 ? [html('<figcaption>'), { type: 'paragraph', children: phrasing(node) }, html('</figcaption>')] : [];
  return [html(tagString(figureTag(attrs))), picture, ...caption, html('</figure>')];
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
