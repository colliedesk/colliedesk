# Configuring email

This page sets up the [email channel](../user/email-channel.md) on a server you run. On Colliedesk
Cloud none of it is needed: every workspace already has a forwarding address.

Colliedesk does not run a mail server. A forwarder you already use (Cloudflare Email Routing,
Postmark, Amazon SES, or your own MTA) posts each raw message to Colliedesk. Replies leave through
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
| `EMAIL_FROM_POLICY` | `verified` | `verified`: send as the support address only after its domain is verified ([verify the domain](../user/email-channel.md#send-from-your-own-domain)). `any`: always send as the support address. |
| `MAIL_FROM_DOMAIN` | empty | Domain used to send for workspaces whose domain is not verified, for example `mail.example.com`. |
| `MAIL_FROM_INBOUND_SECRET` | empty | Secret (32+ characters) for the forwarder of `MAIL_FROM_DOMAIN` (step 4). |
| `HOSTED_MAIL_DOMAIN` | empty | Hosted cloud only. Turns on `support@<workspace slug>.<this domain>` for every workspace. Needs `MAIL_FROM_INBOUND_SECRET`. Leave empty when you self-host. |
| `RESEND_API_KEY` | empty | Resend API key. When set, domain verification uses the DNS records Resend lists (DKIM and SPF) instead of one TXT record. Needs a key that can add and verify domains. |
| `EMAIL_TRUSTED_AUTHSERV_ID` | empty | Name of the receiving mail server whose SPF/DKIM/DMARC result you trust (step 3). Empty: no result is trusted. |

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

When the policy is `verified`, also [verify the domain](../user/email-channel.md#send-from-your-own-domain).

## 3. Forward mail to Colliedesk

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

## 4. Receive replies and bounces for MAIL_FROM_DOMAIN (shared relay)

Workspaces without a verified domain send from `reply+<token>@<MAIL_FROM_DOMAIN>`. Most customers
reply to the support address (it is in `Reply-To`), but some mail clients reply to the From
address. A bounce goes to it only when your relay keeps that address as the envelope sender; relays
that own the envelope sender (Resend, Amazon SES) send bounces to themselves instead. The server
operator routes the domain once for all workspaces:

1. Point the MX records of `MAIL_FROM_DOMAIN` at a forwarder, for example Cloudflare Email Routing
   with a catch-all rule to a Worker.
2. Set `MAIL_FROM_INBOUND_SECRET` on the server to a random value of at least 32 characters.
3. Use the same Worker code as in step 3, with `INBOUND_SECRET` set to `MAIL_FROM_INBOUND_SECRET`.
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

### Release a domain held by another workspace

With `RESEND_API_KEY`, only one workspace can verify a given domain name. When the owner of a domain
asks for it and another workspace holds it, release it:

```sh
docker compose exec server /app/collie email release-domain <domain>
```

### Lift a pause after spam complaints

When the Resend webhook above is on, Colliedesk [pauses sending](../user/email-channel.md#sending-pauses-after-spam-complaints)
for a workspace with too many spam complaints. The operator:

- gets one email at `BILLING_ALERT_EMAIL` (needs `SMTP_HOST` and `MAIL_FROM_DOMAIN`); without it the
  server only logs a warning;
- lifts the pause after checking the complaints:
  ```sh
  docker compose exec server /app/collie email resume <workspace slug>
  ```
  Replies held by the pause then go out at once. Counting starts again from that moment, so earlier
  complaints do not pause the workspace a second time.
