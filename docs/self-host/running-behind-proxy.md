# Running behind a proxy

The server listens on `127.0.0.1` only. To reach it from outside the host, put a reverse proxy in
front of it that terminates TLS for your domain, for example Caddy or nginx, and set `APP_ORIGIN`
to the public address (see [environment variables](environment-variables.md)).

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
`Retry-After` beyond); set `TRUSTED_PROXIES` so the address is the reader's, not the proxy's. A help
center video never changes under its address (`Cache-Control: public, immutable`), so a caching
proxy or CDN in front of `/help/*/media/` takes that load off the server.

Videos take disk room: the server refuses an upload when the disk has less than the video, the
uploads in progress and 1 GiB free. Back up `ATTACHMENTS_DIR` with the database; a backup and an
[export](../user/export.md) grow with the videos.

## Help center host names

Every workspace's [help center](../user/knowledge-base.md#public-help-center) works at
`APP_ORIGIN/help/<slug>` with no extra setup. To give each one its own host name instead, for
example `acme.help.example.com`:

1. Point a wildcard DNS record, `*.help.example.com`, at the proxy.
2. Set `HELP_CENTER_DOMAIN=help.example.com` and restart the server. The help center then answers
   on `<slug>.help.example.com` with the scheme and port of `APP_ORIGIN`, and `APP_ORIGIN/help/<slug>`
   redirects there (301). A slug that cannot be a host name (for example `www`, `mail` or `support`)
   keeps the path address. On those host names the server answers only help center pages: the
   app, the API and MCP stay on `APP_ORIGIN`.
3. Give the proxy certificates for the host names. With Caddy, get each one on demand and let the
   server say which names it serves; it answers `200` only for a help center that is on, and
   `404` for everything else:

```caddyfile
{
	on_demand_tls {
		ask http://127.0.0.1:8080/api/v1/public/help-center/tls-ask
	}
}

*.help.example.com {
	tls {
		on_demand
	}
	reverse_proxy 127.0.0.1:8080
}
```

Let's Encrypt issues at most 50 certificates per registered domain a week; a wildcard certificate
(DNS challenge) avoids that limit if you serve many workspaces. Keep `APP_ORIGIN`'s own site block:
a more specific site wins over the wildcard.
