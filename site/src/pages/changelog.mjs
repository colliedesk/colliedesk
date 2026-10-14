import { techArticle } from '../schema.mjs';

// Newest first. Each entry also feeds /changelog.xml.
export const ENTRIES = [
  {
    version: 'v0.1',
    date: '2026-10-14',
    title: 'v0.1: first pre-alpha',
    md: `First public pre-alpha. The interface and database schema may change before the first stable release.

- **Passwordless sign-in.** One-time pairing and invite links, role ceilings on invites, revocable sessions. See [Sign in without passwords](/docs/sign-in).
- **Shared inbox.** Replies, private notes, status, priority, assignee, tags, search and pagination. See [Work the inbox](/docs/inbox).
- **Workspaces.** Every workspace is isolated from every other. Per-workspace timezone with a per-agent override.
- **For developers.** One OpenAPI contract for the API, an end-to-end test and CI.

Not in this release: the email channel and the Freshdesk importer (coming in v0.2). See the [roadmap](/roadmap).`,
  },
];

const body = ENTRIES.map((e) => `## ${e.title}\n\n*Released ${e.date}*\n\n${e.md}`).join('\n\n');

const page = {
  path: '/changelog',
  section: 'Product',
  title: 'CollieDesk changelog: what shipped, release by release',
  description:
    'Every CollieDesk release with its date and what changed. v0.1 (14 October 2026): passwordless sign-in, shared inbox, isolated workspaces. RSS feed available.',
  summary: 'Dated release notes; v0.1 pre-alpha on 2026-10-14; RSS at /changelog.xml',
  eyebrow: 'Changelog',
  h1: 'Changelog',
  lede: 'What shipped, when. Newest first. Subscribe with the [RSS feed](/changelog.xml).',
  body,
};
page.jsonLd = [techArticle(page)];
export default page;
