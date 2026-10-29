# AI chat

AI chat answers your customers on your help center, from your **published help articles** only, with
the articles it used shown under each answer. A customer who wants a person selects **Talk to a
person**, and a conversation opens in your inbox ([Help chat](help-chat.md)).

## Turn it on

1. **Settings → Built-in AI → Features → AI chat**: pick the provider and model and turn it on. On
   Colliedesk Cloud the server's provider answers with Claude Haiku 5.5; on your own server, add a
   provider first. A server with no provider offers no AI chat.
2. **Settings → Channels → Add channel → Add help chat**: what the chat says first and the questions it
   offers ([Help chat](help-chat.md)).

Both must be on. Off, the chat pages answer `404`, as a help center that is off.

## What the AI reads and says

1. **Your published articles**, the ones the help center shows, except those marked **Keep away from
   AI**. Drafts, articles in the trash and translations are never read. With no published article the
   chat says it cannot find an answer and offers **Talk to a person**.
2. It answers in the language of the question, cites the articles it used, and says so when the
   articles do not answer the question. It never makes up a price, an address or a link: an email
   address, a phone number or a link that is not in your articles is hidden in its answer.
3. **Extra instructions** of the channel are added to what it is told, but cannot add facts.
4. The text a customer types goes to the provider of the AI chat feature, as for any AI feature
   ([AI](ai.md#what-a-provider-receives)). AI chat never sees your conversations, contacts or notes.

## Limits

On Colliedesk Cloud's own provider, AI chat has a monthly limit of answers and of spend, per plan, and
a day may use a tenth of each. It never uses the included AI of your team's features.

| Plan | Answers a month |
|---|---|
| Trial | 50 |
| Solo | 300 |
| Small | 750 |
| Team | 1,500 |
| Team 25 | 3,000 |
| Founding | 600 |

When a limit is reached, customers see the help articles that match their question and **Talk to a
person**, until the next day or month (UTC). On your own provider, or a server without plan limits,
an admin can set a **monthly spend limit** for AI chat on the **AI chat** tab of **Settings → Built-in AI**; empty is
no limit. **Settings → Built-in AI → AI chat** shows the month's answers, spend, chats, hand-offs and
how many answers were rated helpful, with a warning at 80% and when answers are paused.

## Chats and transcripts

**Settings → Built-in AI → AI chat → Chats** lists the chats, newest first, with where each started
(the chat page, an article, your site), its state (open, handed off, ended) and whether an answer was
rated not helpful. Open one to read it with the articles each answer cited. Every member of the team
reads them there (the tab shows only the chats to someone who is not an admin), and from a conversation
a hand-off opened (**Open chat transcript**); only an admin deletes one, for good (the conversation
keeps its own copy).

A chat's texts are kept 90 days after its last question, then erased; its numbers stay a year. They
are in the [export](export.md) (`ai-chat-sessions.ndjson`).

## On the customer's side

A chat is kept in the customer's browser for 30 days with one cookie that only works for your help
center; **New chat** starts over. When the chat is on, the chat page and article pages load a small
script of the help center; off, the help center has no script and no cookie.
