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
4. No streaming: features run as background tasks, so an answer is a resource the web app, the API
   and MCP read the same way. Streaming would need an SSE route that no MCP tool can mirror.
5. No tool use: the model never acts. The worst a bad answer can do is a suggestion a person
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
`rate_limited`, `overloaded`, `quota_exhausted`, `timeout`, `unavailable`, `invalid_response`), the
HTTP status, `retry-after` and the provider's request id. It never holds the provider's message or
body, which can echo the prompt (customer text) or a key, and never the URL. Only rate limits,
overload, server errors and timeouts are retryable. OpenAI answers an empty account with a 429 whose
error code is `insufficient_quota`; that is `quota_exhausted`, not a rate limit, because retrying it
only spends attempts.

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
guarded. That choice is made where the provider is built, not in the adapter.

## Prompt caching

Claude's prompt cache is isolated per Anthropic organization and workspace, not per Colliedesk
workspace, and a hit needs a byte-identical prefix: it cannot return one workspace's text to another,
it only makes a repeated prefix cheaper. Colliedesk keeps no AI answers in memory.

## Testing

`aiprovider.Fake` records every `Request` and answers from a script, without a network. Feature tests
use it to assert what left Colliedesk, for example that no prompt built for workspace A holds a canary
string from workspace B.
