// Static site build: HTML page + Markdown twin per page, llms.txt, llms-full.txt, sitemap,
// robots.txt, RSS and Cloudflare Pages headers. Output: site/_site/. No client JavaScript.
import { copyFileSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { NAME, ORIGIN, POSITIONING, PRICE, REPO, UPDATED } from './site.mjs';
import { LOGO, document, faqHtml, faqMarkdown, hero, mdUrlOf } from './layout.mjs';
import { absolutizeMarkdownLinks, escapeHtml, renderMarkdown, slugify } from './markdown.mjs';
import home from './pages/home.mjs';
import pricing from './pages/pricing.mjs';
import migrateFreshdesk from './pages/migrate-freshdesk.mjs';
import vsFreshdesk from './pages/vs-freshdesk.mjs';
import vsZendesk from './pages/vs-zendesk.mjs';
import vsHelpScout from './pages/vs-help-scout.mjs';
import openSource from './pages/open-source.mjs';
import changelog, { ENTRIES } from './pages/changelog.mjs';
import { docsPages, roadmapPage } from './pages/repo-docs.mjs';

const OUT = fileURLToPath(new URL('../_site/', import.meta.url));
const SRC = fileURLToPath(new URL('./', import.meta.url));
const SECTIONS = ['Product', 'Migrate', 'Compare', 'Docs'];

const pages = [home, pricing, migrateFreshdesk, vsFreshdesk, vsZendesk, vsHelpScout, openSource, roadmapPage(), changelog, ...docsPages()];

function write(rel, content) {
  const file = `${OUT}${rel}`;
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, content);
}
const htmlFileOf = (path) => (path === '/' ? 'index.html' : `${path.slice(1)}.html`);

function main(page) {
  if (page.html) return page.html;
  const source = page.sourceUrl
    ? `<p class="edit-source">This page is rendered from <a href="${page.sourceUrl}" rel="noopener">the repository</a>. Spot a mistake? Open an issue or a pull request.</p>`
    : '';
  const article = `<article class="prose">${renderMarkdown(page.body)}${faqHtml(page.faq)}${source}</article>`;
  const layout = page.aside ? `<div class="docs-layout">${page.aside}${article}</div>` : article;
  return `${hero(page)}${page.preHtml || ''}<div class="wrap page-body">${layout}</div>`;
}

function markdownTwin(page) {
  const lede = page.lede ? `\n> ${page.lede}\n` : '';
  const content = page.md ?? [page.preMd, page.body].filter(Boolean).join('\n\n') + faqMarkdown(page.faq);
  const md = `# ${page.h1}\n${lede}\nSource: ${ORIGIN}${page.path}\nUpdated: ${page.updated || UPDATED}\n\n${content.trim()}\n`;
  return absolutizeMarkdownLinks(md);
}

function llmsTxt() {
  const groups = SECTIONS.map((s) => {
    const lines = pages.filter((p) => p.section === s).map((p) => `- [${p.name || p.h1}](${ORIGIN}${mdUrlOf(p.path)}): ${p.summary}`);
    return `## ${s}\n\n${lines.join('\n')}`;
  });
  return `# ${NAME}

> ${NAME} is an open-source (AGPL-3.0) helpdesk for small support teams by HC Labs: the open-source alternative to Freshdesk, Zendesk and Help Scout. ${POSITIONING} Cloud: ${PRICE.monthly} per workspace per month (${PRICE.yearly} per year). Self-host free. Status: pre-alpha (v0.1).

Every page has a Markdown twin: add \`.md\` to the URL (the home page is ${ORIGIN}/index.md).
All pages in one file: ${ORIGIN}/llms-full.txt
Source code: ${REPO}

${groups.join('\n\n')}
`;
}

const llmsFull = () =>
  `# ${NAME}: all pages\n\nGenerated ${UPDATED}. Each section starts with its Source URL.\n\n${pages.map(markdownTwin).join('\n---\n\n')}`;

const sitemap = () => `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${pages.map((p) => `  <url><loc>${ORIGIN}${p.path}</loc><lastmod>${p.updated || UPDATED}</lastmod></url>`).join('\n')}
</urlset>
`;

// Explicitly welcome search and AI crawlers; nothing on this site is private.
const AI_BOTS = ['GPTBot', 'OAI-SearchBot', 'ChatGPT-User', 'ClaudeBot', 'Claude-User', 'Claude-SearchBot', 'anthropic-ai', 'PerplexityBot', 'Perplexity-User', 'Google-Extended', 'Applebot-Extended', 'CCBot', 'Bingbot', 'DuckAssistBot', 'Meta-ExternalAgent', 'MistralAI-User'];
const robots = () =>
  `${AI_BOTS.map((b) => `User-agent: ${b}\nAllow: /`).join('\n\n')}\n\nUser-agent: *\nAllow: /\n\nSitemap: ${ORIGIN}/sitemap.xml\n`;

const rss = () => `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<title>${NAME} changelog</title><link>${ORIGIN}/changelog</link><description>What shipped in ${NAME}, release by release.</description>
${ENTRIES.map((e) => `<item><title>${escapeHtml(e.title)}</title><link>${ORIGIN}/changelog#${slugify(e.title)}</link><guid isPermaLink="false">colliedesk-${e.version}</guid><pubDate>${new Date(`${e.date}T00:00:00Z`).toUTCString()}</pubDate><description>${escapeHtml(e.md)}</description></item>`).join('\n')}
</channel></rss>
`;

// Cloudflare Pages: serve twins as Markdown and point each twin's canonical at its HTML page.
const headers = () =>
  pages
    .map((p) => `${mdUrlOf(p.path)}\n  Content-Type: text/markdown; charset=utf-8\n  Link: <${ORIGIN}${p.path}>; rel="canonical"`)
    .concat(['/llms.txt\n  Content-Type: text/plain; charset=utf-8', '/llms-full.txt\n  Content-Type: text/plain; charset=utf-8'])
    .join('\n') + '\n';

const notFound = {
  path: '/404', title: 'Page not found · CollieDesk', h1: 'This ticket wandered off',
  description: 'The page you asked for does not exist. Try the home page, the docs or the llms.txt index of every page.',
};
const notFoundHtml = `<section class="page-hero"><div class="wrap"><p class="eyebrow">404</p><h1>${notFound.h1}</h1><p class="lede">The page you asked for does not exist. Try <a href="/">the home page</a>, <a href="/docs">the docs</a> or <a href="/llms.txt">the index of every page</a>.</p></div></section>`;

rmSync(OUT, { recursive: true, force: true });
for (const page of pages) {
  write(htmlFileOf(page.path), document(page, main(page)));
  write(mdUrlOf(page.path).slice(1), markdownTwin(page));
}
write('404.html', document(notFound, notFoundHtml).replace(/<link rel="(canonical|alternate)"[^>]*>\n/g, '').replace('<head>', '<head>\n<meta name="robots" content="noindex">'));
write('llms.txt', llmsTxt());
write('llms-full.txt', llmsFull());
write('sitemap.xml', sitemap());
write('robots.txt', robots());
write('changelog.xml', rss());
write('_headers', headers());
write('favicon.svg', LOGO.replace('class="logo-mark" ', 'xmlns="http://www.w3.org/2000/svg" ').replace(/ aria-hidden="true" focusable="false"/, ''));
copyFileSync(`${SRC}styles.css`, `${OUT}styles.css`);
console.log(`built ${pages.length} pages into _site/`);
