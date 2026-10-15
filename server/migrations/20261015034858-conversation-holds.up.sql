-- A conversation received over the monthly Free quota is stored in full but held: hidden from the
-- inbox and search until the workspace upgrades or the month that held it ends. held_at is the
-- moment it was held, and NULL for every conversation that is not held. Nothing is ever deleted.
ALTER TABLE conversations ADD COLUMN held_at timestamptz;

-- Held conversations are rare, so the index stays tiny: it serves the held count on the billing
-- page and the release of a workspace's held conversations on upgrade, and costs the inbox list
-- nothing for the unheld rows.
CREATE INDEX conversations_held_idx ON conversations(workspace_id, held_at) WHERE held_at IS NOT NULL;
