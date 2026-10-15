# Plan and billing

Billing exists only on the hosted CollieDesk Cloud. A self-hosted instance has no billing page, no
plan and no limits; nothing here applies to it.

Only workspace admins see **Billing** (top bar). Agents do not.

## Plans

| Plan | Price | Agents | New conversations |
|---|---|---|---|
| Free | $0 | up to 3 | up to 100 per UTC calendar month |
| Team | $49 per month or $490 per year, per workspace | up to 10 | no limit |
| Founding | $100 per year, for life | up to 10 | no limit |

The plan belongs to the workspace, not to a person. The page shows how much of each limit you have
used. Conversations are counted by the UTC month in which they were received.

## What happens at the limits (Free plan)

- **Agents.** An agent and a pending invite each take one of the 3 seats. When all seats are taken,
  creating another invite fails with "The Free plan allows 3 agents". Revoke a pending invite, or
  upgrade, and try again. If the workspace is over the limit after a downgrade, nobody is removed;
  an unused invite cannot be accepted until a seat is free or the workspace upgrades. The invite
  stays valid until it expires, so the person can use it again after the upgrade.
- **New conversations.** The 101st new conversation of a UTC month, and every one after it, is
  **held**. A held conversation is stored in full, with its messages and attachments. Nothing is
  dropped and no sender gets a bounce. It is hidden from the inbox list and from search. A banner on
  the inbox and a count on **Billing** show how many are held.
- Held conversations appear on their own as soon as the workspace upgrades, or when the month in
  which they arrived ends. A new month starts with a new quota of 100.
- **Replies always work.** A customer replying to an existing conversation, and an agent replying or
  adding a note, are never blocked. Only new conversations count. A reply to a held conversation
  stays with it.
- Conversations you create yourself in CollieDesk (not received by email) count toward the 100 but
  are never held. You see them at once.
- Conversations brought in by a Freshdesk import are history, not new mail. They do not count toward
  the quota and are never held, however many there are.
- Going back from a paid plan to Free hides and deletes nothing. Only conversations that arrive
  over the quota afterwards are held.

## Upgrade

1. Open **Billing** and select a plan under **Upgrade**.
2. Pay on the page that opens. It is hosted by our payment provider, Polar; CollieDesk never sees
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
- **Cancels**: you cancelled. You keep the plan until the date shown, then the workspace is Free.
- **Last payment failed**: the plan stays active while the provider retries the card. Update the
  card in the portal.
- A subscription that ended, was unpaid or never completed leaves the workspace on Free. Nothing
  is deleted: conversations, contacts and attachments stay.

## Limits

- Billing is per workspace. Moving a subscription to another workspace is not supported yet.
- Prices are in US dollars.
- The page shows the limits and your usage: agents (with pending invites), conversations received
  this month, and how many are held.
