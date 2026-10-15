-- The application sets trial_ends_at from its own clock when it creates a workspace; a path that
-- forgets must fail instead of silently taking the database clock.
ALTER TABLE workspaces ALTER COLUMN trial_ends_at DROP DEFAULT;
