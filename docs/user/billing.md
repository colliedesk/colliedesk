# Plan and billing

Billing exists only on the hosted Colliedesk Cloud. A self-hosted instance has no billing page, no
trial, no plan and no limits; nothing here applies to it.

Only workspace admins see **Billing** (left rail). Agents see how many days of the trial are left in
the banner at the top, and nothing else about billing.

## The free trial

Every new cloud workspace starts a 14-day free trial of the full Team plan. No card is needed.

- A banner under the workspace name shows "Trial: N days left". Admins get a **Choose a plan**
  button in it.
- A workspace that imports its history from Freshdesk gets a longer trial: 30 days in total, once.
  The banner then says the trial was extended.
- One trial per email address. Writing the address with capitals, a `+tag` or (for Gmail) dots does
  not make a new one. If you sign up again with the same address you are told to sign in instead.
- Sign-up is limited to 3 new workspaces per network address per day.

### Limits during the trial

- **Agents.** Up to 10. An agent and a pending invite each take one seat. Creating an invite when all
  seats are taken fails with a message that names the limit. Revoke an invite you no longer need and
  try again. Sending a second invite to the same address, or inviting someone who already is an
  agent, does not take another seat.
- **Replies by email.** Up to 200 replies in any 24 hours. The next reply is refused with the time to
  wait, and your text stays in the composer. Retrying a failed delivery counts again.
- **Who you can write to.** During the trial a reply goes only to customers who wrote to you first
  (mail that came in through your Colliedesk address, or imported history). To write to anyone,
  verify your sending domain in **Settings**, or choose a plan.
- **Attachments.** 2 GiB in total. Files that came in with a Freshdesk import do not count. At 80%
  the banner and **Billing** warn you. At the limit, mail with a larger attachment is refused by
  the mail system and bounces to the sender; nothing is dropped silently.

Nothing else is limited. Conversations, contacts, tags, notes and search work as on a paid plan.

## Plans

| Plan | Price | Agents |
|---|---|---|
| Team | $49 per month or $490 per year, per workspace | up to 10 (contact us for more) |
| Founding | $100 per year, for life | up to 10 |

The plan belongs to the workspace, not to a person. Paid plans have no reply cap and no recipient rule. Team has no storage cap; Founding has 20 GiB
(see "Attachment storage" below). Prices are in US dollars.

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

## When the trial or the subscription ends

The workspace is **locked**. Nothing is deleted. Everyone who signs in sees a page that says the
trial (or the subscription) has ended, instead of the inbox.

- **Admins** can choose a plan (or open **Manage billing** when a subscription ended or failed), **Export all data**, or sign out. Choosing a plan and paying opens
  the workspace again within a few moments of the provider confirming the payment.
- **Agents** are told to ask a workspace admin to choose a plan, and can sign out.
- Customers who write to a locked workspace get a bounce. Nothing is stored, so nothing is lost
  that you did not already have.
- In the app only paying, exporting and signing out work until the workspace has a plan.
- A locked workspace is kept for at least 12 months. See "What happens to a locked workspace" below.

## Upgrade

1. Open **Billing** (or select **Choose a plan** in the banner or on the locked page).
2. Select a plan under **Upgrade**.
3. Pay on the page that opens. It is hosted by our payment provider, Polar; Colliedesk never sees
   your card.
4. You return to **Billing**. The plan updates within a few moments, once the provider confirms
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
- A subscription that ended, was unpaid or never completed locks the workspace once the trial is
  over. Nothing is deleted: conversations, contacts and attachments stay until the retention period
  above ends.

## What happens to a locked workspace

1. The workspace stays locked, with all its data, for at least 12 months while you decide. You can
   choose a plan at any time, and everything is there again.
2. Before a locked workspace is deleted, every admin gets an email with the exact deletion date, at
   least 30 days ahead. It is sent from Colliedesk, not from your workspace's address.
3. When the workspace is deleted, it is deleted for good: conversations, contacts and attachments.
   Choosing a plan before that date cancels the deletion, and a later lock starts a new period.
4. To keep a copy, export all your data before the deletion date. See [Export all data](export.md).
   Export works while the workspace is locked.

Deleted data can stay in Colliedesk's encrypted nightly backups until they expire; it is not
restored on request after the deletion date.

## What the Billing page shows

The current plan and when the trial or subscription ends or renews, agents used (with pending
invites), attachment storage against the 2 GiB trial limit, and replies sent in the last 24 hours
during the trial.

## Limits

- Billing is per workspace. Moving a subscription to another workspace is not supported yet.
- Prices are in US dollars.
