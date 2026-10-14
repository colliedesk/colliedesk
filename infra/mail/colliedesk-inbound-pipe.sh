#!/bin/sh
# Deliver one message from an MTA pipe (Postfix pipe(8), Exim pipe transport) to CollieDesk.
#
# Environment: COLLIEDESK_INBOUND_URL, and COLLIEDESK_INBOUND_SECRET or COLLIEDESK_INBOUND_SECRET_FILE.
# Exit codes follow sysexits(3), which is what MTAs act on:
#   0  stored, threaded, duplicate or dropped as automatic mail
#   75 EX_TEMPFAIL: the MTA keeps the message and retries (429, 5xx, 401, network or configuration error)
#   65 EX_DATAERR: the MTA bounces it (400 not an email, 413 larger than 25 MiB)
set -u

CURL=${CURL:-curl}
URL=${COLLIEDESK_INBOUND_URL:-}
SECRET=${COLLIEDESK_INBOUND_SECRET:-}
if [ -z "$SECRET" ] && [ -n "${COLLIEDESK_INBOUND_SECRET_FILE:-}" ]; then
  SECRET=$(cat "$COLLIEDESK_INBOUND_SECRET_FILE") || SECRET=
fi
if [ -z "$URL" ] || [ -z "$SECRET" ]; then
  echo 'colliedesk-inbound-pipe: COLLIEDESK_INBOUND_URL and the inbound secret are required' >&2
  exit 75
fi

WORK=$(mktemp -d) || exit 75
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
# The secret goes in a private header file, never in argv, which other local users can read.
(umask 077 && printf 'X-CollieDesk-Inbound-Secret: %s\n' "$SECRET" >"$WORK/headers") || exit 75

STATUS=$("$CURL" --silent --show-error --max-time 120 --output /dev/null --write-out '%{http_code}' \
  -X POST -H 'Content-Type: message/rfc822' -H "@$WORK/headers" --data-binary @- "$URL") || {
  echo 'colliedesk-inbound-pipe: CollieDesk is unreachable; the MTA will retry' >&2
  exit 75
}

case $STATUS in
  2??) exit 0 ;;
  400 | 413)
    echo "colliedesk-inbound-pipe: CollieDesk rejected the message (HTTP $STATUS)" >&2
    exit 65
    ;;
  *)
    echo "colliedesk-inbound-pipe: CollieDesk answered HTTP $STATUS; the MTA will retry" >&2
    exit 75
    ;;
esac
