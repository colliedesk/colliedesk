# Email channel

Customers write to your support address. Each new email thread becomes a conversation, and the
sender becomes a contact. Customer replies join the same conversation. Your replies go back to
the customer by email. Private notes are never emailed.

Colliedesk does not run a mail server. A forwarder you already use (Cloudflare Email Routing,
Postmark, Amazon SES, or your own MTA) posts each raw message to Colliedesk. Replies leave through
an SMTP relay.

## Colliedesk Cloud: forward to your address

On Colliedesk Cloud every workspace has a ready-made address, `support@<workspace>.colliedesk.com`.
You do not run a forwarder or a Worker. A workspace started from the sign-up page receives mail at
that address at once: its Support address is set to it. To keep your own address for customers,
change where your mailbox forwards:

1. Open **Settings → Email channel** and enter your **Support address**, for example
   `support@example.com`. Select **Save support address**. A workspace created another way accepts
   mail only after this step. The button reads "Saving…" until the save ends, then "Saved" appears
   next to it.
2. Copy the **Forwarding address** (select **Copy forwarding address**).
3. In your mailbox, forward all mail for the support address to the forwarding address:
   - **Gmail:** Settings → See all settings → Forwarding and POP/IMAP → Add a forwarding address.
     Gmail sends a confirmation message to the forwarding address. It appears as a new conversation
     in Colliedesk; open it and select the confirmation link. Then choose **Forward a copy of
     incoming mail** and save.
   - **Outlook:** Settings → Mail → Forwarding, enter the address and select **Start forwarding**.
4. Send a test message to your support address. It opens a conversation.

Forwarded mail keeps its original sender, so the conversation is with your customer, not with your
mailbox. Replies leave as described in step 3 below (domain verification).

An admin can change the part before `.colliedesk.com` under **Workspace address name**. The old
address stops receiving mail at once, so change the forwarding in your mailbox at the same time.
A name is 1 to 63 lowercase letters, digits or hyphens; names such as `app`, `www` and `mail` are
reserved. Do not set the Support address to a `colliedesk.com` address of another workspace.

The sections below describe self-hosting.

## 1. Configure the server

Set these variables on the server, then restart it. With the installer, add them to `.env` and
run `docker compose up -d`.

| Variable | Default | Meaning |
|---|---|---|
| `SMTP_HOST` | empty | SMTP relay host. Empty disables outgoing email. |
| `SMTP_PORT` | `587` | Relay port. Port 465 (implicit TLS) is not supported; use 587. |
| `SMTP_USERNAME` | empty | Login. Leave empty for a relay that does not need one. |
| `SMTP_PASSWORD` | empty | Password or API key for the login. |
| `SMTP_STARTTLS` | `true` | Require STARTTLS before login. |
| `EMAIL_FROM_POLICY` | `verified` | `verified`: send as the support address only after its domain is verified (step 3). `any`: always send as the support address. |
| `MAIL_FROM_DOMAIN` | empty | Domain used to send for workspaces whose domain is not verified, for example `mail.example.com`. |
| `MAIL_FROM_INBOUND_SECRET` | empty | Secret (32+ characters) for the forwarder of `MAIL_FROM_DOMAIN` (step 5). |
| `HOSTED_MAIL_DOMAIN` | empty | Hosted cloud only. Turns on `support@<workspace slug>.<this domain>` for every workspace. Needs `MAIL_FROM_INBOUND_SECRET`. Leave empty when you self-host. |
| `RESEND_API_KEY` | empty | Resend API key. When set, step 3 uses the DNS records Resend lists (DKIM and SPF) instead of one TXT record. Needs a key that can add and verify domains. |
| `EMAIL_TRUSTED_AUTHSERV_ID` | empty | Name of the receiving mail server whose SPF/DKIM/DMARC result you trust (step 4). Empty: no result is trusted. |

**Self-hosted with your own relay:** set `EMAIL_FROM_POLICY=any`. You own the relay, so every
workspace may send as its support address.

**Shared relay (several teams on one server):** keep `verified` and set `MAIL_FROM_DOMAIN` to a
domain the relay is allowed to send for.

The relay must be allowed to send for the From domain. Set up SPF and DKIM for that domain with
your relay provider, or receiving servers will mark replies as spam.

If outgoing email cannot work, replies still appear on the timeline with **Delivery failed** and
the reason, and the composer shows a notice. Fix the cause, then select **Retry delivery**.

## 2. Connect the support address (admin)

1. Open **Settings → Email channel**.
2. Enter the **Support address**, for example `support@example.com`, and select
   **Save support address**.
3. Select **Create inbound secret**. Copy the **Inbound secret** now. It is shown only once.
4. Copy the **Inbound URL**.

Rotate the secret with **Rotate inbound secret**. The old secret stops working immediately, so
update your forwarder at the same time. **Disconnect email** asks first, then stops inbound mail and
outgoing replies. Conversations and messages stay.

## 3. Verify the domain (when the policy is `verified`)

1. In **Settings → Email channel**, copy the **TXT record name** (for example
   `_colliedesk.example.com`) and the **TXT record value**.
2. Add that TXT record at your DNS provider.
3. Select **Verify domain**. DNS changes can take a few minutes; try again if it fails.

**When the server has a Resend key** (`RESEND_API_KEY`, the hosted cloud), verification has two steps:

1. Publish the TXT record shown in Settings. It proves you control the domain. Select **Verify domain**;
   until the TXT record is found, Colliedesk asks for it and does nothing else.
2. Colliedesk then lists a table **DNS records to publish** (DKIM, SPF and an MX record for bounces),
   each with its status (Not started, Pending, Verified, Failed). Add every record at your DNS provider and
   select **Verify domain** again. The domain is verified only when Resend reports it verified; DNS changes
   can take from a few minutes to a day. The button works once every 30 seconds per workspace and is
   disabled while a check runs.

The records stay valid for 7 days. A domain that was never verified and is still not verified after that is released at Resend
and Settings says the records expired; select **Verify domain** to get new ones.

If Resend refuses the domain name, Colliedesk shows Resend's reason; any other Resend problem shows as
"could not be reached" and the operator is told. If Resend cannot be reached, nothing changes. Changing the
support address to another domain replaces the list; the old domain is deleted at Resend in the background.
**Only one workspace can verify a given domain name this way.** If another workspace already has the
domain verified, Colliedesk says so and you keep sending from the platform address; if you own the domain,
contact support and the operator can release it (`collie email release-domain <domain>`). If Resend later
stops verifying your domain (for example the records were removed), Colliedesk stops sending as your
address until you verify it again.

Until the domain is verified, replies are sent as **"Your workspace via Colliedesk"** from
`reply+…@<MAIL_FROM_DOMAIN>`, with `Reply-To` set to your support address. Customer replies still
reach you and thread. Settings shows **Replies are sent from …** with the current address.

Changing the support address resets verification. With a TXT record it also creates a new value; with Resend the same domain keeps its records and a new domain gets its own list.

## 4. Forward mail to Colliedesk

Any forwarder works if it can send one HTTP request per message:

```
POST <Inbound URL>
Content-Type: message/rfc822
X-Colliedesk-Inbound-Secret: <Inbound secret>

<the complete raw message, headers and body, unchanged>
```

Responses:

| Status | Meaning | Forwarder action |
|---|---|---|
| `200` | Stored, threaded, a duplicate, or dropped as automatic mail. The JSON `status` says which. | Done. |
| `400` | Not an email with a sender. | Do not retry. Keep a copy for a person. |
| `401` | Wrong, rotated, or missing secret. | Fix the secret, then retry. |
| `413` | Larger than 25 MiB. | Do not retry. Keep a copy for a person. |
| `429` | Too much mail from one sender, or too many new conversations for the workspace. | Retry after `Retry-After` seconds. |
| `500` | Temporary server failure. Nothing was stored. | Retry. |

Sending the same message twice is safe: Colliedesk stores a message once per `Message-ID`.

**Sender verification.** Colliedesk shows a sender verification result only from the
`Authentication-Results` header written by your receiving server. Set `EMAIL_TRUSTED_AUTHSERV_ID`
to the name at the start of that header (the text before the first `;`). Headers with any other
name are ignored, because a sender can write them. Find the name in the original source of a
message your receiving server delivered.

### Example: Cloudflare Email Routing

1. In Cloudflare, create a Worker with the code below.
2. Add two secrets to the Worker: `INBOUND_URL` and `INBOUND_SECRET`. Add a plain variable
   `FALLBACK_ADDRESS` with a verified destination address.
3. In **Email Routing → Routing rules**, send your support address to the Worker.
4. To use reply tokens (`support+r-…@example.com`), enable subaddressing in Email Routing or
   add a catch-all rule to the same Worker. Threading by `In-Reply-To` works without it.
5. Set `EMAIL_TRUSTED_AUTHSERV_ID` to the name Cloudflare writes in its `Authentication-Results`
   header. At the time of writing that is `mx.cloudflare.net`; confirm it in the source of a
   message delivered through your route.

```js
export default {
  async email(message, env) {
    // Prepend the envelope recipient so reply tokens work even when the message hides it (Bcc).
    const envelope = new TextEncoder().encode(`X-Original-To: ${message.to}\r\n`);
    const raw = await new Blob([envelope, await new Response(message.raw).arrayBuffer()]).arrayBuffer();
    const response = await fetch(env.INBOUND_URL, {
      method: "POST",
      headers: {
        "Content-Type": "message/rfc822",
        "X-Colliedesk-Inbound-Secret": env.INBOUND_SECRET,
      },
      body: raw,
    });
    if (!response.ok) {
      // Keep the message: deliver it to a person instead of dropping it.
      await message.forward(env.FALLBACK_ADDRESS);
    }
  },
};
```

### Example: an MTA pipe (Postfix, Exim)

To refuse unknown recipients during the SMTP session instead of bouncing them later, point a Postfix
`check_policy_service` at `infra/mail/colliedesk-rcpt-policy.py`; it asks
`POST /api/v1/channels/email/inbound/recipient` with the server-wide inbound secret.

Use the wrapper shipped in `infra/mail/colliedesk-inbound-pipe.sh`. MTAs keep a message and retry
only when the pipe command exits with `75` (`EX_TEMPFAIL`); any other failure bounces the
message to the sender. The wrapper maps the responses:

| Result | Exit | MTA does |
|---|---|---|
| `2xx` | `0` | Delivered. |
| `429`, `5xx`, `401`, network error, missing configuration | `75` | Keeps the message and retries. |
| `400`, `413` | `65` | Bounces it to the sender. |

1. Copy the script to `/usr/local/bin/colliedesk-inbound-pipe` and make it executable.
2. Store the secret in a file only the pipe user can read, for example
   `/etc/colliedesk/inbound-secret` with mode `600`.
3. Postfix `master.cf`:
   ```
   colliedesk unix - n n - - pipe
     flags=q user=colliedesk argv=/usr/bin/env
       COLLIEDESK_INBOUND_URL=https://desk.example.com/api/v1/channels/email/inbound
       COLLIEDESK_INBOUND_SECRET_FILE=/etc/colliedesk/inbound-secret
       /usr/local/bin/colliedesk-inbound-pipe
   ```
   Route the support address to the `colliedesk` transport. To keep a copy of rejected mail for a
   person, also deliver the support address to a human mailbox (for example with an alias that
   lists both).
4. Set `EMAIL_TRUSTED_AUTHSERV_ID` to the name your MTA's DMARC or DKIM milter writes in
   `Authentication-Results` (often the server host name).

The secret is passed in a private header file, never on the command line.

## 5. Receive replies and bounces for MAIL_FROM_DOMAIN (shared relay)

Workspaces without a verified domain send from `reply+<token>@<MAIL_FROM_DOMAIN>`. Most customers
reply to the support address (it is in `Reply-To`), but some mail clients reply to the From
address. A bounce goes to it only when your relay keeps that address as the envelope sender; relays
that own the envelope sender (Resend, Amazon SES) send bounces to themselves instead. The server
operator routes the domain once for all workspaces:

1. Point the MX records of `MAIL_FROM_DOMAIN` at a forwarder, for example Cloudflare Email Routing
   with a catch-all rule to a Worker.
2. Set `MAIL_FROM_INBOUND_SECRET` on the server to a random value of at least 32 characters.
3. Use the same Worker code as in step 4, with `INBOUND_SECRET` set to `MAIL_FROM_INBOUND_SECRET`.
   Keep the `X-Original-To` line: it carries the `reply+<token>` address.

Colliedesk finds the conversation from the token and files the message in its workspace, like any
reply. A bounce that reaches this address marks the reply **Delivery failed**. Mail for an unknown
token is answered `200` with status `dropped` and reason `unknown_recipient`; the Worker forwards
nothing for it.

On the hosted cloud, `MAIL_FROM_DOMAIN` may sit below `HOSTED_MAIL_DOMAIN`, for example
`mail.colliedesk.com`. A recipient at `MAIL_FROM_DOMAIN` is always a reply address, never a workspace
slug. The server refuses to start when that name is below the hosted domain and is not a reserved
label such as `mail`, because a workspace could then own it. Give `MAIL_FROM_DOMAIN` its own MX
record: a wildcard MX does not cover a name that has other records below it.

### Bounces through Resend

Resend keeps bounces and complaints and reports them by webhook instead. To show them as
**Delivery failed**:

1. In Resend, add a webhook to `https://<your server>/api/v1/webhooks/resend` for the events
   `email.bounced` and `email.complained`.
2. Set `RESEND_WEBHOOK_SECRET` on the server to the webhook's signing secret (`whsec_...`).
   It needs `MAIL_FROM_DOMAIN`. Without the secret the endpoint does not exist.

A bounce or a spam complaint marks the reply it is about **Delivery failed**, with the reason.
Each reply is sent from its own address, `reply+<token>-<reply id>@MAIL_FROM_DOMAIN`, so the event
names exactly one reply; a customer who answers that address still reaches the conversation. Limits:

- Only replies sent from the platform domain are matched. A reply sent as a verified support address
  is not, because that address does not name a conversation. Replies sent before each reply had its
  own address are matched only when Resend kept the Message-ID.
- A bounce for the contact fails the reply. A bounce for Cc addresses only never fails it: the reply
  stays sent and the missed copies are named under it. An event that names the contact next to
  others, or an address the reply was not sent to, changes nothing and is logged for the operator.
- A temporary bounce or an `email.delivery_delayed` event changes nothing; the relay keeps trying.
- Resend sends an event again until it gets an answer; a repeated event changes nothing.

### Sending pauses after spam complaints

When the Resend webhook above is on, Colliedesk counts spam complaints per workspace. A workspace is
paused when it has **3 or more complaints in the last 7 days and they are more than 0.3% of the
replies it sent in those 7 days** (3 on 1,000 replies is exactly 0.3% and does not pause; 4 does).

- **Settings → Email channel** shows **Email sending is paused** with the counts. Admins cannot lift it.
- Replies written during the pause stay **Queued** with the note "Email sending is paused after spam
  complaints; contact support." Nothing is failed and no attempt is used up. They are sent after
  the pause ends.
- The operator gets one email at `BILLING_ALERT_EMAIL` (needs `SMTP_HOST` and `MAIL_FROM_DOMAIN`);
  without it the server only logs a warning.
- The operator lifts the pause after checking the complaints:
  `collie email resume <workspace slug>`. Replies held by the pause then go out at once. Counting starts
  again from that moment, so earlier complaints do not pause the workspace a second time.
- Complaints count for replies sent from the platform address and for replies sent as your own verified
  support address.

## What you see in the inbox

- Incoming messages show the sender address. A message with an HTML version is shown formatted,
  inline in the conversation, as tall as the mail (no scrolling inside the message), with tables, bold
  text and images. Mail wider than the column is scaled down to fit. **Show plain text** switches to the
  plain version; **Show formatted email** switches back. A message with no HTML is shown as plain text,
  where only web addresses written in the text become links.
- The formatted view is locked: no scripts run, forms do nothing, and the mail cannot send you away
  from Colliedesk. Links open in a new tab. Quoted earlier mail under a reply is folded behind a
  **•••** button; select it to read the quote.
- Remote images (logos, charts, banners) are shown, but your browser never contacts the sender:
  Colliedesk fetches each image on its server, keeps a copy for up to 30 days, and serves it to you. The
  sender sees the server's address, not yours. The first time anyone opens the mail the server does fetch
  the images, so a tracking pixel in a mail still tells its sender that the mail was opened by someone on
  your team, as it does in Freshdesk. Only PNG, JPEG, GIF and WebP images up to 5 MiB are shown, at most 50
  distinct images per message; an SVG, a private or internal address, or a port other than 80 or 443 is not.
  The copies are a cache: they do not count against your storage, are not part of the export, and the
  oldest are removed when the cache is full.
- Attachments are listed under the message, including the files you sent with a reply; select a
  file name to download it. An image that Colliedesk embedded in the formatted message is not
  listed again; it returns to the list in the plain-text view. **Download original email** gives the
  complete message as received (`.eml.gz`), so nothing is lost even if a part could not be shown.
- **Failed sender verification**: the trusted receiving server reported an SPF, DKIM, or DMARC
  failure. Treat the message as possibly forged.
- **Sender not verified**: no trusted verification result was present.
- Your replies show **Queued**, **Sent**, or **Delivery failed**. Hover **Delivery failed** to
  read the reason, for example the relay's error or a bounce. Select **Retry delivery** to send it
  again.

## How threading works

A customer reply joins the conversation when any of these match, in this order:

1. `In-Reply-To` names a message in the conversation.
2. `References` names a message in the conversation.
3. The reply went to the reply address in our `Reply-To` (`support+r-<token>@example.com`).

Otherwise the message starts a new conversation. A reply to a **Resolved** or **Closed**
conversation reopens it.

A message from someone other than the conversation contact (for example a colleague in Cc, who
answers with reply-all) also joins the thread, with their address shown on the message. That person
does not become the contact of the conversation.

## Copy other people on a reply (Cc)

When a customer writes with colleagues in Cc, copy them on your reply so the thread does not split
on their side.

- Under the message box, select **Cc** to add addresses. Type or paste an address and press Enter,
  comma, semicolon or space. Select the **x** on an address to remove it, or press Backspace in the
  empty box to remove the last one. While the box holds text that is not a valid address, **Send**
  stays off: fix the address or clear it.
- The box opens with the people the customer copied on their latest message (and a copied colleague
  who answered with reply-all). Remove any of them before you send. Mail received before Colliedesk
  kept Cc lines offers nothing, and so does mail whose sender did not pass verification: anyone can
  write a Cc line, so you add those people yourself if you mean to.
- The reply goes to the conversation contact in **To** and to every Cc address. There is no Bcc.
  The timeline shows **Cc: …** under the reply, and under customer mail that copied someone.
- At most 10 addresses per reply. Plain addresses only (`name@example.com`, no display names). You
  cannot copy your own support address (also in its `support+…@` form), the forwarding address or
  the platform sending domain.
- Private notes cannot be copied: they never leave your team.
- **Retry delivery** sends to the same Cc list again.
- A copied address that does not exist never stops the reply to the customer. The reply shows
  **Sent**, with a line naming each copied address it did not reach, whether the mail system
  refused it at once or bounced it later.
- During the **trial**, as long as the sending domain is not verified, you can copy only addresses
  the customer already wrote from or copied. Any other address is refused, the message names each
  refused address, your text stays in the composer, and nothing is stored. Verify the domain or choose a plan to lift
  it. The daily trial limit counts people, not replies: a reply to the customer with two people in Cc
  counts 3 of the 200.
- A copied person who answers with reply-all joins the same conversation.

Through the API, send `cc` (a list of addresses) with `kind: "outgoing"`; in a multipart request
send one `cc` field per address before the files. See `createMessage` in `contracts/openapi.yaml`.

## Limits

- A reply carries at most 10 attachments and 10 MiB of files in total (the raw size, before email
  encoding). The limit is also in `GET /api/v1/config` as `limits.outgoing_attachments_bytes` and
  `limits.outgoing_attachments_files`. More answers `413` with the reason, and nothing is stored
  or sent. Make sure your mail relay accepts a message of about 14 MiB; email encoding adds a third.
  Some recipient servers cap incoming mail at about 10 MB, so a large reply can bounce; it then shows
  **Delivery failed** with **Retry delivery**.
- If a stored file is gone when the reply is sent, the reply shows **Delivery failed** and names the
  file. Colliedesk never sends the reply without it.
- A message may be up to 25 MiB with at most 100 attachments. Larger mail gets `413` and nothing
  is stored; the forwarder should keep a copy for a person.
- If attachment storage fails, the forwarder gets `500` and nothing is stored; it retries.
- Mail marked as automatic is dropped: auto-replies (`Auto-Submitted`, out-of-office), bulk mail
  (`Precedence: bulk` or `junk`, or list headers together with a bulk-sender marker such as
  one-click unsubscribe), and delivery reports (`MAILER-DAEMON`, `postmaster`). A delivery report
  about one of your replies marks that reply **Delivery failed**. Mail sent through a Google Group
  or list alias in front of your support address is not dropped.
- Mail from your own support address and copies of your own replies are dropped.
- One sender can send 10 messages per 10 minutes, and a workspace can open 200 new conversations
  per 10 minutes. Above that the forwarder gets `429` and should retry.
- Each workspace sends at most 300 emailed replies per hour; more wait and send later.
- Mail Colliedesk writes by itself (operator alerts, notices, reports) carries
  `Auto-Submitted: auto-generated` (RFC 3834). Colliedesk drops inbound mail with that header, so an
  automatic answer to it never opens a conversation. Replies you write carry no such header.
- Text longer than 5 MiB is cut and marked. Subjects longer than 250 characters are shortened in
  replies.
- Replies are plain text. Markdown is sent as written.
- One support address per workspace.
