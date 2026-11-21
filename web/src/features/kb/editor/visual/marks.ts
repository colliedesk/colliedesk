import { Mark } from '@tiptap/core';
import Code from '@tiptap/extension-code';
import type { Palette } from '../format-tags';
import { rules } from '../format-tags';
import { linkAllowed } from '../paste-html-inline';

// The marks an article's text can carry beyond Markdown's own: each is one formatting tag the server keeps
// (allowlist.json "formatting"), shown with the help center's classes (formatting.css).

// Link is a Markdown link: [text](href "title"). A javascript: or data: address shows as a link with no address,
// so a click never runs it; the article keeps the address, and the help center drops it as before.
export const Link = Mark.create({
  name: 'link',
  inclusive: false,
  addAttributes: () => ({ href: { default: '' }, title: { default: null } }),
  parseHTML: () => [{
    tag: 'a[href]',
    getAttrs: (element) => ({ href: element.getAttribute('href'), title: element.getAttribute('title') }),
  }],
  renderHTML: ({ HTMLAttributes }) => {
    const href = String(HTMLAttributes.href ?? '');
    return ['a', { href: linkAllowed(href) ? href : null, title: HTMLAttributes.title, rel: 'noopener noreferrer' }, 0];
  },
});

const palette: readonly string[] = rules.colors;

// Color is <span data-color="…">, one of the palette's colours.
export const Color = Mark.create({
  name: 'color',
  addAttributes: () => ({ color: { default: 'accent' as Palette, rendered: false } }),
  parseHTML: () => [{
    tag: 'span[class]',
    getAttrs: (element) => {
      const color = [...element.classList].map((name) => name.replace(/^kb-color-/, '')).find((name) => palette.includes(name));
      return color ? { color } : false;
    },
  }],
  renderHTML: ({ mark }) => ['span', { class: `kb-color-${mark.attrs.color as string}` }, 0],
});

// Highlight is <mark>.
export const Highlight = Mark.create({
  name: 'highlight',
  parseHTML: () => [{ tag: 'mark' }],
  renderHTML: () => ['mark', 0],
});

// Inline code may sit inside a link or bold text, as Markdown allows ([`x`](…), **`x`**).
export const InlineCode = Code.extend({ excludes: '' });
