// Inline HTML (text, emphasis, code, links, images) to Markdown, with the escaping rules of the
// Freshdesk import (server/internal/freshdesk). A line break is kept as LINE_BREAK until the
// paragraph is finished, so the white space around it can be trimmed.
export const LINE_BREAK = '\u0001';

const ELEMENT = 1;
const TEXT = 3;

export function inlineMarkdown(nodes: Iterable<Node>, images: Set<string>): string {
  let out = '';
  for (const node of nodes) out += inlineNode(node, images);
  return out;
}

function inlineNode(node: Node, images: Set<string>): string {
  if (node.nodeType === TEXT) return escapeText((node.nodeValue ?? '').replace(/[ \t\n\r\f]+/g, ' '));
  if (node.nodeType !== ELEMENT) return '';
  const el = node as Element;
  switch (el.tagName) {
    case 'BR': return LINE_BREAK;
    case 'STRONG': case 'B': return emphasis('**', inlineMarkdown(el.childNodes, images));
    case 'EM': case 'I': return emphasis('*', inlineMarkdown(el.childNodes, images));
    case 'DEL': case 'S': case 'STRIKE': return emphasis('~~', inlineMarkdown(el.childNodes, images));
    case 'CODE': case 'KBD': case 'SAMP': case 'TT': return codeSpan(el.textContent ?? '');
    case 'A': return link(el, inlineMarkdown(el.childNodes, images));
    case 'IMG': return image(el, images);
    default: return inlineMarkdown(el.childNodes, images);
  }
}

// Spaces inside emphasis move outside it: "<b> x </b>" is " **x** ", never "** x **".
function emphasis(mark: string, content: string): string {
  const [, before, inner, after] = /^( *)([\s\S]*?)( *)$/.exec(content) ?? ['', '', content, ''];
  return inner ? `${before}${mark}${inner}${mark}${after}` : before + after;
}

function codeSpan(raw: string): string {
  const text = raw.replace(/[ \t\n\r\f]+/g, ' ');
  if (!text.trim()) return '';
  const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((run) => run.length));
  const fence = '`'.repeat(longest + 1);
  const pad = text.startsWith('`') || text.endsWith('`') ? ' ' : '';
  return `${fence}${pad}${text}${pad}${fence}`;
}

function link(el: Element, content: string): string {
  const href = (el.getAttribute('href') ?? '').trim();
  if (!href || !linkAllowed(href)) return content;
  const [, before, inner, after] = /^( *)([\s\S]*?)( *)$/.exec(content) ?? ['', '', content, ''];
  if (!inner) return before + after;
  const title = el.getAttribute('title');
  const quoted = title ? ` "${title.replace(/[\\"]/g, '\\$&').replace(/\s+/g, ' ')}"` : '';
  return `${before}[${inner}](${markdownAddress(href)}${quoted})${after}`;
}

function image(el: Element, images: Set<string>): string {
  const src = (el.getAttribute('src') ?? '').trim();
  if (!src || !imageAllowed(src)) return '';
  if (/^(https?|blob|data):/i.test(src)) images.add(src);
  const alt = (el.getAttribute('alt') ?? '').replace(/\s+/g, ' ').trim().replace(/[\\[\]]/g, '\\$&');
  return `![${alt}](${markdownAddress(src)})`;
}

// What a browser would read as the scheme: it ignores spaces and control characters inside it.
function scheme(url: string): string | null {
  const plain = url.replace(/[\u0000- \u007f]/g, '').toLowerCase();
  return /^([a-z][a-z0-9+.-]*):/.exec(plain)?.[1] ?? null;
}

// The links the import keeps: web, mail, relative and in-page. A javascript: or data: link
// keeps only its text.
export function linkAllowed(href: string): boolean {
  const name = scheme(href);
  return name === null || name === 'http' || name === 'https' || name === 'mailto';
}

// The images a paste keeps: web, relative, a picture the browser holds (blob:) and a raster
// data: image. Each absolute one is stored in the media library before the paste lands.
export function imageAllowed(src: string): boolean {
  const name = scheme(src);
  if (name === 'data') return /^data:image\/(png|jpe?g|gif|webp|avif|bmp)[;,]/i.test(src.replace(/\s/g, ''));
  return name === null || name === 'http' || name === 'https' || name === 'blob';
}

// markdownAddress writes a link or image address so Markdown reads it whole.
export function markdownAddress(url: string): string {
  return url.replace(/[ ()<>\n\r\t]/g, (c) => encodeURIComponent(c));
}

// Characters that would start Markdown in running text: backslash, backtick, opening bracket,
// a * or _ that is not followed by a space, and angle brackets (raw HTML).
function escapeText(text: string): string {
  return text
    .replace(/\\/g, '\\\\')
    .replace(/`/g, '\\`')
    .replace(/\[/g, '\\[')
    .replace(/[*_](?! )/g, '\\$&')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// A line of text that would read as a heading, a list item or a numbered item.
export function escapeLineStart(line: string): string {
  if (/^#{1,6}( |$)/.test(line) || /^[-+*]( |$)/.test(line)) return `\\${line}`;
  return line.replace(/^(\d{1,9})([.)])( |$)/, '$1\\$2$3');
}
