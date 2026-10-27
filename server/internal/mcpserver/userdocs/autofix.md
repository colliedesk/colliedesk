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
before it joins the workspace, and forgets that authorization at once.

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
- turn on **Send internal notes with fixes** (off by default): the person sending a fix may then pick
  internal notes to include. A note an AI agent wrote is never sent;
- **Disconnect** it: fixes in progress on it are cancelled; its fixes keep the repository's name.

A repository is **paused** (it takes no new fix) when it turned public, when it was removed from the
installation on GitHub, or while the installation is suspended. It resumes by itself when the cause
goes away: private again, added back, unsuspended.

### 3. Coding agents

Turn on the agents your team uses; each card says what to set up on the vendor's side and asks you to
agree to the data it receives.

## AI agents

An agent token can read the settings (`autofix_settings:read`, `read` tier) and, at the `admin` tier
(`autofix_settings:update`), connect and disconnect repositories, set their coding agent, stop notes
going with fixes and disconnect GitHub. Installing the App and letting notes go with fixes always wait
for a person's approval: the person who approves installs the App as themselves. See the tools in
[Connect an AI agent (MCP)](mcp.md).
