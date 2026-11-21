import { Node } from '@tiptap/core';
import { kbMessages } from '../../kb-messages';
import { figureClass, rules, tagString, within } from '../format-tags';
import type { FormatTag } from '../format-tags';

export interface FigureAttrs {
  src: string;
  alt: string | null;
  title: string | null;
  href: string | null;
  width: string | null;
  align: string | null;
  border: boolean;
}

// figureTag is the <figure> tag a figure's attributes write, in the server's one spelling.
export function figureTag(attrs: FigureAttrs): FormatTag {
  return {
    name: 'figure',
    close: false,
    attrs: { 'data-width': attrs.width ?? undefined, 'data-align': attrs.align ?? undefined, 'data-border': attrs.border ? '' : undefined },
  };
}

const ownImage = (src: string) => src.startsWith('/api/v1/kb/images/');

// ArticleFigure is an image in a <figure>: its width, alignment and border are attributes, and its content
// is the caption's text. It shows as the help center shows it, with the same classes.
export const ArticleFigure = Node.create({
  name: 'articleFigure',
  group: 'block',
  content: 'inline*',
  marks: '_',
  defining: true,
  isolating: true,
  addAttributes: () => ({
    src: { default: '', rendered: false },
    alt: { default: null, rendered: false },
    title: { default: null, rendered: false },
    href: { default: null, rendered: false },
    width: { default: null, rendered: false },
    align: { default: null, rendered: false },
    border: { default: false, rendered: false },
  }),
  parseHTML: () => [{
    tag: 'figure[data-figure]',
    contentElement: 'figcaption',
    getAttrs: (element) => {
      const image = element.querySelector('img, span.kb-external-image');
      const width = element.getAttribute('data-width');
      const align = element.getAttribute('data-align');
      return {
        src: image?.getAttribute('src') ?? image?.getAttribute('data-src') ?? '',
        alt: image?.getAttribute('alt') ?? image?.getAttribute('data-alt') ?? null,
        href: element.getAttribute('data-href'),
        width: width && ((rules.figure.widths as readonly string[]).includes(width) || within(width, 1, rules.figure.pixels)) ? width : null,
        align: align && (rules.figure.align as readonly string[]).includes(align) ? align : null,
        border: element.hasAttribute('data-border'),
      };
    },
  }],
  renderHTML: ({ node }) => {
    const attrs = node.attrs as FigureAttrs;
    const tag = figureTag(attrs);
    const pixels = attrs.width && within(attrs.width, 1, rules.figure.pixels) ? { width: attrs.width } : {};
    const image = ownImage(attrs.src)
      ? ['img', { src: attrs.src, alt: attrs.alt ?? '', ...pixels }]
      : ['span', { class: 'kb-external-image', 'data-src': attrs.src, 'data-alt': attrs.alt ?? '' }, kbMessages.externalImage(attrs.alt ?? '', attrs.src)];
    const data = {
      'data-figure': tagString(tag),
      ...(attrs.width ? { 'data-width': attrs.width } : {}),
      ...(attrs.align ? { 'data-align': attrs.align } : {}),
      ...(attrs.border ? { 'data-border': '' } : {}),
      ...(attrs.href ? { 'data-href': attrs.href } : {}),
    };
    return ['figure', { class: figureClass(tag).join(' '), ...data }, ['div', { contenteditable: 'false' }, image], ['figcaption', 0]];
  },
});
