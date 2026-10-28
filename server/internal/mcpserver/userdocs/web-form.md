# Web form

A form on your website can open a conversation in your workspace. The visitor types an email
address (and, if you want, a message). Colliedesk finds or creates a contact for that address,
opens a conversation with the text as an incoming message, subject `Web form: <source>` and the tag
`web-form`. Colliedesk sends **nothing** to the visitor. The message is marked **Unverified sender
(web form)**: anyone can type any address, so check before you trust it.

You can reply from the conversation. On a Colliedesk Cloud **trial**, a reply to a web form
conversation is refused (error `trial_outbound_restricted`), because the form cannot prove who owns
the address; a paid plan has no such rule. Self-hosted installs have no trial.

It works the same on self-hosted installs and on Colliedesk Cloud.

## Turn it on

1. Open **Settings → Channels**, select **Add a web form** (or **Add channel → Add a web form**)
   (admins only; agents see the state).
2. Under **Allowed origins** list every site that hosts the form, one per line, for example
   `https://example.com`. An origin is scheme, host and optional port, exactly as the browser shows
   it. No path, no trailing slash, no wildcard. `https://example.com` and `https://www.example.com`
   are two origins. At most 20.
3. Select **Save web form**. The button reads "Saving…" and stays disabled until the save ends. The
   form is added and on; its page opens on the **Embed** tab, where you change the origins later.

The form is **off** until you add it. Turn it off and on again with its switch in **Settings →
Channels** (see [Channels](channels.md#turn-a-channel-off)); your origins stay saved. While it is off, the form answers `404`, the same as a workspace that does not
exist. A locked workspace (trial over, nothing paid) answers the same way.

## Several forms

A workspace can have several web forms, for example one on the shop and one on the careers page: add
another with **Settings → Channels → Add channel → Add a web form**. Each form has its own Submit URL
and its own allowed origins (two forms may list the same site), and a submission opens its
conversation in the form it was posted to, so the inbox can filter by form. Replies to a form
conversation are emailed from the default mailbox, and keep leaving from the mailbox of the first
reply.

The older slug address belongs to the form that had it, whichever form is the default; removing that
form leaves the older address answering `404`.

## Try it

The **Embed** tab explains the form in plain words and has a small **Try the form**: enter an email
address and a message and select **Try the form**. It sends one submission from that page, the way a
visitor's browser would, and says when the conversation is in your inbox. The form must be on; the
test needs no allowed origin, because it comes from Colliedesk's own page.

## Send a submission

The settings page shows the form's **Submit URL**: `<your Colliedesk address>/api/v1/public/web-forms/<form id>`.
A workspace's first form also keeps the older address, `<your Colliedesk address>/api/v1/public/forms/<workspace slug>`,
so sites that embed it keep working; its **Embed** tab shows that address and says the form also has
its own. Post JSON from a page on an allowed origin:

```js
await fetch('https://desk.example.com/api/v1/public/forms/acme', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email, message, source: 'site:updates', website: '' }),
});
```

| Field | Rule |
| --- | --- |
| `email` | Required, a plain valid address, at most 254 characters. |
| `message` | Optional, at most 5000 characters. Empty shows as "(no message)". |
| `source` | Optional, at most 100 characters. Becomes the subject `Web form: <source>`. Without it the subject is `Web form`. |
| `website` | A hidden field that real visitors leave empty. If it holds text the request is treated as spam: it answers `202` like a success and stores nothing. |

Answers:

- `202 {"ok": true}`: stored (or dropped as spam).
- `404`: unknown workspace or form, form off or removed, or workspace locked.
- `422`: invalid input; `error.fields` names each field, for example `{"email": "Enter a valid email address."}`.
- `429`: too many submissions (see Limits). `Retry-After` says how long to wait.
- `415`: the body is not `application/json`.

## Limits and safety

- A browser may call the form only from a listed origin. Other origins get no CORS permission.
  Origin checks protect visitors' browsers; they do not authenticate the caller, because any script
  can set an `Origin` header. That is why the next limits exist.
- 5 submissions per minute for each visitor address and workspace, and 30 requests per minute for
  each visitor address across all forms (this includes the browser's preflight). A workspace also
  accepts at most 50 form conversations in 10 minutes. That ceiling is separate from email, so form
  spam never blocks incoming mail. The per-address counts live in the server process: a restart
  clears them.
- The limits count per visitor address. On your own server behind a reverse proxy, the server must
  trust that proxy to read the address; see [running behind a proxy](../self-host/running-behind-proxy.md).
- Extra fields in the JSON body, such as `name`, are ignored.
- Answers to a request that is refused before the form is found (too many requests from one
  address) carry no CORS permission on purpose (the origin list is not known yet and the server never echoes
  an unchecked `Origin`), so a browser reports a network error instead of the `429`. Wait a minute
  and retry.
- Anyone can submit any email address. A submission proves nothing about who owns the address, so
  do not treat it as verified.
