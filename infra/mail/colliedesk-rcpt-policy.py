#!/usr/bin/env python3
"""Postfix policy service (access_policy_delegation): asks Colliedesk whether a hosted recipient exists
before the MTA accepts RCPT TO, so mail for an address nobody owns is refused in the SMTP session and
never bounced afterwards to a sender that may be forged.

Run by Postfix spawn(8) on a UNIX socket; one process answers many requests on stdin/stdout.
Environment: COLLIEDESK_RECIPIENT_URL (…/api/v1/channels/email/inbound/recipient) and
COLLIEDESK_INBOUND_SECRET_FILE (the server-wide inbound secret). The secret travels in a header,
never in argv.

Answers: 204 -> DUNNO (later restrictions decide), 404 -> 550 5.1.1, anything else or an error ->
451 4.3.0 so the sender retries instead of losing mail.
"""
import json
import os
import sys
import urllib.error
import urllib.request

ACCEPT = "DUNNO"
UNKNOWN = "550 5.1.1 No Colliedesk workspace receives mail at this address"
DEFER = "451 4.3.0 Colliedesk cannot check this recipient now, try again later"


def decide(attributes, check):
    if attributes.get("protocol_state") != "RCPT":
        return ACCEPT
    try:
        status = check(attributes.get("recipient", ""))
    except Exception:  # network, TLS, timeout: never refuse mail because we could not ask
        return DEFER
    if status == 204:
        return ACCEPT
    if status == 404:
        return UNKNOWN
    return DEFER


def serve(stdin, stdout, check):
    attributes = {}
    for line in stdin:
        line = line.rstrip("\n")
        if line:
            name, _, value = line.partition("=")
            attributes[name] = value
            continue
        stdout.write(f"action={decide(attributes, check)}\n\n")
        stdout.flush()
        attributes = {}


def http_check(url, secret):
    def check(recipient):
        body = json.dumps({"recipient": recipient}).encode()
        request = urllib.request.Request(url, data=body, method="POST", headers={
            "Content-Type": "application/json", "X-Colliedesk-Inbound-Secret": secret})
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return response.status
        except urllib.error.HTTPError as error:
            return error.code
    return check


def main():
    url = os.environ.get("COLLIEDESK_RECIPIENT_URL", "")
    secret_file = os.environ.get("COLLIEDESK_INBOUND_SECRET_FILE", "")
    try:
        with open(secret_file, encoding="utf-8") as handle:
            secret = handle.read().strip()
    except OSError:
        secret = ""
    if not url or not secret:
        print("colliedesk-rcpt-policy: COLLIEDESK_RECIPIENT_URL and the inbound secret are required", file=sys.stderr)
        serve(sys.stdin, sys.stdout, lambda recipient: 0)  # defers every request rather than refusing mail
        return
    serve(sys.stdin, sys.stdout, http_check(url, secret))


if __name__ == "__main__":
    main()
