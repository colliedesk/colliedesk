# Plan and billing

Billing exists only on the hosted Colliedesk Cloud. A self-hosted instance has no billing page, no
trial, no plan and no limits; nothing here applies to it.

Only workspace admins see **Settings → Plan and billing**. Agents see how many days of the trial are left in
the banner at the top, and nothing else about billing.

If the server's operator turns billing off, nothing is charged or locked meanwhile and nothing can be bought.
A workspace with a subscription still sees **Plan and billing** with a note saying so, and can change or
cancel the subscription under **Manage billing**.

## The free trial

Every new cloud workspace starts a 14-day free trial with every feature, for up to 25 agents. No card is needed.

- A banner under the workspace name shows "Trial: N days left". Admins get a **Choose a plan**
  button in it. In a new workspace the [setup guide](setup-guide.md)'s next step stays the main button; Choose a plan is
  a plain one.
- A workspace that imports its history from Freshdesk gets a longer trial: 30 days in total, once.
  The banner then says the trial was extended.
- One trial per email address. Writing the address with capitals, a `+tag` or (for Gmail) dots does
  not make a new one. If you sign up again with the same address you are told to sign in instead.
- Sign-up is limited to 3 new workspaces per network address per day.
- Coming from the website's Founding link, a visitor who is not signed in goes straight to sign-up,
  which says the plan they came for: under the title, "$99 every 3 months · up to 10 agents · 30-day
  money-back guarantee". They enter their work email and the emailed code, name the workspace, and pay.
  The button reads **Create workspace and continue to payment**, and the payment window opens as soon as
  the workspace exists. The workspace name is the only answer the form needs; the three questions of
  [sign-in](sign-in.md#create-a-workspace-hosted-cloud) wait under "Tell us about your team (optional)".
  The plan rides in the sign-in address, so it survives the identity provider's round trip; opening
  the plain sign-in page later in the same tab (the website's **Log in**) is a normal sign-in again.
- Coming from the website's Team 25 link (`/sign-in?intent=trial&plan=team25`), a visitor signs up for the
  trial as usual: no payment window opens. The browser remembers the plan they came for (for a day, in
  that tab), so **Choose a plan** in the banner, **Plan and billing** and the locked page show the Team 25
  card first, marked **Your pick**. Any other `plan` value is ignored. The sign-up form asks for the
  workspace name only here too.
- Admins get an email three days before the trial ends and another on the day it ends, in the workspace's
  timezone (the day starts at its midnight). It gives the end date and time, says that the workspace locks,
  that nothing is deleted and that export stays available, and links to **Plan and billing**. Each admin
  gets each email once. A workspace that has a plan, or whose trial is over, gets none. If the trial end
  moves (for example after a Freshdesk import extends it), the new end gets its own two emails.

### Limits during the trial

- **Agents.** Up to 25, so a team of 11 to 25 agents can try Colliedesk with everyone. An agent and a pending invite each take one seat. Creating an invite when all
  seats are taken fails with a message that names the limit. Revoke an invite you no longer need and
  try again. Sending a second invite to the same address, or inviting someone who already is an
  agent, does not take another seat.
- **Replies by email.** Up to 200 people in any 24 hours. Each reply counts the customer and
  everyone you copy, so a reply with two people in Cc counts 3. A reply that would pass the limit
  is refused with the time to wait, and your text stays in the composer. A reply that left and then
  bounced still counts, and retrying a failed delivery counts again.
- **Who you can write to.** During the trial a reply goes only to customers who wrote to you first
  (mail that came in through your Colliedesk address, or imported history). To write to anyone,
  verify your sending domain in **Settings**, or choose a plan. When the customer did write but their
  mail does not count, the refusal says why:
  - The mail was posted to your **inbound URL** (on your own forwarder) instead of arriving at your
    Colliedesk **forwarding address**, so nothing proves who sent it (API reason
    `mail_not_via_hosted_address`). Forward your mailbox to the forwarding address shown in
    **Settings → Email**; mail that arrives there counts.
  - The mail failed sender verification (SPF, DKIM or DMARC), so it may not come from that address
    (API reason `sender_authentication_failed`).

  In both cases verifying your sending domain or choosing a plan lets you reply.

  One reply needs no customer: the answer to your own
  [test message](email-channel.md#send-yourself-a-test-message). It goes only to you, so the trial
  allows it, as long as nobody else is copied.
- **Attachments.** 2 GiB in total. Files that came in with a Freshdesk import do not count. At 80%
  the banner and **Plan and billing** warn you. At the limit, mail with a larger attachment is refused by
  the mail system and bounces to the sender; nothing is dropped silently.

Nothing else is limited. Conversations, contacts, tags, notes and search work as on a paid plan.

## Plans

| Plan | Price | Agents |
|---|---|---|
| Solo | $19 per month or $190 per year, per workspace | 1 |
| Small | $39 per month or $390 per year, per workspace | up to 3 |
| Team | $69 per month or $690 per year, per workspace | up to 10 |
| Team 25 | $149 per month or $1,490 per year, per workspace | up to 25 (contact us for more) |
| Founding | $99 every 3 months, for life | up to 10 |

There is no free plan; every workspace starts with the free trial. Unlimited, for teams with more
than 25 agents at $349 per month billed yearly, is coming soon.

Founding includes a free Freshdesk migration, done for you, and a 30-day money-back guarantee
(see the [terms](https://colliedesk.com/terms#refunds)). Founding is limited to 10 subscriptions. While a Founding slot is free, the billing page and the locked page offer two plans:
Founding ($99 every 3 months, $396 a year) and Team 25 ($149 a month or $1,490 a year, for up to 25 agents), with a link to
self-host for free. A workspace that does not fit Founding (its 10-agent or 20 GiB cap) sees the Founding card greyed, with
the reason, and can still choose Team 25. When all 10 are taken Founding is closed for good and the pages offer every plan.

When all seats of a plan are taken, the refusal names the next plan up (Solo, then Small, then
Team, then Team 25). Switch plans under **Manage billing**.

A plan your workspace does not fit cannot be chosen: its card says why. That happens when you have
more agents and pending invites than the plan allows, or more stored attachments, imported files
included, than its storage limit. Deactivate agents or revoke invites, then choose it.

If you move to a smaller plan under **Manage billing** while you have more agents than it allows,
nobody is removed. Agents are paused before admins, the most recently added first: they can only
sign out and see why. The first admin of the workspace is never paused, and a paused admin can still
open **Manage billing**. An agent you reactivate keeps the place of when they were first added. Admins see a notice on every page. Deactivate agents
in **Settings → Team**, or upgrade again, and the paused agents can work again at once.

The plan belongs to the workspace, not to a person. Paid plans have no reply cap and no recipient rule. Every paid plan has an attachment storage cap
(see "Attachment storage" below). Prices are in US dollars.

## Attachment storage

| Plan | Attachment storage |
|---|---|
| Free trial | 2 GiB. Files brought in by an import do not count. |
| Solo | 10 GiB. Files brought in by an import count. |
| Small | 30 GiB. Files brought in by an import count. |
| Team | 100 GiB. Files brought in by an import count. |
| Team 25 | 250 GiB. Files brought in by an import count. |
| Founding | 20 GiB. Files brought in by an import count. |

Knowledge base images count toward the same limit as attachments.

**Plan and billing** shows the attachments you have used, and warns at 80% of the limit.

On a paid plan, at the limit nothing is deleted. You can still read everything, reply without adding
files, and export all your data. **Email from your customers is never refused.** It is stored in
full, attachments included, even when that takes the workspace past the limit; those bytes still count,
so **Plan and billing** shows the usage over the limit. What the limit stops is files you add yourself: an
upload, an attachment on a reply, and a Freshdesk import that would pass it (the import does not
start; see [Import from Freshdesk](import-freshdesk.md)). Each refusal names
the limit. To raise the
limit, switch to a larger plan under **Manage billing** (Solo to Small, Small or Founding to Team), contact us on Team or Team 25, or export your data. The trial is
different: at its 2 GiB limit **all** incoming email is refused, because the size of the whole
message counts, not only its attachments. There is no way to delete an attachment, so the way to
keep receiving email is to upgrade. Files brought in by an import do not count on the trial. A file counts each time it is attached, so the same file on two messages counts twice.

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

1. Open **Settings → Plan and billing** (or select **Choose a plan** in the banner or on the locked page).
2. Under **Choose a plan**, pick **Monthly** or **Yearly, 2 months free**. Each plan is a card with its
   price per month (a yearly plan also shows what is billed once a year; Founding shows its price per
   quarter), its agents and attachment storage. While Founding is open the cards are Founding and Team 25, and
   the Monthly or Yearly choice above them is for Team 25. Select **Choose Solo**, **Choose Small**, **Choose Team**, **Choose Team 25** or **Choose Founding**.
3. Pay in the window that opens on top of Colliedesk. It is run by our payment provider, Polar;
   Colliedesk never sees your card. Pay by card. Apple Pay and Google Pay come later. If you
   close the window, you stay on the page and can start again.
4. You return to **Plan and billing**. The plan updates within a few moments, once the provider confirms
   the payment. Until then the page says it is confirming your plan and the **Choose** buttons stay off,
   so a second click cannot start a second payment. Once the plan changes, the page says "Payment
   confirmed. Welcome to" and the plan's name.

## After you pay for Founding or Team 25

A workspace on a paid Founding or Team 25 plan sees a card on **Plan and billing**: **Next: your Freshdesk
migration**.

- Founding: "Your plan includes a Freshdesk migration done for you." Team 25: "We run the Freshdesk import
  with you."
- **Email us your Freshdesk address** opens an email to support@colliedesk.com with the subject
  "Migration: " and your workspace's name, and a short text that asks for your Freshdesk address and two
  times that suit you.
- **Or import it yourself** opens [Import from Freshdesk](import-freshdesk.md).
- Right after you pay, the card sits at the top of the page with the confirmation. On later visits it
  sits below the current plan. It goes away once the workspace has a finished Freshdesk import (one that was
  not undone).

If the window does not open within 3 seconds (for example a browser extension blocks it, or the
payment provider refuses to show its page inside Colliedesk), Colliedesk says so and opens the same
checkout as a full page in this tab instead.

If the payment provider does not accept your account's email address (for example an address on a
domain that receives no mail), the page shows the provider's reason instead of a generic failure. Sign
in with an address that receives mail, or contact us at support@colliedesk.com.

A workspace that already pays cannot start a second checkout. Change or cancel in the portal.
If two checkouts ever run at once, the page warns that the workspace pays twice; cancel the extra
subscription in the portal. The paid plan stays until the last one ends.

## Ask us to move your Freshdesk data

After you pay for Founding or Team 25, an admin can ask Colliedesk to move the Freshdesk data from
**Plan and billing**. Send your Freshdesk address, like `yourcompany.freshdesk.com`, and optionally the times
that suit you for a call, with a time zone.

- Colliedesk emails your request to support@colliedesk.com, with your admin email as the reply address. We
  reply by email within one working day.
- **Plan and billing** shows when the request was sent. You can send it again, for example to correct the
  address; a workspace can send 5 requests an hour.
- Only an admin can ask. An AI agent can prepare the request, and an admin approves it.
- If this server cannot send email, nothing is sent and Colliedesk says so.

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
