# Web form

A form on your website can open a conversation in your workspace. The visitor types an email
address (and, if you want, a message). Colliedesk finds or creates a contact for that address,
opens a conversation with the text as an incoming message, subject `Web form: <source>` and the tag
`web-form`. Colliedesk sends **nothing** to the visitor; you reply from the conversation as usual.

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
- 5 submissions per minute for each visitor address and workspace, and at most 200 new
  conversations per workspace in 10 minutes (shared with email). The per-address count lives in the
  server process: a restart clears it.
- Behind a reverse proxy, set `TRUSTED_PROXY_CIDRS` on the server to the proxy's address or range
  (comma separated, for example `172.16.0.0/12`). Colliedesk then reads the visitor's address from
  `X-Forwarded-For`. Without it every visitor looks like the proxy and shares one limit.
- Anyone can submit any email address. A submission proves nothing about who owns the address, so
  do not treat it as verified.
