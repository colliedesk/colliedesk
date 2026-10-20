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
