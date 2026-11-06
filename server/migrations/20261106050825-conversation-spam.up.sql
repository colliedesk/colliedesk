-- Spam (docs/user/inbox.md "Spam"). A conversation marked as spam takes the status spam: every view, response
-- clock (conversations_sla_change stops any status but open and pending) and count that reads a status leaves it
-- out with no change of its own; the list without a status filter and the results page leave it out by name.
-- Its status history records the change in and out, so Not spam puts back the status it had.
ALTER TABLE conversations DROP CONSTRAINT conversations_status_check,
    ADD CONSTRAINT conversations_status_check CHECK (status IN ('open', 'pending', 'resolved', 'closed', 'spam'));

-- The sender rule: later mail from a customer marked as spam opens or joins a conversation straight in Spam.
-- Not spam clears it.
ALTER TABLE contacts ADD COLUMN spam_since timestamptz;
