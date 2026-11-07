# Review

**Review** is where people decide what customers wrote in chat channels that the rules could not file
alone. It is on the left rail only when the workspace has a chat channel that collects messages (its
groups, on most chat types); the number on it is how many cards wait for a person. Email and web forms
never wait here: every message there opens or continues a conversation by the channel's rules.

What one customer writes in one go on such a scope is a **card** (see [Intake queue](channels.md#intake-queue)
for how cards are collected and what the rules decide by themselves). A card waits in Review when:

- a group message has no command (**No command**);
- intake is at Suggest: every card waits here with AI's suggestion (**AI suggests, a person decides**);
- at Auto, AI was not sure, did not answer in time, or may not decide this yet;
- it is too old to decide alone, or its conversation changed meanwhile;
- it holds a password or a one-time code, hidden from AI (the card shows it to people): ask the customer to
  change it;
- a person put it back.

At Auto, what a customer writes while their card waits joins that card, and AI reads the whole card again:
it may decide it alone then, and otherwise it comes back here with every message in it. At Suggest, a card of
a customer who has an earlier card still waiting in the same place sits right under that card (**Waiting for
the card above**). Decide the upper one first, so their messages stay in order.

A picture, a video or a file a customer sent shows on the card itself, as in a conversation: a picture to
look at, a video or a voice clip to play, any other file to download, and the gallery opens at the one you
press. A file the channel could not fetch says why, for example when it is larger than the channel accepts
(25 MB a file on Zalo). The card shows its first 8 files; **Decide** shows every file under the message that
carried it. An AI agent reads them with `colliedesk_attachment_get` (`gathering_id`, `item_id`, `file_index`)
or `GET /api/v1/intake/gatherings/{id}/items/{itemId}/files/{index}`, with the `messages:read` permission.

## Decide a card

Press **Decide** on a card to read every message of it, then pick one:

- **New conversation**: opens one conversation with everything the card said, as one message. The
  subject starts as the customer's first line (without a command they typed); a **command** of the
  channel adds its tags and priority. When the channel tells customers their request was received, it
  does so here too.
- **Add to a conversation**: adds the card to a conversation of the same place, so a reply never goes
  to someone else: in a group, its open or pending conversations active in the last 72 hours; one-to-one,
  that customer's on the same channel; then the ones resolved lately, which reopen when a card joins
  them. The conversation the rules would pick comes first. Nothing is sent to the customer.
- **Not a request**: sets the card aside, with why (just chat, answered in chat, already reported,
  something else). Nothing is sent.

When AI intake runs on the channel ([AI per channel](ai.md#what-ai-does-by-itself-per-channel)), a card
it did not decide alone shows **Suggested by AI**: what it would do (open a conversation with a subject and
command, add it to a conversation, or set it aside), how sure it is, the runner-up conversation, its reason,
and why it did not do it alone (not sure enough, too old, a password in it, not allowed yet). **Follow
suggestion** decides the card that way; a suggestion to join a resolved conversation says that following
it reopens it. The decide form also starts from the suggestion. Whether a person followed or changed what
AI suggested is kept with the card's decisions, to measure how often AI is right.

Each decision is one press and is saved once: when a teammate (or AI) decided the card first, the card
leaves Waiting and the page says who decided it, with the conversation it opened or joined. When the
customer wrote again while you read the card, nothing is decided and the page says so: at Auto AI reads the
whole card again and it comes back to Waiting, with every message, if AI is not sure.

## A customer who writes after it was resolved

When a customer only thanks the team after their conversation was resolved ("ok", "thank you"), AI intake
adds the message to that conversation and keeps it **Resolved**, so nobody has to resolve it again and no
draft is written for it. The message says **Written after it was resolved · kept Resolved**, with
**Reopen** while the conversation is still resolved. A closed conversation reopens as usual, and a message
with a new question reopens it.

## AI dismissed and Decided

**AI dismissed** lists the cards AI intake set aside as not a request. **Decided** lists every card
that was decided, newest first, with what happened and **who decided**: **Rules · match** (a command, a
reply to a message, a known thread), **Rules · default** (what the scope does when nothing matched),
**AI**, or the person. A card AI decided by itself also says how sure it was, the model it asked (or the
fallback model that answered when that one failed), when, and the reason it gave (kept 90
days). **Fallback** on an older card means AI did not decide in time and the rules did; since intake
sends such cards to Review instead, new cards no longer have it. A card set aside,
by a person or AI, has **Put back in queue**: it returns to Waiting, and its earlier decisions stay in its
history. A card that opened or joined a conversation links to it, with the code customers know it by (`CHD-2026-0008`, `#12`). AI's reason names the conversations it weighed by that code too.

The line at the top counts the last 7 days: the cards, how many became conversations, and how many of
AI's decisions (what it did and what it suggested) a person decided otherwise. Pick a channel and scope above the list to see only its
cards.

For AI agents and the API, the same cards and decisions are in [Intake queue](channels.md#intake-queue).
