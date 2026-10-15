import { PRICES_READ } from '../site.mjs';
import { faqPage, techArticle } from '../schema.mjs';
import { collieFacts as c, statusTable } from './collie-status.mjs';

const faq = [
  {
    q: 'What is the best open-source alternative to Freshdesk?',
    a: 'CollieDesk is an open-source (AGPL-3.0) helpdesk built for small teams leaving Freshdesk: flat $49 per workspace per month, AI included at cost, and a Freshdesk importer (coming v0.2). It is pre-alpha today, so check the status table before you switch.',
  },
  {
    q: 'How much does Freshdesk cost for 5 agents with AI?',
    a: `At list prices read on ${PRICES_READ}: Freshdesk Pro is $55 per agent per month billed annually, and Freddy AI Copilot is $29 per agent per month. Five agents: 5 × ($55 + $29) = $420 per month.`,
  },
  {
    q: 'Is CollieDesk ready to replace Freshdesk today?',
    a: 'Not for most teams. CollieDesk v0.1 has a shared inbox but no email channel yet; email and the Freshdesk importer arrive in v0.2. If your renewal is months away, the founding offer locks $100 per year for life and includes the migration.',
  },
  {
    q: 'Can I get my tickets out of Freshdesk with the full conversation?',
    a: 'Not with the ticket export in the Freshdesk UI: it exports ticket fields to CSV or Excel, without the conversation. The full thread needs the Freshdesk API. See [how to migrate from Freshdesk](/migrate/freshdesk).',
  },
];

const body = `## The short answer

- **Choose Freshdesk** if you need a mature, broad helpdesk today: many channels, a large marketplace and an AI agent you can switch on now.
- **Choose CollieDesk** if per-agent pricing and paid AI add-ons are the problem, you want your support data in a database you control, and you can plan your move around our [roadmap](/roadmap). Or self-host it free and look around.

## Price, side by side

Freshdesk prices are per agent per month, billed annually, as published on ${PRICES_READ}.

| | Freshdesk | CollieDesk |
|---|---|---|
| Plans | Growth $19, Pro $55, Enterprise $89 per agent per month | ${c.price} |
| AI drafting | Freddy AI Copilot $29 per agent per month, add-on for Pro and Enterprise only | ${c.ai} |
| AI agent | 500 sessions free once per account, then $49 per 100 sessions | Auto-answer with a human in the loop, same AI pricing (coming v0.3) |
| Free option | No free plan on the pricing page; 14-day trial | ${c.free} |
| Source code | Proprietary | ${c.source} |

### What five agents pay

| Team | Freshdesk Pro + Copilot | CollieDesk Cloud |
|---|---|---|
| 5 agents | 5 × ($55 + $29) = **$420 per month** | $49 per month + AI usage |
| 10 agents | 10 × ($55 + $29) = **$840 per month** | $49 per month + AI usage |

List prices before tax. AI usage on CollieDesk depends on ticket volume and the model you pick; with your own key it is billed by your provider.

## Leaving: what Freshdesk gives you

- The ticket export in the Freshdesk UI gives **ticket fields only**, as CSV or Excel. It does not include the conversation, and archived tickets are excluded.
- The API lists **100 tickets per page, up to 300 pages** (30,000 tickets) per filter, and only the **last 30 days** unless you pass \`updated_since\`.
- Closed tickets with no activity for 120 days are **archived** and fetched through a separate endpoint.
- API rate limits are **100, 400 or 700 calls per minute** on Growth, Pro and Enterprise.

CollieDesk keeps your data in PostgreSQL with an open, documented model. ${c.export} The step-by-step guide: [migrate from Freshdesk](/migrate/freshdesk).

## What CollieDesk does today

${statusTable}

## Sources

Read on ${PRICES_READ}. Prices change; check the vendor page before you decide.

- Freshdesk pricing: [freshworks.com/freshdesk/pricing](https://www.freshworks.com/freshdesk/pricing/)
- Ticket export (fields only, archived excluded): [support.freshdesk.com article 225158](https://support.freshdesk.com/support/solutions/articles/225158)
- API pagination, 30-day default, archived tickets: [developers.freshdesk.com/api](https://developers.freshdesk.com/api/)
- API rate limits: [support.freshdesk.com article 225439](https://support.freshdesk.com/support/solutions/articles/225439-what-are-the-rate-limits-for-the-api-calls-to-freshdesk-)`;

const page = {
  path: '/vs/freshdesk',
  section: 'Compare',
  title: 'CollieDesk vs Freshdesk: flat $49 per workspace vs $19–89 per agent',
  description:
    'An honest comparison of CollieDesk and Freshdesk: published prices, AI add-on costs, export limits and what CollieDesk can and cannot do yet. Prices read 14 October 2026.',
  summary: 'Freshdesk $19–89 per agent plus $29 Copilot vs CollieDesk $49 per workspace; export limits; honest status',
  eyebrow: 'Compare · Freshdesk alternative',
  h1: 'CollieDesk vs Freshdesk',
  lede: 'Freshdesk is a mature, broad helpdesk. CollieDesk is a young open-source one with a flat price. Here is the honest difference, with sources.',
  cta: true,
  body,
  faq,
};
page.jsonLd = [techArticle(page), faqPage(faq)];
export default page;
