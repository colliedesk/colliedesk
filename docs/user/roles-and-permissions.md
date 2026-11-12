# Roles and permissions

What a person or an AI agent may do in Colliedesk is a list of **permissions**. A **role** is a named
set of permissions. Everyone on the team holds one role or more and gets every permission of each.
An AI agent's token acts for the person who created it and never holds more than that person.

## Permissions

A permission is `resource:action`: the resource is what you see in the app (conversations, the
knowledge base, the team), the action is `read`, `create`, `update` or `delete`, or a named action
where the risk is not editing: `send` (reaches a customer), `publish` (reaches the public help
center), `export` and `share` (data leaves the workspace), `purge` (no way back),
`manage_credentials` (another service's key), and `invite`, `revoke`, `manage_roles`,
`create_tokens` (people's access). Some permissions are marked as reaching outside the workspace:
they touch a customer, the public or another service.

There are 62 permissions. **Admin** holds them all; **Agent** holds the 42 below that are not marked
*Admin only*.

| Resource | Permissions |
|---|---|
| Conversations | `conversations:read`, `conversations:update` |
| Messages | `messages:read`, `messages:note` (private notes), `messages:send` (replies to customers) |
| AI assist | `ai_assist:read`, `ai_assist:create` (AI drafts, triage suggestions, and reply drafts an agent leaves for a person) |
| Contacts | `contacts:read`, `contacts:create`, `contacts:update` |
| Tags | `tags:read`, `tags:create`, `tags:update`, `tags:delete` |
| Saved replies | `saved_replies:read`, `saved_replies:create`, `saved_replies:update`, `saved_replies:delete` |
| Saved views | `saved_views:read`, `saved_views:create`, `saved_views:update`, `saved_views:delete` |
| Knowledge base articles | `kb_articles:read`, `kb_articles:create`, `kb_articles:update`, `kb_articles:delete`, `kb_articles:publish` |
| Knowledge base media | `kb_media:read`, `kb_media:create`, `kb_media:update`, `kb_media:delete`; Admin only: `kb_media:purge` |
| Help center | `help_center:read`; Admin only: `help_center:update` |
| Results | `results:read`; Admin only: `results:share` |
| Your profile | `profile:update` |
| Team | `team:read`, `team:create_tokens`; Admin only: `team:invite`, `team:revoke`, `team:manage_roles` |
| Channels | `channels:read`; Admin only: `channels:update`, `channels:manage_credentials` |
| Workspace | Admin only: `workspace:update`, `workspace:export` |
| Billing | Admin only: `billing:read`, `billing:manage` |
| Imports | Admin only: `imports:read`, `imports:run`, `imports:manage_credentials`, `imports:undo` |
| AI settings | `ai_settings:read`; Admin only: `ai_settings:update`, `ai_settings:manage_credentials` |
| Autofix | `autofix:read`, `autofix:update`, `autofix:send`, `autofix_settings:read`; Admin only: `autofix_settings:update`, `autofix_settings:manage_credentials` |

"Admin only" describes the two built-in roles; a role you make can hold any of them.
`kb_articles:publish` is needed for anything that changes the public help center: publishing an
article, editing, trashing or restoring a published one, and renaming, moving or deleting a
collection. Some things need no permission at all: signing out, seeing and revoking your own
sessions and tokens, and reporting a problem.

## Roles

**Settings → Members → Roles** lists the workspace's roles, how many permissions each holds and who holds it.
Every workspace has two built-in roles:

- **Admin** holds every permission, including ones added in later releases. It cannot be renamed,
  changed or deleted.
- **Agent** starts with the 42 permissions above. You can rename it and change its permissions, and
  **Reset to default** puts the original 42 back. It cannot be deleted: new invites start with it.
  A permission added in a later release does not join Agent by itself; the release notes say so.

To make a role, choose **New role**, give it a name (up to 60 characters) and an optional
description, and tick its permissions; **Start from** copies another role's permissions to begin
with, and **Duplicate** on a row does the same. **View only** ticks every `:read` permission and
**Clear all** unticks everything. You can give a role only permissions you hold yourself; the others
are shown locked.

Changing a role changes it for everyone who holds it and every token that follows it. When the role
is in use, **Save role** first shows what is added and removed and the people and tokens it reaches;
nothing changes until you choose **Save and apply**.

**Delete role** is at the end of a role's page, and only while no one holds it, no unused invite
gives it and no token follows it; otherwise the page names them. A deleted role is listed under
**Deleted roles**, where **Restore** brings it back.

## What a person with a narrow role sees

The app shows only what your roles let you read. The sidebar lists the areas you can open (Inbox
with `conversations:read`, Help center with `kb_articles:read`, or the media library with only
`kb_media:read`, Reports with `results:read`; Settings always). Its saved views need `saved_views:read`, its
channels `channels:read`, and **Invite teammates** `team:invite`. A role without `conversations:read`
opens on the first of those instead of the inbox. Opening a screen your roles do not cover says so:
"Your roles do not let you open …", names the permission it needs, and links the areas you can open.

In a conversation, **Reply** is shown only with `messages:send`, and **New conversation** (in the sidebar) only with
`messages:send`, `channels:read` and `contacts:read`. Without `messages:read` a conversation opens
but says its messages are not shown. Saved replies, saved views, knowledge base articles, tags and the
team list appear only with their `:read` permission. **Settings** shows only the sections you can read.

## Give people roles

In **Settings → Members → Team**, each person shows their roles. **Change roles** opens the list of roles:
tick the ones they should hold and choose **Apply**. Everyone needs at least one role. An invite
carries roles too: pick them in **Settings → Members → Invites** (Agent is selected); the person gets them when
they join, added to any roles they already hold. See [Invite your team](invites.md) and
[Manage your team](team.md).

Taking a role away from someone also stops every token of theirs that follows that role (the form
names them before you apply). Giving the role back starts those tokens again.

## Safety rules

These hold for people and tokens alike:

1. **A workspace always keeps one active Admin.** Taking Admin from the last active person who holds
   it, or deactivating them, is refused (`last_admin`).
2. **You cannot give more than you hold.** Assigning a role, inviting with a role, or making or
   changing a role needs every permission it holds (`role_exceeds_yours`). Changing roles needs
   `team:manage_roles`; inviting needs `team:invite`.
3. **You cannot change someone who holds more than you.** Changing their roles or display name,
   deactivating or reactivating them, or signing out their sessions is refused when they hold a
   permission you do not (`member_outranks_you`). An Admin can always do it.
4. **A role in use cannot be deleted** (`role_in_use` names the people, invites and tokens).

## AI agent tokens

Create a token in **Settings → AI & agents → External AI agents**. Creating a token is something only a person
does there; an AI agent cannot create one. Under **Access**, choose one of:

- **Follow a role**: pick any role whose permissions you all hold: your own roles, a narrower one,
  and for an Admin every role. The token gets that role's permissions as they are on each call:
  change the role and the token changes with it. If you stop holding every permission of the role
  (it is taken from you, or it gains one you lack), the token stops until you hold them again.
- **Pick permissions (fixed)**: tick the permissions the token gets, out of the ones you hold. The
  list is fixed when you create the token.

Either way, a token never does more than you can do right now: if you lose a permission, every token
of yours loses it too, and if you are deactivated, your tokens stop working. **Settings → AI & agents → Sessions**
shows each token as "Follows role …", "Fixed · N permissions", or "Stopped" with the reason. Revoke a
token there. More about tokens: [Sessions and devices](sessions.md#agent-tokens).

An AI agent does what its token's permissions allow, at once. Nothing waits for approval. An invite,
a new inbound email secret or an export link is returned to the agent so it can give it to you.
Connecting Gmail, Microsoft 365 or GitHub, and paying, still happen in your browser: the agent gets
the address to open, and only you can finish it, signed in to Colliedesk. A reply the agent may not
send can be left as a draft above the reply box (`ai_assist:create`); a person sends, edits or
discards it.

An agent finds out what it may do with `colliedesk_whoami`: your name and roles, how the token was
made, and its permissions.

### Tokens created before roles

A token created before roles keeps working, with the same permissions as before, except those that
used to wait for a person's approval: `results:share`, `team:invite`, `team:create_tokens`,
`team:manage_roles`, `team:revoke`, `channels:update`, `channels:manage_credentials`,
`workspace:update`, `workspace:export`, `billing:manage`, `imports:undo`, `ai_settings:update`,
`autofix:send` and `autofix_settings:update`. A call that needs one of them is refused; create a new
token for it. Sessions shows such a token as "Created before roles" with the tier it had.

## When a permission is missing

A call without the permission it needs is refused with HTTP 403 and the code
`permission_missing`. The answer names what is missing and what to do:

```json
{"error": {"code": "permission_missing",
  "message": "This agent token was not given messages:send. Long Bui holds it: create a token that includes it in Settings → External AI agents, then use that token.",
  "missing_permissions": ["messages:send"], "missing_from": "token"}}
```

- `missing_permissions` lists the permissions the call needs.
- `missing_from` is `token` when you hold the permission but this token was not given it (create a
  token that includes it), or `person` when you do not hold it.
- `ask`, with `missing_from: person`, names people who can change roles: ask one of them for a role
  that includes it.

An action only a person may do, such as creating a token, is refused for tokens with `person_only`
and says where in the app to do it. Through MCP the same message, `missing_permissions` and
`missing_from` come back in the tool's error.

## AI agents and the API

| What | API | MCP tool |
|---|---|---|
| List roles (with their permissions, members and tokens) | `GET /api/v1/roles` | `colliedesk_roles_list` |
| Make a role | `POST /api/v1/roles` | `colliedesk_role_create` |
| Change a role | `PATCH /api/v1/roles/{id}` | `colliedesk_role_update` |
| Delete a role | `DELETE /api/v1/roles/{id}` | `colliedesk_role_delete` |
| Reset Agent to its default | `POST /api/v1/roles/{id}/reset` | `colliedesk_role_reset` |
| List deleted roles, restore one | `GET /api/v1/roles?deleted=true`, `POST /api/v1/roles/{id}/restore` | `colliedesk_role_restore` |
| Set a person's roles | `PUT /api/v1/agents/{id}/roles` | `colliedesk_member_roles_set` |

Reading roles needs `team:read`; the rest needs `team:manage_roles`, and the safety rules above. The
MCP tools take a role or a person by name as well as by id. Roles are part of
[the export](export.md) (`roles.ndjson`, `agent-roles.ndjson`).
