# Customer portal

Customers can sign in on your help center and see the requests they opened: the status, the public
conversation and a place to reply or to open a new request. They never see an internal note. There
is no password. Colliedesk emails a link that works once and expires after 15 minutes.

It works the same on a self-hosted server and on Colliedesk Cloud. The pages use the help center's
title and logo ([Brand](knowledge-base.md#brand)).

## Turn it on

1. Open **Knowledge base → Settings** (needs `help_center:update`, which admins hold; others see
   whether it is on).
2. Tick **Let customers see their requests**.
3. Select **Save help center**.

The help center itself must be on too. While either is off, the portal answers the same "page not
found" as any other missing page. The address is the help center's address plus `/requests`, shown
on the settings page once the portal is on. Every help center page then has a **My requests** link.

An AI agent does the same in one call, `colliedesk_help_center_update`, sending `portal_enabled`
with the help center's `enabled`, `title` and `locale`. Leave `portal_enabled` out and the setting
stays as it is. `colliedesk_help_center_get` reads it (`help_center:read`).

## What a customer does

1. Open **My requests** and enter the email address they write to you from.
2. Open the email and select **Continue**. That signs them in on that browser for 30 days.
3. The list shows their requests, newest first, with the reference, subject, status and when it last
   changed. **New request** asks for a subject and a description.
4. Open a request to read the public conversation and send a reply. A reply on a resolved or closed
   request opens it again.

Sign out from the list or the request. The link in the email is spent only by **Continue**, not by
opening the page, so a mail scanner that only reads the page does not use it up.

## What they can see

A customer sees requests whose contact is their email address. They do not see another person's
requests, another workspace's requests, requests marked spam, or any internal note. After you merge
two contacts, the address you kept sees every request on that contact. The address you merged away
sees the requests the merge moved, and the ones they open afterwards, not the kept address's other
requests.

A request opened here is a normal conversation in the inbox, channel **Customer portal**. The
customer's address is proven by the sign-in link, so on a Colliedesk Cloud trial you can reply to
it. A [web form](web-form.md) submission is different: anyone can type any address, and a trial
cannot reply to it.

## Email has to be set up

The sign-in link is sent by the server's outgoing email. On Colliedesk Cloud that is ready. On a
self-hosted server it is sent from `noreply@` your mail domain when `MAIL_FROM_DOMAIN` is set, and
otherwise from the workspace's support address, so [email](email-channel.md) or the platform mail
domain has to be configured first. Until then the page says the link cannot be sent, and nothing is
stored.

A person can ask for five links in 15 minutes from one network address, and one address can be sent
five links an hour. A hidden field on the form catches automated posts: they get the same "check
your email" page and nothing is sent or stored.

## Limits

- The portal pages are in English. The **My requests** link follows the help center's language.
- A customer does not attach files here. They can still attach files by email.
- There are no custom fields on the new-request form, and no view of a whole company's requests.
- Signing in does not confirm a request that was never sent. A request exists when they send it
  while signed in, or when they email you.
- The list shows the 100 latest requests, and a request shows its latest 200 public messages.
- Times are in UTC.
