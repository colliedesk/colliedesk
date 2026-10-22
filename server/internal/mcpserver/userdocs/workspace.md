# Workspace name

The workspace name is the label your team sees in Colliedesk: in the workspace switcher when you
sign in, and in **Settings**. It starts as the name given when the workspace was created.

Replies never carry the workspace name. They go out with the
[sender name](email-channel.md#sender-name) of the email channel, and renaming the workspace does not
change it. Customers see the workspace name in one place: it is the title of your
[help center](knowledge-base.md#help-center-settings) while the help center has no title of its own.

## Rename the workspace

Only admins can rename the workspace.

1. Open **Settings → General**.
2. Type the new name in **Workspace name**.
3. Select **Save workspace name**. "Saved" appears next to the button.

Everyone sees the new name the next time a screen loads. A name is 2 to 60 characters, as at
sign-up. Spaces at the ends are removed. Line breaks and other control characters are refused. On
Colliedesk Cloud a name with a well-known brand in it (for example "PayPal Support") is refused;
contact us if it is your own brand.

The workspace address (the slug in `support@<address>.colliedesk.com` and the help center address)
is separate. It is changed under **Workspace address name** in
[Email channel](email-channel.md#colliedesk-cloud-forward-to-your-address).

## API and AI agents

- `PATCH /api/v1/workspace/name` with `{"name": "Acme Support Team"}` renames the workspace. It needs
  an admin session or an admin agent token; an agent gets 403.
- An AI agent uses `colliedesk_workspace_rename` (admin tier). With a lower tier, Colliedesk files it as
  an [agent request](agent-requests.md) for an admin to approve.
