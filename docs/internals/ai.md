# AI providers

Decisions and traps behind `server/internal/aiprovider`, the layer every AI feature (triage
suggestions, reply drafts, later backtests and auto-answers) calls a language model through. Issue #89.

## One request in, one answer out

`Provider` has two calls: `Complete` (one request, the whole answer) and `Check` (the smallest real
request that proves an endpoint, key and model work). A `Request` carries a model, an effort level
(`low`, `medium`, `high`), Colliedesk's own system text, the conversation, an output limit and an
optional JSON Schema. There are no sampling knobs, no streaming and no tool use:

1. Claude models refuse `temperature`, `top_p` and `top_k` (HTTP 400 since 2026-10-21) and refuse
   turning thinking off; current Opus and Sonnet refuse a forced tool call. JSON comes from structured
   output (`output_config.format` on Claude, `response_format` on OpenAI-compatible servers).
2. Effort is always sent. Providers default to different levels (Claude Opus 5.5 and Haiku 5.5 to
   medium, Sonnet 5.5 to high), so leaving it out would change cost and quality with the model.
3. A final assistant message (prefill) is refused before it is sent: current Claude models answer
   it with a 400.
4. The output limit is at most 16,000 tokens: requests are not streamed, and the Claude SDK refuses
   a non-streaming request that could run past ten minutes.
5. No streaming: features run as background tasks, so an answer is a resource the web app, the API
   and MCP read the same way. Streaming would need an SSE route that no MCP tool can mirror.
6. No tool use: the model never acts. The worst a bad answer can do is a suggestion a person
   declines or a draft a person rewrites.

## One call is one attempt

Adapters never retry. The Claude SDK retries twice by default; the adapter sets
`option.WithMaxRetries(0)`. Every HTTP attempt is billed by the provider, so the caller (the
metered layer) records one usage row per attempt and decides about retries itself. A test fails if a
500 reaches the fake API more than once.

The same rule keeps "repair" out of the adapters: when a schema was asked for and the answer is not
JSON, `Response.JSON` is empty and the caller decides whether a second, metered call is worth it.

## The Claude SDK must not read the host's environment

`anthropic.NewClient()` without options reads `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
`ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS`, a local `ant` profile and federation variables. On a
multi-workspace server that would send an operator's key, or a bearer header next to a workspace's own
key, to a request that belongs to someone else. The adapter passes `option.WithoutEnvironmentDefaults()`
and sets the key and base URL itself; the server provider gets its key from `AI_SERVER_*` like any
other setting. The test sets each variable and asserts the fake API sees only the configured key.

## Refusal fallbacks bill more than one model

On the Claude API, Opus 5.5, Opus 5, Sonnet 5.5 and Fable 5.1 requests carry `fallbacks: "default"`
(beta `server-side-fallback-2026-07-01`): a policy decline is re-run on another model inside the same
call. Haiku 5.5 has no fallback, and a proxy base URL never gets the parameter. One attempt can then
bill two models: `usage.iterations` holds one entry per model that ran, and the top-level `usage` only
the last. `Response.Hops` keeps each model with its tokens, and cost must be computed per hop at that
model's price, never from `Response.Model` alone. The answer text is what follows the last `fallback`
content block. A refusal is an answer (`StopRefused`), not an error: it may be billed.

## Errors carry no text

`*aiprovider.Error` has a code (`auth`, `permission`, `model_not_found`, `bad_request`, `too_large`,
`rate_limited`, `overloaded`, `quota_exhausted`, `timeout`, `unavailable`, `blocked_address`,
`invalid_response`), the
HTTP status, `retry-after` and the provider's request id. It never holds the provider's message or
body, which can echo the prompt (customer text) or a key, and never the URL. Only rate limits,
overload, server errors and timeouts are retryable. OpenAI answers an empty account with a 429 whose
error code is `insufficient_quota`; that is `quota_exhausted`, not a rate limit, because retrying it
only spends attempts. An answer that is not the API's (a redirect, a proxy's HTML page) is
`invalid_response` with its status, not retried. `retry-after` is capped at one hour: a base URL an
admin typed can answer anything.

## OpenAI-compatible servers

One adapter covers OpenAI, OpenRouter, Groq, Ollama, vLLM and LM Studio through
`POST {base}/chat/completions`. The base URL includes the version path, as each provider documents
it (`https://api.openai.com/v1`, `http://localhost:11434/v1`). On `api.openai.com` the adapter sends
`max_completion_tokens` and `reasoning_effort` (its reasoning models refuse `max_tokens`); every other
host gets `max_tokens` and no reasoning field, which the copies of the API read or reject. A server
that sends no `usage` gives `Usage.Reported = false`: unknown, never zero. Bodies are read through a
size limit.

## Network

Adapters take an `*http.Client`. `NewHTTPClient` has no response-header timeout, because a
non-streaming completion sends its headers only when the whole answer is ready; the request context
bounds the call. Redirects are not followed. A base URL a workspace admin typed is dialled through
`netguard` (public addresses only); the server provider and a private URL the operator allowed are not
guarded. That choice is made where the provider is built; an adapter given no client is guarded, so
forgetting to pass one fails closed. A dial netguard refuses
is `blocked_address`, so Settings can say the address is on a private network.

## Prompt caching

Claude's prompt cache is isolated per Anthropic organization and workspace, not per Colliedesk
workspace, and a hit needs a byte-identical prefix: it cannot return one workspace's text to another,
it only makes a repeated prefix cheaper. Colliedesk keeps no AI answers in memory.

## Settings: providers, slots, consent

`server/internal/ai` holds a workspace's settings (migration `20261022150000-ai-providers`):

1. `ai_providers`: the providers a workspace adds, at most 5. The key is sealed with `tokenseal`
   (`MAILBOX_TOKEN_KEYS`) under the context `ai-provider:<workspace>:<id>`, so a ciphertext copied to
   another row does not open; only the last four characters are ever shown. Without a key ring the
   server refuses a key (409) instead of keeping it in memory like an import: a provider key lives for
   months, and losing it at a restart would stop features silently.
2. A provider is saved only after a check passed. The id is made before the check so the usage row and
   the seal context name the same provider. No transaction is held across the check, so the 5-provider
   cap and the unique name are checked again under an advisory lock in the insert.
3. `ai_slots`: one row per feature, created the first time an admin sets it; no row is off. Every
   feature starts off on every server, Colliedesk Cloud included: turning one on is the admin's consent
   to send its data to that provider, recorded in `consented_by/at` and again whenever the recipient
   changes. Deleting a provider turns its slots off in the same transaction and never moves them to the
   server's provider, which would change who receives the data. A slot on the server's provider whose
   operator removed it (or the model) reads as on but not active, with `off_reason`.
4. `ai_settings_events`: the history, with labels copied so it outlives the provider; `seq` orders the
   events of one transaction, which share `created_at`.
5. Routes: reading is the `read` tier; provider changes are `admin`, and the two routes that carry a key
   are `credentialRoutes` (a lower tier is refused, never filed, because an agent request keeps its
   body). `PUT /api/v1/ai/slots/{slot}` is Approval for every token: it decides whether ticket data
   leaves for a third party, the reason Approval exists.
6. Checks are synchronous (the admin waits, bounded at 20 s, well under proxy and MCP client limits) and
   capped at 10 a minute per workspace, since each is a billed call.

## The server's provider

`AI_SERVER_*` configure one provider for every workspace, read at start like `POLAR_*`: a partial
setting stops the server and names what is missing. It is never a database row, so no key of HC Labs
sits in a workspace's data or export. Its network is the operator's and is not guarded;
`AI_ALLOW_PRIVATE_BASE_URLS=1` is the operator's choice to let workspace providers reach private
addresses too.

## Metering

`ai_usage` has one row per call to a provider, each attempt included. `Service.Metered` commits a
`started` row before the call, makes the call with no transaction open, and finishes the row on a
context the caller cannot cancel (a person closing the tab must not leave it `started`). Provider fields
are copied and there is no foreign key to `ai_providers`: the row exists before the provider does (the
add check) and outlives it. Tokens the provider did not report stay `NULL`. `hops` keeps the per-model
usage of a refusal fallback. `TestProvidersAreCalledOnlyThroughMetered` fails when code outside the
adapters and `meter.go` calls `Complete` or `Check`. No prompt or answer is stored.

## The task queue

`ai_tasks` (migration `20261022160000-ai-tasks`) holds AI work the server runs in the background;
`ai.Worker` in `collie serve` runs it, in the pattern of `importjobs`:

1. Claim under `PrivilegeAIJobClaim`: due tasks (people's before automatic, then oldest) get a
   `claim_token` and a 3-minute lease, longer than the longest call (90 s) plus closing its usage row.
   Every later write names the token, so a task cancelled or leased again ignores a late result.
2. Then, in the task's own workspace: a locked workspace (`PlanLimits`) cancels it
   (`workspace_locked`); an automatic task past the daily limit is cancelled (`automatic_limit`), one
   past the per-minute limit waits for the next minute (a backlog after an outage must not lose its
   work); a feature off or without its provider cancels it (`feature_off`). Only then is the provider
   resolved and the handler run, with no transaction open. A handler writes its result with
   `Run.Record`, in the same transaction that marks the task done and only while it still holds the
   claim: a person who cancelled meanwhile never sees a late result.
3. A retryable provider error queues the task again after `retry-after` or a doubling backoff, three
   attempts at most; every attempt is its own metered call. A process stopping mid-call (a deploy)
   puts the task back at once without counting the attempt. At most 8 calls run in the process, and the
   claim takes no more than 2 per workspace counting what already runs under a live lease, so one
   workspace's backlog never holds back another's.
4. The sweep closes `started` usage rows older than a lease and two minutes (the process died) as
   `error/unknown`, under the same privilege.
5. A restored workspace never gets its tasks back (`restore-workspace.sh` drops them in the scratch
   copy): a queued task would spend again.

Handlers register per kind (`Worker.Handle`); a task without one fails (`no_handler`).

## Automatic limits and the allowance

Automatic tasks count against `ai_automatic_limits` (default 300 a UTC day, 30 a minute) when they
first start; tasks count, not calls, so retries use no more. An advisory lock per workspace serializes
the admission, so two workers cannot both take the last place.

`ai_usage.cost_micro_usd` is computed when a call of the server's provider finishes, per hop at the
served model's price from `AI_SERVER_PRICES` (unknown model: the highest declared price of each kind,
and a log line), and stored: a price change never rewrites history. `Metered` sums the month (UTC) and
refuses a server call once the allowance (`AI_SERVER_MONTHLY_ALLOWANCE`; on the cloud per active agent,
another amount for a trial) cannot pay for the call, before writing a usage row: no call, no row.

A call holds its most possible cost while it runs (`reserved_micro_usd`: its text at one token per 3
bytes, plus 500 for a schema, and all of `MaxOutput`, at the model's price or the highest declared).
The check and the hold are one step under a per-workspace advisory lock, and the month's sum counts
running calls at their hold and finished ones at their cost. Without the hold, calls started at once
(several workers or servers) each saw the same balance and could pass it by what they all cost
together, with no bound (`TestCallsRunningAtOnceNeverPassTheAllowance`). Now the allowance is passed
only when a call costs more than its hold: a refusal fallback answering with an extra hop.

A call that does not fit only because running calls hold the room (`ErrAllowanceHeld`: it would fit
once they give it back) is not a spent allowance: its task goes back to the queue for 5 s, uncounted,
for at most 10 minutes from when it was queued; a hold left by a dead process goes when the sweep
closes its row. Only a call that would not fit with nothing running fails `ai_allowance_reached`.

## Reply drafts

1. **The draft's state is its task's.** `ai_drafts.status` is only `pending`, `ready` or `cancelled`;
   a pending draft reads as its task (`queued`, `running`, `failed`, `cancelled`). A pending draft whose
   task is gone (the restore script drops `ai_tasks`) reads as failed with `restored`, never as a draft
   that waits forever. Do not copy the task's state into the draft: two rows to keep in step.
2. **The prompt holds no instruction from data.** The system turn is Colliedesk's text only. The
   conversation, articles and past tickets go in the user turn inside `<untrusted_customer_content>`,
   `<kb_article>` and `<past_conversation>`, with `<` and `>` replaced so a message cannot close its
   section; the person's request is the only `<agent_request>`. Private notes are never read.
3. **Sources come from the database.** The model answers with `source_ids` (`kb1`, `past2`) that name
   what the prompt offered; an id it was not offered is dropped, and the title, number and link come
   from the retrieval rows. Warnings list emails, numbers of six digits or more and other customers'
   names in the draft that the conversation, subject and prompt do not hold. The articles a ready draft
   used are also rows of `ai_draft_sources`, for the article's monthly count: a containment search on
   `ai_drafts.sources` cannot use an index under row-level security (non-leakproof operator).
4. **Refused before queueing.** `POST` answers 409 `ai_feature_off`, 402 `ai_allowance_reached` and 429
   `ai_drafts_rate_limited` (30 a minute in a workspace, under an advisory lock) at once: a looping
   token must not fill the queue or a workspace's own key, which no allowance bounds.
5. **How it ended.** A reply that names `ai_draft_id` locks the draft (this conversation's, ready,
   undecided) before the insert, so another workspace's or conversation's id never reaches the
   foreign key, and records `sent_unchanged` (word similarity ≥ 0.9) or `sent_edited`. A reply sent
   without it discards the sender's ready drafts; a new draft supersedes the old; resolving or closing
   discards (trigger); the worker's sweep expires a ready draft after 7 days and erases text and prompt
   90 days after a draft ended. A web client whose draft went meanwhile sends the reply again without
   the id: losing the metric is better than refusing a reply. `DraftOutcomes` counts them for #100.
6. **Drafts left.** The allowance shows about how many drafts what is left pays for, at the workspace's
   average draft cost over 30 days, or a typical draft (6,000 tokens in, 500 out) before its first.

## Triage suggestions

1. **Queued by a trigger.** `ai_triage_on_first_message` (AFTER INSERT ON messages) queues an automatic
   task and a pending suggestion for the first `incoming` message of a conversation when triage is on.
   Every way a conversation starts inserts that message; an import sets `origin`, so it queues nothing.
   A trigger, not a call in each path: the five paths that create messages cannot forget it. The worker
   is not woken by it; it finds the task at its next look (5 s).
2. **Context.** The prompt holds the ticket's subject and first customer message, the workspace's tags
   and active teammates, and `AIRetrieve` (3 articles, 3 solved conversations) with how each solved
   conversation was sorted (priority, tags, assignee). Private notes never go.
3. **Checked against the database.** The model answers with tag and teammate names from the lists in
   the prompt; unknown names, another workspace's tags and deactivated teammates are dropped. A prose
   answer (no structured output) fails the task (`invalid_answer`): names in prose cannot be checked.
   Sources come from the ids the prompt gave (`citedSources`, shared with drafts), never from the model.
4. **Compared with the conversation when the answer lands.** The trigger and `CreateTriage` store
   `base` (priority and assignee at that moment). In `Record`: a tag the conversation has, or a
   priority or assignee equal to the current one, is dropped; a field that differs from `base` was set
   by a person meanwhile, so it is dropped and kept in `stale` for the card to say so.
5. **Applied part by part through the ordinary changes.** Accepting adds each tag with
   `AddConversationTag` (a tag someone added meanwhile stays) and sets priority and assignee with
   `UpdateConversation` as the person (`assigned_by` recorded), then `AcceptedTriage` merges the part
   into `applied` under the row lock, so two people accepting two parts both count. All of it applied
   → `accepted`. Dismiss, a newer suggestion, or the 7-day sweep close an open one as
   `partly_accepted` when a part was applied, else `dismissed`/`superseded`/`expired`. A part already
   applied is not applied again: "Accept the rest" after a person changed an accepted field leaves it.
6. **What it no longer names.** A tag deleted or a teammate deactivated after the answer is filtered
   out when the suggestion is read, so accepting the rest never fails halfway on it. A suggestion
   that names nothing (all filtered as above, or stale) cannot be accepted (409 `ai_triage_nothing`)
   and never expires: nobody failed to use it, so it does not count as unused.
7. **Failed, not cancelled.** A task the worker stopped (automatic limit, feature off) reads as
   `failed` with its code; only a person's stop (`cancelled`) reads as cancelled.
8. **One minute cap.** Drafts and suggestions asked for by hand share the 30-a-minute cap; automatic
   ones are bounded by the automatic limits instead.
9. **Restore.** `restore-workspace.sh` copies rows with `session_replication_role = replica`, so the
   trigger does not fire for restored messages, and it deletes `ai_tasks` first (a pending suggestion
   reads `restored`).

## Testing

`aiprovider.Fake` records every `Request` and answers from a script, without a network. Feature tests
use it to assert what left Colliedesk, for example that no prompt built for workspace A holds a canary
string from workspace B.
