import { Extension, Node } from '@tiptap/core';
import Document from '@tiptap/extension-document';
import { TableCell, TableHeader } from '@tiptap/extension-table';
import { kbMessages } from '../../kb-messages';
import { rules } from '../format-tags';

// The article's blocks beyond the stock ones. A callout and a Markdown block stand only at the top of the
// article, as the help center reads a callout only there.
export const ArticleDocument = Document.extend({ content: '(block | topBlock)+' });

const classOf = (prefix: string, value: unknown) => (value ? { class: `${prefix}${String(value)}` } : {});
const classValue = (element: HTMLElement, prefix: string, allowed: readonly string[]) =>
  [...element.classList].filter((name) => name.startsWith(prefix)).map((name) => name.slice(prefix.length)).find((value) => allowed.includes(value)) ?? null;

const aligns: readonly string[] = rules.align;
const indents = Array.from({ length: rules.indent }, (_, i) => String(i + 1));

const align = {
  default: null as string | null,
  parseHTML: (element: HTMLElement) => classValue(element, 'kb-align-', aligns),
  renderHTML: (attrs: Record<string, unknown>) => classOf('kb-align-', attrs.align),
};

// BlockFormat gives paragraphs and headings the <p>/<hN> wrapper's alignment and indent (0 is none).
export const BlockFormat = Extension.create({
  name: 'blockFormat',
  addGlobalAttributes: () => [{
    types: ['paragraph', 'heading'],
    attributes: {
      align,
      indent: {
        default: 0,
        parseHTML: (element: HTMLElement) => Number(classValue(element, 'kb-indent-', indents) ?? 0),
        renderHTML: (attrs: Record<string, unknown>) => classOf('kb-indent-', attrs.indent),
      },
    },
  }],
});

// ListSpacing keeps a list loose (blank lines between items, each item a paragraph) when the article
// wrote it so; the page shows nothing of it in the editor.
export const ListSpacing = Extension.create({
  name: 'listSpacing',
  addGlobalAttributes: () => [{
    types: ['bulletList', 'orderedList', 'taskList'],
    attributes: { spread: { default: false, parseHTML: () => false, renderHTML: () => ({}) } },
  }],
});

const palette: readonly string[] = rules.colors;
const cellAttributes = {
  align,
  bg: {
    default: null as string | null,
    parseHTML: (element: HTMLElement) => classValue(element, 'kb-bg-', palette),
    renderHTML: (attrs: Record<string, unknown>) => classOf('kb-bg-', attrs.bg),
  },
};

// Cells carry their alignment and background as the help center's classes, never a style attribute.
export const ArticleCell = TableCell.extend({ addAttributes() { return { ...this.parent?.(), ...cellAttributes }; } });
export const ArticleHeader = TableHeader.extend({ addAttributes() { return { ...this.parent?.(), ...cellAttributes }; } });

// Callout is GitHub's alert syntax (> [!NOTE]), shown as the help center shows it.
export const Callout = Node.create({
  name: 'callout',
  group: 'topBlock',
  content: 'block+',
  defining: true,
  addAttributes: () => ({ type: { default: 'note', rendered: false } }),
  parseHTML: () => [{ tag: 'aside[data-callout]', getAttrs: (element) => ({ type: element.getAttribute('data-callout') }) }],
  renderHTML: ({ node }) => {
    const type = node.attrs.type as keyof typeof kbMessages.calloutLabels;
    return ['aside', { class: `callout callout-${type}`, 'data-callout': type, 'data-label': kbMessages.calloutLabels[type] }, 0];
  },
});

// MarkdownBlock is a block the visual editor cannot show as itself (HTML, a footnote, a link reference…):
// it keeps the block's Markdown as written and shows it read-only.
export const MarkdownBlock = Node.create({
  name: 'markdownBlock',
  group: 'topBlock',
  atom: true,
  selectable: true,
  addAttributes: () => ({ source: { default: '', rendered: false } }),
  parseHTML: () => [{ tag: 'div[data-markdown-block]', getAttrs: (element) => ({ source: element.textContent ?? '' }) }],
  renderHTML: ({ node }) => ['div', { class: 'kb-markdown-block', 'data-markdown-block': '', contenteditable: 'false' }, ['pre', node.attrs.source as string]],
});
