import { FOUNDING_HREF, PRICES_READ } from '../site.mjs';
import { faqPage, techArticle } from '../schema.mjs';

const faq = [
  {
    q: 'How do I migrate from Freshdesk without losing tickets?',
    a: 'Export through the Freshdesk API, not the CSV export: the CSV has ticket fields only. Walk the ticket list in date windows with `updated_since` to stay under the 30,000-ticket cap per filter, fetch archived tickets separately, fetch every conversation and attachment, then compare counts and checksums before you close the account. The CollieDesk importer (coming v0.2) does these steps and writes a reconcile report.',
  },
  {
    q: 'Does the Freshdesk CSV export include the conversation?',
    a: 'No. The ticket export in the Freshdesk UI exports the ticket fields you choose, as CSV or Excel. It does not include replies or notes, and archived tickets are excluded.',
  },
  {
    q: 'Is the CollieDesk Freshdesk importer available now?',
    a: `Not yet; it is coming in v0.2. Founding customers get their migration done for them, free. See [the founding offer](${FOUNDING_HREF}).`,
  },
  {
    q: 'When should I close my Freshdesk account?',
    a: 'Only after the reconcile report shows every ticket, reply, note and attachment arrived. A third-party guide reports that inline images hosted on Freshdesk stop loading after the account closes, so the importer must copy them first.',
  },
];

const body = `> **Status:** the CollieDesk Freshdesk importer is **coming in v0.2**. This page explains the full process so you can plan, or do it yourself with the Freshdesk API today. Founding customers get the migration done for them, free.

## 1. Know what you are moving

Write down, from your Freshdesk admin:

1. The number of tickets, including closed ones.
2. The number of agents, groups, tags and custom ticket fields.
3. Roughly how much attachment storage you use.
4. Your renewal date. Plan to finish before it, with a margin.

## 2. Do not rely on the CSV export

Freshdesk's ticket export in the UI gives you **ticket fields only**, as CSV or Excel. It is useful for a spreadsheet. It is not a migration.

| Data | Freshdesk UI export (CSV/Excel) | Freshdesk API | CollieDesk importer (**Coming v0.2**) |
|---|---|---|---|
| Ticket fields (subject, status, priority, dates) | Yes | Yes | Yes |
| Full conversation: replies and private notes | No | Yes, extra calls per ticket | Yes |
| Attachments | Not included | Download URLs | Downloaded and checksummed (SHA-256) |
| Archived tickets (closed, inactive 120 days) | Excluded | Separate endpoint | Yes |
| Tags and contacts | Tags as a field | Yes | Yes |
| Proof that nothing was lost | No | No | Reconcile report |
| Time entries | No | Yes | Not in v0.2 (time tracking arrives in v0.4) |

## 3. Know the API traps

From Freshdesk's API documentation, read on ${PRICES_READ}:

- **30-day default.** Listing tickets returns only the last 30 days unless you pass \`updated_since\`.
- **30,000-ticket cap.** Up to 100 tickets per page and 300 pages per filter. Larger accounts must walk the list in date windows.
- **Archived tickets.** Closed tickets inactive for 120 days are archived and fetched one by one from a separate endpoint.
- **Conversations.** \`include=conversations\` embeds at most 10; fetch the rest from \`/api/v2/tickets/{id}/conversations\`.
- **Rate limits.** 100, 400 or 700 calls per minute on Growth, Pro and Enterprise, shared across your account.

## 4. Run the import

The importer reads your Freshdesk account through its API with an API key from your Freshdesk profile. It moves tickets, replies, private notes, attachments, tags and contacts into one CollieDesk workspace. The exact command and flags will be documented here when v0.2 ships.

## 5. Check the reconcile report

Every import ends with a reconcile report: counts for each kind of record in Freshdesk and in CollieDesk, and a checksum for every attachment. **A partial import is a failed import.** If one number does not match, the import has failed and you keep using Freshdesk.

## 6. Switch over

1. Point your support address at CollieDesk (the email channel arrives in v0.2).
2. Plan a final pass for tickets that changed after the first import (documented when the importer ships).
3. Keep Freshdesk read-only for a short overlap.
4. Close Freshdesk only after the final reconcile report matches.

## Sources

- Ticket export: [support.freshdesk.com article 225158](https://support.freshdesk.com/support/solutions/articles/225158)
- API: [developers.freshdesk.com/api](https://developers.freshdesk.com/api/)
- Rate limits: [support.freshdesk.com article 225439](https://support.freshdesk.com/support/solutions/articles/225439-what-are-the-rate-limits-for-the-api-calls-to-freshdesk-)
- Inline images after account closure (third-party report): [meticulosity.com](https://www.meticulosity.com/freshdesk-to-hubspot-migration)`;

const page = {
  name: 'Migrate from Freshdesk',
  path: '/migrate/freshdesk',
  section: 'Migrate',
  title: 'Migrate from Freshdesk without losing tickets: step-by-step guide',
  description:
    'How to leave Freshdesk with every ticket, reply, note and attachment: why the CSV export is not enough, the API limits to plan around, and the reconcile check before you close the account.',
  summary: 'Step-by-step Freshdesk exit: CSV vs API vs CollieDesk importer (coming v0.2), API traps, reconcile report',
  eyebrow: 'Migrate · Freshdesk',
  h1: 'Leave Freshdesk with all your data',
  lede: 'An honest, step-by-step guide: what the Freshdesk export keeps, what it loses, and how to prove nothing was left behind.',
  cta: true,
  body,
  faq,
};
page.jsonLd = [techArticle(page), faqPage(faq)];
export default page;
