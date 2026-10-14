import { Marked } from 'marked';
import { ORIGIN } from './site.mjs';

export const slugify = (text) =>
  text.toLowerCase().replace(/<[^>]+>/g, '').replace(/&[a-z]+;/g, '').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

export const escapeHtml = (s) =>
  String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// Markdown -> HTML with heading ids (deep links), scrollable tables and external links marked.
export function renderMarkdown(md, { rewriteLink = (href) => href } = {}) {
  const marked = new Marked({ gfm: true });
  marked.use({
    walkTokens(token) {
      if (token.type === 'link') token.href = rewriteLink(token.href);
    },
    renderer: {
      heading({ tokens, depth }) {
        const text = this.parser.parseInline(tokens);
        return `<h${depth} id="${slugify(text)}">${text}</h${depth}>\n`;
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const external = /^https?:\/\//.test(href) && !href.startsWith(ORIGIN);
        const attrs = external ? ' rel="noopener"' : '';
        const t = title ? ` title="${escapeHtml(title)}"` : '';
        return `<a href="${escapeHtml(href)}"${t}${attrs}>${text}</a>`;
      },
    },
  });
  return marked
    .parse(md)
    // "**Coming v0.2**" / "**Shipped v0.1**" in Markdown become status badges in HTML.
    .replace(/<strong>(Coming (?:v0\.\d|later))<\/strong>/g, '<span class="badge badge-coming">$1</span>')
    .replace(/<strong>(Shipped v0\.\d)<\/strong>/g, '<span class="badge badge-shipped">$1</span>')
    .replace(/<table>/g, '<div class="table-wrap" role="region" aria-label="Table" tabindex="0"><table>')
    .replace(/<\/table>/g, '</table></div>');
}

// Markdown twin links must work outside the site, so root-relative links become absolute.
export const absolutizeMarkdownLinks = (md) => md.replace(/\]\(\/(?!\/)/g, `](${ORIGIN}/`);
