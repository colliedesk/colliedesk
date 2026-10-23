-- Restores the column and index exactly as conversation-holds created them. Nothing is held again.
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS held_at timestamptz;
CREATE INDEX IF NOT EXISTS conversations_held_idx ON conversations(workspace_id, held_at) WHERE held_at IS NOT NULL;
