# Workspace name

The workspace name is the label your team sees in Colliedesk: in the workspace switcher when you
sign in, and in **Settings**. It starts as the name given when the workspace was created.

Customers never see the workspace name. Replies go out with the
[sender name](email-channel.md#sender-name) of the email channel, and renaming the workspace does not
change it. Give a trial or test workspace any name you like.

## Rename the workspace

Only admins can rename the workspace.

1. Open **Settings → General**.
2. Type the new name in **Workspace name**.
3. Select **Save workspace name**. "Saved" appears next to the button.

Everyone sees the new name the next time a screen loads. A name is 1 to 100 characters. Spaces at
the ends are removed. Line breaks and other control characters are refused.

The workspace address (the slug in `support@<address>.colliedesk.com` and the help center address)
is separate. It is changed under **Workspace address name** in
[Email channel](email-channel.md#colliedesk-cloud-forward-to-your-address).

## API and AI agents

- `PATCH /api/v1/workspace/name` with `{"name": "Acme Support Team"}` renames the workspace. It needs
  an admin session or an admin agent token; an agent gets 403.
- An AI agent uses `colliedesk_workspace_rename` (admin tier). With a lower tier, Colliedesk files it as
  an [agent request](agent-requests.md) for an admin to approve.
