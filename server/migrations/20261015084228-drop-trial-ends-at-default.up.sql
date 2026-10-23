-- Every code path now sets trial_ends_at itself (workspace creation, sign-up, the operator
-- commands), so the column needs no default. An insert that forgets it fails instead of silently
-- giving a workspace a free 14 days. Run only after an image that sets it has been in production:
-- an older image inserts workspaces without the column.
ALTER TABLE workspaces ALTER COLUMN trial_ends_at DROP DEFAULT;
