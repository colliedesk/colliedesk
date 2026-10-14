// Pages rendered from Markdown that lives in the repository: docs/user/*.md and ROADMAP.md.
// The repo files stay the single source; the site only rewrites their links.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { REPO } from '../site.mjs';
import { techArticle } from '../schema.mjs';

const repoRoot = fileURLToPath(new URL('../../../', import.meta.url));
const readRepo = (rel) => readFileSync(`${repoRoot}${rel}`, 'utf8');

// Order and slugs follow the links in docs/user/README.md, so a new guide only needs a line there.
export const DOC_FILES = [...readRepo('docs/user/README.md').matchAll(/\]\(([a-z-]+)\.md\)/g)].map((m) => m[1]);

function rewriteLink(href, fromDir) {
  if (/^(https?:|mailto:|#)/.test(href)) return href;
  const [path, hash = ''] = href.split('#');
  const frag = hash ? `#${hash}` : '';
  const resolved = new URL(path, `https://repo/${fromDir}`).pathname.slice(1); // repo-relative
  if (resolved === 'ROADMAP.md') return `/roadmap${frag}`;
  if (resolved === 'docs/user/README.md') return `/docs${frag}`;
  const doc = resolved.match(/^docs\/user\/([a-z-]+)\.md$/);
  if (doc) return `/docs/${doc[1]}${frag}`;
  return `${REPO}/blob/main/${resolved}${frag}`;
}

const rewriteLinks = (md, fromDir) => md.replace(/\]\(([^)\s]+)\)/g, (_, href) => `](${rewriteLink(href, fromDir)})`);

// Splits "# Title\n\nFirst paragraph\n\nrest" into parts. The first paragraph becomes the lede
// unless it is a list, table or code block.
function split(md) {
  const [, title, rest] = md.match(/^# (.+)\n+([\s\S]*)$/);
  const [first, ...others] = rest.split(/\n\s*\n/);
  const isProse = first && !/^(\s*[-|*]\s|\s*\d+\.\s|```)/.test(first) && !/\n\s*[-*]\s/.test(first);
  return isProse
    ? { title, lede: first.replace(/\n/g, ' '), body: others.join('\n\n') }
    : { title, lede: '', body: rest };
}

const plain = (md) => md.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[*_`]/g, '');

function docsNav(current) {
  const items = [['/docs', 'Overview'], ...DOC_FILES.map((f) => [`/docs/${f}`, titleOf(f)])];
  const lis = items
    .map(([href, label]) => `<li><a href="${href}"${href === current ? ' aria-current="page"' : ''}>${label}</a></li>`)
    .join('');
  return `<nav class="docs-nav" aria-label="Docs"><p class="docs-nav-title">User guides</p><ul>${lis}</ul></nav>`;
}

const titleOf = (file) => readRepo(`docs/user/${file}.md`).match(/^# (.+)$/m)[1];

function docPage(path, file, summaryFallback) {
  const fromDir = file.slice(0, file.lastIndexOf('/') + 1);
  const { title, lede, body } = split(rewriteLinks(readRepo(file), fromDir));
  const description = `${plain(lede || summaryFallback)} CollieDesk user guide, v0.1 (pre-alpha).`;
  const page = {
    path,
    section: 'Docs',
    title: `${title} · CollieDesk docs`,
    description,
    summary: plain(lede || summaryFallback),
    eyebrow: 'Docs',
    h1: title,
    lede,
    body,
    aside: docsNav(path),
    sourceUrl: `${REPO}/blob/main/${file}`,
  };
  page.jsonLd = [techArticle(page)];
  return page;
}

export function docsPages() {
  const index = docPage('/docs', 'docs/user/README.md', 'One page per CollieDesk feature: what it does, how to use it, and its limits.');
  index.h1 = 'CollieDesk docs';
  index.title = 'CollieDesk docs: install, sign in, invite your team, work the inbox';
  return [index, ...DOC_FILES.map((f) => docPage(`/docs/${f}`, `docs/user/${f}.md`, `How ${titleOf(f).toLowerCase()} works in CollieDesk.`))];
}

export function roadmapPage() {
  const page = docPage('/roadmap', 'ROADMAP.md', '');
  Object.assign(page, {
    section: 'Product',
    aside: '',
    eyebrow: 'Roadmap',
    h1: 'CollieDesk roadmap',
    title: 'CollieDesk roadmap: email and Freshdesk import in v0.2, AI in v0.3',
    description:
      'What CollieDesk has shipped and what comes next: email channel and Freshdesk importer (v0.2), AI drafting and auto-answer (v0.3), automations and SLA (v0.4). Dates are targets.',
    summary: 'Shipped v0.1; next v0.2 email + Freshdesk importer + Cloud; v0.3 AI included; later automations, SLA, reports, MCP',
  });
  page.jsonLd = [techArticle(page)];
  return page;
}
