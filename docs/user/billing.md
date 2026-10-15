# Plan and billing

Billing exists only on the hosted Colliedesk Cloud. A self-hosted instance has no billing page, no
plan and no limits; nothing here applies to it.

Only workspace admins see **Billing** (top bar). Agents do not.

## Plans

| Plan | Price | Agents |
|---|---|---|
| Trial | $0, 14 days from the day the workspace is created (30 days after a Freshdesk import) | up to 10 |
| Team | $49 per month or $490 per year, per workspace | up to 10 (contact us for more) |
| Founding | $100 per year, for life | up to 10 |

There is no permanent free plan. A new workspace starts a trial with the full Team features. When
the trial ends and nothing is paid, the workspace is locked (see "What happens to a locked
workspace" below). The plan belongs to the workspace, not to a person.

## Attachment storage

| Plan | Attachment storage |
|---|---|
| Free trial | 2 GiB. Files brought in by an import do not count. |
| Team | no limit |
| Founding | 20 GiB. Files brought in by an import count. |

**Billing** shows the attachments you have used, and warns at 80% of the limit.

On Founding, at the limit nothing is deleted. You can still read everything, reply without adding
files, and export all your data. **Email from your customers is never refused.** It is stored in
full, attachments included, even when that takes the workspace past 20 GiB; those bytes still count,
so **Billing** shows the usage over the limit. What the limit stops is files you add yourself: an
upload, an attachment on a reply, and a Freshdesk import that would pass it (the import does not
start; see [Import from Freshdesk](import-freshdesk.md)). Each refusal names the limit. To lift the
limit, switch to the Team plan under **Manage billing**, or export your data. The trial is
different: its 2 GiB limit also applies to incoming email, and files brought in by an import do not
count. A file counts each time it is attached, so the same file on two messages counts twice.

## Limits during the trial

- **Agents.** An agent and a pending invite each take one of the 10 seats. When all seats are taken,
  creating an invite for another person fails. Sending a second invite to the same address, or
  inviting someone who already is an agent, does not use another seat. Revoke a pending invite, or
  subscribe, and try again.
- **Replies.** Email goes only to customers who wrote to you first, until you verify your sending
  domain in Settings or subscribe. At most 200 replies leave in any 24 hours.
- **Attachments.** Files received by email count toward 2 GiB of storage. Files brought in by a
  Freshdesk import do not count. A message that would pass the limit is refused, not dropped
  silently: the sender's mail server reports it.
- A paid plan removes the reply limits. Team removes the storage limit; Founding raises it to 20 GiB
  (see "Attachment storage" above).

## Upgrade

1. Open **Billing** and select a plan under **Upgrade**.
2. Pay on the page that opens. It is hosted by our payment provider, Polar; Colliedesk never sees
   your card.
3. You return to **Billing**. The plan updates within a few moments, once the provider confirms
   the payment.

A workspace that already pays cannot start a second checkout. Change or cancel in the portal.
If two checkouts ever run at once, the page warns that the workspace pays twice; cancel the extra
subscription in the portal. The paid plan stays until the last one ends.

## Change the card, get invoices or cancel

Select **Manage billing**. This opens the provider's portal for your workspace. You can update the
card, download invoices and cancel there. The button stays available after a subscription ends.

## What the status means

- **Renews**: the plan continues on the date shown.
- **Cancels**: you cancelled. You keep the plan until the date shown, then the workspace is locked.
- **Last payment failed**: the plan stays active while the provider retries the card. Update the
  card in the portal.
- A subscription that ended, was unpaid or never completed locks the workspace: you can sign in,
  choose a plan and export your data, and nothing else.

## What happens to a locked workspace

1. The workspace stays locked, with all its data, while you decide. You can pick a plan in
   **Settings > Billing** at any time, and everything is there again.
2. About 11 months after the lock, 30 days before deletion, every admin gets an email with the exact
   deletion date. It is sent from Colliedesk, not from your workspace's address.
3. 12 months after the lock, the workspace is deleted for good: conversations, contacts and
   attachments. Choosing a plan before that date cancels the deletion, and a later lock starts a new
   12 months.
4. To keep a copy, export all your data before the deletion date (see "Export all your data").
   Export works while the workspace is locked.

Deleted data can stay in Colliedesk's encrypted nightly backups until they expire; it is not restored on request after the deletion date.

## Limits

- Billing is per workspace. Moving a subscription to another workspace is not supported yet.
- Prices are in US dollars.
- The page shows the limits and your usage: agents (with pending invites), replies in the last 24
  hours (trial) and attachment storage.
