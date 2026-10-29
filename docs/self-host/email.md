# Configuring email

This page sets up the [email channel](../user/email-channel.md) on a server you run. On Colliedesk
Cloud none of it is needed: every workspace already has a forwarding address.

Colliedesk does not run a mail server. A forwarder you already use (Cloudflare Email Routing,
Postmark, Amazon SES, or your own MTA) posts each raw message to Colliedesk. Replies leave through
an SMTP relay.

## 1. Configure the server

Save these settings with `collie config` (or in the operator console); a running server applies them
within seconds. Each is named by its key or by the variable it replaced, shown in brackets:

```sh
docker compose exec server /app/collie config set smtp.host=smtp.example.net smtp.username=apikey email.from_policy=any
docker compose exec -T server /app/collie config set-secret smtp.password < relay-password.txt
```

| Setting | Default | Meaning |
|---|---|---|
| `smtp.host` (`SMTP_HOST`) | empty | SMTP relay host. Empty disables outgoing email. |
| `smtp.port` (`SMTP_PORT`) | `587` | Relay port. Port 465 (implicit TLS) is not supported; use 587. |
| `smtp.username` (`SMTP_USERNAME`) | empty | Login. Leave empty for a relay that does not need one. |
| `smtp.password` (`SMTP_PASSWORD`) | empty | Password or API key for the login (a secret: `set-secret`). |
| `smtp.starttls` (`SMTP_STARTTLS`) | `true` | Require STARTTLS before login. |
| `email.from_policy` (`EMAIL_FROM_POLICY`) | `verified` | `verified`: send as the support address only after its domain is verified ([verify the domain](../user/email-channel.md#send-from-your-own-domain)). `any`: always send as the support address. |
| `mail_from.domain` (`MAIL_FROM_DOMAIN`) | empty | Domain used to send for workspaces whose domain is not verified, for example `mail.example.com`. |
| `mail_from.inbound_secret` (`MAIL_FROM_INBOUND_SECRET`) | empty | Secret (32+ characters) for the forwarder of `MAIL_FROM_DOMAIN` (step 4). |
| `hosted_mail.domain` (`HOSTED_MAIL_DOMAIN`) | empty | Hosted cloud only. Turns on `support@<workspace slug>.<this domain>` for every workspace. Needs `MAIL_FROM_INBOUND_SECRET`. Leave empty when you self-host. |
| `resend.api_key` (`RESEND_API_KEY`) | empty | Resend API key. When set, domain verification uses the DNS records Resend lists (DKIM and SPF) instead of one TXT record. Needs a key that can add and verify domains. |
| `email.trusted_authserv_id` (`EMAIL_TRUSTED_AUTHSERV_ID`) | empty | Name of the receiving mail server whose SPF/DKIM/DMARC result you trust (step 3). Empty: no result is trusted. |

**Self-hosted with your own relay:** set `email.from_policy=any`. You own the relay, so every
workspace may send as its support address.

**Shared relay (several teams on one server):** keep `verified` and set `mail_from.domain` to a
domain the relay is allowed to send for.

The relay must be allowed to send for the From domain. Set up SPF and DKIM for that domain with
your relay provider, or receiving servers will mark replies as spam.

Mail the server writes by itself (import notices, the weekly [results](../user/results.md) email to
admins) leaves from `noreply@<mail_from.domain>` through the same relay, so it needs `smtp.host` and
`mail_from.domain`. Without them the results page says the weekly email cannot be sent.

If outgoing email cannot work, replies still appear on the timeline with **Delivery failed** and
the reason, and the composer shows a notice. Fix the cause, then select **Retry delivery**.

## 2. Connect the support address (admin)

1. Open **Settings → Channels** and select **Connect your support email** (or **Email**).
2. Enter the **Support address**, for example `support@example.com`, and select
   **Save support address**.
3. Select **Create inbound secret**. Copy the **Inbound secret** now. It is shown only once.
4. Copy the **Inbound URL**. To check it before your forwarder is ready, run the **Test request** the
   page shows under the secret (**Copy test request**): it posts one message, which opens a conversation.

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
`Authentication-Results` header written by your receiving server. Set `email.trusted_authserv_id`
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
5. Set `email.trusted_authserv_id` to the name Cloudflare writes in its `Authentication-Results`
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
4. Set `email.trusted_authserv_id` to the name your MTA's DMARC or DKIM milter writes in
   `Authentication-Results` (often the server host name).

The secret is passed in a private header file, never on the command line.

## 4. Receive replies and bounces for MAIL_FROM_DOMAIN (shared relay)

Workspaces without a verified domain send from `reply+<token>@<mail_from.domain>`. Most customers
reply to the support address (it is in `Reply-To`), but some mail clients reply to the From
address. A bounce goes to it only when your relay keeps that address as the envelope sender; relays
that own the envelope sender (Resend, Amazon SES) send bounces to themselves instead. The server
operator routes the domain once for all workspaces:

1. Point the MX records of `mail_from.domain` at a forwarder, for example Cloudflare Email Routing
   with a catch-all rule to a Worker.
2. Set `mail_from.inbound_secret` on the server to a random value of at least 32 characters.
3. Use the same Worker code as in step 3, with `INBOUND_SECRET` set to `mail_from.inbound_secret`.
   Keep the `X-Original-To` line: it carries the `reply+<token>` address.

Colliedesk finds the conversation from the token and files the message in its workspace, like any
reply. A bounce that reaches this address marks the reply **Delivery failed**. Mail for an unknown
token is answered `200` with status `dropped` and reason `unknown_recipient`; the Worker forwards
nothing for it.

On the hosted cloud, `mail_from.domain` may sit below `hosted_mail.domain`, for example
`mail.colliedesk.com`. A recipient at `mail_from.domain` is always a reply address, never a workspace
slug. The server refuses to start when that name is below the hosted domain and is not a reserved
label such as `mail`, because a workspace could then own it. Give `mail_from.domain` its own MX
record: a wildcard MX does not cover a name that has other records below it.

### Bounces through Resend

Resend keeps bounces and complaints and reports them by webhook instead. To show them as
**Delivery failed**:

1. In Resend, add a webhook to `https://<your server>/api/v1/webhooks/resend` for the events
   `email.bounced` and `email.complained`.
2. Set `resend.webhook_secret` on the server to the webhook's signing secret (`whsec_...`).
   It needs `mail_from.domain`. Without the secret the endpoint does not exist.

A bounce or a spam complaint marks the reply it is about **Delivery failed**, with the reason.
Each reply is sent from its own address, `reply+<token>-<reply id>@mail_from.domain`, so the event
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

With `resend.api_key`, only one workspace can verify a given domain name. When the owner of a domain
asks for it and another workspace holds it, release it:

```sh
docker compose exec server /app/collie email release-domain <domain>
```

### Set a sender name with a brand in it

When billing is on (Colliedesk Cloud), the API refuses a
[sender name](../user/email-channel.md#sender-name) with a well-known brand in it, such as
"PayPal Support". When the workspace owns that brand, set the name for it:

```sh
docker compose exec server /app/collie email sender-name [--by <name>] <workspace slug> "<name>"
```

The command checks the name like the API does: 1 to 100 characters after trimming, no line breaks or
other control characters. It skips only the brand check. It prints the old and the new name, and
logs who made the change: `--by`, else the operating system user. It changes only that workspace's
email channel. A workspace without an email channel is refused; an admin saves a support address
first. If an admin later saves another name in **Settings**, the brand check applies to that name.

### Lift a pause after spam complaints

When the Resend webhook above is on, Colliedesk [pauses sending](../user/email-channel.md#sending-pauses-after-spam-complaints)
for a workspace with too many spam complaints. The operator:

- gets one email at `billing.alert_email` (needs `smtp.host` and `mail_from.domain`); without it the
  server only logs a warning;
- lifts the pause after checking the complaints:
  ```sh
  docker compose exec server /app/collie email resume <workspace slug>
  ```
  Replies held by the pause then go out at once. Counting starts again from that moment, so earlier
  complaints do not pause the workspace a second time.

## Send replies through Gmail

An admin connects a Gmail or Google Workspace mailbox as described in
[send replies from Gmail](../user/email-channel.md#send-replies-from-gmail). Gmail sending uses your own Google OAuth client. Without these settings **Send from your mailbox** names
what is missing and Colliedesk keeps sending through SMTP.

1. In [Google Cloud console](https://console.cloud.google.com/), create a project, enable the
   **Gmail API**, and set up the consent screen (Google Auth Platform). Add the scopes `openid`,
   `.../auth/userinfo.email` and `.../auth/gmail.send`. `gmail.send` is a sensitive scope: an
   **External** app needs Google's verification before people outside your test users can connect;
   an **Internal** app (Google Workspace) does not.
2. Create a client of type **Web application**. Add the **Redirect URI** that **Send from your mailbox**
   shows, which is your `APP_ORIGIN` followed by `/api/v1/channels/email/gmail/callback`, for example
   `https://desk.example.com/api/v1/channels/email/gmail/callback`.
3. Save these settings (`collie config set`, the secret with `collie config set-secret`):

| Variable | Meaning |
|---|---|
| `google.client_id` | Client ID of the Web application client. |
| `google.client_secret` | Its client secret. |
| `MAILBOX_TOKEN_KEYS` | Key that encrypts the stored Google access: `k1:` followed by 32 random bytes in base64, for example `k1:$(openssl rand -base64 32)`. Keep it secret and keep a copy: without it the server cannot read the connections and every admin must connect Gmail again. |

To change the key, put the new one first and keep the old one after a comma
(`k2:<new>,k1:<old>`): new connections use the first key, old ones keep working, and each is moved to
the new key the next time it sends a reply. Remove the old key only after every workspace that connected
Gmail has sent a reply since, or connected again. If the keys are wrong, replies wait and retry with a note
naming `MAILBOX_TOKEN_KEYS`; the connection is kept, so fixing the keys is enough.

A server with Gmail and no SMTP relay works for workspaces that connected Gmail; for the others,
replies are marked **Delivery failed** until a relay is set.

## Send replies through Microsoft 365

An admin connects a Microsoft 365 (work or school) or Outlook.com (personal) mailbox to an email channel
as described in [send replies from Microsoft 365](../user/email-channel.md#send-replies-from-microsoft-365). Colliedesk asks Microsoft only for permission to
send email and to read the account's address (`Mail.Send`, `User.Read`, `offline_access`, `openid`); it
cannot read the mailbox. Microsoft sending uses your own app registration in Microsoft Entra. Without
these settings Colliedesk names what is missing and keeps sending through SMTP.

1. In the [Microsoft Entra admin center](https://entra.microsoft.com), open **Entra ID** → **App
   registrations** → **New registration**.
   - **Supported account types:** for a server that only your organization uses, choose **Single tenant**
     and set `microsoft.tenant` to your tenant id. A multi-tenant app whose publisher Microsoft has
     not verified is often blocked in other organizations until their admin approves it, so single tenant
     is the simple choice for self-hosting. Choose **Any Entra ID Tenant + Personal Microsoft accounts**
     only to accept Outlook.com and other organizations too (keep `microsoft.tenant` at `common`).
   - **Redirect URI:** platform **Web**, your `APP_ORIGIN` followed by
     `/api/v1/channels/email/microsoft/callback`, for example
     `https://desk.example.com/api/v1/channels/email/microsoft/callback`. Then, under **Authentication**,
     add a second Web redirect URI ending in `/api/v1/channels/email/microsoft/admin-consent`, where an
     organization's admin returns after approving Colliedesk. Both are shown in the channel settings.
2. **API permissions** → **Add a permission** → **Microsoft Graph** → **Delegated permissions**: `Mail.Send`,
   `User.Read`, `offline_access`, `openid`. None needs admin consent; with a single-tenant app you may
   still select **Grant admin consent** so nobody in your organization is asked.
3. **Certificates & secrets** → **New client secret**. Microsoft allows at most 24 months and recommends
   less than 12. Copy the **Value** at once (it is shown only once). Write down the date it expires: when it
   expires, every Microsoft 365 channel stops sending at the same time. Replies then wait with a note naming
   `microsoft.client_secret` and the connections are kept, so setting a new secret is enough.
4. Save these settings (`collie config set`, the secret with `collie config set-secret`):

| Variable | Meaning |
|---|---|
| `microsoft.client_id` | **Application (client) ID** of the app registration. |
| `microsoft.client_secret` | The client secret's **Value**. |
| `microsoft.tenant` | `common` (default: work, school and personal accounts), `organizations` (work and school only), `consumers` (personal only), or your tenant id or domain for a single-tenant app. |
| `MAILBOX_TOKEN_KEYS` | The same key ring as Gmail: it encrypts the stored Microsoft access too. |

Microsoft replaces the stored access every time it is used and drops it after 90 days unused, so the
server renews a connection that sent nothing for a week. Microsoft has no way for Colliedesk to revoke its
access: disconnecting deletes it from your server (it is useless without your client secret), and the
person removes Colliedesk in their Microsoft account as the settings page explains.
