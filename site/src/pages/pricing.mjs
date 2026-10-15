import { REPO, FOUNDING_HREF } from '../site.mjs';
import { faqPage, softwareApplication } from '../schema.mjs';

const faq = [
  {
    q: 'Does CollieDesk charge per agent?',
    a: 'No. CollieDesk Cloud Team is $49 per workspace per month or $490 per year, for up to 10 agents. A workspace is one support team. Adding an agent does not change the price. More than 10 agents: email us.',
  },
  {
    q: 'What does "AI at LLM cost +30%" mean?',
    a: 'On CollieDesk Cloud you can use our AI key. You pay what the model provider charges for the tokens your tickets used, plus 30%. Example: if the provider charges $1.00, you pay $1.30. Or bring your own key and pay your provider directly.',
  },
  {
    q: 'Is there a free plan on Cloud?',
    a: 'Yes. Cloud Free costs $0 for up to 3 agents and 100 conversations a month, with every feature, the Freshdesk importer and AI with your own key. When you go over 100 conversations in a month, you can still read and reply to every conversation; new ones wait until you upgrade or the next month starts.',
  },
  {
    q: 'Is self-hosting really free?',
    a: `Yes. Self-hosting is free, forever, with every feature. The code is AGPL-3.0 on [GitHub](${REPO}). Read the [install guide](/docs/install).`,
  },
  {
    q: 'What is the founding offer?',
    a: 'A founding customer pays $100 per workspace per year, and that price is locked for life. We also migrate your Freshdesk data for free. It is for teams who want to leave Freshdesk and help shape CollieDesk while it is young.',
  },
  {
    q: 'When can I use CollieDesk Cloud?',
    a: 'CollieDesk is pre-alpha (v0.1). Cloud opens for the first founding customers with v0.2, together with the email channel and the Freshdesk importer. See the [roadmap](/roadmap).',
  },
];

const plans = `<section class="wrap plans" aria-label="Plans">
  <article class="plan">
    <h2 class="plan-name">Self-host</h2>
    <p class="plan-price"><strong>$0</strong> <span>forever</span></p>
    <ul>
      <li>Every feature, no paywalled edition</li>
      <li>AGPL-3.0, your server, your PostgreSQL</li>
      <li>AI with your own model key</li>
    </ul>
    <a class="btn btn-ghost" href="${REPO}" rel="noopener">Self-host free <span aria-hidden="true">↗</span></a>
  </article>
  <article class="plan">
    <h2 class="plan-name">Cloud Free <span class="badge badge-coming">Coming v0.2</span></h2>
    <p class="plan-price"><strong>$0</strong> <span>per workspace</span></p>
    <ul>
      <li>Up to 3 agents</li>
      <li>100 conversations a month</li>
      <li>Every feature, including the Freshdesk importer</li>
      <li>AI with your own model key</li>
    </ul>
  </article>
  <article class="plan plan-featured" aria-labelledby="plan-founding">
    <p class="plan-flag">Founding customers</p>
    <h2 class="plan-name" id="plan-founding">Founding</h2>
    <p class="plan-price"><strong>$100</strong> <span>per workspace / year</span></p>
    <ul>
      <li>Price locked for life</li>
      <li>Free Freshdesk migration, with a reconcile report</li>
      <li>Your whole team in one workspace, no per-agent fees</li>
      <li>AI at LLM cost +30%, or bring your own key</li>
    </ul>
    <a class="btn btn-primary" href="${FOUNDING_HREF}">Get founding access</a>
  </article>
  <article class="plan">
    <h2 class="plan-name">Cloud Team <span class="badge badge-coming">Coming v0.2</span></h2>
    <p class="plan-price"><strong>$49</strong> <span>per workspace / month</span></p>
    <p class="plan-alt">or $490 per year</p>
    <ul>
      <li>We run, update and back it up</li>
      <li>Up to 10 agents, unlimited conversations</li>
      <li>No per-agent fees</li>
      <li>AI at LLM cost +30%, or bring your own key</li>
    </ul>
  </article>
</section>`;

const plansMd = `| Plan | Price | What you get |
|---|---|---|
| Self-host | $0, forever | Every feature. AGPL-3.0. AI with your own model key. |
| Cloud Free (coming v0.2) | $0 | Up to 3 agents, 100 conversations a month. Every feature. AI with your own model key. |
| Founding | $100 per workspace per year, locked for life | Free Freshdesk migration with a reconcile report. Your whole team, no per-agent fees. AI at LLM cost +30% or bring your own key. |
| Cloud Team (coming v0.2) | $49 per workspace per month, or $490 per year | Hosted by HC Labs. Up to 10 agents, unlimited conversations, no per-agent fees. AI at LLM cost +30% or bring your own key. |`;

const body = `## What you pay for

Self-hosting is free. We sell two things: **CollieDesk Cloud** (we run it for you) and **migration** (we move your Freshdesk history and prove nothing was lost). That is the whole business. There is no "enterprise edition" with the good features held back.

## AI is included, not an add-on

**Coming v0.3**. AI features: draft replies and triage from your own past replies and knowledge base, and auto-answer for repetitive tickets with a human in the loop. There is no AI seat licence and no per-resolution fee.

- **Use our key on Cloud:** you pay the model provider's cost for your usage, plus 30%.
- **Bring your own key:** on Cloud or self-hosted, you pay your provider directly.

## How this compares

Most helpdesks charge per agent, then charge again for AI. For five agents, Freshdesk Pro with Freddy AI Copilot lists at $420 per month (billed annually). CollieDesk Cloud Team is $49 per month for the workspace, plus AI usage. Prices and sources: [vs Freshdesk](/vs/freshdesk), [vs Zendesk](/vs/zendesk), [vs Help Scout](/vs/help-scout).

## Honest status

CollieDesk is **pre-alpha**. Today it has a shared inbox, private notes, tags, assignment and passwordless sign-in. It cannot receive email yet: the email channel and the Freshdesk importer arrive in v0.2. If you must switch this week, CollieDesk is not ready for you yet. If you are planning your exit before your next Freshdesk renewal, [the founding offer](${FOUNDING_HREF}) is for you.`;

export default {
  name: 'Pricing',
  path: '/pricing',
  section: 'Product',
  title: 'CollieDesk pricing: free plan, $49 per workspace, no per-agent fees',
  description:
    'CollieDesk Cloud is free for up to 3 agents and 100 conversations a month. Team costs $49 per workspace per month ($490 per year), up to 10 agents, no per-agent fees. Self-hosting is free.',
  summary: 'Cloud Free up to 3 agents and 100 conversations a month; Team $49 per workspace per month or $490 per year, up to 10 agents, no per-agent fees; founding offer; AI at LLM cost +30% or your own key; self-host free',
  eyebrow: 'Pricing',
  h1: 'One flat price per workspace. No per-agent fees.',
  lede: 'Start free. Add your whole team, up to 10 agents, for the same $49 a month. Self-host for $0. Pay for AI at cost plus 30%, or bring your own key.',
  preHtml: plans,
  preMd: plansMd,
  body,
  faq,
  jsonLd: [softwareApplication(), faqPage(faq)],
};
