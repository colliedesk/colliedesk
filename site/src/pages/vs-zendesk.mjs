import { PRICES_READ } from '../site.mjs';
import { faqPage, techArticle } from '../schema.mjs';
import { collieFacts as c, statusTable } from './collie-status.mjs';

const faq = [
  {
    q: 'Is there a Zendesk alternative that is not priced per agent?',
    a: 'Yes. CollieDesk Cloud is $10 per workspace per month, whatever the number of agents, and self-hosting is free. CollieDesk is open source (AGPL-3.0) and pre-alpha today.',
  },
  {
    q: 'How does Zendesk charge for AI?',
    a: `On the Zendesk pricing page read on ${PRICES_READ}: Copilot is €50 per agent per month billed annually, and AI agents are billed per automated resolution, €1.50 committed or €2.00 pay-as-you-go, with some resolutions included per agent.`,
  },
  {
    q: 'Can I export all my Zendesk tickets?',
    a: 'Zendesk\'s native data export is off by default: the account owner must ask Zendesk Support to enable it, and it is not available on Team plans. The CSV format has no comments. The Incremental Export API returns full comments but is limited to 10 requests per minute.',
  },
  {
    q: 'Does CollieDesk import from Zendesk?',
    a: 'Not yet. The first importer is Freshdesk (coming v0.2). Zendesk and Help Scout importers are on the roadmap for later.',
  },
];

const body = `## The short answer

- **Choose Zendesk** if you need the enterprise default: a huge app marketplace, many channels, and AI agents that are already in production.
- **Choose CollieDesk** if you are a small team where per-agent seats and per-resolution AI fees make up most of the bill, and you want an open-source helpdesk you can self-host or rent for a flat price. CollieDesk is pre-alpha; read the status table first.

## Price, side by side

Zendesk prices below are from the Zendesk pricing page in euros (our request for zendesk.com/pricing was served the euro page), per agent per month, read on ${PRICES_READ}.

| | Zendesk | CollieDesk |
|---|---|---|
| Plans (annual / monthly billing) | Support Team €19 / €25, Suite Team €55 / €69, Suite Professional €115 / €149; Enterprise on quote | ${c.price} |
| AI drafting | Copilot €50 per agent per month, billed annually | ${c.ai} |
| AI agent | Per automated resolution: €1.50 committed, €2.00 pay-as-you-go; some included per agent | Auto-answer with a human in the loop, same AI pricing, no per-resolution fee (coming v0.3) |
| Free option | No free plan; 14-day trial | ${c.free} |
| Source code | Proprietary | ${c.source} |

Third-party sites list US prices of $19, $55 and $115 for the same three plans; we could not confirm them on Zendesk's own site.

### What five agents pay

| Team | Zendesk Suite Professional + Copilot (annual) | CollieDesk Cloud |
|---|---|---|
| 5 agents | 5 × (€115 + €50) = **€825 per month**, plus AI agent resolutions beyond the included amount | $10 per month + AI usage |

## Leaving: what Zendesk gives you

- The native export (JSON, CSV, XML) is **off by default**. The account owner asks Zendesk Support to turn it on. It is **not available on Team plans**.
- The **CSV export has no comments**, so it cannot carry a conversation.
- The **Incremental Export API** returns tickets with full comments (\`include=comment_events\`), 1,000 per page, at **10 requests per minute**.

${c.export} A Zendesk importer is on the [roadmap](/roadmap) for later.

## What CollieDesk does today

${statusTable}

## Sources

Read on ${PRICES_READ}. Prices change; check the vendor page before you decide.

- Zendesk pricing (euro page): [zendesk.de/pricing](https://www.zendesk.de/pricing/)
- Native data export: [support.zendesk.com article 203662346](https://support.zendesk.com/hc/en-us/articles/203662346-Exporting-data-to-a-JSON-CSV-or-XML-file)
- Incremental Export API: [developer.zendesk.com](https://developer.zendesk.com/api-reference/ticketing/ticket-management/incremental_exports/)
- API rate limits: [developer.zendesk.com rate limits](https://developer.zendesk.com/api-reference/introduction/rate-limits/)`;

const page = {
  path: '/vs/zendesk',
  section: 'Compare',
  title: 'CollieDesk vs Zendesk: a flat-priced, open-source Zendesk alternative',
  description:
    'CollieDesk vs Zendesk for small support teams: per-agent seats and per-resolution AI fees vs one flat workspace price. Sourced prices, export limits and honest status.',
  summary: 'Zendesk per-agent seats, €50 Copilot and per-resolution AI vs CollieDesk flat $10 per workspace; export off by default',
  eyebrow: 'Compare · Zendesk alternative',
  h1: 'CollieDesk vs Zendesk',
  lede: 'Zendesk is the enterprise default, and it earns that. For a team of five, the seat count and the AI meter decide the bill. Here are the numbers, with sources.',
  cta: true,
  body,
  faq,
};
page.jsonLd = [techArticle(page), faqPage(faq)];
export default page;
