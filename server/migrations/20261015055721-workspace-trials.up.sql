-- Every workspace has a trial end. Existing rows get migration time plus 14 days; the operator sets
-- the real end before billing is switched on. The trial state is computed from this column at read
-- time, so no job moves a workspace between states.
ALTER TABLE workspaces
	ADD COLUMN trial_ends_at timestamptz NOT NULL DEFAULT (now() + interval '14 days'),
	ADD COLUMN trial_extended_at timestamptz;
