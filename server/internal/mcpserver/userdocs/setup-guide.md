# Setup guide

The setup guide sits in the bottom-left corner of the inbox and shows what is left before your team
can receive and answer customer email. It stays in the inbox, also when conversations exist, until
every step is done or you hide it.

Heading: **Finish setting up**, with how many steps are done (for example "1 of 4") and a progress bar.

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

| Step | Done when | Opens |
|---|---|---|
| **Receive customer email** | The email channel works: a customer message arrived. While nothing has arrived yet, the step reads "Waiting for the first email". | [Email channel](email-channel.md) |
| **Import from Freshdesk** | An import finished and was not undone. While one runs, the step reads "Importing". | [Import from Freshdesk](import-freshdesk.md) |
| **Invite your team** | A teammate joined. An invite that was sent but not used yet reads "Invited". | [Invites](invites.md) |
| **Answer your first email** | A reply was delivered to a customer. A note does not count, and neither does a reply marked "Delivery failed". The reply to your own test message counts. | The inbox |

The step that fits your sign-up goal comes first and is marked as the recommended one. The row
you should do next has the main button.

## Who sees it

- **Admins** see all four steps.
- **Agents** see only the steps they can take.
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
