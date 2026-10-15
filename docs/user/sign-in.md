# Sign in without passwords

CollieDesk has no passwords. You sign in with a one-time link.

- **Pairing link**: printed by the `create-workspace` or `pair` command for a specific person.
- **Invite link**: created by an admin in **Settings → Invites** (see [Invite your team](invites.md)).

Each link works once and expires after 15 minutes. CollieDesk stores only a hash of the link,
so nobody, including us, can read it back later. If a link is invalid, expired or already used,
ask your admin for a new one.

Opening a link signs that browser in for 30 days; using CollieDesk extends it. A link for another
workspace switches the browser to that workspace; a bar at the top of every page always shows which workspace and
address you are signed in as. You can see and
revoke every signed-in device in [Sessions and devices](sessions.md).

## Cloud sign-in

Some hosted CollieDesk instances show **Sign in** on the same page. Use it to authenticate with the
identity methods enabled by the instance owner, such as Google, Microsoft, email, or MFA.

Cloud sign-in does not create a CollieDesk agent. Your verified primary email must already belong to
an agent in at least one workspace. If it belongs to several workspaces, choose the workspace after
authentication. CollieDesk then creates its own ordinary browser session; the identity provider does
not receive or manage that CollieDesk session.

Self-hosted instances without cloud configuration behave exactly as before and do not load the cloud
sign-in code. One-time pairing and invite links remain available when cloud sign-in is enabled.
