import type { JSONContent } from '@tiptap/core';
import type { Node as PMNode } from '@tiptap/pm/model';
import { fromMarkdown } from 'mdast-util-from-markdown';
import { gfmFromMarkdown } from 'mdast-util-gfm';
import { gfm } from 'micromark-extension-gfm';
import { takeMarker } from '../../callouts';
import { formatTree, imageOf } from '../format-tree';
import type { FormatNode } from '../format-tree';
import { inline, Unsupported, unsupported } from './from-markdown-inline';
import { gfmTable, kbTable } from './from-markdown-table';
import { schema } from './schema';

// markdownToDoc reads an article's Markdown as the visual editor shows it. A top-level block it cannot
// show as itself (raw HTML, a footnote, a link reference, a table that is not a grid…) becomes a
// Markdown block holding that block's text as written, so saving never changes it.
export function markdownToDoc(text: string): PMNode {
  const tree = fromMarkdown(text, { extensions: [gfm()], mdastExtensions: [gfmFromMarkdown()] }) as FormatNode;
  formatTree(tree);
  const content = (tree.children ?? []).map((node) => topBlock(node, text));
  return schema.nodeFromJSON({ type: 'doc', content: content.length > 0 ? content : [{ type: 'paragraph' }] });
}

function topBlock(node: FormatNode, text: string): JSONContent {
  try {
    const json = (node.type === 'blockquote' && callout(node)) || block(node);
    schema.nodeFromJSON(json).check();
    return json;
  } catch (error) {
    if (!(error instanceof Unsupported) && !(error instanceof RangeError)) throw error;
    const { start, end } = node.position!;
    return { type: 'markdownBlock', attrs: { source: text.slice(start.offset, end.offset) } };
  }
}

// callout reads a top-level quote whose first line is a marker alone ([!NOTE]) as a callout.
function callout(node: FormatNode): JSONContent | undefined {
  const first = node.children?.[0];
  if (first?.type !== 'paragraph' || first.data?.kbWrap) return undefined;
  const type = takeMarker(first);
  if (!type) return undefined;
  const rest = (first.children ?? []).length === 0 ? node.children!.slice(1) : node.children!;
  return { type: 'callout', attrs: { type }, content: rest.length > 0 ? blocks(rest) : [{ type: 'paragraph' }] };
}

const blocks = (nodes: FormatNode[]) => nodes.map(block);

// wrapAttrs is the alignment and indent a <p>/<hN> wrapper gives the block inside it.
function wrapAttrs(node: FormatNode) {
  const attrs = node.data?.kbWrap?.attrs ?? {};
  return { align: attrs['data-align'] ?? null, indent: Number(attrs['data-indent'] ?? 0) };
}

interface ListNode extends FormatNode { ordered?: boolean; start?: number | null; checked?: boolean | null; spread?: boolean | null }
interface CodeNode extends FormatNode { lang?: string | null; meta?: string | null }

function block(node: FormatNode): JSONContent {
  switch (node.type) {
    case 'paragraph':
      return { type: 'paragraph', attrs: wrapAttrs(node), content: inline(node.children ?? []) };
    case 'heading':
      return { type: 'heading', attrs: { level: node.depth, ...wrapAttrs(node) }, content: inline(node.children ?? []) };
    case 'blockquote':
      // An empty quote (">") is the one an editor saves with an empty paragraph in it.
      return { type: 'blockquote', content: (node.children ?? []).length > 0 ? blocks(node.children!) : [{ type: 'paragraph' }] };
    case 'list':
      return list(node as ListNode);
    case 'code': {
      const code = node as CodeNode;
      if (code.meta) return unsupported();
      return { type: 'codeBlock', attrs: { language: code.lang ?? null }, content: code.value ? [{ type: 'text', text: code.value }] : [] };
    }
    case 'thematicBreak':
      return { type: 'horizontalRule' };
    case 'table':
      return gfmTable(node);
    case 'kbTable':
      return kbTable(node, blocks);
    case 'kbFigure':
      return figure(node);
  }
  return unsupported();
}

interface ImageNode extends FormatNode { url?: string; alt?: string | null; title?: string | null }

// figure reads an image's <figure>: the link around the image, if any, and the caption's text.
function figure(node: FormatNode): JSONContent {
  const [shown, caption] = node.children ?? [];
  const image = imageOf(shown) as ImageNode;
  const attrs = node.tag!.attrs;
  return {
    type: 'articleFigure',
    attrs: {
      src: image.url ?? '', alt: image.alt ?? null, title: image.title ?? null,
      href: shown!.type === 'link' ? ((shown as ImageNode).url ?? '') : null,
      linkTitle: shown!.type === 'link' ? ((shown as ImageNode).title ?? null) : null,
      width: attrs['data-width'] ?? null, align: attrs['data-align'] ?? null, border: attrs['data-border'] !== undefined,
    },
    content: caption ? inline(caption.children ?? []) : [],
  };
}

const itemContent = (item: FormatNode) => ((item.children ?? []).length > 0 ? blocks(item.children!) : [{ type: 'paragraph' }]);

// list reads a list; a task list is one whose every item has a checkbox.
function list(node: ListNode): JSONContent {
  const items = (node.children ?? []) as ListNode[];
  // A list is loose, its items shown as paragraphs, when it or any of its items is spread.
  const spread = Boolean(node.spread) || items.some((item) => Boolean(item.spread));
  const tasks = items.filter((item) => typeof item.checked === 'boolean').length;
  if (tasks > 0 && tasks < items.length) return unsupported();
  if (tasks > 0) return { type: 'taskList', attrs: { spread }, content: items.map((item) => ({ type: 'taskItem', attrs: { checked: item.checked }, content: itemContent(item) })) };
  const content = items.map((item) => ({ type: 'listItem', content: itemContent(item) }));
  return node.ordered ? { type: 'orderedList', attrs: { start: node.start ?? 1, spread }, content } : { type: 'bulletList', attrs: { spread }, content };
}
