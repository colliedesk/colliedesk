# Workspace name

The workspace name is the label your team sees in Colliedesk: in the workspace switcher when you
sign in, and in **Settings**. It starts as the name given when the workspace was created.

Replies never carry the workspace name. They go out with the
[sender name](email-channel.md#sender-name) of the email channel, and renaming the workspace does not
change it. Customers see the workspace name in one place: it is the title of your
[help center](knowledge-base.md#help-center-settings) while the help center has no title of its own.

## Settings pages

**Settings** has one page per item of its menu, each at its own address. The menu shows only the pages
you can use:

- **Account** (`/settings/account`): your profile and timezone, your preferences, where you are signed
  in, and the AI apps you connected.
- **General** (`/settings/general`): the workspace name and timezone, and conversation numbers.
- **Members** (`/settings/members`): the team, invites and roles.
- **Channels**, **Response targets**, **AI & agents** (the built-in AI, agent tokens and their
  sessions) and **Saved replies**.
- **Data** (`/settings/data`): import from Freshdesk and export all data.
- **Plan and billing**, on Colliedesk Cloud.

While you are in Settings, its menu takes the place of the sidebar, under **Back to app**. The sheet shows
the page alone. **Back to app** returns to the page you were on before Settings: the conversation you
had open, or the inbox when there was none. **Esc** does the same when nothing on the page has the focus
or when the focus is in the Settings menu. When the focus is in a page's own controls, or something is open
(a dialog, a menu, the account menu, a role picker), Esc only closes that, so nothing you type is lost. You can still drag the sidebar's edge to
resize it, or fold it: folded, it keeps **Back to app** as an arrow, and the menu comes back when you
open it again.

The help center's settings are in the knowledge base, under **Knowledge base → Settings**, which is not a
Settings page and keeps the app's sidebar. An old link to a section of Settings, such as
`/settings#invites-heading`, opens the page that holds it now. On a phone, **Settings** shows the menu;
open a page from it, and **‹ Settings** above its title goes back.

## Rename the workspace

Only people with `workspace:update` (the Admin role) can rename the workspace.

1. Open **Settings → General**.
2. Type the new name in **Workspace name**.
3. Select **Save workspace name**. "Saved" appears next to the button.

Everyone sees the new name the next time a screen loads. A name is 2 to 60 characters, as at
sign-up. Spaces at the ends are removed. Line breaks and other control characters are refused. On
Colliedesk Cloud a name with a well-known brand in it (for example "PayPal Support") is refused;
contact us if it is your own brand.

The workspace address (the slug in `support@<address>.colliedesk.com`, the desk address and the help
center address) is separate. It is changed under **Workspace address name** in
[Email channel](email-channel.md#2-forward-its-mail-to-colliedesk).

## The workspace address

On Colliedesk Cloud your team works at the workspace's own address, `https://<address>.colliedesk.com`:
the inbox and every page are there, and the help center is at `/help` on it. Bookmark it.

- **Signing in.** Sign in on your workspace's address. If your email is not an agent of that
  workspace, the page says so and links to `https://app.colliedesk.com`, where you find your workspace.
- **app.colliedesk.com** is where a new team signs up and where you find your workspace: sign in there,
  choose the workspace if you are in several, and the browser continues on its address, signed in.
  An old bookmark of `app.colliedesk.com` opens the same page on your workspace's address.
- **One workspace per address.** A sign-in belongs to the address it was made on. Signed in to two
  workspaces, you have one tab per address; one never switches the other.
- **Links Colliedesk sends you** (invites, emails, payment and connection returns) open your workspace's
  address.
- **Changing the workspace address** moves the desk to the new address, and the browser that changed it
  follows, still signed in. Everyone else signs in again on the new address.

A server you run yourself works the same way when it sets
[`HELP_CENTER_DOMAIN`](../self-host/running-behind-proxy.md#workspace-host-names); without it everything
stays on the server's one address.

## API and AI agents

- `PATCH /api/v1/workspace/name` with `{"name": "Acme Support Team"}` renames the workspace. It needs
  `workspace:update` (the Admin role has it), from a person or an agent token; without it the answer is
  403 `permission_missing`.
- An AI agent uses `colliedesk_workspace_rename` (`workspace:update`), and changes the address with
  `colliedesk_workspace_slug_change`. A token created before roles cannot; it needs a new token.
