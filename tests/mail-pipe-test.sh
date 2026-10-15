#!/bin/sh
# Checks the exit codes of infra/mail/colliedesk-inbound-pipe.sh against stubbed curl results.
# MTAs (Postfix pipe, Exim pipe transport) retry only on EX_TEMPFAIL (75); any other failure bounces.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT=$ROOT/infra/mail/colliedesk-inbound-pipe.sh
TMP=${TMPDIR:-/tmp}/colliedesk-mail-pipe-test-$$
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

cat >"$TMP/curl" <<'STUB'
#!/bin/sh
printf '%s\n' "$@" >"$STUB_DIR/argv"
cat >"$STUB_DIR/stdin"
if [ "$STUB_EXIT" != 0 ]; then
  exit "$STUB_EXIT"
fi
printf '%s' "$STUB_CODE"
STUB
chmod +x "$TMP/curl"

run() {
  printf 'From: a@example.com\r\nSubject: s\r\n\r\nbody\r\n' |
    STUB_DIR=$TMP STUB_CODE=$1 STUB_EXIT=${2:-0} CURL=$TMP/curl \
    COLLIEDESK_INBOUND_URL=https://desk.example.com/api/v1/channels/email/inbound \
    COLLIEDESK_INBOUND_SECRET=key.top-secret-value sh "$SCRIPT" 2>/dev/null
}

expect() {
  name=$1 want=$2
  shift 2
  set +e
  run "$@"
  got=$?
  set -e
  if [ "$got" != "$want" ]; then
    printf 'FAIL %s: exit %s, want %s\n' "$name" "$got" "$want" >&2
    exit 1
  fi
}

expect 'accepted' 0 200
expect 'rate limited' 75 429
expect 'server error' 75 500
expect 'bad gateway' 75 502
expect 'wrong secret keeps the mail' 75 401
expect 'connection failure' 75 000 7
expect 'not an email' 65 400
expect 'too large' 65 413
expect 'mailbox closed' 65 403
expect 'unknown recipient' 67 404

if grep -F top-secret-value "$TMP/argv" >/dev/null; then
  echo 'FAIL secret appears in curl argv' >&2
  exit 1
fi
if ! grep -F 'Subject: s' "$TMP/stdin" >/dev/null; then
  echo 'FAIL message was not passed to curl unchanged' >&2
  exit 1
fi
set +e
printf 'x' | CURL=$TMP/curl sh "$SCRIPT" 2>/dev/null
missing=$?
set -e
if [ "$missing" != 75 ]; then
  printf 'FAIL missing configuration: exit %s, want 75\n' "$missing" >&2
  exit 1
fi
echo 'mail pipe tests passed'
