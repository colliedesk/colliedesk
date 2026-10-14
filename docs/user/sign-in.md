# Sign in without passwords

CollieDesk has no passwords. You sign in with a one-time link.

- **Pairing link**: printed by the `create-workspace` or `pair` command for a specific person.
- **Invite link**: created by an admin in **Settings → Invites** (see [Invite your team](invites.md)).

Each link works once and expires after 15 minutes. CollieDesk stores only a hash of the link,
so nobody, including us, can read it back later. If a link is invalid, expired or already used,
ask your admin for a new one.

Opening a link signs that browser in for 30 days; using CollieDesk extends it. You can see and
revoke every signed-in device in [Sessions and devices](sessions.md).
