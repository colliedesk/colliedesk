# Running behind a proxy

The server listens on `127.0.0.1` only. To reach it from outside the host, put a reverse proxy in
front of it that terminates TLS for your domain, for example Caddy or nginx, and set `APP_ORIGIN`
to the public address (see [environment variables](environment-variables.md)).

## The Host header

The proxy **must** pass the visitor's `Host` header on unchanged: the server decides from it what to
answer. It serves the app, the API, MCP and attachments when `Host` is `APP_ORIGIN`'s host name,
`localhost`, or an IP address (the shipped health check calls `http://127.0.0.1:8080/`, and Caddy asks
its certificate question on `127.0.0.1`), and, with `HELP_CENTER_DOMAIN`, on each workspace's own host
`<slug>.<HELP_CENTER_DOMAIN>` for that workspace only ([workspace host names](#workspace-host-names)).
A [custom domain](#custom-help-center-domains) serves only its help center, and any other name gets
the help center's `404`, never the app. A name the server does not own therefore never shows the
sign-in page, even if its DNS still points at the proxy.

- **Caddy** keeps `Host` by default.
- **nginx** sends the upstream's name unless told otherwise; add `proxy_set_header Host $host;` to
  every `location` that proxies to Colliedesk. Without it, a `proxy_pass http://colliedesk:8080;` sends
  `Host: colliedesk:8080`, and every request gets `404`.
- **Another proxy or load balancer:** forward the original `Host`, not the address of the upstream.

## Trusted proxies

Behind a reverse proxy, Colliedesk must know the proxy to read the visitor's address from
`X-Forwarded-For`. Rate limits, for example those of the [web form](../user/web-form.md), count per
visitor address.

The server trusts **no** proxy unless `TRUSTED_PROXIES` names one, whether the variable is unset or
empty. The shipped `compose.yaml` and installer file set it to loopback and the Docker bridge range
(`127.0.0.0/8,::1/128,172.16.0.0/12`) when you do not, which fits a proxy on the same host. For
another setup, set it to your proxy's addresses or ranges, comma separated, for example
`TRUSTED_PROXIES=10.0.0.5,10.1.0.0/16`. With no trusted proxy the server logs a warning at startup,
and behind a proxy every visitor counts as the proxy and shares one limit.

**Warning:** do not publish the server port on `0.0.0.0` (or any public address) without a proxy
in front while the compose default is active. A client inside a trusted range, or one that
reaches the server directly through Docker's bridge, could then send a fake `X-Forwarded-For` and
pick its own address for the limits. The shipped files publish on `127.0.0.1` only. If you
expose the port directly, set `TRUSTED_PROXIES=` (empty).

## Large uploads

Knowledge base videos go up in parts of at most 8 MiB, each one request. Caddy takes them as they
are. nginx refuses a body over 1 MB by default, so raise it for the API:
`client_max_body_size 16m;`. A part must arrive within 120 seconds.

A video plays and seeks with HTTP ranges (`Range`, `206 Partial Content`); keep them working through
the proxy, and do not compress `video/*` (Caddy's `encode` leaves video alone; with nginx, leave
`video/mp4` and `video/webm` out of `gzip_types`).

A help center sends at most 4 videos at once to one reader's address and 64 in all (503 with
`Retry-After` beyond). Set `TRUSTED_PROXIES` so the address is the reader's: behind a proxy that is
not trusted, every reader has the proxy's address and the whole site plays 4 videos at once. The 64
are a safety valve: a few addresses holding slow connections can still take them all. A help center
video never changes under its address (`Cache-Control: public, immutable`), so a caching proxy or
CDN in front of `/help/*/media/` is what takes that load off the server.

Videos take disk room: the server refuses an upload when the disk has less than the video, the
uploads in progress and 1 GiB free. Back up `ATTACHMENTS_DIR` with the database; a backup and an
[export](../user/export.md) grow with the videos.

## Workspace host names

Without extra setup, everything runs on `APP_ORIGIN`, and each workspace's
[help center](../user/knowledge-base.md#public-help-center) is at `APP_ORIGIN/help/<slug>`. To give
each workspace its own host name instead, for example `acme.example.com`:

1. Point a wildcard DNS record, `*.example.com`, at the proxy.
2. Set `HELP_CENTER_DOMAIN=example.com` and restart the server. Each workspace then has the host name
   `<slug>.example.com`, with the scheme and port of `APP_ORIGIN`:
   - `https://acme.example.com/` runs the workspace's desk: the inbox, every page of the app, its
     API and MCP. The team works there every day.
   - `https://acme.example.com/help` is the workspace's help center. `APP_ORIGIN/help/acme` redirects
     there (301), and so do the help center's addresses from before it moved under `/help`
     (`https://acme.example.com/a/12-reset` moves to `https://acme.example.com/help/a/12-reset`).
     `robots.txt` stays at the root of the host.
   - `APP_ORIGIN` keeps sign-up, "find your workspace" (sign in there, choose the workspace) and the
     fixed callback addresses of Google, Microsoft, GitHub and Polar. A browser signed in on
     `APP_ORIGIN` moves to the same page on its workspace's host, signed in there by a one-time token
     that works once, for one minute, only on that host. A callback that arrives on `APP_ORIGIN` is
     sent, with its query, to the same callback on the host of the workspace that started it, which
     finishes it for the person signed in there.
3. Give the proxy certificates for the host names. With Caddy, get each one on demand and let the
   server say which names it serves; it answers `200` for an existing workspace's host name, and
   `404` for everything else:

```caddyfile
{
	on_demand_tls {
		ask http://127.0.0.1:8080/api/v1/public/help-center/tls-ask
	}
}

*.example.com {
	tls {
		on_demand
	}
	reverse_proxy 127.0.0.1:8080
}
```

**Sessions belong to a host.** The session cookie has no `Domain` attribute, so a browser sends it
only to the host that set it. On a workspace host the server accepts only that workspace's sessions
and agent tokens: another workspace's session gets `401` and its cookie is cleared, and a write made
with the cookie must come from that host's own origin. Cloud sign-in on a workspace host signs in to
that workspace only, whatever the browser asks for.

**Names that never become workspace hosts.** A slug that cannot be a host name (a reserved name such as
`www`, `mail`, `support` or `help-staging`) keeps everything on `APP_ORIGIN`. `APP_ORIGIN`'s own host is
never a workspace's, also when it is inside `HELP_CENTER_DOMAIN` (`APP_ORIGIN=https://app.example.com`
beside `HELP_CENTER_DOMAIN=example.com`) or is `HELP_CENTER_DOMAIN` itself
(`APP_ORIGIN=https://staging.example.com` beside `HELP_CENTER_DOMAIN=staging.example.com`, workspaces on
`<slug>.staging.example.com`). Give every one-label name that has its own DNS record under the domain a
reserved slug, or the explicit record would take a workspace's host.

**Changing `HELP_CENTER_DOMAIN`** changes every workspace's host name and every custom domain's `CNAME`
target (`hc-<claim>.<HELP_CENTER_DOMAIN>`). The server answers only the names of the current value.

Let's Encrypt issues at most 50 certificates per registered domain a week; a wildcard certificate
(DNS challenge) avoids that limit if you serve many workspaces. Keep `APP_ORIGIN`'s own site block:
a more specific site wins over the wildcard.

## Custom help center domains

A workspace can also serve its help center on a domain of its own, such as `help.example.com`
([custom domain](../user/knowledge-base.md#custom-domain)). It works with or without
`HELP_CENTER_DOMAIN`:

- **With `HELP_CENTER_DOMAIN`**, the owner points the name with a `CNAME` to the target the card
  shows, `hc-<claim>.<HELP_CENTER_DOMAIN>`, which also proves the workspace owns it. The target is
  random, made when the domain is set and kept until it is removed or replaced, and no workspace
  slug can take its shape; the wildcard record already covers it, so there is nothing to add for it.
  The workspace's own `<slug>.<HELP_CENTER_DOMAIN>` proves nothing: a slug can be renamed and then
  taken by another workspace. The wildcard record of `HELP_CENTER_DOMAIN` must be `A`/`AAAA` records,
  not a `CNAME`: the check reads where the name's `CNAME` chain ends, and a wildcard that is itself a
  `CNAME` makes every chain end elsewhere. The owner can always use the `TXT` record instead.
- **Without it**, the owner points the name at the server (the card shows a `CNAME` to `APP_ORIGIN`'s
  host, or an `A`/`AAAA` record when `APP_ORIGIN` is an IP address) and proves ownership with the `TXT`
  record `_help-center-verify.<name>`.

The proxy must send every other host name to the server and get its certificate on demand. The
server answers the `ask` with `200` only for an active custom domain whose help center is on, or a
pending one whose DNS check passes at that moment (it then becomes active, so one DNS change is
enough). It runs at most 4 such checks at once and at most one per name every 30 seconds, asks DNS
only for names a workspace has claimed, and records a certificate in the database at most once an
hour per name; every other name gets `404`. With Caddy, add a catch-all site after your other sites:

```caddyfile
{
	on_demand_tls {
		ask http://127.0.0.1:8080/api/v1/public/help-center/tls-ask
	}
}

desk.example.com {
	reverse_proxy 127.0.0.1:8080
}

# Every other host name: custom help center domains. Caddy asks the server before it gets a certificate.
https:// {
	tls {
		on_demand
	}
	reverse_proxy 127.0.0.1:8080
}
```

The proxy must keep the `Host` header ([the Host header](#the-host-header)): the server decides from it
which help center to show. On a custom domain the server answers only help center pages; the app, the
API and MCP stay on `APP_ORIGIN`. nginx cannot get certificates on demand: issue one per custom domain
yourself and add a `server` block for it.

A domain removed in Settings stops being served with the next request, on every server process:
each page reads the domain's row. Each process remembers for up to 30 seconds which host names are
custom domains and which workspace's address to give out, and forgets a name at once when a domain
write through that process changes it. With several server processes behind one proxy, a domain that
has just become active through one of them may therefore get the help center's `404` on another for
up to 30 seconds, and API answers there may give the previous address for as long. A host name the
server does not know costs one database lookup; each client address may cause at most 10 a minute
(the help center's busy page beyond), so set [`TRUSTED_PROXIES`](#trusted-proxies) or every visitor
shares the proxy's 10.

The server checks every active custom domain at start and then daily. A domain whose records no
check has found for 7 days goes back to pending: it stops serving, and its other addresses serve the
help center again. Another workspace whose records prove a host name while the active workspace's
records do not takes it over at once. Both show in Settings and in the API (`pending_reason`).
