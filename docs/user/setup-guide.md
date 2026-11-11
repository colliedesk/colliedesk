# Setup guide

The setup guide sits in the bottom-left corner of the inbox and shows what is left before your team
can receive and answer customer email. It stays in the inbox, also when conversations exist, until
the four core steps are done (receive email, import, invite, answer the first email) or you hide it.
**Turn on AI drafts** and **Set reply targets** are offers: their rows show where they stand, but
they never keep the guide open or the setup unfinished. A workspace that finished the four core steps
does not see the guide again.

Heading: **Finish setting up**, with how many steps are done (for example "1 of 6") and a progress bar.
Each step opens its screen of the [setup flow](#setup-flow).

## Open and folded

The guide floats over the foot of the conversation list, clear of the reply box and its **Send reply**
button on the right.

- **Folded**, it is one button: **Finish setting up** with a progress ring and the count. Press it to
  open the steps.
- **Open**, it shows every step. **Minimize** (the arrow in its header) or **Esc** folds it again.
- A new workspace with no conversation and no mailbox yet opens the guide on its own: there is
  nothing for it to cover. Once a mailbox or a conversation exists it starts folded, so it never
  covers **Send yourself a test message** or a conversation.
- Your browser remembers whether you left it open or folded. This is separate from **Hide**.
- On a phone the guide shows on the conversation list and steps aside while a conversation is open.

| Step | Done when | Opens | More |
|---|---|---|---|
| **Receive customer email** | The email channel works: a customer message arrived. While nothing has arrived yet, the step reads "Waiting for the first email". | `/setup/email` | [Email channel](email-channel.md) |
| **Import from Freshdesk** | An import finished and was not undone. While one runs, the step reads "Importing". | `/setup/import` | [Import from Freshdesk](import-freshdesk.md) |
| **Turn on AI drafts** | Reply drafts are on and write a draft for each new customer message (Default for channels: Suggest). Without built-in AI on the server and no provider of your own, the step reads "Nothing to do: no built-in AI on this server" and is not counted. | `/setup/ai_drafts` | [Built-in AI](ai.md) |
| **Set reply targets** | Response targets are on. | `/setup/reply_targets` | [Response targets](response-targets.md) |
| **Invite your team** | A teammate joined. An invite that was sent but not used yet reads "Invite sent". | `/setup/invite` | [Invites](invites.md) |
| **Answer your first email** | A reply was delivered to a customer. A note does not count, and neither does a reply marked "Delivery failed". The reply to your own test message counts. | `/setup/first_reply` | The inbox |

The step that fits your sign-up goal comes first and is marked as the recommended one. The row
you should do next has the main button.

## Setup flow

The setup flow shows one step per screen, full screen, and you do the step on that screen. A new team
that signs up on Colliedesk Cloud (without choosing a plan first) starts here. Open it any time at
`/setup`: it goes to the first step whose part is not done yet.

Each screen shows:

- At the top: your workspace name and the address you are signed in with, and **Exit setup** (the
  cross on the right), which opens the inbox.
- Where you are ("Step 3 of 6 · AI drafts") and the step's title. The step your sign-up goal recommends is marked
  **Recommended**.
- The step itself, the same form as in Settings:
  - **Receive customer email**: your support address and how its mail reaches Colliedesk. After you
    save the address, the setup continues on this screen. Mail always arrives by forwarding. To send
    replies from your own mailbox, **Connect Gmail** or **Connect Microsoft 365** signs in with
    Google or Microsoft (permission to send only) and brings you back to this step, which says
    whether it worked. When it did not, it says why; a Microsoft 365 account that needs its admin's
    approval gets the approval link to send them.
  - **Import from Freshdesk**: the [import](import-freshdesk.md), without leaving the flow.
  - **Turn on AI drafts** ("Let AI draft your replies"): an example of a conversation in Today with
    its draft ready (only a picture: its buttons do nothing), how drafts work, what is sent to the AI
    provider and what it costs. The numbers are the server's estimate, as in Settings → Built-in AI:
    about how many automatic drafts half of your plan's included AI covers this month, and at most how many
    a day. What is sent and what it costs name the provider the drafts will actually use. **Turn on AI
    drafts** makes the same call as Settings → Built-in AI: Reply drafts with **Default for channels** set
    to **Suggest**, keeping the provider, model and effort the feature already has; when it has none, the
    server's built-in AI if it offers drafts, else your own provider. It records your consent, as Settings →
    Built-in AI does (History). Triage suggestions are not turned on here. **Turn off** sets **Default for
    channels** back to **Off**, as Settings does; drafts on request stay as they were. When the AI settings
    cannot be read, the step says why, with **Try again**. With no import and no published help center article, a note says drafts have little to read
    yet; turning on still works. On a server without built-in AI, **Add a provider** opens Settings →
    Built-in AI in a new tab, and you can skip the step.
  - **Set reply targets** ("Choose when a reply is due"): three presets side by side, each with the
    reply and resolve target of every priority. A day is one business day of the hours the preset applies
    with: 9 hours for 09:00 to 18:00, 12 hours for 08:00 to 20:00. When days differ, it is the length most
    open days have (the longest on a tie). The table below is in 09:00 to 18:00 days.

    | Preset | Urgent | High | Normal | Low |
    |---|---|---|---|---|
    | **Within the hour** | 15 min / 2 h | 30 min / 4 h | 1 h / 1 day | 4 h / 2 days |
    | **Same day** | 1 h / 4 h | 2 h / 1 day | 8 h / 3 days | 1 day / 5 days |
    | **Next business day** | 4 h / 1 day | 1 day / 2 days | 1 day / 3 days | 2 days / 5 days |

    The preset that fits is chosen for you. It comes from the median first reply of the conversations you
    imported that were opened in the last 30 days: up to 1 hour is **Within the hour**, up to 8 hours
    **Same day**, longer **Next business day**, tagged "Fits your history". With no such history, or while
    the import still runs, **Same day** is tagged "Suggested". The main button names the preset, for
    example **Use Same day**. It saves the targets, turns them on with business hours Monday to Friday,
    09:00 to 18:00 in the workspace's timezone (your own hours, when Settings already has some: a preset
    never replaces them), sets **Due soon** to 30 minutes, and opens the next step. The example rows of
    Today and the worked example count in those same hours. The choice follows the suggestion, for
    example when an import ends, until you pick a preset yourself. When someone else saved the targets in
    between, the save is refused, the step reads them again, and the next press saves. Targets start with
    the next new conversation.
    Change any of it later in Settings → [Response targets](response-targets.md).
  - **Invite your team**: create an invite. Its one-time link stays on screen until you leave the step.
  - **Answer your first email**: **Send yourself a test message** opens a conversation from your own
    address. Answer it, and the reply is emailed to you. **Open the inbox** is there too: conversations
    come in with your email or your import, and you answer one from the inbox.
- A main button that names the step it opens (for example **Next: Import from Freshdesk**, and
  **Finish setup** on the last step), **I'll do this later** and **Back**. On the AI drafts and reply
  targets steps the main button does the step (**Turn on AI drafts**, **Use Same day**). The email setup has a
  **Continue** of its own: it moves between the parts of that setup only.

The main button works once your own part of the step is done, as the server reports it: the address saved
and able to receive ("Waiting for the first email"), an import read or running, an invite sent, a reply
sent. Until then, a line under the step says what is missing. What others do next (the first customer
email, a teammate joining) does not hold you back. The screen checks again every 5 seconds.

**I'll do this later** goes to the next step without waiting. After the last step, a summary lists every
step with where it stands (Done, its waiting state, or To do). Each step that is not done links back
to its screen. **Open the inbox** ends the flow.

On a phone the flow is the same single column, and every button is large enough to tap. On the AI drafts
and reply targets steps the buttons stay pinned at the bottom of the screen, and the presets stack.

## Who sees it

- **Admins** see all six steps.
- **Everyone else** sees only the steps their roles can take, in the guide and in the setup flow:
  email and the import need `channels:update`, AI drafts `ai_settings:update`, reply targets
  `workspace:update`, inviting `team:invite` and `team:read` (to list the invites and the roles), as in
  Settings. **Revoke** on a pending invite needs `team:revoke`.
  Answering the first email is everyone's step.
- **Hide** removes the guide from your inbox only. Other team members keep their own choice.
  To bring it back, open **Preferences** and turn on **Show the setup guide**.

The guide refreshes when the inbox changes; it does not poll on its own.

## Brand-new inbox

In an empty inbox without a mailbox the guide opens on its own and replaces the old list of buttons. **Send yourself a
test message** stays: it puts a first conversation in the inbox so you can answer it.

## API and AI agents

- `GET /api/v1/workspace/setup` returns the steps with their names and states (`email`: `none`,
  `waiting`, `working`, `needs_attention`; `import`: `none`, `running`, `done`; `ai_drafts`: `none`,
  `on`, `nothing_to_read`, `unavailable`; `reply_targets`: `none`, `on`; `invite`: `alone`, `invited`,
  `joined`; `first_reply`: `none`, `sent`), `complete` (the four core steps only), the `recommended`
  step, the reply targets presets (`reply_target_presets`, in business minutes), the hours they apply
  with (`reply_target_hours`: `kept` when they are the workspace's own, `day_minutes`, `hours`) and the
  one that fits (`reply_target_suggestion`). The suggestion is `null` when reply targets are on or the
  caller may not change them (`workspace:update`): the median is read only when someone can use it.
  The MCP tool is `colliedesk_setup_progress_get`.
- An AI agent does the two new steps with tools that already exist: `colliedesk_ai_slot_set` (slot
  `draft`, enabled, `default_level` `suggest`, with source `server` when the server's built-in AI offers
  drafts, else source `provider` with the `provider_id` of one of the workspace's providers; a slot that
  already has a source keeps it) and `colliedesk_response_targets_set` with a preset's targets, enabled,
  `warn_before_minutes` 30 and the hours of `reply_target_hours`.
- `PUT /api/v1/agents/me/setup-guide` with `{"hidden": true}` or `{"hidden": false}` hides or shows
  the guide for the caller. The MCP tool is `colliedesk_setup_guide_set`. `GET /api/v1/auth/me`
  returns `setup_guide_hidden`.

The web app reports two [usage events](telemetry.md) about it: `onboarding_step_started` and
`onboarding_step_completed`, each with the step name only.
