-- Hand-offs live a minute and are worth nothing once spent or expired, so they are deleted rather than
-- rewritten: the older constraint cannot store them. A browser mid hand-off signs in again.
-- The row-level security policy shows pairing_links rows only to a privileged transaction, as the
-- migration runner marks it.
SELECT set_config('app.privileged', 'on', true);
DELETE FROM pairing_links WHERE kind = 'handoff';
DROP INDEX pairing_links_handoff_expiry_idx;
ALTER TABLE pairing_links DROP CONSTRAINT pairing_links_kind_check;
ALTER TABLE pairing_links ADD CONSTRAINT pairing_links_kind_check CHECK (kind IN ('pairing', 'invite'));
