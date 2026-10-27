# Autofix: send a conversation to a coding agent

When a conversation is a bug, Autofix sends it to a coding agent your team already uses, on a private
GitHub repository, and follows it on the conversation until the fix is released: the agent works,
opens a pull request, a person merges it, and you mark it released and answer the customer.

Colliedesk runs no agent and keeps no model key. Claude and Copilot work through a GitHub issue that
Colliedesk creates as you; Cursor works through its API with a key an admin sets. Three steps are
always a person's: sending a conversation, merging the pull request, and replying to the customer.

On a self-hosted server Autofix appears once the operator has set up its GitHub App (see
[Autofix: the GitHub App](../self-host/github-app.md)).

## Set up: Settings → Autofix

An admin sets Autofix up in three steps; everyone else sees them. Every step can be undone.

### 1. Connect GitHub

**Connect GitHub** opens GitHub to install the Colliedesk GitHub App on your account or organization.
Choose **Only select repositories** and pick the repositories fixes may go to. GitHub then sends you
back to Colliedesk, which checks with your own GitHub authorization that the installation is yours
before it joins the workspace, and forgets that authorization at once. You must administer every
repository the installation reaches: in an organization, an owner connects it (a member who only
reaches some of its repositories is told the installation is not theirs).

The App asks for three permissions on the repositories you pick, and nothing more:

| Permission | Why |
|---|---|
| Metadata: read | See the repository and whether it is private. |
| Issues: read and write | Read the issues fixes create (Claude, Copilot) and close one when you cancel a fix. |
| Pull requests: read | Follow the agent's pull request to its merge. |

It cannot read or write your code.

- **Blocked: the installation reaches every repository.** If you chose **All repositories**, the
  installation is kept but blocked: no repository can be connected. Change it to **Only select
  repositories** on GitHub (**Change repositories on GitHub**); Colliedesk unblocks it by itself.
- **Requested.** In an organization where you are not an owner, GitHub asks the owners to install
  the App. Once they did, press **Connect GitHub** again.
- **Disconnect** uninstalls the App from that account on GitHub. The repositories stay in the list,
  paused, so their fixes still show where they went; fixes in progress on them are cancelled. Connect
  GitHub again to bring them back.
- A GitHub installation belongs to one Colliedesk workspace.

### 2. Connect repositories

The list shows the repositories the installation reaches. Only **private** repositories can be
connected: a fix's issue holds what your customer wrote, and on a public or internal repository it
would be visible to people outside your team. Colliedesk reads the visibility from GitHub when you
connect, before every fix, and whenever GitHub reports a change.

For each connected repository you can:

- choose the **coding agent** a new fix on it suggests;
- turn on **Private notes may be sent** (off by default; Colliedesk asks you to confirm): the person
  sending a fix may then pick internal notes to include. A note an AI agent wrote is never sent;
- **Disconnect** it: fixes in progress on it are cancelled; its fixes keep the repository's name.
  Colliedesk stops their Cursor runs (a run it cannot stop is shown, with where to stop it). The
  issues of Claude and Copilot fixes stay open, and an agent that started may still run: each fix links
  its issue to stop it on GitHub.

A repository is **paused** (it takes no new fix) when it turned public, when it was removed from the
installation on GitHub, or while the installation is suspended. It resumes by itself when the cause
goes away: private again, added back, unsuspended.

### 3. Coding agents

Turn on the agents your team uses; each card says what to set up on the vendor's side and asks you to
agree to the data it receives.

Claude and Copilot cards list what to do on GitHub first (Claude: install Anthropic's GitHub App, add
the `ANTHROPIC_API_KEY` secret and the workflow; Copilot: a plan with the coding agent, turned on for the
repositories). Colliedesk cannot see those from its App, so you tick them yourself; the ticks are kept in
this browser only. **Check** reads what Colliedesk already knows, without asking GitHub: an installation
is active, and the repositories taking fixes were private when last read (Colliedesk reads each one again
on GitHub just before it sends a fix).

## Send a conversation to a coding agent

Autofix sits at the top of a conversation's details (on a narrow screen, a line under the conversation's
title opens them). **Send to agent** opens a preview. Pick the repository and the agent (only
agents an admin turned on are offered). Colliedesk builds what will be sent, on the server, and you can
edit it before sending:

- **Masked.** Email addresses, phone and card numbers, and strings that look like keys, tokens or
  passwords (also in links) are replaced with `[email removed]`, `[phone removed]`, `[card removed]` or
  `[secret removed]`. The preview counts what was taken out.
- **Mentions and references made inert.** An `@name`, a `#123` or a `GH-123` from the conversation gets
  an invisible character (U+200B) after its `@`, `#` or `GH-`: it reads the same, but GitHub pings nobody,
  links no other issue, and the repository's `@claude` workflow does not start on the customer's words.
  What you edit is masked again when you send, so a mention you type stays inert too.
- **The customer's words are marked as untrusted.** Each customer message sits in a
  `<customer-message untrusted="true">` block that tells the agent it is data, not instructions.
- **Internal notes stay out** unless the repository allows them; then you pick which ones go. A note an
  AI agent wrote is never offered.
- **Files are never sent**; the preview says how many stay behind.

The bottom of the preview says where it goes, and **Continue on GitHub** or **Send to Cursor** sends it. For **Claude** and **Copilot**: *an issue in
`owner/repo` created with your GitHub account; GitHub opens to confirm*. Sending takes your browser to
GitHub, which asks you to authorize the Colliedesk App (it may skip the question once you did), then
back to the conversation, where the fix shows **Sending** and then **Sent**. In between, Colliedesk:

1. checks with your authorization that you can write to the repository (the agents need it from the
   person who starts them) and, for Copilot, that you can assign Copilot on it;
2. checks again that the repository is private;
3. creates the issue as you, then comments `@claude` on it (Claude), or assigns it to Copilot;
4. revokes your authorization. It is never stored; it lives a minute at most in the server's memory.

If a check fails, nothing is created on GitHub and the fix says why (see below). If you close GitHub
without confirming, the fix ends by itself after 15 to 25 minutes (*authorization abandoned*). After two
minutes the fix offers **Retry**, which ends it and sends it again with the GitHub account of whoever
presses it (an approver of an AI agent's request cannot reopen the confirmation), or **Cancel**. One fix of a conversation is in progress at a time.

**Copilot** is assigned from Colliedesk when GitHub allows it. When GitHub refuses the assignment, the
issue is still created and the fix says **Assign to Copilot on GitHub**: assign it there and the fix moves
to *Agent running* once GitHub reports it.

### Follow a fix

The fix moves on by itself from GitHub's events (and a check every ten minutes for a server GitHub
cannot reach). Its track shows **sent → agent → PR → merged → released**; a stop the fix went past
without a sign (a pull request with no sign the agent ran) is striped. A fix sent 30 minutes ago with no
word from the agent turns amber with a list of what to check on GitHub. Then:

- **Mark as released** when the merge is deployed (*Released*); **Unmark released** if it was rolled back.
  Once released, **Draft reply** puts a reply in the reply box: an AI draft written from the outcome
  (nothing the agent wrote is used), or a plain sentence when AI drafts are off.
- **Link a PR** when Colliedesk did not find the agent's one (it must be a pull request of
  the fix's repository); **Unlink PR** undoes one you linked.
- **Cancel** a fix in progress: its issue is closed with a comment. An agent already running may still
  finish on GitHub, so stop it there (the fix links the issue); a pull request already open stays open.
  If GitHub did not close the issue, the fix's timeline says so: close it on GitHub.
- **Retry** after a fix failed or was cancelled: a new attempt, with the preview filled in again (**Send
  again** when nothing had left Colliedesk).

Why a fix ended:

| Reason | What happened | What to do |
|---|---|---|
| No repository access | Your GitHub account cannot write to the repository, or the App no longer reaches it | Ask a repository admin for write access, or check the installation |
| Copilot unavailable | Copilot's coding agent cannot be assigned on that repository by your account | Turn Copilot's coding agent on for the repository on GitHub |
| Repository public | The repository is no longer private | Make it private; it resumes by itself |
| Interrupted | The server stopped while sending and found no issue | Send again |
| Authorization abandoned / denied | You did not confirm on GitHub, or said no | Send again |
| Agent turned off | An admin turned the agent off before the fix left | Ask an admin, then send again |

## Fixes

**Fixes** in the navigation (shown once a repository is connected) lists every fix in three groups:
**Needs a person** (failed, merged but not released, or no word from the agent), **Running** and **Done**.
Filter by repository and agent. The badge counts the fixes that need a person.

## Requests from AI agents

An AI agent's token cannot send a fix, install the app, allow notes or turn an agent on by itself: it files
a request. The request says in words what approving starts. Approving a fix that goes through GitHub takes
you to GitHub and you become the sender. Turning an agent on is approved in the request itself, after you
tick the consent; if the consent text changed since the agent asked, reject it and turn the agent on in
Settings → Autofix.


## AI agents

An agent token can read the settings (`autofix_settings:read`, `read` tier) and, at the `admin` tier
(`autofix_settings:update`), connect and disconnect repositories, set their coding agent and stop notes
going with fixes. Installing the App, disconnecting GitHub and letting notes go with fixes always wait
for a person's approval: the person who approves installs the App as themselves. A `read` token previews a
fix (`autofix:read`); a `draft` token cancels a fix, marks or takes back its release and links or
unlinks its pull request (`autofix:update`). Sending and retrying a fix always wait for a person's
approval (`autofix:send`): the person who approves confirms on GitHub, and the issue is created with
their GitHub account; the agent never sees the confirmation address. See the tools in
[Connect an AI agent (MCP)](mcp.md).

## Cursor

Cursor takes a fix through its API with the workspace's own Cursor key. It needs the Colliedesk GitHub
App on the repository like the other agents: Colliedesk checks the repository is private right before
each fix leaves, and follows the pull request Cursor opens to its merge.

**Set it up** (Settings → Autofix → Cursor, admins only):

1. Create a Cursor API key. A [service account key](https://cursor.com/docs/account/enterprise/service-accounts)
   is advised: a user key acts with all of its owner's rights on every repository they reach.
2. Paste it in **Cursor API key**. Colliedesk tries it with Cursor before keeping it; a key Cursor refuses
   is not kept. The key is sealed on the server and never shown again, not one character: the card only
   says who set it and when. **Replace** sets another, **Remove** erases it.
3. Read what Cursor receives and agree to it, then turn Cursor on. Cursor keeps what it receives (the
   fix's title and text) for as long as its own retention says, indefinitely by default.
4. **Check** tries the key again and, when Cursor can say, that it reaches every connected repository
   (Cursor lists repositories once a minute, and never for a service account key: then the first fix
   shows it).

An agent token sets the key only with the admin tier (`autofix_settings:manage_credentials`): a lower
tier is refused, never filed as a request, because a request keeps what was sent. Turning Cursor on
always waits for a person to approve.

**What happens when you send a fix to Cursor:** it shows **Sending** until Colliedesk created the Cursor
agent (seconds), then **Sent** with a link to the agent on Cursor, **Agent running** while it works and
**PR open** once it opened its pull request. Colliedesk never sends the same fix twice, even when the
server restarts while sending.

| The fix says | Why | What to do |
| --- | --- | --- |
| Cursor refused the workspace key (`vendor_auth`) | Cursor refused the key, or no key is set | Set a working key, then retry |
| Cursor can’t reach this repository (`vendor_repo_access`) | Cursor cannot reach the repository | Give Cursor's GitHub integration access to it, then retry |
| The coding agent rejected the request (`vendor_rejected`) | Cursor refused the fix (the timeline says what it said) | Edit the text, then retry |
| GitHub or Cursor did not answer after 5 tries (`provider_unavailable`) | Cursor did not answer after 5 tries | Retry later |
| Cursor stopped with an error (`agent_error`) | Cursor's run failed or finished without a pull request | Read Cursor's reply on the timeline, then retry |
| Cursor stopped the run before it finished (`timed_out`) | Cursor's run expired | Retry |
| Not sent: an admin turned this agent off (`connector_disabled`) | Cursor was turned off (or its consent is to an older text) before the fix left | Nothing was sent; turn Cursor on again, then retry |
| Not sent: the repository is no longer private (`repo_public`), or can’t be reached (`no_repo_access`) | The repository is not private any more, or the App no longer reaches it | Nothing was sent; fix the repository on GitHub, then **Retry** |

**Cancel** stops the Cursor run. Removing the key while a run goes on does not stop it: Colliedesk
follows it again once a key is set.
