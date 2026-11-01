# Setup guide

The setup guide sits in the bottom-left corner of the inbox and shows what is left before your team
can receive and answer customer email, or finish moving from Freshdesk. It stays in the inbox, also when conversations exist, until
every step is done or you hide it. If you skip domain steps, the folded guide remains available
so you can restore them later.

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

## Moving from Freshdesk

A workspace that chose **Move off Freshdesk** sees six steps instead of the four email setup steps:
import, preview, TXT verification, plan, CNAME and Search Console. Follow
[Finish the move](import-freshdesk.md#finish-the-move) for the full order and what each step checks.

The import text says the knowledge base and images come along and old article links keep working.
The next link names any prerequisite: import when there are no public articles, or turn the help
center on before opening its preview. Confirm the preview after checking it. TXT verification and
a successful custom-domain address check complete their steps automatically; paying completes the
plan step. Search Console is done only after you confirm preserving verification and submitting
the sitemap there. When a Google verification tag is already present, the guide says so. Saving a verification token
alone does not complete the step.

**No custom domain needed** skips TXT, plan, CNAME and Search Console. **Use a custom domain**
restores skipped steps while the guide is open. Confirmed milestones never turn back into undone
steps, even if you later remove the domain or turn off the help center.

## Who sees it

- **Admins** see all the steps for their workspace’s goal.
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
  `joined`; `first_reply`: `none`, `sent`), `complete`, the `recommended` step and `next_url`. Each step carries its `url`. Move steps use `none`, `done`, or `skipped` (import can also be `running`). The MCP tool is
  `colliedesk_setup_progress_get`.
- `PUT /api/v1/workspace/setup/move` with `step` (`preview`, `custom_domain`, `search_console`) and
  `state` confirms or skips a move step. Use `done` for preview or Search Console, `skipped` for
  custom_domain, or `none` to restore skipped domain steps. The MCP tool is
  `colliedesk_setup_move_step_set` (admin; lower-tier tokens file an approval request). It returns
  all the steps and the next URL. Automatic import, payment and domain milestones cannot be
  manually marked done. The server refuses missing prerequisites with an explanation.
- `PUT /api/v1/agents/me/setup-guide` with `{"hidden": true}` or `{"hidden": false}` hides or shows
  the guide for the caller. The MCP tool is `colliedesk_setup_guide_set`. `GET /api/v1/auth/me`
  returns `setup_guide_hidden`.

The web app reports two [usage events](telemetry.md) about it: `onboarding_step_started` and
`onboarding_step_completed`, each with the step name only.
