# Trial offers

On a server with billing (Colliedesk Cloud, or your own server with billing configured), every new
workspace gets a free trial. When a team's trial ran out while they were still evaluating, an
operator can send them a one-time **trial offer**: an admin of that workspace who opens the link and
presses **Continue trial** gets 14 days from that moment. Colliedesk sends no email for it: you put
the link in your own email.

Run these on the server shell, for example `docker compose exec server /app/collie trial offer …`.

## Make an offer

```sh
collie trial offer --workspace acme
```

It prints the offer's id, when the link stops working, and the link itself. Copy the link into your
email to the team.

- The link works for 30 days by default. `--valid 7d` (days) or `--valid 36h` (any duration) shortens it.
  More than 30 days is refused.
- `--by <name>` records who made the offer; the default is the operating system user.
- Only a hash of the link is stored, as for invite links. If you lose the link, revoke the offer and
  make a new one.

## What the team sees

1. Opening the link changes nothing, so a mail scanner that opens it first does no harm.
2. Someone who is not signed in signs in and comes back to the same page.
3. The page names the workspace and says "Continue your trial: 14 days, starting when you press
   Continue." It has one button, **Continue trial**.
4. Pressing it sets the trial to end 14 days from that moment, and a locked workspace opens at once.
   A trial that already ends later is kept, never shortened. The usual trial-ending emails then
   follow the new end.

Only a signed-in member with `billing:manage` (the admins) of that same workspace can accept. An
agent is told which admins can; someone signed in to another workspace is refused without the other
workspace being named. An offer that expired, was revoked or was already accepted, or a workspace
that has a plan, is refused with the current trial end and support@colliedesk.com. An AI agent can
accept the link too, with the MCP tool `colliedesk_trial_offer_accept`.

## See and revoke offers

```sh
collie trial offers --workspace acme
collie trial offer revoke <offer id>
```

`trial offers` lists every offer of the workspace with its state: open (and until when), accepted (by
whom, when), expired or revoked (by whom, when). `trial offer revoke` closes an open offer; its link
then says it was withdrawn. An offer that is no longer open cannot be revoked.

Each accepted offer that moved the trial end is recorded with the other trial changes (`collie trial
set`): the previous and the new end, when, and `trial offer <id> accepted by <admin>`.

## Set the trial end directly

`collie trial set --workspace acme --ends 2026-12-01T00:00:00Z` sets the end to an exact time
without asking the team. Prefer an offer: the team chooses when its 14 days start.
