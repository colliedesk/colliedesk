# AI providers and features

Colliedesk can use a language model to suggest how to sort a new ticket (tags, priority, assignee)
and to draft a reply an agent edits and sends. A model only suggests and drafts: a person accepts each
suggestion, and nothing reaches a customer until someone presses **Send**. **Settings > AI** decides
which model each feature uses and records who agreed to send data to it.

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
2. **OpenAI**: an API key from platform.openai.com. The address is filled in.
3. **OpenRouter**: one OpenRouter key for hundreds of models. The address is filled in.
4. **Other or self-hosted**: any OpenAI-compatible server (Groq, vLLM, Ollama, LM Studio). Enter the
   base URL with its version path, for example `http://localhost:11434/v1`.

After the key (and the address), choose **Load models**: Colliedesk asks the provider which models
the key may use and lists them, so you tick the ones you want instead of typing names. The list holds
only models that answer what Colliedesk sends (a JSON answer, and a reasoning effort): for Anthropic,
the Claude models whose capabilities say so; for OpenAI, its reasoning models (GPT-5 and later, the o
series), not the older GPT-4 and GPT-3.5 or the image, audio and embedding models; for OpenRouter, the
models that take structured output. If a check still refuses a model, the message names it. Loading the
list saves nothing and costs nothing. A server that keeps no list (some self-hosted ones) says so; type
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

With **Reply drafts** on, the reply box of a conversation has **Draft with AI**; while they are off it
says **Off**, and takes an admin to **Settings > AI** (others are told to ask an admin). Say what the reply
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
when a setting is the cause (the provider's key or credit, the included AI), the way to Settings > AI.
A draft's text and prompt are erased 90 days after it ended; the numbers stay. At most 30 drafts a
minute are asked for in a workspace.

### Write a draft when a customer writes

Admins can let drafts write themselves: **Settings > AI**, under **Reply drafts**, turn on **Write a
draft when a customer writes**. A dialog says first what it would have meant in the last 30 days (how
many customer messages, about what a draft costs, and how much of the included AI it may use, or that
your own key is billed). It is off until an admin turns it on, and it turns off when drafts turn off
or move to another provider; it stays off until an admin turns it on again.

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
failure says why and what to do next, for example to open **Settings > AI** when the included AI is
used up.

Imported tickets get no suggestion. A suggestion nobody used for 7 days expires; its reason is erased
90 days after it ended. Colliedesk keeps whether each one was accepted, in part, or dismissed.

### Apply suggestions automatically

Admins can let suggestions apply by themselves: **Settings > AI**, under **Triage suggestions**, turn on
**Apply suggestions automatically** (a dialog says what it does first). It is off until an admin turns
it on, and it turns off when triage turns off or moves to another provider; it stays off until an admin
turns it on again. Pick what applies by itself: **Tags** and **Priority** (the default) and
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

## Limits

1. **Automatic runs.** Work Colliedesk starts on its own (a triage suggestion for each new ticket, a
   draft for each customer message when that is on) stops at 300 a day (UTC) and 30 a minute in a
   workspace, of which 100 a day may be drafts, whichever provider runs it, so a flood of
   tickets cannot run up a bill. Past the daily limit automatic suggestions wait until 00:00 UTC; what
   an agent asks for still runs. Admins change both numbers in **Settings > AI > Features**
   (**Automatic runs**, **Change**). Each task counts once, however many times it is retried.
2. **Included AI.** The server's provider can come with a monthly allowance (on Colliedesk Cloud,
   included in your plan). When it is used up, features on the server's provider stop until the first
   day of the next month (UTC), and Settings says so; your own providers keep working. **Settings > AI >
   Usage** shows what is used. A call that starts sets aside the most it can cost, so calls running at
   the same time never spend more than is left; the last few cents of a month can stay unused.

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
reply still needs the send tier or a person's approval. With a draft token it reads, asks for, applies
and dismisses triage suggestions too (`colliedesk_ai_triage_get`, `_create`, `_accept`, `_dismiss`). Through [MCP](mcp.md), an AI agent with a read
token can read them, and one with an admin token can add, check, change and delete providers (a token
below admin is refused outright on the two calls that carry a key, so a key never waits in a request).
Turning a feature on or moving it always becomes a [request a person approves](agent-requests.md).

## Sign in with a subscription

A Claude Pro or Max plan, or a Gemini subscription, cannot be used: their terms do not allow another
application to use them. Use an API key.
