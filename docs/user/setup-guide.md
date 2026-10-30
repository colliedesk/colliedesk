# Setup guide

The setup guide sits in the bottom-left corner of the inbox and shows what is left before your team
can receive and answer customer email. It stays in the inbox, also when conversations exist, until
every step is done or you hide it.

Heading: **Finish setting up**, with how many steps are done (for example "1 of 4") and a progress bar.
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
- Where you are ("Step 2 of 4") and the step's title. The step your sign-up goal recommends is marked
  **Recommended**.
- The step itself, the same form as in Settings:
  - **Receive customer email**: your support address and how its mail reaches Colliedesk. After you
    save the address, the setup continues on this screen.
  - **Import from Freshdesk**: the [import](import-freshdesk.md), without leaving the flow.
  - **Invite your team**: create an invite. Its one-time link stays on screen until you leave the step.
  - **Answer your first email**: **Send yourself a test message** opens a conversation from your own
    address. Answer it, and the reply is emailed to you. Without a mailbox yet, replies start once email
    or an import brings conversations.
- A main button that names the step it opens (for example **Next: Import from Freshdesk**, and
  **Finish setup** on the last step), **I'll do this later** and **Back**. The email setup has a
  **Continue** of its own: it moves between the parts of that setup only.

The main button works once your own part of the step is done, as the server reports it: the address saved
and able to receive ("Waiting for the first email"), an import read or running, an invite sent, a reply
sent. Until then, a line under the step says what is missing. What others do next (the first customer
email, a teammate joining) does not hold you back. The screen checks again every 5 seconds.

**I'll do this later** goes to the next step without waiting. After the last step, a summary lists every
step with where it stands (Done, its waiting state, or To do). Each step that is not done links back
to its screen. **Open the inbox** ends the flow.

On a phone the flow is the same single column, and every button is large enough to tap.

## Who sees it

- **Admins** see all four steps.
- **Agents** see only the steps they can take, in the guide and in the setup flow.
- **Hide** removes the guide from your inbox only. Other team members keep their own choice.
  To bring it back, open **Preferences** and turn on **Show the setup guide**.

The guide refreshes when the inbox changes; it does not poll on its own.

## Brand-new inbox

In an empty inbox without a mailbox the guide opens on its own and replaces the old list of buttons. **Send yourself a
test message** stays: it puts a first conversation in the inbox so you can answer it.

## API and AI agents

- `GET /api/v1/workspace/setup` returns the steps with their states (`email`: `none`, `waiting`,
  `working`, `needs_attention`; `import`: `none`, `running`, `done`; `invite`: `alone`, `invited`,
  `joined`; `first_reply`: `none`, `sent`), `complete`, and the `recommended` step. The MCP tool is
  `colliedesk_setup_progress_get`.
- `PUT /api/v1/agents/me/setup-guide` with `{"hidden": true}` or `{"hidden": false}` hides or shows
  the guide for the caller. The MCP tool is `colliedesk_setup_guide_set`. `GET /api/v1/auth/me`
  returns `setup_guide_hidden`.

The web app reports two [usage events](telemetry.md) about it: `onboarding_step_started` and
`onboarding_step_completed`, each with the step name only.
