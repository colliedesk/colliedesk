// Email HTML is untrusted. It is shown only in a sandboxed frame that runs no scripts, under a policy
// that blocks every load but inline styles, data: images and the Colliedesk image proxy, and the markup
// is stripped of everything that can navigate or fetch: CSP alone does not stop
// <meta http-equiv="refresh"> or a clicked link.
const proxyPath = '/api/v1/mail-image';
// SVG animation can set href to javascript: after this sanitizer has looked, so it goes with <use> and
// <foreignObject>. Both spellings are listed because selectors match SVG element names case-sensitively.
const removedElements = 'meta, base, link, script, noscript, iframe, frame, frameset, object, embed, form, portal, '
  + 'set, animate, animatemotion, animateMotion, animatetransform, animateTransform, discard, use, foreignobject, foreignObject';
const urlAttributes = ['href', 'src', 'srcset', 'action', 'formaction', 'xlink:href', 'ping', 'background', 'poster', 'cite', 'longdesc', 'target'];
const linkTargets = /^(?:https?:|mailto:|tel:)/i;

// The frame is same-origin with the app and sends its cookie, so img-src names the proxy path and not the
// whole origin: CSS url() in a mail must not be able to reach /api/v1/export or any other route. Every other
// fetch directive (font, media, connect, object, child, worker, manifest) falls back to default-src 'none',
// and style-src without a host blocks @import.
function policy(origin: string): string {
  return `default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data: ${origin}${proxyPath}; form-action 'none'; base-uri 'none'; frame-src 'none'`;
}

// A block sized by the viewport (100vh) is sized by the frame, and the frame is sized by its content: the two
// chase each other upward. height and min-height in viewport units become auto and 0; a max-height ceiling
// cannot grow the frame and stays.
function withoutViewportHeights(css: string): string {
  return css
    .replace(/((?:^|[;{\s])min-height)\s*:\s*[\d.]+(?:vh|svh|dvh|lvh)\b/gi, '$1:0')
    .replace(/((?:^|[;{\s])height)\s*:\s*[\d.]+(?:vh|svh|dvh|lvh)\b/gi, '$1:auto');
}

// After the mail's own rules, so it wins: html and body take the height of their content, whatever the
// template says (height:100% inside a frame would equal the frame itself).
const overrideStyle = '<style>html,body{height:auto!important;min-height:0!important}</style>';

// Ours comes first so the mail's own styles win: a sans-serif base only fills in what the mail leaves open.
// Wide mail is made to fit by max-width here and, when a fixed layout still overflows, by fit-frame.ts.
const baseStyle = 'html{-webkit-text-size-adjust:100%}'
  + 'body{margin:0;padding:10px 12px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;font-size:14px;line-height:1.5;overflow-wrap:break-word}'
  + 'img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}'
  + 'summary{display:inline-block;cursor:pointer;list-style:none;padding:0 8px;border-radius:10px;background:#e8eaed;color:#555;font-size:12px;line-height:16px;letter-spacing:1px}'
  + 'summary::-webkit-details-marker{display:none}details.quoted-history[open]>summary{margin-bottom:6px}'
  + 'a{color:#1a5fb4}blockquote{margin:0 0 0 .4em;padding-left:1em;border-left:3px solid #d0d7de}';

function keepsAttribute(element: Element, name: string, value: string): boolean {
  const tag = element.tagName;
  if (tag === 'IMG' && name === 'src') return value.trim().startsWith('data:image/') || value.startsWith(`${proxyPath}?`);
  return tag === 'A' && name === 'href' && linkTargets.test(value.trim());
}

function hasTextBefore(element: Element): boolean {
  for (let node: Element | null = element; node && node.tagName !== 'BODY'; node = node.parentElement) {
    for (let sibling = node.previousSibling; sibling; sibling = sibling.previousSibling) {
      if ((sibling.textContent ?? '').trim() !== '') return true;
    }
  }
  return false;
}

// Quoted history under a reply folds behind a ••• toggle, as in the Freshdesk agent view. <details> needs
// no script. A message that is only a quote (a forward) stays open.
function collapseQuotes(parsed: Document): void {
  parsed.querySelectorAll('div.gmail_quote, div.yahoo_quoted, blockquote').forEach((quote) => {
    if (quote.closest('details.quoted-history') || !hasTextBefore(quote)) return;
    const details = parsed.createElement('details');
    details.className = 'quoted-history';
    const summary = parsed.createElement('summary');
    summary.textContent = '•••';
    summary.title = 'Show quoted text';
    quote.replaceWith(details);
    details.append(summary, quote);
  });
}

// Of the mail's own <body>, only what paints or names it survives: its colours are part of the design (a white
// logo on a dark body is blank without them) and its class may be what the mail's styles select. Event
// handlers and every URL attribute went with the sanitizer pass.
const bodyAttributes = ['style', 'bgcolor', 'text', 'class', 'id', 'dir'];

function bodyTag(body: HTMLElement): string {
  const attributes = bodyAttributes.filter((name) => body.hasAttribute(name))
    .map((name) => ` ${name}="${(body.getAttribute(name) ?? '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')}"`);
  return `<body${attributes.join('')}>`;
}

const ownStructure = 'br, p, table, ul, ol, li, pre, h1, h2, h3, h4, h5, h6, blockquote, hr, details';

// Plain text kept inside a <div> (a chat message, a note an integration wrote) has newlines and no markup for
// them. HTML would fold them into one paragraph, and the sender saw them as lines. They are kept when the
// mail has no structure of its own and the newlines are inside text, not indentation between tags.
function keepsTextBreaks(body: HTMLElement): boolean {
  if (body.querySelector(ownStructure)) return false;
  let hardBreak = false;
  const walker = body.ownerDocument.createTreeWalker(body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = (node as Text).data;
    if (text.trim() === '') {
      const indentation = text.includes('\n') && node.previousSibling?.nodeType === 1 && node.nextSibling?.nodeType === 1;
      if (indentation) return false;
    } else if (text.trim().includes('\n')) hardBreak = true;
  }
  return hardBreak;
}

function sanitize(html: string): string {
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  parsed.querySelectorAll(removedElements).forEach((element) => element.remove());
  parsed.querySelectorAll('*').forEach((element) => {
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || (urlAttributes.includes(name) && !keepsAttribute(element, name, attribute.value))) element.removeAttribute(attribute.name);
      else if (name === 'style') attribute.value = withoutViewportHeights(attribute.value);
    }
  });
  parsed.querySelectorAll('style').forEach((style) => { style.textContent = withoutViewportHeights(style.textContent ?? ''); });
  parsed.querySelectorAll('a[href]').forEach((link) => {
    link.setAttribute('target', '_blank');
    link.setAttribute('rel', 'noopener noreferrer');
  });
  collapseQuotes(parsed);
  const styles = Array.from(parsed.head.querySelectorAll('style')).map((style) => style.outerHTML).join('');
  const breaks = keepsTextBreaks(parsed.body) ? '<style>body{white-space:pre-wrap}</style>' : '';
  return styles + bodyTag(parsed.body) + parsed.body.innerHTML + breaks + overrideStyle;
}

// emailDocument is the srcdoc of the frame. origin is the Colliedesk origin the image proxy lives on.
export function emailDocument(html: string, origin: string): string {
  return `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy(origin)}"><style>${baseStyle}</style>${sanitize(html)}`;
}
