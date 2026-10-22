# AI drafts on your own server

AI drafts, triage and auto-answer ([user guide](../user/ai-drafts.md)) use a model provider you choose,
with your own key. Without these variables the AI controls are hidden, nothing is queued and nothing
leaves your server.

| Variable | Default | Meaning |
|---|---|---|
| `AI_SERVER_PROVIDER` | empty (AI off) | `anthropic` (the Claude API) or `openai_compatible` (OpenAI, OpenRouter, Groq, Ollama, vLLM, LM Studio). |
| `AI_SERVER_API_KEY` | empty | The provider key. Required for `anthropic`; empty is fine for a local server without authentication. |
| `AI_SERVER_BASE_URL` | the Claude API | For `openai_compatible`, the base URL with its version path, for example `https://api.openai.com/v1` or `http://ollama:11434/v1`. A private address is allowed: you chose it. |
| `AI_SERVER_MODEL` | `claude-sonnet-5-5` for `anthropic` | The model that writes drafts. Required for `openai_compatible`. |
| `AI_SERVER_SELECT_MODEL` | `claude-haiku-5-5` for `anthropic`, none otherwise | A small model that first picks articles from the list of titles, so a customer who words a question differently from the article still gets it. Empty uses the keyword search only. |
| `AI_SERVER_EFFORT` | `low` | `low`, `medium` or `high`. |

A setting the server cannot use (an unknown provider, a missing key) turns AI off and logs the reason;
the inbox keeps working.

## What it costs

Measured on a public help center of 102 articles: about 15,000 input and 800 output tokens per draft
with the defaults, and one call to the small model. Every call is stored in `ai_usage` per workspace,
with its tokens (NULL when the provider sends none), and the last 30 days are in Settings, AI drafts.

## What leaves the server

For each draft: the conversation's newest public messages (never a private note), the subject, the
names of the workspace's tags, and the articles and resolved conversations retrieval found, with the
customer's name, email, company and long numbers masked ([AI retrieval](../internals/ai-retrieval.md)).
The chooser gets the article titles and the newest customer message.
