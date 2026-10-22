# AI providers and features

Colliedesk can use a language model to suggest how to sort a new ticket (tags, priority, assignee)
and to draft a reply an agent edits and sends. A model only suggests and drafts: a person accepts each
suggestion, and nothing reaches a customer until someone presses **Send**. Reply drafts are here;
triage suggestions are being built ([#89](https://github.com/colliedesk/colliedesk/issues/89)).
**Settings > AI** decides which model each feature uses and records who agreed to send data to it.

## Every feature starts off

No AI feature runs until an admin turns it on, on Colliedesk Cloud too. Turning a feature on is your
consent that its ticket data goes to the provider it uses; Colliedesk records who agreed and when,
and asks again whenever the feature moves to another provider. Turn a feature off at any time in
**Settings > AI > Features**.

## Where a feature runs

Each feature (**Triage suggestions**, **Reply drafts**) picks one of:

1. **The server's provider.** Set by the server operator for every workspace. On Colliedesk Cloud it
   is **Colliedesk AI**: Anthropic's Claude models, run for HC Labs and included in your plan. On your
   own server it is whatever the operator configured (see
   [environment variables](../self-host/environment-variables.md#ai-provider)); without one, Settings
   explains the two ways to get started.
2. **A provider you add.** Your workspace pays that provider directly.
3. **None.** The feature stays off.

## Add your own provider

In **Settings > AI > Providers**, admins choose **Add provider**:

1. **Anthropic**: an API key from console.anthropic.com. Requests go to the Claude API.
2. **OpenAI-compatible**: OpenAI, OpenRouter, Groq, vLLM, Ollama, LM Studio. Enter the base URL with
   its version path, for example `https://api.openai.com/v1` or `https://openrouter.ai/api/v1`.

List the models you want to use; the first is the default. Before anything is saved, Colliedesk sends
one small request to the first model. If the provider refuses (a wrong key, a model it does not have,
an account out of credit), nothing is saved and Settings says why. That request is billed by the
provider like any other.

A workspace can add 5 providers. Keys are encrypted on the server and never shown again: you see
their last 4 characters. **Change key** checks the new key first; if it fails, the old key keeps
working. **Check** sends the same small request again and shows whether the provider still works. At
most 10 checks a minute run in a workspace.

**Delete** erases the key at once. The features that used the provider turn off and stay off until
an admin picks another provider: Colliedesk never moves your data to another provider on its own. To
undo a delete, add the provider again.

A base URL must be a public `https` address. A server on your own network (an Ollama on the same
machine) works only when the server operator allows private addresses; then a provider without a key
is accepted too.

## What a provider receives

A feature that is on sends its provider the ticket's messages, the customer's name, matching published
knowledge base articles, and short excerpts of similar solved tickets with other customers' names and
emails removed. Private notes are never sent. Colliedesk does not store the prompts it sends or the
provider's answers beyond the draft or suggestion itself. Each provider keeps data under its own
policy; read it before you add one, for example [Anthropic's](https://www.anthropic.com/legal/privacy).

To keep data on your own servers, add an OpenAI-compatible provider on your network (vLLM, Ollama)
and use it for every feature.

## Work runs in the background

Suggestions and drafts run as background tasks, so no screen waits for a model: a task that hits a
busy provider tries again a little later (three times at most), and one whose feature was turned off
meanwhile is dropped without calling anyone. A locked workspace runs none.

## Reply drafts

With **Reply drafts** on, the reply box of a conversation has **Draft with AI**. Say what the reply
should say (optional, up to 2,000 characters: "offer a refund", "ask for the order number"), or leave
it empty for a reply to the customer's newest message. You keep working while it is written; the
conversation never waits for the model.

The draft arrives as plain text with:

1. **Sources**: the knowledge base articles and solved tickets it used, named by Colliedesk, never by
   the model. A source the model invents is dropped.
2. **Missing information**: what the reply needs that no source said, instead of a guess.
3. **Check before sending**: an email address, a number of six digits or more, or another customer's
   name that is in the draft but not in this conversation. A model can copy one from a past ticket or
   be told to add one by a message; read them before you send.

**Replace** puts the draft in the reply box in place of what you typed; **Insert below** adds it under
your text; **Discard** throws it away. Nothing is sent until you press **Send**, and you can edit
every word. Asking again replaces your earlier draft. **Stop** ends one still being written; it is not
counted. A draft nobody used for 7 days expires, and resolving or closing the conversation discards
it.

Colliedesk keeps how each draft ended (sent nearly as written, sent edited, discarded, replaced,
expired), so a team can see whether drafts help, and an article's **Used by AI** box says how many
drafts used it this month. A reply sent from a draft says so on the timeline.
A draft's text and prompt are erased 90 days after it ended; the numbers stay. At most 30 drafts a
minute are asked for in a workspace.

## Limits

1. **Automatic runs.** Work Colliedesk starts on its own (a triage suggestion for each new ticket)
   stops at 300 a day (UTC) and 30 a minute in a workspace, whichever provider runs it, so a flood of
   tickets cannot run up a bill. Past the daily limit automatic suggestions wait until 00:00 UTC; what
   an agent asks for still runs. Admins change both numbers in **Settings > AI > Features**
   (**Automatic runs**, **Change**). Each task counts once, however many times it is retried.
2. **Included AI.** The server's provider can come with a monthly allowance (on Colliedesk Cloud,
   included in your plan). When it is used up, features on the server's provider stop until the first
   day of the next month (UTC), and Settings says so; your own providers keep working. **Settings > AI >
   Usage** shows what is used. Calls still running are counted when they finish, so the last calls of a
   month can go slightly past the allowance.

## History and usage

**Settings > AI > History** lists who added, changed or deleted a provider or a key, and who turned a
feature on, off or elsewhere, with keys shown only by their last 4 characters. Every call to a provider
leaves one usage record (the feature, the model, tokens in and out, whether it failed, and for the
server's provider its cost at that day's prices), never the text sent or received. **Settings > AI >
Usage** sums a month per feature and provider. Both are in the [export](export.md).

## Agents and AI agents

Agents see these settings but only admins change them. An AI agent with a draft token asks for a
reply draft, reads it and discards it like a person (`colliedesk_ai_draft_create`,
`colliedesk_ai_draft_get`, `colliedesk_ai_drafts_list`, `colliedesk_ai_draft_discard`); sending a
reply still needs the send tier or a person's approval. Through [MCP](mcp.md), an AI agent with a read
token can read them, and one with an admin token can add, check, change and delete providers (a token
below admin is refused outright on the two calls that carry a key, so a key never waits in a request).
Turning a feature on or moving it always becomes a [request a person approves](agent-requests.md).

## Sign in with a subscription

A Claude Pro or Max plan, or a Gemini subscription, cannot be used: their terms do not allow another
application to use them. Use an API key.
