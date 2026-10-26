# Moving messages

A person or an AI agent moves customer messages that landed in the wrong conversation: to another
conversation, into a new one (a split), or, for a chat message, back to the intake queue. Code:
`server/internal/store/message-moves.go`, routes in `httpapi/message-move-handlers.go`.

1. **Only customer messages move.** Replies and notes stay; a reply's delivery, reply number and thread
   belong to the conversation it was sent from. Files follow their message (`attachments.message_id`).
2. **Every move is a row.** `message_moves` keeps the messages, both conversations, the statuses it
   changed and the thread keys it took. Its undo marks `undone_at`; nothing is deleted. Only a message's
   newest live move (by `seq`, not by time) is undone, so two undos never fight over one message.
3. **Lock order.** Both conversations are locked `FOR UPDATE` in id order, then the messages. Two moves
   between the same conversations in opposite directions wait instead of deadlocking.
4. **Thread keys move only when a message recorded them.** `conversation_thread_keys.message_id` is
   set at ingest (`Thread.Record`, chat types); the key moves with that message so the customer's next
   chat message follows it. Keys recorded before the column, and email's reply token (the conversation's,
   not a message's), stay. Email needs nothing more: a reply's `In-Reply-To` is matched against
   `messages.external_id` wherever the message now is.
5. **Another contact's message stays theirs.** `messages.other_contact_id` is the contact whose
   conversation a message came from while it sits in another contact's; it returns to NULL when the
   message goes back. Outgoing mail leaves those messages out of `References`/`In-Reply-To` and out of
   the addresses that prove the customer wrote first (trial Cc), and the web app's suggested Cc skips
   them. A channel never sets the column: a colleague answering by email with reply-all is not moved.
6. **Statuses.** A resolved or closed target reopens (the status change names the actor). Undo gives it
   back only while the target is still open, no status change other than to open followed, and no
   customer wrote in it since. `close_source` closes the source with `merged_into_id`; its undo
   reopens it while it is still closed pointing there. A split's conversation, emptied by undo, closes
   pointing back.
7. **Correcting AI.** Moving messages whose intake card AI decided last records a person's
   `intake_decisions` row with `corrects_decision_id`: the label evaluation reads and the "AI corrected"
   count. Moving what a person placed records nothing.
8. **Chat items.** A move ends the item's live `conversation_thread_items` link and starts a new one; a
   link is never rewritten, so a conversation's chat keeps what it once held.
