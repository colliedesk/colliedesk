import type { Mark, Node as PMNode } from '@tiptap/pm/model';
import { tagString } from '../format-tags';

// MD is a Markdown tree (mdast) node as the writer builds it.
export interface MD {
  type: string;
  value?: string;
  children?: MD[];
  [key: string]: unknown;
}

export const html = (value: string): MD => ({ type: 'html', value });

// The order marks open in, outermost first; code is always the text itself, innermost.
const order = ['link', 'color', 'highlight', 'underline', 'bold', 'italic', 'strike'];

function container(mark: Mark): MD {
  switch (mark.type.name) {
    case 'link': return { type: 'link', url: mark.attrs.href as string, title: (mark.attrs.title as string | null) ?? null, children: [] };
    case 'bold': return { type: 'strong', children: [] };
    case 'italic': return { type: 'emphasis', children: [] };
    case 'strike': return { type: 'delete', children: [] };
    case 'underline': return { type: 'kbTag', open: '<u>', close: '</u>', children: [] };
    case 'highlight': return { type: 'kbTag', open: '<mark>', close: '</mark>', children: [] };
  }
  const open = tagString({ name: 'span', close: false, attrs: { 'data-color': mark.attrs.color as string } });
  return { type: 'kbTag', open, close: '</span>', children: [] };
}

function leaf(node: PMNode): MD {
  if (node.type.name === 'hardBreak') return { type: 'break' };
  if (node.type.name === 'image') {
    const { src, alt, title } = node.attrs as { src: string; alt: string | null; title: string | null };
    return { type: 'image', url: src, alt: alt ?? '', title: title ?? null };
  }
  if (node.marks.some((mark) => mark.type.name === 'code')) return { type: 'inlineCode', value: node.text ?? '' };
  return { type: 'text', value: node.text ?? '' };
}

// phrasing writes a paragraph's or heading's inline content: a mark stays open over the runs that keep
// it, and the marks a run adds open inside it, in the order above.
export function phrasing(block: PMNode): MD[] {
  const root: MD[] = [];
  const stack: { mark: Mark; node: MD }[] = [];
  // Markdown has no line break at the end of a block: it would show as a backslash.
  let end = block.childCount;
  while (end > 0 && block.child(end - 1).type.name === 'hardBreak') end -= 1;
  block.forEach((child, _offset, index) => {
    if (index >= end) return;
    const wanted = child.marks.filter((mark) => mark.type.name !== 'code')
      .sort((a, b) => order.indexOf(a.type.name) - order.indexOf(b.type.name));
    const kept = stack.findIndex((open) => !wanted.some((mark) => mark.eq(open.mark)));
    if (kept >= 0) stack.length = kept;
    for (const mark of wanted) {
      if (stack.some((open) => open.mark.eq(mark))) continue;
      const node = container(mark);
      (stack.at(-1)?.node.children ?? root).push(node);
      stack.push({ mark, node });
    }
    (stack.at(-1)?.node.children ?? root).push(leaf(child));
  });
  return flatten(root.flatMap(trimEdges));
}

const attention = ['strong', 'emphasis', 'delete'];

// trimEdges moves the spaces at the edges of bold, italic or struck text outside it, where Markdown
// needs them for the text to stay bold.
function trimEdges(node: MD): MD[] {
  if (!node.children) return [node];
  node.children = node.children.flatMap(trimEdges);
  if (!attention.includes(node.type)) return [node];
  const first = node.children[0];
  const last = node.children.at(-1);
  const before = first?.type === 'text' ? /^\s*/.exec(first.value!)![0] : '';
  if (before) first!.value = first!.value!.slice(before.length);
  const after = last?.type === 'text' ? /\s*$/.exec(last.value!)![0] : '';
  if (after) last!.value = last!.value!.slice(0, last!.value!.length - after.length);
  node.children = node.children.filter((child) => child.type !== 'text' || child.value);
  const out: MD[] = node.children.length > 0 ? [node] : [];
  return [...(before ? [{ type: 'text', value: before }] : []), ...out, ...(after ? [{ type: 'text', value: after }] : [])];
}

// flatten writes each <u>, <mark> and <span> as its opening tag, its text and its closing tag.
function flatten(nodes: MD[]): MD[] {
  return nodes.flatMap((node) => {
    if (node.children) node.children = flatten(node.children);
    if (node.type !== 'kbTag') return [node];
    return [html(node.open as string), ...node.children!, html(node.close as string)];
  });
}
