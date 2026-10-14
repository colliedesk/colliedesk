# Email channel

Customers write to your support address. Each new email thread becomes a conversation, and the
sender becomes a contact. Customer replies join the same conversation. Your replies go back to
the customer by email. Private notes are never emailed.

CollieDesk does not run a mail server. A forwarder you already use (Cloudflare Email Routing,
Postmark, Amazon SES, or your own MTA) posts each raw message to CollieDesk. Replies leave through
an SMTP relay.

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
update your forwarder at the same time. **Disconnect email** stops inbound mail and outgoing
replies. Conversations and messages stay.

## 3. Verify the domain (when the policy is `verified`)

1. In **Settings → Email channel**, copy the **TXT record name** (for example
   `_colliedesk.example.com`) and the **TXT record value**.
2. Add that TXT record at your DNS provider.
3. Select **Verify domain**. DNS changes can take a few minutes; try again if it fails.

Until the domain is verified, replies are sent as **"Your workspace via CollieDesk"** from
`reply+…@<MAIL_FROM_DOMAIN>`, with `Reply-To` set to your support address. Customer replies still
reach you and thread. Settings shows **Replies are sent from …** with the current address.

Changing the support address resets verification and creates a new TXT value.

## 4. Forward mail to CollieDesk

Any forwarder works if it can send one HTTP request per message:

```
POST <Inbound URL>
Content-Type: message/rfc822
X-CollieDesk-Inbound-Secret: <Inbound secret>

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

Sending the same message twice is safe: CollieDesk stores a message once per `Message-ID`.

**Sender verification.** CollieDesk shows a sender verification result only from the
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
        "X-CollieDesk-Inbound-Secret": env.INBOUND_SECRET,
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
address, and bounces always go to it. The server operator routes that domain once for all
workspaces:

1. Point the MX records of `MAIL_FROM_DOMAIN` at a forwarder, for example Cloudflare Email Routing
   with a catch-all rule to a Worker.
2. Set `MAIL_FROM_INBOUND_SECRET` on the server to a random value of at least 32 characters.
3. Use the same Worker code as in step 4, with `INBOUND_SECRET` set to `MAIL_FROM_INBOUND_SECRET`.
   Keep the `X-Original-To` line: it carries the `reply+<token>` address.

CollieDesk finds the conversation from the token and files the message in its workspace, like any
reply. A bounce marks the reply **Delivery failed**. Mail for an unknown token is answered `200`
with status `dropped` and reason `unknown_recipient`; the Worker forwards nothing for it.

## What you see in the inbox

- Incoming messages show the sender address and are shown as plain text: no remote images load,
  and only web addresses written in the text become links.
- Attachments are listed under the message; select a file name to download it. **Show original**
  opens the HTML version in a locked frame; inline images in it come from the stored message,
  never from the internet. **Download original email** gives the complete message as received
  (`.eml.gz`), so nothing is lost even if a part could not be shown.
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

A message from someone other than the conversation contact (for example a colleague in Cc) also
joins the thread, with their address shown on the message. Your replies still go only to the
conversation contact.

## Limits

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
- Text longer than 5 MiB is cut and marked. Subjects longer than 250 characters are shortened in
  replies.
- Replies are plain text. Markdown is sent as written.
- One support address per workspace.
