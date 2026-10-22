# AI drafts, triage and auto-answer

When a customer writes, Colliedesk drafts a reply and a triage suggestion from your own knowledge
base and resolved conversations. The draft is ready a few seconds later, above the reply box. Nothing
reaches the customer until a person sends it, unless you turn on auto-answer for topics where drafts
have proved right.

The server needs a model provider. On your own server the administrator sets one up
([self-host: AI drafts](../self-host/environment-variables.md#ai-drafts)); without one the AI controls are hidden and nothing is sent anywhere.

## The draft

Open a conversation. Under the timeline, **AI draft** shows:

- the reply, in the customer's language;
- how far to trust it:
  - **From your knowledge base**: every statement in the reply cites a published article that was
    shown to the model, the model was sure, and none of the risks below applies;
  - **Check before sending**: right in part, or resting on a past conversation only, or a risk applies.
    The card says why;
  - **AI suggests a person answers this**: the knowledge base does not settle it, or only your team can
    act (the customer's own account, a refund). The note says what is missing;
- the sources it cites, which open the article or the conversation;
- a triage suggestion: tags (only tags that exist in your workspace) and a priority.

**Send** sends the reply as it is. **Edit** changes it first. **Discard** throws it away. **Apply**
adds the suggested tags and sets the priority; undo them like any other change. **Write a new draft**
or **Ask AI for a draft** asks again, for example after you published a missing article (60 times an
hour per workspace at most).

A draft is never written from: private notes, unpublished or trashed articles, articles you excluded
from AI ([knowledge base](knowledge-base.md)), another workspace's data. Customer names, emails,
companies and long numbers from past conversations are masked before a model reads them.

A draft whose reply holds a link or an email address that no source contains is never rated "From your
knowledge base" either. Before a draft gets that rating, a second read checks it against the sources it
cites, as a support lead would: does it answer everything, with every step, condition and caveat the
customer needs? If not, it shows as "Check before sending" with what to add.

The risks that keep a draft from "From your knowledge base": a refund or an exception, the customer's
own account or order, account access or security, a legal matter or formal complaint, an upset
customer, something down right now, or work only your team can do.

## Auto-answer

Admins turn it on in **Settings → AI drafts**. It is off by default. A draft goes out on its own only
when all of these hold:

1. it is rated "From your knowledge base" and answers the customer (not a question back, not a handoff);
2. the knowledge base article it rests on has a record: at least **10** of its drafts were reviewed by a
   person (sent or discarded), and at least **90 %** of those were sent with small changes at most
   (the sent reply shares 85 % of its words, in order, with the draft). Both numbers are settings;
3. the newest public message is still the customer's, the conversation is open, and that message is
   email that passed DMARC at your mail relay. Web form messages and mail that failed or skipped the
   check are never answered on their own: anyone can type an address into a form or forge a From line.

It waits **5 minutes** first (a setting). The card shows **Sends on its own at …** with **Stop**, which
keeps it as a draft for a person. A reply from anyone, a new message from the customer, or turning
auto-answer off stops it too. A reply sent on its own says **Answered by AI on its own** on the
timeline, and the conversation moves to **Pending**. Answers sent on their own never count toward a
topic's record, so auto-answer cannot earn its own trust, and neither do replies an AI agent sends
through MCP. A reply a person writes in their own words while a draft was ready counts as a miss for
that draft's article, as a discard does.

**Settings → AI drafts** lists each article's record (reviewed, sent as drafted, earned or not) and the
last 30 days: drafts, how many were sent unchanged, edited or discarded, sent on their own, and tokens.

## How accurate it is

We measure drafts on a public help center of 102 real articles with customer emails written by a
model, some vague, in other languages, or needing a person; a stronger model grades each draft against
the article that answers it. On emails the knowledge base answers, 92 % of the drafts rated "From your
knowledge base" were correct as written and none was wrong (79 drafts over two independent sets);
every email that needed a person was handed over. About a third to a half of the answerable emails got
that rating; the rest got "Check before sending". Your own numbers are in Settings once your team
reviews drafts. The method is in [the internals](https://github.com/colliedesk/colliedesk/blob/main/docs/internals/ai-drafts.md#measuring).

## AI agents (MCP)

`colliedesk_ai_draft_get` reads the draft (read tier). `colliedesk_ai_draft_update` asks for a new
one, discards it, applies its triage or stops a scheduled answer (draft tier). `colliedesk_ai_settings_get`
and `colliedesk_ai_settings_update` (admin tier) read and change the settings. See [MCP](mcp.md).
