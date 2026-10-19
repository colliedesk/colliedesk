# Report a problem

If something in Colliedesk does not work, press **Report a problem** in the account menu (your
avatar at the bottom of the left rail). You describe what happened, and Colliedesk adds the facts a
person needs to check it. Nothing is stored or sent until you press **Send**.

## What a report contains

Before you send, the dialog lists every field under **What will be sent**. A report has:

| Part | Example | Why |
|---|---|---|
| Your description | "Replies spin forever after I open a conversation" | What you saw and expected. Up to 5000 characters. |
| Screenshots (optional) | up to 3 PNG, JPEG or WebP images, 5 MB in total | You choose each file. Nothing is captured for you. |
| App version, browser, window size | `0.0.41`, `Firefox 133 on Linux`, `1280x720` | To reproduce the problem. |
| Page | `/conversations/…` | Where you were. The query string is removed. |
| Recent errors in the browser (up to 20) | message, source, time | The error the page raised. |
| Recent failed requests (up to 20) | `GET /api/v1/conversations` 500, **request id** `9f3ac1d2b7e4`, time | The request id finds that exact request in the server log. |
| Names of the last actions (up to 30) | `conversation_opened`, `inbox_searched` | What led to the problem. Names only. |
| Who and where, added by the server | your agent id, email, display name and role; the workspace id, name, address and plan; the server version; the id of this report | So the team knows whose report it is. |

A report never contains the subject, text, contact, email address or attachments of any
conversation, and the pages you visited are sent without their query strings. The browser keeps the
list of recent errors, failed requests and action names in memory only; closing or reloading the tab
forgets it.

## Where the report goes

- **Colliedesk Cloud:** the report becomes a conversation in the HC Labs support desk, from your
  email address. You get the report's reference after sending; quote it when you write to support.
  The team may reply by email.
- **A self-hosted server:** nothing is sent to HC Labs. The dialog offers **Open a GitHub issue**,
  which opens `github.com/colliedesk/colliedesk/issues/new` with the description and a summary of
  the diagnostics already filled in. The summary has no email addresses. You read it and press
  *Submit* on GitHub yourself, or you do not.
- **Your own support desk (self-hosted):** the server operator can send reports from every other
  workspace to one workspace on the same server, as conversations tagged `problem-report`; see
  [problem reports](../self-host/configuration.md#problem-reports).

An agent may send 5 reports an hour. The 6th is refused until an hour has passed.

## Request ids

Every answer of the API has an `X-Request-Id` header, and every error body has `error.request_id`.
The server log lines of that request carry the same id. If you put a reverse proxy in front of
Colliedesk, it may set `X-Request-Id` itself (8 to 64 characters: letters, digits, `.`, `_`, `-`); the
server keeps it. Otherwise the server makes a 12-character id.

## For AI agents

The MCP tool `colliedesk_problem_report` and `POST /api/v1/support/problem-reports` send the same
report. Any token tier may use them. An agent should pass the request ids of the failed calls in
`diagnostics.failed_api_calls`. On a server that takes no reports the tool says so and points to
GitHub issues.

The web app also reports that you sent a report as the product event `problem_reported` (the name
only), when [usage reporting](telemetry.md) is on.
