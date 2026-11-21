import type { JSONContent } from '@tiptap/core';
import type { FormatNode } from '../format-tree';

// Unsupported marks Markdown the visual editor cannot show as itself; its whole top-level block is kept
// as a Markdown block.
export class Unsupported extends Error {}
export const unsupported = (): never => {
  throw new Unsupported();
};

type MarkJSON = NonNullable<JSONContent['marks']>[number];

// withMark adds mark to marks; an inner mark of a type already there takes its place (an inner colour wins).
const withMark = (marks: MarkJSON[], mark: MarkJSON) => [...marks.filter((m) => m.type !== mark.type), mark];

interface LinkNode extends FormatNode { url?: string; title?: string | null; alt?: string | null }

const wrapMark: Record<string, string> = { emphasis: 'italic', strong: 'bold', delete: 'strike' };
const tagMark: Record<string, string> = { u: 'underline', mark: 'highlight' };

// inline reads a paragraph's or heading's text with its marks.
export function inline(children: FormatNode[], marks: MarkJSON[] = []): JSONContent[] {
  return children.flatMap((child): JSONContent[] => {
    const node = child as LinkNode;
    const text = (value: string, with_: MarkJSON[]) => (value ? [{ type: 'text', text: value, ...(with_.length > 0 ? { marks: with_ } : {}) }] : []);
    const marked = (with_: MarkJSON[]) => (with_.length > 0 ? { marks: with_ } : {});
    switch (node.type) {
      case 'text':
        return text((node.value ?? '').replace(/\n/g, ' '), marks);
      case 'inlineCode':
        return text(node.value ?? '', withMark(marks, { type: 'code' }));
      case 'emphasis': case 'strong': case 'delete':
        return inline(node.children ?? [], withMark(marks, { type: wrapMark[node.type]! }));
      case 'link':
        return inline(node.children ?? [], withMark(marks, { type: 'link', attrs: { href: node.url ?? '', title: node.title ?? null } }));
      case 'break':
        return [{ type: 'hardBreak', ...marked(marks) }];
      case 'image':
        return [{ type: 'image', attrs: { src: node.url ?? '', alt: node.alt ?? null, title: node.title ?? null }, ...marked(marks) }];
      case 'kbInline':
        return inline(node.children ?? [], kbMark(node, marks));
    }
    return unsupported();
  });
}

// kbMark is the mark a <u>, <mark> or <span data-color> gives; a span with no colour gives none.
function kbMark(node: FormatNode, marks: MarkJSON[]) {
  const tag = node.tag!;
  if (tagMark[tag.name]) return withMark(marks, { type: tagMark[tag.name]! });
  const color = tag.attrs['data-color'];
  if (tag.name === 'span' && color) return withMark(marks, { type: 'color', attrs: { color } });
  if (tag.name === 'span') return marks;
  return unsupported();
}
