import { mergeAttributes } from '@tiptap/core';
import Image from '@tiptap/extension-image';
import { kbMessages } from '../../kb-messages';

const ownImage = (src: unknown) => typeof src === 'string' && src.startsWith('/api/v1/kb/images/');

// ArticleImage shows the workspace's own images and never loads one from another site: the preview and the
// help center do not show it either, and loading it would tell that site who reads the draft. The note in its
// place keeps the image's source and alt text, so the article saves it unchanged.
export const ArticleImage = Image.extend({
  parseHTML() {
    return [
      { tag: 'span.kb-external-image[data-src]', getAttrs: (element) => ({ src: element.getAttribute('data-src'), alt: element.getAttribute('data-alt') }) },
      ...(this.parent?.() ?? []),
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    const { src, alt } = node.attrs as { src: unknown; alt: string | null };
    if (ownImage(src)) return ['img', mergeAttributes(this.options.HTMLAttributes, HTMLAttributes)];
    const source = typeof src === 'string' ? src : '';
    return ['span', { class: 'kb-external-image', 'data-src': source, 'data-alt': alt ?? '' }, kbMessages.externalImage(alt ?? '', source)];
  },
}).configure({ inline: true });
