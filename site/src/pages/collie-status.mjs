// CollieDesk's own capability status, from ROADMAP.md. Shared by comparison pages.
export const statusTable = `| Capability | CollieDesk |
|---|---|
| Shared inbox: replies, private notes, status, priority, assignee, tags, search | **Shipped v0.1** |
| Passwordless sign-in, revocable sessions, isolated workspaces | **Shipped v0.1** |
| Email channel (inbound and outbound, threading) | **Coming v0.2** |
| Freshdesk importer with reconcile report | **Coming v0.2** |
| One-command install, CollieDesk Cloud | **Coming v0.2** |
| AI draft replies, triage, auto-answer with a human in the loop | **Coming v0.3** |
| Knowledge base and public help center | **Coming v0.3** |
| Automations, macros, SLA, business hours, time tracking | **Coming v0.4** |
| CSAT and reports | **Coming v0.5** |
| Zendesk and Help Scout importers, live chat widget, MCP server | **Coming later** |`;

export const collieFacts = {
  price: '$10 per workspace per month, or $100 per year. No per-agent fees. Founding customers: $100 per year, locked for life.',
  ai: 'Included (coming v0.3). LLM cost +30% on our key, or bring your own key.',
  free: 'Self-host free, every feature.',
  source: 'Open source, AGPL-3.0. Self-host or use Cloud.',
  export: 'Your data lives in PostgreSQL. Self-hosted, you can take a database dump at any time.',
};
