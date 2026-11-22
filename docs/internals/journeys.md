# Journeys

End-to-end tests of what people do in Colliedesk. They run the real server, CLI, PostgreSQL and a
browser; only external services are local stand-ins (Freshdesk API, identity provider JWKS, Mailpit as
the SMTP relay). The rules are in AGENTS.md (`## Journeys`). `corepack pnpm test` fails when a list
below and the tests carrying its tag in `web/e2e/` differ (`web/scripts/check-journeys.mjs`).

## Onboarding journeys

Customers leaving Freshdesk judge Colliedesk in their first hour. Tag `@onboarding`; CI jobs
`onboarding` and `install-smoke`.

- `self-host first run: fresh database, create-workspace, pairing link, empty inbox that explains the next step` (`web/e2e/onboarding-self-host.spec.ts`)
- `invite teammate: admin invites, teammate joins with the Agent role, admin revokes, revoked session refused` (`web/e2e/onboarding-invite.spec.ts`)
- `email channel: forwarded mail opens a conversation, the reply is delivered by SMTP with threading headers, the customer reply threads back` (`web/e2e/onboarding-email.spec.ts`)
- `freshdesk import: reconcile OK with exact counts, tickets searchable with original timestamps, undo removes exactly the import` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: an account without canned responses still migrates its tickets and says so` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: a wrong API key stops before writing anything and says why` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: an archive refused with 403 is reported UNVERIFIED, never OK` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `in-app freshdesk import: an admin reads the account, imports it in the background, reconcile OK with exact counts, and undoes it` (`web/e2e/onboarding-in-app-import.spec.ts`)
- `cloud sign-in: a verified identity of an existing agent gets a Colliedesk session; every other token is refused` (`web/e2e/onboarding-cloud-sign-in.spec.ts`)
- `cloud signup: a verified identity starts a trial, the same email cannot start a second one, the expired trial is locked and still exports` (`web/e2e/onboarding-cloud-signup.spec.ts`)
- `golden path: a new team signs up on the cloud, imports Freshdesk in the app, reconcile OK, answers its first conversation by email` (`web/e2e/onboarding-golden-path.spec.ts`; #99 adds its step to this test)
- `link errors: expired, reused, revoked, mistyped, missing and another workspace's link explain themselves` (`web/e2e/onboarding-link-errors.spec.ts`)
- `test message: a new cloud workspace sends itself a test message from the empty inbox, answers it on the trial, and the reply is delivered` (`web/e2e/onboarding-test-message.spec.ts`)
- `operator console: the installer's operator link signs in, the operator sets SMTP and sends a test, the change applies without a restart, history restores the previous version` (`web/e2e/onboarding-operator-console.spec.ts`)

Also: `tests/install-smoke.sh` (real `install.sh` with Docker against the image built from the
commit; the printed link must sign in) and `tests/install-test.sh` (installer with stubbed Docker).
Run the installer smoke locally:
`docker build -t colliedesk:smoke -f server/Dockerfile . && COLLIE_DESK_IMAGE=colliedesk:smoke sh tests/install-smoke.sh`.

## Daily-use journeys

What a team does every day after its first hour: answer and route conversations, find one again,
work together, keep the knowledge base. Tag `@journey`; CI job `e2e`.

- `redeems a CLI pairing link and completes the inbox workflow` (`web/e2e/inbox-workflow.spec.ts`)
- `a conversation opens at its newest message from the list, from its address and when another one is picked` (`web/e2e/thread-newest-message.spec.ts`)
- `an opened conversation takes the screen, and Back or the browser back returns to the list where it was` (`web/e2e/phone-thread.spec.ts`)
- `the open conversation stays on screen with its reply box while an action moves it out of the list` (`web/e2e/open-conversation-stays.spec.ts`)
- `an unsent reply survives a reload and the browser Back, and is gone once sent` (`web/e2e/reply-draft-persists.spec.ts`)
- `selects conversations with the keyboard and resolves them in one action` (`web/e2e/bulk-actions.spec.ts`)
- `two people on one conversation: who is writing, a warning before a second reply, and leaving` (`web/e2e/collision.spec.ts`)
- `a private note asks a teammate with @: Mentions counts it, opening it marks it read, the writer sees it was seen` (`web/e2e/mentions.spec.ts`)
- `review: the rail counts the cards, a person opens one as a conversation, sets one aside and puts it back` (`web/e2e/review.spec.ts`)
- `knowledge base: a collection, an article written in the Markdown editor with a pasted image, published, edited, restored from history, deleted and restored from the trash` (`web/e2e/knowledge-base.spec.ts`)
- `knowledge base list: tags set in the editor, a hidden column stays hidden after a reload, and a saved view opens its filters again` (`web/e2e/knowledge-base-list.spec.ts`)
- `kb article list: at desktop width with 1, 4 and 8 columns, with bulk select and in a collection's order view, the head lines up with the cells, the title keeps the free width, the open button stays on its row` (`web/e2e/knowledge-base-list-grid.spec.ts`)
- `kb article list: on a narrow window the head still lines up with the cells and the chosen columns give way in order` (`web/e2e/knowledge-base-list-grid.spec.ts`)
- `knowledge base review: the author sends a draft for review, the reviewer approves it from Reviews, the author publishes it` (`web/e2e/knowledge-base-review.spec.ts`)

## Running them

Ports, databases and temp files derive from the checkout path (`web/e2e/support/checkout.ts`), so two
checkouts can run them at once; `COLLIEDESK_E2E_PORT_BASE` overrides the ports. Each test server
(main, mail, cloud) runs on a new database of its own, named after `TEST_DATABASE_URL`'s.
