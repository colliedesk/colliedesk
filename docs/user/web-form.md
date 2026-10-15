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

1. Open **Settings → Web form** (admins only; agents see the state).
2. Under **Allowed origins** list every site that hosts the form, one per line, for example
   `https://example.com`. An origin is scheme, host and optional port, exactly as the browser shows
   it. No path, no trailing slash, no wildcard. `https://example.com` and `https://www.example.com`
   are two origins. At most 20.
3. Select **Accept web form submissions** and **Save web form**.

The form is **off** until you do this. Clear the checkbox and save to turn it off again; your
origins stay saved. While it is off, the form answers `404`, the same as a workspace that does not
exist. A locked workspace (trial over, nothing paid) answers the same way.

## Send a submission

The settings page shows the **Submit URL**: `<your Colliedesk address>/api/v1/public/forms/<workspace slug>`.
Post JSON from a page on an allowed origin:

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
- `404`: unknown workspace, form off, or workspace locked.
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
- Behind a reverse proxy, Colliedesk must know the proxy to read the visitor's address from
  `X-Forwarded-For`. When `TRUSTED_PROXIES` is **not set**, the server trusts loopback and the
  Docker bridge range (`127.0.0.0/8,::1/128,172.16.0.0/12`), which fits a proxy on the same host.
  For another setup, set it to your proxy's addresses or ranges, comma separated, for example
  `TRUSTED_PROXIES=10.0.0.5,10.1.0.0/16` (with the installer, put it in `.env`). Set it to an
  empty value to trust no proxy; the server then logs a warning at startup, and behind a proxy every
  visitor counts as the proxy and shares one limit.
- **Warning:** do not publish the server port on `0.0.0.0` (or any public address) without a proxy
  in front while the default is active. A client inside a trusted range, or one that reaches the
  server directly through Docker's bridge, could then send a fake `X-Forwarded-For` and pick its own
  address for the limits. The shipped files publish on `127.0.0.1` only. If you expose the port
  directly, set `TRUSTED_PROXIES=` (empty).
- Extra fields in the JSON body, such as `name`, are ignored.
- Answers to a request that is refused before the form is found (too many requests from one
  address) carry no CORS permission on purpose (the origin list is not known yet and the server never echoes
  an unchecked `Origin`), so a browser reports a network error instead of the `429`. Wait a minute
  and retry.
- Anyone can submit any email address. A submission proves nothing about who owns the address, so
  do not treat it as verified.
