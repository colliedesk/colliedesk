# AI providers and features

Colliedesk can use a language model to suggest how to sort a new ticket (tags, priority, assignee),
to draft a reply an agent edits and sends, and to answer customers on your help center with the
[AI chat](ai-chat.md). In a conversation a model only suggests and drafts: a person accepts each
suggestion, unless an admin turns on [Apply suggestions automatically](#apply-suggestions-automatically),
which lets the picked parts (tags, priority, assignee) apply by themselves and can be undone, and a
draft never reaches a customer until someone presses **Send**. The AI chat is the one feature that
answers customers by itself: only once an admin turns it on, and only from the help center's public
articles. **Settings → Built-in AI** decides which model each feature uses and records who agreed to
send data to it.

## Every feature starts off

No AI feature runs until an admin turns it on, on Colliedesk Cloud too. Turning a feature on is your
consent that its ticket data goes to the provider it uses; Colliedesk records who agreed and when,
and asks again whenever the feature moves to another provider. Turn a feature off at any time in
**Settings → Built-in AI → Features**.

## Where a feature runs

Each feature (**Triage suggestions**, **Reply drafts**, and **Chat intake** for chat channels) picks one of:

1. **The server's provider.** Set by the server operator for every workspace. On Colliedesk Cloud it
   is **Colliedesk AI**: Anthropic's Claude models, run for HC Labs and included in your plan. On your
   own server it is whatever the operator configured (see
   [environment variables](../self-host/environment-variables.md#ai-provider)); without one, Settings
   explains the two ways to get started. If the operator switches the server's provider off, the features
   set to it stop and Settings says so; they run again once it is on, or pick another provider.
2. **A provider you add.** Your workspace pays that provider directly.
3. **None.** The feature stays off.

## Add your own provider

In **Settings → Built-in AI → Providers**, admins choose **Add provider**:

1. **Anthropic**: an API key from console.anthropic.com. Requests go to the Claude API.
2. **OpenAI**: an API key from platform.openai.com. The address is filled in.
3. **OpenRouter**: one OpenRouter key for hundreds of models. The address is filled in.
4. **Other or self-hosted**: any OpenAI-compatible server (Groq, vLLM, Ollama, LM Studio). Enter the
   base URL with its version path, for example `http://localhost:11434/v1`.

After the key (and the address), choose **Load models**: Colliedesk asks the provider which models
the key may use and lists them, so you tick the ones you want instead of typing names. The list holds
only models that answer what Colliedesk sends (a JSON answer, and a reasoning effort): for Anthropic,
the Claude models whose capabilities say so; for OpenAI, its reasoning models (GPT-5 and later, the o
series), not the older GPT-4 and GPT-3.5 or the image, audio and embedding models; for OpenRouter, the
models that take structured output, without the batch-only entries. A model the provider has retired
or set to shut down is left out. If a check still refuses a model, the message names it. With many
models, a search box finds one whatever the punctuation or spacing: `gpt5 mini` finds `gpt-5-mini`.
Loading the list saves nothing and costs nothing. A server that keeps no list (some self-hosted ones) says so; type
the model names then, separated by commas.

Pick the models you want to use; the first is the default. Before anything is saved, Colliedesk sends
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
emails removed. Private notes are never sent. Colliedesk does not keep what it sent the provider, nor the
provider's answers, apart from the draft or suggestion itself. A draft keeps its text and the request
that asked for it until 90 days after it ended, and both are in the export (`ai-drafts.ndjson`). Each provider keeps data under its own
policy; read it before you add one, for example [Anthropic's](https://www.anthropic.com/legal/privacy).

To keep data on your own servers, add an OpenAI-compatible provider on your network (vLLM, Ollama)
and use it for every feature.

### OpenRouter: which providers serve your requests

OpenRouter sends each request to one of the companies that serve the model. Its **routing** keeps your
requests on some of them: `data_collection: deny` only on providers that do not store or train on what
they are sent, `zdr: true` only on providers with zero data retention
([OpenRouter's provider routing](https://openrouter.ai/docs/features/provider-routing)). Set it with the
provider (`routing` on `POST /api/v1/ai/providers` and `PATCH /api/v1/ai/providers/{id}`, or the MCP
tools `colliedesk_ai_provider_add` and `colliedesk_ai_provider_update`); Settings does not show it yet.
A model that no such provider serves fails its check, so you know before a feature uses it. Tightening
the routing takes effect at once. Loosening it (deny to allow, zero retention off) lets customers' data
reach more providers, so from an AI agent it always becomes a [request a person approves](agent-requests.md).
Other servers have no routing, and refuse it.

On OpenRouter, a feature's **effort** goes to the models OpenRouter lists as reasoning at that effort;
another model answers without one, as before.

## Work runs in the background

Suggestions and drafts run as background tasks, so no screen waits for a model: a task that hits a
busy provider tries again a little later (three times at most), and one whose feature was turned off
meanwhile is dropped without calling anyone. A locked workspace runs none.

## Reply drafts

With **Reply drafts** on, the reply box of a conversation has **Draft with AI**; while they are off it
says **Off**, and takes an admin to **Settings → Built-in AI** (others are told to ask an admin). Say what the reply
should say (optional, up to 2,000 characters: "offer a refund", "ask for the order number"), or leave
it empty for a reply to the customer's newest message. You keep working while it is written; the
conversation never waits for the model.

The draft arrives as plain text with:

1. **Sources**: the knowledge base articles and solved tickets it used, named by Colliedesk, never by
   the model. A source the model invents is dropped.
2. **Missing information**: what the reply needs that no source said, instead of a guess.
3. **Check before sending**: an email address, a number of six digits or more, or another customer's
   name that is in the draft but not in this conversation. A model can copy one from a past ticket or
   be told to add one by a message; read them before you send. A word in an alphabet the conversation
   does not use (a model slipping into another language) is listed too, and so is a masked detail
   (`[email]`, `[number]`, `[company]`) copied from a past ticket for you to fill in. Where the model
   copied `[customer]`, Colliedesk puts this customer's name (or leaves the greeting without one).

**Replace** puts the draft in the reply box in place of what you typed; **Insert below** adds it under
your text; **Discard** throws it away. Nothing is sent until you press **Send**, and you can edit
every word. Asking again replaces your earlier draft. **Stop** ends one still being written; it is not
counted. A draft nobody used for 7 days expires, and resolving or closing the conversation discards
it.

Colliedesk keeps how each draft ended (sent nearly as written, sent edited, discarded, replaced,
expired), so a team can see whether drafts help, and an article's **Used by AI** box says how many
drafts used it this month. A reply sent from a draft says so on the timeline: **from an AI draft, sent as written** or
**from an AI draft, edited**. A draft that failed says why, with **Try again** (the same prompt) and,
when a setting is the cause (the provider's key or credit, the included AI), the way to Settings → Built-in AI.
A draft's text and prompt are erased 90 days after it ended; the numbers stay. At most 30 drafts a
minute are asked for in a workspace.

### Write a draft when a customer writes

Admins can let drafts write themselves: **Settings → Built-in AI**, under **Reply drafts**, set **Default
for channels** to **Suggest** (or do it on one channel, in its **AI** tab). A dialog says first what it
would have meant in the last 30 days (how many customer messages, about what a draft costs, and how
much of the included AI it may use, or that your own key is billed). It is **Off** until an admin
picks Suggest, and goes back to Off when drafts turn off or move to another provider; it stays Off until
an admin picks Suggest again.

Each new customer message (by email, the web form or a problem report) then gets a draft. It waits a
minute for more messages from the customer, so several in a row get one draft, which answers them
all. Imported tickets, auto-replies, bounces and mail that fails its sender check get none, and
neither does a message a teammate has answered by the time the draft would be written. A newer
customer message replaces a draft nobody used once its new draft is written (until then, and if a
limit stops the new one, the earlier draft stays); a person who asks for a draft with **Draft with AI**
replaces the automatic one.

The draft waits above the reply box for everyone on the team, with its sources: **Use draft** puts it
in an empty reply box; with text in the box, **Insert below** or **Replace my text**; **Discard**
throws it away. **AI never sends**: a person reads it and presses **Send**. When the model thinks the
message needs no reply (a thank-you), the draft is folded with **Show draft**. On a short screen
every draft starts folded to one line with its buttons, so the conversation stays in view; **Hide**
folds an open one.

Automatic drafts stop at 100 a day (UTC; admins change it in **Automatic runs**), at 5 per
conversation in 24 hours, and, on the server's provider, once they used half of the month's included
AI: the other half stays for drafts people ask for and for triage. Paused, the conversation and
Settings say until when; **Draft with AI** keeps working. Restoring a workspace from a backup turns
the automatic mode off.

## Triage suggestions

With **Triage suggestions** on, a new ticket's first customer message (by email, the web form or the
API) asks the model for tags, a priority and the teammate best placed to answer. It reads the ticket,
your matching knowledge base articles, and how your team sorted similar solved tickets. The suggestion
appears at the top of the conversation's details as **Suggested by AI** within a few seconds, with a
one-sentence reason and the articles and tickets it rests on. While it is written you keep working;
**Cancel** stops it. On a narrow screen the **Show details** button says **AI suggestion** while one
waits.

It names only your own tags and active teammates; anything else the model names is dropped. What the
conversation already has is not suggested, and a field someone changed before the suggestion came is
left alone: the card says "Not suggested: you set Urgent before this arrived".

Nothing changes until someone accepts. Each part (tags, priority, assignee) has **Accept**; **Accept
all** (or **Accept the rest**) takes what is left, and **Dismiss** drops it. Tags are added, never
removed, and anything accepted is changed back like any other change. **Suggest again** asks by
hand. When the automatic limit below is reached, the card says automatic suggestions are paused
until the next midnight UTC (shown in your timezone) and **Suggest now** still asks for one. A
failure says why and what to do next, for example to open **Settings → Built-in AI** when the included AI is
used up.

Imported tickets get no suggestion. A suggestion nobody used for 7 days expires; its reason is erased
90 days after it ended. Colliedesk keeps whether each one was accepted, in part, or dismissed.

### Apply suggestions automatically

Admins can let suggestions apply by themselves: **Settings → Built-in AI**, under **Triage suggestions**, set
**Default for channels** to **Auto** (or do it on one channel, in its **AI** tab; a dialog says what it does
first). Turning triage on starts at **Suggest**; moving it to another provider goes back to Suggest, and
it stays there until an admin picks Auto again. **Off** makes suggestions only when someone asks. Pick what applies by itself: **Tags** and **Priority** (the default) and
**Assignee**. A part not picked waits for someone to accept it, as above.

It only fills what nobody set: tags are added, never removed; the priority is set only while it is
still Normal on a ticket a customer started (by email, the web form or a problem report); a teammate is
assigned only when nobody is. What it leaves says why on the card ("Not set: Sam Ortiz was already
assigned"). Only the suggestion a new ticket gets applies by itself; one asked for with **Suggest
again** waits for a person.

Each change shows **Applied by AI** on the conversation's timeline and in its details, with **Undo**
for each part and **Undo all**. Undo takes back only what is still AI's: a part someone changed since
is kept, and the card says so. **Apply again** applies it once more, filling only what is still
empty. A teammate AI assigns gets the "Assigned to you by AI" notification. It costs nothing more: it
uses the suggestion already made. Settings shows how many suggestions AI applied and how many were
undone in the last 30 days. Restoring a workspace from a backup turns the automatic mode off.

## What AI does by itself, per channel

Each feature has a level: what it does without anyone asking.

| Feature | Off | Suggest | Auto |
|---|---|---|---|
| Triage | Only when someone presses **Suggest** | Suggests for every new conversation | Applies the picked parts by itself (above), with **Undo** |
| Reply drafts | Only **Draft with AI** | Writes a draft when a customer writes | Never: AI never sends |
| Intake (chat channels only) | The channel's rules decide | The rules decide; AI leaves a suggestion for review | AI decides what is clear enough; otherwise the rules decide. Until a channel's own decisions show AI is ready, AI only suggests here too, and the rules decide at once |

The workspace's level is **Default for channels** under each feature in **Settings → Built-in AI**. A
channel can set its own level for a feature, and a chat channel for each of its scopes (one-to-one
messages, groups), for example triage Auto on the support mailbox and Off on the sales one. A scope
without its own level follows the channel, and the channel follows the workspace. A level never goes
past what the workspace allows: while a feature is off for the workspace, it is off on every channel,
whatever the channel says. People can always ask by hand (**Suggest**, **Draft with AI**) while the
feature is on.

Email and web forms have no intake: every message or form sent is a conversation, as the channel's
rules always made it, and triage and drafts run after it is created. Intake is for chat channels,
where many short messages arrive and some are not requests.

When a feature moves to another provider, or is turned on again, its consent is new: every channel
that ran it by itself goes a step down (Auto to Suggest; drafts written by themselves to drafts asked
for), and the history says why. Intake also goes back to Suggest on a channel when it moves to
another model, since what it was checked on was the old model's decisions. Restoring a workspace from
a backup does the same.

Admins set a channel's levels in **Settings → Channels → <the channel> → AI**: a row per feature, a
column per scope, each set to the default it follows or to Off, Suggest or Auto; under it, what the level
does, or "Off: Triage is off for the workspace" with **Turn on**. Raising triage to Auto or drafts to
Suggest asks with the same dialog as Built-in AI. Under each feature, **Built-in AI** lists the
channels that differ ("Support · Auto"), each a link to its AI tab, or says that every channel follows
the default. The same is `PUT /api/v1/channels/{id}/ai/{slot}` and the MCP tools
`colliedesk_channel_ai_set` and `colliedesk_channel_ai_get`. Every change is in the AI history and in
the channel's history. An AI agent lowers a level with an admin token; raising one (or loosening intake's
settings) always becomes a [request a person approves](agent-requests.md).

Before intake's AI reads a chat, names become "Customer A" or "Team 1", passwords and codes are hidden,
and phone numbers, email addresses, IBANs and the numbers after an English account or ID word ("account
number", "passport") are masked. A channel in another language adds its own words in intake's settings:
`redact_keywords` for the words that come before a password or a code, `contact_labels` (`account`, `id`)
for those before a bank account or an identity document number, and `redact_patterns` for the shapes of
an identifier no word names (a national ID number, a phone number written without its prefix): each a
pattern in RE2 syntax with what it masks (`phone`, `id_number`, `account_number` or `hidden`), at most 20
of 200 characters; a pattern that would match nothing at all, is too large or does not compile is refused.
Taking a word or a pattern out masks less, so it needs the approval raising AI needs.

Intake may also send pictures customers sent, the customer's profile, or an integration's data, only
once an admin agreed to each (`colliedesk_ai_source_consent_request`, taken back with
`colliedesk_ai_source_consent_remove`). The conversation's own text is covered by turning the feature on.
Pictures go to intake's AI only when the workspace agreed to send them, the channel's images source is
on, and its model (and its fallback model) reads pictures, as the provider says: every Claude model does;
on OpenRouter, a model that lists images among its inputs. At most four of a card's pictures go, each at
most 5 MB, never with messages that talk about a password or a code. A card that is only pictures is then
decided like any other.

## When AI acts on its own

At **Auto**, intake's AI opens, joins or sets aside a card by itself only for an action its labels show it
gets right on that channel's scope (it never writes to the customer: a conversation it opens sends the
channel's own received notice, if it has one, as when the rules open one); any other action stays a suggestion on the card and the rules decide
at once. Labels are only explicit ones: a person who decides a card as AI suggested (it was right), decides
otherwise or moves the messages AI placed (it was wrong), and offline evaluations an operator recorded for
the model intake runs and this server's prompt version (`collie intake-eval`). A decision nobody looked at counts for nothing: silence
is not a label.

| Action | Opens with | Never with |
|---|---|---|
| Open a conversation | 30 labels, 90 % right | more than one conversation opened for chatter in a week |
| Add to a conversation | 30 labels, 95 % right | a new request merged into an old conversation (in the last 30 days) |
| Set aside | 30 labels, 95 % right | a request set aside (in the last 30 days) |

People's labels count for 30 days; an evaluation counts only for the model and prompt version it ran, and
a newer run of the same dataset replaces it. Since cards at Auto go to the rules when AI may not act, most
never reach a person: in practice a gate opens with an evaluation, or with an admin's override.
An admin may open an action by hand before its labels do (`gate_override` with a `gate_override_reason`,
which needs the approval raising AI needs). Readiness is `GET /api/v1/channels/{id}/ai/intake/readiness?scope=`
(each action's gate, labels, what it misses, what AI did on its own and how much people corrected, and what
a month costs) and the MCP tool `colliedesk_channel_ai_intake_readiness_get`; evaluations are
`POST`/`GET /api/v1/channels/{id}/ai/intake/evaluations` (`colliedesk_channel_ai_intake_evaluation_record_request`,
which always waits for a person, and `colliedesk_channel_ai_intake_evaluations_list`).

## When intake's model fails

Intake can have a **fallback model**: another of its provider's models, tried once when its model
times out, is busy, refuses for its rate, is gone, or answers what Colliedesk cannot read. A refused key
or an account out of credit is not tried again on the other model. Each call is in the usage. Set it
with `fallback_model` on `PUT /api/v1/ai/slots/intake` (an empty string removes it); the provider cannot
drop a model intake uses this way. A new fallback model takes intake back to Suggest on every channel,
as another model does.

## Limits

1. **Automatic runs.** Work Colliedesk starts on its own (a triage suggestion for each new ticket, a
   draft for each customer message when that is on) stops at 300 a day (UTC) and 30 a minute in a
   workspace, of which 100 a day may be drafts, whichever provider runs it, so a flood of
   tickets cannot run up a bill. Intake's decisions on chat channels count too, at most 300 a day of
   them (`intake_per_day`), so a busy group cannot spend the day's triage and drafts. Past the daily
   limit automatic suggestions wait until 00:00 UTC; what an agent asks for still runs. Admins change both numbers in **Settings → Built-in AI → Features**
   (**Automatic runs**, **Change**). Each task counts once, however many times it is retried.
2. **Included AI.** The server's provider can come with a monthly allowance (on Colliedesk Cloud,
   included in your plan). When it is used up, features on the server's provider stop until the first
   day of the next month (UTC), and Settings says so; your own providers keep working. **Settings → Built-in AI →
   Usage** shows what is used. A call that starts sets aside the most it can cost, so calls running at
   the same time never spend more than is left; the last few cents of a month can stay unused.

3. **AI chat.** The help center's [AI chat](ai-chat.md) has limits of its own and never uses the
   included AI above, so customers asking questions cannot stop your team's drafts and suggestions.

## AI chat

**AI chat** answers your customers on the help center from your published articles. It is a feature
of this page like the others (provider, model, on or off), and it needs a [Help chat](help-chat.md)
channel too. Its use, its chats and their transcripts are on the **AI chat** tab; see
[AI chat](ai-chat.md).

## History and usage

**Settings → Built-in AI → History** lists who added, changed or deleted a provider or a key, and who turned a
feature on, off or elsewhere, with keys shown only by their last 4 characters. Every call to a provider
leaves one usage record (the feature, the model, tokens in and out, whether it failed, and for the
server's provider its cost at that day's prices), never the text sent or received. When the provider
reports what a call cost (OpenRouter does with every answer), that is kept too, in the provider's own
unit: OpenRouter's credits, never converted to dollars. **Settings → Built-in AI →
Usage** sums a month per feature and provider. Both are in the [export](export.md).

## Agents and external AI agents

Agents see these settings but only admins change them. An AI agent with a draft token asks for a
reply draft, reads it and discards it like a person (`colliedesk_ai_draft_create`,
`colliedesk_ai_draft_get`, `colliedesk_ai_drafts_list`, `colliedesk_ai_draft_discard`); sending a
reply still needs the send tier or a person's approval. A send token that replies from a draft passes
its id (`ai_draft_id` on `colliedesk_reply_send`), so the draft records how close the reply stayed to
it, as when a person sends it. With a draft token it reads, asks for, applies
and dismisses triage suggestions too (`colliedesk_ai_triage_get`, `_create`, `_accept`, `_dismiss`). Through [MCP](mcp.md), an AI agent with a read
token can read them, and one with an admin token can add, check, change and delete providers (a token
below admin is refused outright on the two calls that carry a key, so a key never waits in a request).
Turning a feature on or moving it always becomes a [request a person approves](agent-requests.md).

## Sign in with a subscription

A Claude Pro or Max plan, or a Gemini subscription, cannot be used: their terms do not allow another
application to use them. Use an API key.
