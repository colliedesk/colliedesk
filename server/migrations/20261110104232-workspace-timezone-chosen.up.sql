-- When an admin last chose the workspace's timezone (Settings → General, the API or MCP). NULL while it is
-- still the UTC default nobody chose: the first sign-in of the person who created the workspace then
-- starts it on their browser's zone (docs/user/timezones.md). No history says whether an existing
-- workspace's zone was chosen, so every workspace from before this migration counts as chosen and keeps
-- its zone, UTC included.
ALTER TABLE workspaces ADD COLUMN timezone_chosen_at timestamptz;
UPDATE workspaces SET timezone_chosen_at = now();
