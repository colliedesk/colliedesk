-- Cc recipients of an emailed reply, stored on the message so a retry sends to the same list.
-- NULL means no Cc (every older message). The API enforces the address rules; the check only
-- bounds the list. Older images ignore the column.
ALTER TABLE messages ADD COLUMN cc text[] CHECK (cc IS NULL OR cardinality(cc) BETWEEN 1 AND 10);
