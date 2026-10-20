# Troubleshooting and FAQs

Run the commands on this page in the directory that holds `compose.yaml`.

## Check that the containers run

```sh
docker compose ps
```

You should see `postgres` and `server`, both `healthy`. The installer waits up to 120 seconds for
the server to become healthy and stops with an error if it does not.

## Read the server log

```sh
docker compose logs server
```

The server applies database migrations when it starts, so a failed migration or a wrong
`DATABASE_URL` shows here first. Startup warnings, for example about `TRUSTED_PROXIES`, are here
too.

## The sign-in link expired

A pairing link works once and expires after 15 minutes. Print a new one:

```sh
docker compose exec server /app/collie pair --workspace <slug> --email admin@example.com
```

## Which version do I run?

```sh
docker compose exec server /app/collie version
```

## Replies show "Delivery failed"

The timeline shows the reason under the reply. Most often the SMTP relay is not set or refuses the
From domain; see [configuring email](email.md). Fix the cause, then select **Retry delivery**.

## Every visitor shares one rate limit

The server does not trust your reverse proxy, so it sees the proxy's address for every visitor.
Set `TRUSTED_PROXIES`; see [running behind a proxy](running-behind-proxy.md).

## Create an agent token from the command line

Besides the API described in [sessions and devices](../user/sessions.md#agent-tokens), you can
create an agent token on the server:

```sh
docker compose exec server /app/collie token create \
  --workspace <slug> --email you@example.com --scope draft --label "Claude Code"
```

## Still stuck?

See [support](support.md).
