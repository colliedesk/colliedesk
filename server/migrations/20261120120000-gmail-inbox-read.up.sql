-- Reading the connected Gmail inbox (issue #95, phase B), on a server whose operator turned on
-- google.read_mailbox and for a grant that includes gmail.readonly.
-- inbox_history_id is where the next read starts: Gmail's history id once every message before it was
-- stored. NULL means reading has not started; the first read records the mailbox's current history id,
-- so only mail that arrives after it comes in (no backfill).
-- inbox_read_at is when the last read finished; when Gmail no longer keeps the history id, the next read
-- lists the inbox from that time, and messages already in Colliedesk are recognised by their Message-ID.
-- inbox_error_code says why reading stopped while sending goes on: 'permission' (Google refuses to read
-- for this grant, such as gmail.readonly unticked or withdrawn). Reconnecting clears it.
ALTER TABLE mailbox_connections
    ADD COLUMN inbox_history_id text CHECK (inbox_history_id ~ '^[0-9]{1,30}$'),
    ADD COLUMN inbox_read_at timestamptz,
    ADD COLUMN inbox_error_code text CHECK (inbox_error_code IN ('permission'));
