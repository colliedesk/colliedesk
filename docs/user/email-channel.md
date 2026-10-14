# Email channel

Customers write to your support address. Each new email thread becomes a conversation, and the
sender becomes a contact. Customer replies join the same conversation. Your replies go back to
the customer by email. Private notes are never emailed.

CollieDesk does not run a mail server. A forwarder you already use (Cloudflare Email Routing,
Postmark, Amazon SES, or your own MTA) posts each raw message to CollieDesk. Replies leave through
any SMTP relay.

## 1. Configure outgoing mail (server)

Set these variables on the server, then restart it. With the installer, add them to `.env` and
run `docker compose up -d`.

| Variable | Default | Meaning |
|---|---|---|
| `SMTP_HOST` | empty | SMTP relay host. Empty disables outgoing email. |
| `SMTP_PORT` | `587` | Relay port. |
| `SMTP_USERNAME` | empty | Login. Leave empty for a relay that does not need one. |
| `SMTP_PASSWORD` | empty | Password or API key for the login. |
| `SMTP_STARTTLS` | `true` | Require STARTTLS before login. |

The relay must be allowed to send as your support address. Set up SPF and DKIM for that domain
with your relay provider, or receiving servers will mark your replies as spam.

**Settings → Email channel → Outgoing email (SMTP)** shows what the server uses. It never shows
the password.

## 2. Connect the support address (admin)

1. Open **Settings → Email channel**.
2. Enter the **Support address**, for example `support@example.com`, and select
   **Save support address**.
3. Select **Create inbound secret**. Copy the **Inbound secret** now. It is shown only once.
4. Copy the **Inbound URL**.

Rotate the secret with **Rotate inbound secret**. The old secret stops working immediately, so
update your forwarder at the same time. **Disconnect email** stops inbound mail and outgoing
replies. Conversations and messages stay.

## 3. Forward mail to CollieDesk

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
| `400` | Not an email with a sender. | Do not retry. |
| `401` | Wrong, rotated, or missing secret. | Fix the secret. |
| `413` | Larger than 25 MiB. | Do not retry. |
| `429` | This sender sent more than 10 messages in 10 minutes. | Retry after `Retry-After` seconds. |
| `500` | Temporary server failure. Nothing was stored. | Retry. |

Sending the same message twice is safe: CollieDesk stores a message once per `Message-ID`.

### Example: Cloudflare Email Routing

1. In Cloudflare, create a Worker with the code below.
2. Add two secrets to the Worker: `INBOUND_URL` and `INBOUND_SECRET`. Add a plain variable
   `FALLBACK_ADDRESS` with a verified destination address.
3. In **Email Routing → Routing rules**, send your support address to the Worker.
4. To use reply tokens (`support+r-…@example.com`), enable subaddressing in Email Routing or
   add a catch-all rule to the same Worker. Threading by `In-Reply-To` works without it.

```js
export default {
  async email(message, env) {
    const raw = await new Response(message.raw).arrayBuffer();
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

Pipe the message to `curl`. A non-zero exit makes the MTA retry later.

```sh
curl --fail --silent --show-error -X POST "$INBOUND_URL" \
  -H "Content-Type: message/rfc822" \
  -H "X-CollieDesk-Inbound-Secret: $INBOUND_SECRET" \
  --data-binary @-
```

## What you see in the inbox

- Incoming messages show the sender address.
- **Failed sender verification**: the receiving server reported an SPF, DKIM, or DMARC failure.
  Treat the message as possibly forged.
- **Sender not verified**: the forwarder sent no verification result.
- Your replies show **Queued**, **Sent**, or **Delivery failed**. Hover **Delivery failed** to
  read the relay's error. Select **Retry delivery** to send it again.

## How threading works

A customer reply joins the conversation when any of these match, in this order:

1. `In-Reply-To` names a message in the conversation.
2. `References` names a message in the conversation.
3. The reply went to the reply address in our `Reply-To` (`support+r-<token>@example.com`).

Otherwise the message starts a new conversation. A reply to a **Resolved** or **Closed**
conversation reopens it.

## Limits

- Attachments are not imported yet. The timeline lists their file names.
- Mail marked as automatic is dropped: auto-replies (`Auto-Submitted`, out-of-office), bulk and
  mailing-list mail (`Precedence`, `List-Id`), and delivery reports (`MAILER-DAEMON`,
  `postmaster`). Mail from your own support address and copies of your own replies are dropped too.
- Replies are plain text. Markdown is sent as written.
- One support address per workspace.
