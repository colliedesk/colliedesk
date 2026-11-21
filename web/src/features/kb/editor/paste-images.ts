import type { Named } from './editor-types';
import { markdownAddress } from './paste-html-inline';

// An image pasted by its address is named after its file; one the browser holds (blob:, data:) is "image".
export const nameOf = (src: string): Named => {
  if (!/^https?:/i.test(src)) return { name: 'image' };
  try {
    return { name: decodeURIComponent(new URL(src).pathname.split('/').pop() ?? '') || 'image' };
  } catch {
    return { name: 'image' };
  }
};

const escaped = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// stored writes each image at the address the library gave it; one it could not store is left out, and the
// blank lines it leaves close up.
export function stored(markdown: string, images: string[], urls: (string | undefined)[]) {
  let text = markdown;
  images.forEach((src, at) => {
    const address = markdownAddress(src);
    const url = urls[at];
    text = url ? text.split(`](${address})`).join(`](${markdownAddress(url)})`)
      : text.replace(new RegExp(`!\\[(?:\\\\.|[^\\]\\\\])*\\]\\(${escaped(address)}\\)`, 'g'), '');
  });
  return text.replace(/\n{3,}/g, '\n\n').replace(/^\n+|\n+$/g, '');
}
