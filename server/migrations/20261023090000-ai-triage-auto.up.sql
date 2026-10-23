-- Triage suggestions applied by AI (#89): an admin lets a workspace's automatic suggestions fill the
-- tags, priority or assignee nobody set yet, with an undo. Off unless an admin turns it on; it turns
-- off with the feature and when the feature moves to another provider.
ALTER TABLE ai_slots
    ADD COLUMN automatic boolean NOT NULL DEFAULT false,
    -- Which parts of a triage suggestion apply by themselves.
    ADD COLUMN auto_parts text[] NOT NULL DEFAULT '{}',
    ADD CHECK (NOT automatic OR enabled),
    ADD CHECK (auto_parts <@ ARRAY['tags', 'priority', 'assignee']),
    ADD CHECK (slot = 'triage' OR auto_parts = '{}'),
    ADD CHECK (NOT (automatic AND slot = 'triage') OR cardinality(auto_parts) > 0);

ALTER TABLE ai_triage_suggestions
    -- When AI applied it, and what: {"tag_ids": [...], "priority": "...", "assignee_agent_id": "..."}.
    ADD COLUMN auto_applied_at timestamptz,
    ADD COLUMN auto_applied jsonb NOT NULL DEFAULT '{}',
    -- What AI left because a person had set it: {"priority": {"reason": "set_by_person", "value": "high"}, ...}.
    ADD COLUMN skipped jsonb NOT NULL DEFAULT '{}',
    -- What an undo took back, and what it kept because a person changed it after AI: {"priority": "urgent"}.
    ADD COLUMN undone_parts jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN kept jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN undone_at timestamptz,
    ADD COLUMN undone_by_agent_id uuid,
    ADD FOREIGN KEY (workspace_id, undone_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (undone_by_agent_id),
    DROP CONSTRAINT ai_triage_suggestions_outcome_check,
    ADD CONSTRAINT ai_triage_suggestions_outcome_check
        CHECK (outcome IN ('accepted', 'partly_accepted', 'dismissed', 'superseded', 'expired', 'auto_applied', 'undone'));
-- The conversation's newest suggestion AI applied, which a later "Suggest again" does not hide.
CREATE INDEX ai_triage_auto_applied ON ai_triage_suggestions(workspace_id, conversation_id, auto_applied_at DESC) WHERE auto_applied_at IS NOT NULL;
