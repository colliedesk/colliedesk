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
  header gives 401 with `WWW-Authenticate: Bearer`; cookies are never read. A person's own session
  as a bearer is also 401: it has no tier, no one-year cap and no attribution, so an AI client holding
  one would act with that person's full rights and leave no trace of the agent
  (`TestMCPRequiresABearerTokenAndNeverACookie`). Every refusal of a
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
- **Stdio bridge.** `collie mcp` (package `internal/mcpbridge`, standard library only) is for hosts
  that only start stdio servers (Claude Desktop). It reads one JSON-RPC line, POSTs it as it is to
  `/mcp` and writes each answer as one line; it registers no tools, so it cannot drift from the
  server it talks to. Two builds carry it: the server's `collie`, and `cmd/collie-client`, the
  about 6 MB `collie` a person installs (only `mcp`, `version`, `help`;
  `TestTheClientBuildLeavesTheServerOut` keeps the web app and pgx out of it). Decisions:
  - The token comes from `COLLIEDESK_TOKEN` or `--token-file` (0600 checked on Unix only), never
    argv, which `ps` and host config files show. `http://` is refused except on loopback.
  - A failure (401, 404 or HTML, 5xx, network) answers the waiting id with a JSON-RPC error that
    says what to do, and the bridge stays up: hosts otherwise show only "server disconnected". A
    JSON-RPC error the server answers itself (400 for a version or header mismatch) passes as it is.
  - stderr has one line per event (start: host and version; failure: the method and the HTTP
    status), never a header or body. Claude Desktop keeps it in its MCP log.

`/mcp/operator` serves the operator tools (`Tier: operator`) to operator tokens only, with its own
instructions; `/mcp` never registers them. See [operator console internals](operator-console.md#operator-realm).

## Traps

- **The bridge sets the HTTP headers a stdio host cannot.** `MCP-Protocol-Version` comes from the
  request's `_meta` (2026-07-28) or from the `initialize` answer the bridge saw; from 2026-07-28 the
  server also refuses a POST without `Mcp-Method` and, for `tools/call`, `prompts/get` and
  `resources/read`, `Mcp-Name`. Piping two go-sdk `Connection`s into each other loses these
  headers: the SDK's HTTP client sets them only when an `mcp.Client` owns the connection
  (`setMCPHeaders` after `initialize`). Hence the hand-written proxy. No tool uses `x-mcp-header`
  (`Mcp-Param-*`); a tool that adds one needs the bridge to set it too.
- **Bridge framing.** One message per stdout line: answers are `json.Compact`ed (the server may
  indent), stdin is read with `ReadBytes` (an image upload is one line of megabytes; a default
  `bufio.Scanner` stops at 64 KiB), and ids stay `json.RawMessage` so a large number or a string
  comes back as sent. `TestBridge*` in `mcpserver` runs a real SDK client through the bridge to
  the real server for `2025-06-18`, `2025-11-25` and `2026-07-28`.

- **Loopback Host check.** The SDK refuses a request that arrives on a loopback listener with a
  non-loopback `Host`. A reverse proxy on the same machine forwards the public host name, so this
  would 403 every self-hosted install behind Caddy or nginx. `DisableLocalhostProtection` is on:
  the bearer requirement is the defence (a browser cannot add the header cross-origin).
  `TestMCPServesTheHostNameOfAReverseProxyOnALoopbackListener` pins it.
- **Locked workspace is 402, not 423.** REST answers `402 workspace_locked` for every route except
  the allowlist in `httpapi/locked.go`; the tools word it ("only export works"). `whoami` still
  answers because `GET /auth/me` is allowed, and reports `workspace_locked`.
- **A new tool needs its route open in `tokenTiers`** (and the hand-written copy in
  `agent_token_scope_api_test.go`), or every token gets `scope_insufficient`;
  `TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool` fails on a tool that lists a closed
  route. When a tool starts calling a route in `mcpToolPending`, remove it there. The tool's tier in
  the catalog is only used to word the error and to list `tools_you_can_use`; REST decides.
- **An agent request is a result, not an error.** A REST call answered 202 with a `request_id`
  comes back from `restClient.do` as `*agentRequested`; `execute` turns it into
  `{agent_request, next}` with `isError` false. `next` says nothing changed yet, how to follow the
  request, and that the rest of a multi-step call did not run (`conversation_update` files only its
  first write). Every writing tool's advertised output schema is widened by `outputSchema` to accept
  that shape (`anyOf` of the usual `required` list and `agent_request`), so a client that validates
  structured output accepts it. The answer of an approved call is never shown to the model by
  `colliedesk_request_get`: it can hold customer text; `result_status` says whether it worked.
- **Approval tools.** Each route that needs approval for every tier has a tool that only files the
  request (`colliedesk_token_create_request`, `..._workspace_export_request`, `..._billing_request`,
  `..._email_channel_disconnect_request`, `..._email_secret_rotate_request`,
  `..._workspace_slug_change_request`); `colliedesk_reply_draft` sends `draft: true` so even a `send`
  token only drafts. No tool approves or rejects: those routes are `PersonOnly`.
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
- **Destructive tools.** `Spec.Destructive` sets the MCP `destructiveHint` for a tool whose effect
  cannot be undone: `reply_send` and `reply_retry` (an email left), `tag_delete` (the tag is gone from
  every conversation; a new tag of the same name is empty) and `token_revoke`. Their descriptions say
  "cannot be undone" and tell the model to get the user's yes first; the annotation test checks both.
  Every other writing tool states `destructiveHint=false`.
- **Saved reply tools are not destructive.** Delete moves a reply to the trash for 30 days and every
  edit is a restorable version, so `colliedesk_saved_reply_delete` states `destructiveHint=false` and
  says how to undo. Saved reply text comes back under `team_text`, not `untrusted_customer_content`:
  the team wrote it, but the model is still told it is text, never instructions. Writes need
  `updated_at` from a read, so a model cannot overwrite an edit it never saw.
- **Attachments are read, not streamed.** `GET /attachments/{id}` streams bytes, so `attachment_get`
  uses `restClient.download`, which keeps the first 128 KiB and stops the handler with `errEnough`
  (the handler logs one `stream:` line for a larger file; that is expected). A plain UTF-8 file comes
  back as text cut after 20,000 characters; anything else as name, size, SHA-256 and a sniffed type
  only. No base64 and no link: an image or a PDF in the model context is one more injection channel,
  and the web app already opens files. The file name is customer text even on a team note, because a
  team member often forwards a customer's file. `conversation_get` lists attachment ids and sizes.
- **Bulk update creates no tag.** `PATCH /conversations` is one transaction; creating a missing tag
  before it would leave the tag behind when the transaction refuses an id. So
  `conversations_bulk_update` takes only existing tag names. It has no priority because the REST bulk
  route has none.
- **`token_revoke` is `POST /auth/logout`** with the token as bearer: it revokes that session row only.
  The tool's result is the last thing the token can read; `tools/list` after it is 401.
- **Settings tools** (`catalog-settings.go`, `tools-settings.go`) read at `read` and change at
  `admin`; below `admin` the REST gate files the change as a request and the tool returns it like any
  other. They copy only named fields out of the REST answers (`emailChannelView`, `inviteView`,
  `sessionsList`), so a field added to REST later does not reach a model by accident; the REST
  answers to tokens already carry no link, token or inbound secret. `billing_get` is `admin` and
  read only, so its output schema lists the request shape itself (`outputSchema` skips read-only
  tools). On a self-hosted server the billing route is not mounted and the tool says so.
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
- **Generated tool table.** The table under "Tools" in `docs/user/mcp.md` sits between
  `<!-- generated:tools -->` and `<!-- /generated -->` and comes from the catalog (package
  `mcpserver/docgen`): the sentence from `toolDocs` (`Spec.Doc`, written for people; `Description`
  is written for the model), the tier from `Tier`, and "Cannot be undone" from `Destructive`. Run
  `make docs-mcp` after changing a tool; `TestGeneratedDocsMatchTheCatalog` fails on a stale table,
  `TestEveryToolHasADocSentence` on a tool without a sentence, and `TestUserGuidesNameOnlyRealTools`
  on a guide naming a tool that does not exist. `docs_search` skips the marker lines.
- **Generated host guides.** `docs/user/connect-<host>.md` (Claude Code, Codex, Cursor, VS Code,
  Claude Desktop), the protocol versions and the host list of `mcp.md`, and
  `web/src/features/settings/connect-snippets.generated.ts` come from `docgen/connect.go` and
  `docgen/templates`. The commands and JSON a person copies are one map (`snippets`), so a guide and
  the Connect panel of Settings never differ; the web app swaps `https://desk.example.com` for its
  own origin. The token is never in a snippet, only its variable or the host's prompt. Release file
  names come from `.github/scripts/client-assets.txt`; `TestHostGuidesHoldValidJSONAndRealFileNames`
  parses every JSON block and checks every `releases/latest/download/` link against that list. The
  plugin and skills live in `plugins/colliedesk` (`TestSkillsNameRealToolsAtTheirTier`).
- **Host checks by hand.** `tests/host-connect/tools-list.sh` compares `tools/list` over HTTP with
  the same call through `collie mcp` (no model needed). `tests/host-connect/hosts.sh` runs the
  Claude Code and Codex commands of the guides in a clean `node:22` container and, given a model API
  key, asks each to call `colliedesk_whoami`. Neither is in CI: CI has no model key, and the hosts
  are downloaded at pinned versions that a person bumps when checking a guide.
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
