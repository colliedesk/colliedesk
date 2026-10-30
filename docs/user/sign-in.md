# Sign in without passwords

Colliedesk has no passwords. You sign in with a one-time link.

- **Pairing link**: printed by the `create-workspace` or `pair` command for a specific person.
- **Invite link**: created by an admin in **Settings → Invites** (see [Invite your team](invites.md)).

Each link works once and expires after 15 minutes. Colliedesk stores only a hash of the link,
so nobody, including us, can read it back later. If a link is invalid, expired or already used,
ask your admin for a new one.

Opening a link signs that browser in for 30 days; using Colliedesk extends it. A link for another
workspace switches the browser to that workspace; a bar at the top of every page always shows which workspace and
address you are signed in as. You can see and
revoke every signed-in device in [Sessions and devices](sessions.md).

## Cloud sign-in

Some hosted Colliedesk instances show **Sign in** on the same page. Use it to authenticate with the
identity methods enabled by the instance owner, such as Google, Microsoft, email, or MFA.

Enter your email address, then the six-digit code Clerk sends to that address. You do not need to
create a Clerk account first: the same form works for everyone, and an agent who has been invited can
sign in this way even before they have one.

Signing in does not by itself add you to a workspace. Your verified primary email must belong to an
agent in at least one workspace. If it belongs to several workspaces, choose the workspace after
authentication. Colliedesk then creates its own ordinary browser session; the identity provider does
not receive or manage that Colliedesk session.

## Create a workspace (hosted cloud)

New to Colliedesk Cloud? Use the same sign-in form. When the
verified email is not an agent of any workspace, the page asks you to name a workspace and starts a
14-day free trial with you as its admin. No card is needed. See [Plan and billing](billing.md).
A workspace is your team's account in Colliedesk: one name, such as Acme Support, for everyone who
answers your customers. Under the form, a line links the [Terms](https://colliedesk.com/terms), the
[Privacy Policy](https://colliedesk.com/privacy) and the [pricing](https://colliedesk.com/pricing):
continuing means you agree to the terms.
The website's **Start free trial** link opens this page as `/sign-in?intent=trial`, titled
**Start your free trial**; it is the same form, so someone who already has a workspace is
simply signed in. The Team 25 card links to `/sign-in?intent=trial&plan=team25`: the trial starts as
usual, and [Plan and billing](billing.md) then shows Team 25 first. No other `plan` value does anything.

The same page asks three short questions, each with a fixed list of answers: what you want
Colliedesk to do for you first, your role, and where you heard of Colliedesk. Your first goal decides
which step the [setup guide](setup-guide.md) in the inbox lists and recommends first (for example **Import from Freshdesk** when you are moving
off a helpdesk), and the answers help us send you emails that fit. The web form needs an answer to
each; for API clients `goal`, `role` and `source` are optional.

- **This email already started a trial.** Each mailbox gets one trial. Sign in to that workspace
  instead (**Sign in to your workspace**), or ask us for help.
- **Too many workspaces from this network.** At most 3 a day per network address. Try again tomorrow.
- **This name is not available.** Some names of well-known brands are reserved. Choose another
  name, or contact us if it is your own brand.
- **The name must be 2 to 60 characters.**
- **Use another account** signs the identity out so you can pick a different email.

On an instance without sign-up, an email that is not an agent is told to ask a workspace admin for
an invite link.

Self-hosted instances without cloud configuration behave exactly as before and do not load the cloud
sign-in code. One-time pairing and invite links remain available when cloud sign-in is enabled.
