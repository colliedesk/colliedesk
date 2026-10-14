import { POSITIONING, PRICES_READ, REPO, TAGLINE } from '../site.mjs';
import { ctaButtons, faqHtml, faqMarkdown } from '../layout.mjs';
import { faqPage, organization, softwareApplication } from '../schema.mjs';
import { renderMarkdown } from '../markdown.mjs';
import { statusTable } from './collie-status.mjs';
import { homeSections, homeSectionsMd } from './home-sections.mjs';

const faq = [
  {
    q: 'What is CollieDesk?',
    a: 'CollieDesk is an open-source (AGPL-3.0) helpdesk for small support teams, made by HC Labs. It is the open-source alternative to Freshdesk, Zendesk and Help Scout: a shared inbox for your team, AI included at cost, one flat price per workspace.',
  },
  {
    q: 'Is CollieDesk free?',
    a: 'Self-hosting is free, forever, with every feature. CollieDesk Cloud costs $10 per workspace per month or $100 per year, with no per-agent fees. See [pricing](/pricing).',
  },
  {
    q: 'Is CollieDesk ready to use?',
    a: 'It is pre-alpha (v0.1). The shared inbox, notes, tags, assignment and passwordless sign-in work today. The email channel and Freshdesk importer arrive in v0.2, AI in v0.3. See the [roadmap](/roadmap).',
  },
  {
    q: 'How does AI pricing work?',
    a: 'AI is included, not an add-on. On CollieDesk Cloud you pay the model cost plus 30%, or bring your own key. Self-hosted installs use your own key.',
  },
  {
    q: 'Can I move my Freshdesk tickets to CollieDesk?',
    a: 'Yes, with the Freshdesk importer (coming v0.2): tickets, replies, notes, attachments, tags and contacts, with a reconcile report. Founding customers get the migration done for free. Read [the migration guide](/migrate/freshdesk).',
  },
];

const terminal = `<figure class="terminal" aria-label="Self-host CollieDesk with one command">
  <div class="terminal-bar"><span></span><span></span><span></span><figcaption>self-host in one command</figcaption></div>
<pre><code><span class="t-prompt">$</span> curl -fsSL https://raw.githubusercontent.com/colliedesk/colliedesk/main/install.sh \\
    | sh -s -- --name "Acme Support" --email "you@acme.com"
<span class="t-link">http://localhost:8080/redeem?token=…</span>
<span class="t-out"># one-time sign-in link, no passwords</span></code></pre>
</figure>`;

const statusHtml = `<section class="section" aria-labelledby="today"><div class="wrap">
  <p class="eyebrow">Honest status</p>
  <h2 id="today">What works today, and what is coming</h2>
  <p class="section-lede">We label everything that has not shipped. Dates are targets, not promises.</p>
  <div class="prose prose-wide">${renderMarkdown(statusTable)}</div>
</div></section>`;

const html = `<section class="home-hero"><div class="wrap hero-grid">
  <div>
    <p class="eyebrow"><span class="dot" aria-hidden="true"></span> Open source · AGPL-3.0 · pre-alpha v0.1</p>
    <h1>${TAGLINE.replace('alternative to', 'alternative to<br class="br-lg">')}</h1>
    <p class="lede">${POSITIONING}</p>
    ${ctaButtons('btn-lg')}
    <p class="hero-note">$10 per workspace per month. Self-host for $0. <a href="/pricing">See pricing</a></p>
  </div>
  ${terminal}
</div></section>
${homeSections.replace('<!--STATUS-->', statusHtml)}
<section class="section section-faq"><div class="wrap"><div class="prose">${faqHtml(faq)}</div></div></section>`;

const md = `${POSITIONING}

Status: pre-alpha (v0.1). Self-host free: ${REPO}. Cloud: $10 per workspace per month or $100 per year.

${homeSectionsMd}

## What works today, and what is coming

${statusTable}
${faqMarkdown(faq)}`;

export default {
  name: 'CollieDesk overview',
  path: '/',
  section: 'Product',
  title: 'CollieDesk: the open-source alternative to Freshdesk, Zendesk and Help Scout',
  description:
    'CollieDesk is an open-source helpdesk for small support teams. Leave Freshdesk with all your data. AI included, not an add-on. $10 per workspace, no per-agent fees.',
  summary: `What CollieDesk is, what we sell, status, founding offer; competitor prices read ${PRICES_READ}`,
  h1: TAGLINE,
  lede: POSITIONING,
  html,
  md,
  faq: [],
  bodyClass: 'home',
  jsonLd: [organization(), softwareApplication(), faqPage(faq)],
};
