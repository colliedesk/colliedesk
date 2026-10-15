# MCP server internals

`/mcp` (package `server/internal/mcpserver`) lets an AI agent work the inbox. User-facing page:
`docs/user/mcp.md`. The server adapts the REST API; it adds no data access of its own.

## Shape

- **One catalog.** `Catalog()` in `catalog*.go` holds every tool: name, title, description, tier,
  annotations, input and output JSON schema, the REST routes it calls and the handler. The server
  registers from it, and `TestCatalogToolsMapOntoRealRoutesAndStateTheirTier` checks every mapped
  route exists in `httpapi.Routes()`. A guide or reference generated from the catalog reads this
  list and nothing else.
- **In-process dispatch.** A tool builds an `http.Request` with the caller's own bearer and serves it
  through the `httpapi` handler (`dispatch.go`). Authentication, the token tier (`tokenTiers`),
  workspace isolation (RLS), the locked-workspace refusal and validation therefore run exactly as
  for a REST client. Do not copy any of them into a tool. Each REST call authenticates again
  (about 3 ms); that is the price of one code path.
- **Gate.** `requireBearer` runs before the SDK reads any message. No `Authorization: Bearer`
  header gives 401 with `WWW-Authenticate: Bearer`; cookies are never read. Every refusal of a
  credential (unknown, revoked, expired, an agent token past its one-year limit) wraps
  `auth.ErrInvalidCredential` and is 401; any other `Authenticate` error is 503, so a client does not
  throw a good token away over a database outage (`TestMCPAnswers401ForEveryRefusedToken`,
  `TestMCPAnswers503WhileTheDatabaseIsDown`).
- **Stateless.** `StreamableHTTPOptions{Stateless: true, JSONResponse: true}`: no `Mcp-Session-Id`,
  no server state, `GET` and `DELETE` answer 405. One handler serves protocol `2025-11-25`
  (`initialize`) and `2026-07-28` (`server/discover`); `TestToolsListIsTheCatalogInFixedOrderWithAnnotations`
  runs both. The SDK lists tools sorted by name, which is what makes the order fixed.
- **Mounting.** `mcpserver.Mount` wraps the web handler in `serve`; only the exact path `/mcp` goes
  to MCP, every other request takes the old path untouched.

## Traps

- **Loopback Host check.** The SDK refuses a request that arrives on a loopback listener with a
  non-loopback `Host`. A reverse proxy on the same machine forwards the public host name, so this
  would 403 every self-hosted install behind Caddy or nginx. `DisableLocalhostProtection` is on:
  the bearer requirement is the defence (a browser cannot add the header cross-origin).
  `TestMCPServesTheHostNameOfAReverseProxyOnALoopbackListener` pins it.
- **Locked workspace is 402, not 423.** REST answers `402 workspace_locked` for every route except
  the allowlist in `httpapi/locked.go`; the tools word it ("only export works"). `whoami` still
  answers because `GET /auth/me` is allowed, and reports `workspace_locked`.
- **A new tool needs its route in `tokenTiers`** (and the hand-written copy in
  `agent_token_scope_api_test.go`), or every token gets `scope_insufficient`. The tool's tier in
  the catalog is only used to word the error and to list `tools_you_can_use`; REST decides.
- **Tags are created by name.** `colliedesk_conversation_update` creates a name in `add_tags` that no
  tag has, because `POST /tags` is a `draft` route and triage must not stall on a missing tag. Every
  other check runs first (assignee, unknown `remove_tags`, a tag in both lists, the conversation
  exists), so a refused or wrong call leaves no tag behind
  (`TestConversationUpdateCreatesNoTagWhenTheCallIsRefusedOrInvalid`). `remove_tags` never creates:
  an unknown name there is a typo and the error lists the real tags.
- **Tag names follow REST uniqueness.** REST keeps `(workspace_id, name)` unique by exact spelling, so
  `Refund` and `refund` can both exist. `findTag` takes the exact spelling first, then the one tag that
  differs only in case, and refuses several such tags with a message that lists them; an `add_tags`
  name with no match at all is created with the spelling given (`TestTagNamesMatchExactlyFirstAndAmbiguousCaseVariantsAreRefused`).
  The inbox filter and `remove_tags` use the same rule. Two callers that create the same name at once:
  the loser's create is refused, `createTag` reads the list again and uses the winner's tag
  (`TestConversationUpdateUsesATagAnotherCallerCreatedMeanwhile`).
- **A tool call is not atomic.** `colliedesk_conversation_update` is a few REST writes in a row (tags
  created, then the PATCH, then each tag put or delete). `call.wrote` records the first successful
  write; a failure after it ends with "some changes may already be applied" and, for an idempotent
  tool, "safe to repeat" (`failureText`). `TestConversationUpdateSaysWhatMayBeAppliedWhenALaterStepFails`.
- **Attribution is free.** A note a tool adds goes through `POST /messages` with the caller's bearer,
  so it carries the token (`TestNoteAddedThroughMCPNamesTheTokenInTheTimeline`). The known limit
  (changes other than notes are not attributed) is in `docs/internals/auth.md`.
- **Size.** One in-process call buffers at most 16 MiB of REST answer; beyond that the tool says the
  answer is too large (`TestAnswerTooLargeForOneCallIsExplainedNotAnInternalError`) instead of
  reporting an internal problem. `detail=full` of a very long thread is the usual cause.
- **Pagination.** REST pages by `page` and `page_size`. The cursor is the next page number plus a
  fingerprint of the search, so a cursor used with other filters is refused. `next_cursor` is set
  when the page is full, so the last page can be empty; an exact "has more" would cost a second
  list query, which is the slow one (see Performance).
- **Customer text.** Anything a customer or a contact record supplied (subject, incoming body and
  `From`, attachment names of incoming mail, contact name, email, organization) sits only under a
  field named `untrusted_customer_content`; `TestCustomerWrittenTextAppearsOnlyUnderUntrustedCustomerContent`
  walks every string of the result. A new output field must be classified the same way.
  The marker lowers prompt-injection risk; it does not remove it, so tiers stay the control.
- **Provenance decides trust, not `kind`.** `nativeTeamMessage` (`tools-conversation.go`) trusts a
  message only when it is not `incoming`, has `author_agent_id`, and has no `origin` or an `origin.kind`
  of `reply` or `private_note`. The Freshdesk importer stores a public note or a forward as `kind=note`,
  and the contact can have written it; a matched agent's public note stays untrusted on purpose. An
  imported reply whose author matched no Colliedesk agent has only `author_name`, so it is untrusted
  too, name included. Do not widen the rule without a source that proves the author.
- **Token-written text is model output.** `written_by_agent_token {label, scope}` appears on a
  trusted message when `agent_token` is set, and `note_add` returns it for the note it wrote. The
  instructions and the tool descriptions say to read it as model output. A person's name is shown
  as author even when a token typed the text, so the token field is the only tell.
- **Small REST additions** exist for this server and are in the OpenAPI contract:
  `GET /auth/me` returns `scope` (the token tier); `GET /contacts?ids=` fetches contacts by id; and
  `GET /contacts?limit=` stops the list and sets `has_more` (the store asks for one row more, so no
  count is made). Without `ids`, naming the customers of a page of 50 conversations meant
  `GET /contacts/{id}` per customer, which loads every conversation of that customer (a shared sender
  address can have thousands). The `ids` handler parses uuids (at most 100) and the store runs a
  statement of its own over a `uuid[]`: comparing `id::text` could not use the `(workspace_id, id)`
  index and scanned every contact of every workspace (see Performance).
- **Embedded guides.** `go:embed` cannot read outside the module, so `docs_search` reads a copy of
  `docs/user` in `server/internal/mcpserver/userdocs`. Run `make sync-user-docs` after editing a
  guide; `TestEmbeddedUserGuidesMatchTheDocsDirectory` fails on a stale copy. `README.md` is not
  indexed.
- **SDK dependency.** `github.com/modelcontextprotocol/go-sdk` v1.8.0 (Apache-2.0, compatible with
  AGPL-3.0-only). It pulls `golang.org/x/sys` v0.41.0, which carries an OSV advisory (Windows
  only); `go.mod` pins v0.44.0 so the PR gate stays clean. The SDK's stateless-session behaviour
  has changed between minor releases (`MCPGODEBUG=allowsessionsinstateless`, removed in 1.9), so
  upgrade it as its own change and rerun `internal/mcpserver`.

## Performance

`TestInboxSearchLatencyBudget` seeds 5,000 conversations (3 messages each, 50 customers) and 100,000
contacts over two workspaces, then calls `colliedesk_inbox_search` and `colliedesk_conversation_get` 50
times each. Budget: p95 at most 100 ms, the same as the REST inbox gate (`performance.md` describes
the fixture). Measured on the dev host: tool 58 ms (status filter) and 71 ms (text search), thread
read 27 ms; `colliedesk_whoami` 11 ms. The tool adds a few small REST calls (identity, agents,
contacts by id) to one list query. If the gate fails, look at the list query first, then at the
contact lookup by id, not at the adapter.
