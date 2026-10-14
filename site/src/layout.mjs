import { FOOTER, FOUNDING_HREF, NAME, NAV, ORIGIN, REPO, UPDATED } from './site.mjs';
import { escapeHtml as e, renderMarkdown } from './markdown.mjs';

export const mdUrlOf = (path) => (path === '/' ? '/index.md' : `${path}.md`);

export const LOGO = `<svg class="logo-mark" viewBox="0 0 32 32" width="28" height="28" aria-hidden="true" focusable="false">
<rect width="32" height="32" rx="8" fill="#15171a"/>
<circle cx="7.5" cy="8.5" r="2.4" fill="#fff"/><circle cx="12" cy="12.5" r="2.2" fill="#fff"/><circle cx="15.5" cy="17" r="2" fill="#fff"/>
<path d="M19.5 13.5v10h7v-10" fill="none" stroke="#f08a50" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`;

const link = ({ href, label }, current) => {
  const ext = href.startsWith('http') ? ' rel="noopener"' : '';
  const cur = current === href || (href !== '/' && current.startsWith(`${href}/`)) ? ' aria-current="page"' : '';
  return `<a href="${href}"${ext}${cur}>${label}</a>`;
};

export const ctaButtons = (size = '') => `<div class="cta-row">
  <a class="btn btn-primary ${size}" href="${FOUNDING_HREF}">Get founding access</a>
  <a class="btn btn-ghost ${size}" href="${REPO}" rel="noopener">Self-host free <span aria-hidden="true">↗</span></a>
</div>`;

function header(path) {
  const items = NAV.map((n) => `<li>${link(n, path)}</li>`).join('');
  return `<header class="site-header"><div class="wrap header-inner">
  <a class="brand" href="/">${LOGO}<span>${NAME}</span></a>
  <nav aria-label="Main" class="nav-desktop"><ul>${items}<li>${link({ href: REPO, label: 'GitHub' }, path)}</li></ul></nav>
  <a class="btn btn-primary btn-sm header-cta" href="${FOUNDING_HREF}">Get founding access</a>
  <details class="nav-mobile"><summary>Menu</summary><nav aria-label="Main (mobile)"><ul>${items}<li>${link({ href: REPO, label: 'GitHub' }, path)}</li></ul></nav></details>
</div></header>`;
}

function footer() {
  const cols = FOOTER.map(
    (c) => `<div><h2 class="footer-title">${c.title}</h2><ul>${c.links.map((l) => `<li>${link(l, '')}</li>`).join('')}</ul></div>`,
  ).join('');
  return `<footer class="site-footer"><div class="wrap footer-grid">
  <div class="footer-about"><a class="brand" href="/">${LOGO}<span>${NAME}</span></a>
  <p>Open-source helpdesk by HC Labs. AGPL-3.0. Every page on this site has a plain Markdown version: add <code>.md</code> to the URL.</p></div>
  ${cols}
</div><div class="wrap footer-legal"><p>© 2026 HC Labs. Pre-alpha software: things will change, and we will say so in the <a href="/changelog">changelog</a>.</p></div></footer>`;
}

export function hero(page) {
  const eyebrow = page.eyebrow ? `<p class="eyebrow">${page.eyebrow}</p>` : '';
  const lede = page.lede ? `<p class="lede">${renderInline(page.lede)}</p>` : '';
  return `<section class="page-hero"><div class="wrap">${eyebrow}<h1>${e(page.h1)}</h1>${lede}${page.cta ? ctaButtons() : ''}</div></section>`;
}

const renderInline = (md) => renderMarkdown(md).replace(/^<p>|<\/p>\n?$/g, '');

export function faqHtml(faq) {
  if (!faq?.length) return '';
  const items = faq.map((f) => `<div class="faq-item"><h3>${e(f.q)}</h3>${renderMarkdown(f.a)}</div>`).join('');
  return `<section class="faq" aria-labelledby="faq"><h2 id="faq">Questions people ask</h2>${items}</section>`;
}

export function faqMarkdown(faq) {
  if (!faq?.length) return '';
  return `\n\n## Questions people ask\n\n${faq.map((f) => `### ${f.q}\n\n${f.a}`).join('\n\n')}\n`;
}

export function document(page, main) {
  const canonical = `${ORIGIN}${page.path}`;
  const ld = (page.jsonLd || []).map((o) => `<script type="application/ld+json">${JSON.stringify(o)}</script>`).join('\n');
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${e(page.title)}</title>
<meta name="description" content="${e(page.description)}">
<link rel="canonical" href="${canonical}">
<link rel="alternate" type="text/markdown" href="${mdUrlOf(page.path)}">
<link rel="alternate" type="application/rss+xml" title="CollieDesk changelog" href="/changelog.xml">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="/styles.css">
<meta name="theme-color" content="#ffffff">
<meta property="og:type" content="website">
<meta property="og:site_name" content="${NAME}">
<meta property="og:title" content="${e(page.title)}">
<meta property="og:description" content="${e(page.description)}">
<meta property="og:url" content="${canonical}">
<meta name="twitter:card" content="summary">
<meta name="last-modified" content="${page.updated || UPDATED}">
${ld}
</head>
<body class="${page.bodyClass || ''}">
<a class="skip" href="#main">Skip to content</a>
${header(page.path)}
<main id="main">
${main}
</main>
${footer()}
</body>
</html>
`;
}
