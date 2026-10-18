// Email HTML is untrusted. It is shown only in a sandboxed frame that runs no scripts, under a policy
// that blocks every load but inline styles, data: images and the Colliedesk image proxy, and the markup
// is stripped of everything that can navigate or fetch: CSP alone does not stop
// <meta http-equiv="refresh"> or a clicked link.
const proxyPath = '/api/v1/mail-image?';
const removedElements = 'meta, base, link, script, noscript, iframe, frame, frameset, object, embed, form, portal';
const urlAttributes = ['href', 'src', 'srcset', 'action', 'formaction', 'xlink:href', 'ping', 'background', 'poster', 'cite', 'longdesc', 'target'];
const linkTargets = /^(?:https?:|mailto:|tel:)/i;

function policy(origin: string): string {
  return `default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data: ${origin}; form-action 'none'; base-uri 'none'; frame-src 'none'`;
}

// Ours comes first so the mail's own styles win: a sans-serif base only fills in what the mail leaves open.
// Wide mail is made to fit by max-width here and, when a fixed layout still overflows, by fit-frame.ts.
const baseStyle = 'html{-webkit-text-size-adjust:100%}'
  + 'body{margin:0;padding:0;background:#fff;color:#222;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;font-size:14px;line-height:1.5;overflow-wrap:break-word}'
  + 'img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}'
  + 'a{color:#1a5fb4}blockquote{margin:0 0 0 .4em;padding-left:1em;border-left:3px solid #d0d7de}';

function keepsAttribute(element: Element, name: string, value: string): boolean {
  const tag = element.tagName;
  if (tag === 'IMG' && name === 'src') return value.trim().startsWith('data:image/') || value.startsWith(proxyPath);
  return tag === 'A' && name === 'href' && linkTargets.test(value.trim());
}

function sanitize(html: string): string {
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  parsed.querySelectorAll(removedElements).forEach((element) => element.remove());
  parsed.querySelectorAll('*').forEach((element) => {
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || (urlAttributes.includes(name) && !keepsAttribute(element, name, attribute.value))) element.removeAttribute(attribute.name);
    }
  });
  parsed.querySelectorAll('a[href]').forEach((link) => {
    link.setAttribute('target', '_blank');
    link.setAttribute('rel', 'noopener noreferrer');
  });
  const styles = Array.from(parsed.head.querySelectorAll('style')).map((style) => style.outerHTML).join('');
  return styles + parsed.body.innerHTML;
}

// emailDocument is the srcdoc of the frame. origin is the Colliedesk origin the image proxy lives on.
export function emailDocument(html: string, origin: string): string {
  return `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy(origin)}"><style>${baseStyle}</style>${sanitize(html)}`;
}
