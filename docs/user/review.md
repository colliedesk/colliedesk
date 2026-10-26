# Review

**Review** is where people decide what customers wrote in chat channels that the rules could not file
alone. It is on the left rail only when the workspace has a chat channel that collects messages (its
groups, on most chat types); the number on it is how many cards wait for a person. Email and web forms
never wait here: every message there opens or continues a conversation by the channel's rules.

What one customer writes in one go on such a scope is a **card** (see [Intake queue](channels.md#intake-queue)
for how cards are collected and what the rules decide by themselves). A card waits in Review when:

- a group message has no command (**No command**);
- AI intake was not sure, did not answer in time, or may not decide this yet;
- it is too old to decide alone, or its conversation changed meanwhile;
- it holds a password or a one-time code, hidden from AI (the card shows it to people): ask the customer to
  change it;
- a person put it back.

A card of a customer who has an earlier card still waiting in the same place sits right under that card
(**Waiting for the card above**). Decide the upper one first, so their messages stay in order.

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

Each decision is one press and is saved once: when a teammate (or AI) decided the card first, the card
says who and shows how it stands.

## AI dismissed and Decided

**AI dismissed** lists the cards AI intake set aside as not a request. **Decided** lists every card
that was decided, newest first, with what happened and **who decided**: **Rules · match** (a command, a
reply to a message, a known thread), **Rules · default** (what the scope does when nothing matched),
**AI**, or the person. **Fallback** means AI did not decide in time and the rules did. A card set aside,
by a person or AI, has **Put back in queue**: it returns to Waiting, and its earlier decisions stay in its
history. A card that opened or joined a conversation links to it.

The line at the top counts the last 7 days: the cards, how many became conversations, and how many of
AI's automatic decisions a person corrected. Pick a channel and scope above the list to see only its
cards.

For AI agents and the API, the same cards and decisions are in [Intake queue](channels.md#intake-queue).
