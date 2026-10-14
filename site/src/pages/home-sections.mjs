// Home page sections between the hero and the status table, as HTML and as Markdown.
import { PRICES_READ, REPO } from '../site.mjs';

// 5 agents, list price per month, billed annually, USD only (Zendesk publishes EUR; shown in text).
const BARS = [
  { name: 'Freshdesk Pro + Freddy AI Copilot', usd: 420, math: '5 × ($55 + $29)', href: '/vs/freshdesk' },
  { name: 'Help Scout Plus', usd: 225, math: '5 × $45, + $0.75 per AI resolution', href: '/vs/help-scout' },
  { name: 'CollieDesk Cloud', usd: 10, math: 'one workspace, + AI usage at cost +30%', href: '/pricing', ours: true },
];
const MAX = Math.max(...BARS.map((b) => b.usd));

const bars = BARS.map(
  (b) => `<li class="bar${b.ours ? ' bar-ours' : ''}">
  <div class="bar-label"><a href="${b.href}">${b.name}</a><span class="bar-math">${b.math}</span></div>
  <div class="bar-track"><span class="bar-fill" style="width:${Math.max((b.usd / MAX) * 100, 1.5).toFixed(1)}%"></span><strong>$${b.usd}<small>/mo</small></strong></div>
</li>`,
).join('');

export const homeSections = `
<section class="section" aria-labelledby="selling"><div class="wrap split">
  <div>
    <p class="eyebrow">Wait, what are you selling me?</p>
    <h2 id="selling">Self-hosting is free. We sell two things.</h2>
    <p class="section-lede">No enterprise edition with the good parts held back. Every feature is in the open-source code.</p>
  </div>
  <ol class="sell-list">
    <li><h3>CollieDesk Cloud</h3><p>We run it for you. One flat price per workspace, not per agent: <strong>$10 a month</strong> or $100 a year.</p></li>
    <li><h3>Migration</h3><p>We move your Freshdesk history (tickets, replies, attachments, tags) and give you a reconcile report that proves nothing was lost.</p></li>
  </ol>
</div></section>

<section class="section section-alt" aria-labelledby="pillars"><div class="wrap">
  <h2 id="pillars" class="visually-hidden">Why teams switch</h2>
  <div class="pillars">
    <article class="pillar"><p class="pillar-k">01 · Your data</p><h3>Leave Freshdesk with all of it</h3>
      <p>Freshdesk's ticket export has fields only, no conversation. Our importer brings tickets, replies, notes, checksummed attachments, tags and contacts, then proves the counts match.</p>
      <p><span class="badge badge-coming">Coming v0.2</span> <a href="/migrate/freshdesk">Read the migration guide</a></p></article>
    <article class="pillar"><p class="pillar-k">02 · AI included</p><h3>AI that answers tickets, not an add-on</h3>
      <p>Drafts and triage from your own past replies and knowledge base, auto-answer for repetitive tickets with a human in the loop. Pay model cost +30%, or bring your own key.</p>
      <p><span class="badge badge-coming">Coming v0.3</span> <a href="/roadmap">See the roadmap</a></p></article>
    <article class="pillar"><p class="pillar-k">03 · Flat price</p><h3>No per-agent fees</h3>
      <p>Hire a fifth agent, or a tenth, and the bill stays $10 a month for the workspace. Self-host and it stays $0.</p>
      <p><span class="badge badge-shipped">Price published</span> <a href="/pricing">See pricing</a></p></article>
  </div>
</div></section>

<section class="section" aria-labelledby="bill"><div class="wrap split">
  <div>
    <p class="eyebrow">The bill for five agents</p>
    <h2 id="bill">Per-agent pricing, then per-agent AI</h2>
    <p class="section-lede">List prices per month, billed annually, read on ${PRICES_READ}. Zendesk publishes euros: Suite Professional with Copilot is 5 × (€115 + €50) = €825. <a href="/vs/zendesk">Details</a>.</p>
  </div>
  <ul class="bars" aria-label="Monthly cost for five agents">${bars}</ul>
</div></section>

<!--STATUS-->

<section class="section section-ink" aria-labelledby="collie"><div class="wrap split">
  <div>
    <p class="eyebrow">Why a collie?</p>
    <h2 id="collie">A sheepdog herds every ticket home. Your team stays in charge.</h2>
  </div>
  <div class="prose-invert">
    <p>A collie does not own the flock. It brings every sheep back to the pen and the shepherd decides what happens next. That is the job we give AI in CollieDesk (coming in v0.3): sort, draft and chase every ticket so none is lost, while a person on your team approves what goes out.</p>
  </div>
</div></section>

<section class="section" aria-labelledby="fork"><div class="wrap split">
  <div>
    <p class="eyebrow">AGPL-3.0</p>
    <h2 id="fork">If you don't like something, fork it</h2>
    <p class="section-lede">You were held hostage at renewal once. That should not happen again. The code, your data and the export format are yours.</p>
    <p><a href="/open-source">How the license and the business fit together</a></p>
  </div>
  <pre class="code-card"><code><span class="t-prompt">$</span> gh repo fork colliedesk/colliedesk --clone
<span class="t-prompt">$</span> cd colliedesk &amp;&amp; make dev</code></pre>
</div></section>

<section class="section section-alt" aria-labelledby="quotes"><div class="wrap">
  <h2 id="quotes">From founding customers</h2>
  <!-- SLOT: real founding-customer quotes go here, verbatim, with name and team. Never invent one. -->
  <div class="quote-slot"><p><strong>No quotes yet.</strong> CollieDesk is pre-alpha and we will not make them up. The first founding customers' words will go here, unedited, with their names.</p></div>
</div></section>

<section class="section" id="founding" aria-labelledby="founding-title"><div class="wrap">
  <div class="founding">
    <div>
      <p class="eyebrow">Founding offer</p>
      <h2 id="founding-title">$100 a year. Locked for life.</h2>
      <p class="section-lede">For small teams planning to leave Freshdesk. Same price as Cloud's yearly plan today; it never goes up for you, and we migrate your Freshdesk data for free.</p>
    </div>
    <ul class="check-list">
      <li>$100 per workspace per year, locked for life</li>
      <li>Free Freshdesk migration with a reconcile report</li>
      <li>Your whole team, no per-agent fees</li>
      <li>AI at model cost +30%, or bring your own key</li>
      <li>CollieDesk Cloud opens for founding customers with v0.2</li>
    </ul>
    <div class="founding-cta">
      <!-- TODO(owner): replace this placeholder with the founding checkout link. -->
      <span class="btn btn-primary btn-lg is-placeholder" aria-disabled="true">Checkout opens soon</span>
      <p>The checkout link is being set up. Until then, <a href="${REPO}" rel="noopener">watch the repository on GitHub</a> or self-host it today.</p>
    </div>
  </div>
</div></section>`;

export const homeSectionsMd = `## Wait, what are you selling me?

Self-hosting is free, forever, with every feature. We sell two things:

1. **CollieDesk Cloud**: we run it for you. $10 per workspace per month or $100 per year, not per agent.
2. **Migration**: we move your Freshdesk history (tickets, replies, attachments, tags) and give you a reconcile report that proves nothing was lost.

## Why teams switch

- **Leave Freshdesk with all your data** (importer coming v0.2): tickets, replies, notes, checksummed attachments, tags and contacts, with a reconcile report. [Migration guide](/migrate/freshdesk).
- **AI that answers tickets, included** (coming v0.3): drafts, triage and auto-answer with a human in the loop. Model cost +30%, or bring your own key.
- **Flat price**: $10 per workspace per month, no per-agent fees. Self-host for $0.

## The bill for five agents

List prices per month, billed annually, read on ${PRICES_READ}.

${BARS.map((b) => `- ${b.name}: $${b.usd} (${b.math})`).join('\n')}
- Zendesk Suite Professional + Copilot: 5 × (€115 + €50) = €825 ([details](/vs/zendesk))

## Why a collie?

A collie does not own the flock. It brings every sheep back to the pen and the shepherd decides what happens next. CollieDesk's AI sorts, drafts and chases every ticket so none is lost, while a person on your team approves what goes out.

## If you don't like something, fork it

CollieDesk is AGPL-3.0. The code, your data and the export format are yours. [How the license and the business fit together](/open-source).

## Founding offer

$100 per workspace per year, locked for life, with free Freshdesk migration and a reconcile report. No per-agent fees. AI at model cost +30% or your own key. CollieDesk Cloud opens for founding customers with v0.2. The checkout link is coming soon.`;
