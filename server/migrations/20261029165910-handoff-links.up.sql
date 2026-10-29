-- A hand-off from APP_ORIGIN to a workspace's own host is its own kind of one-time link: it signs in
-- only on that host and only as a cookie session, and it does not prove an address the way a pairing
-- link does. Additive: existing rows keep their kind.
ALTER TABLE pairing_links DROP CONSTRAINT pairing_links_kind_check;
ALTER TABLE pairing_links ADD CONSTRAINT pairing_links_kind_check CHECK (kind IN ('pairing', 'invite', 'handoff'));
-- Making a hand-off deletes the workspace's expired ones; this keeps that delete from scanning the table.
CREATE INDEX pairing_links_handoff_expiry_idx ON pairing_links(workspace_id, expires_at) WHERE kind = 'handoff';
