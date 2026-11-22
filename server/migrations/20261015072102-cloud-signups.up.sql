-- One row per email that ever started a cloud trial, so deleting a workspace does not hand out a
-- second trial. Only keyed hashes are stored: HMAC-SHA256 under SIGNUP_HASH_KEY of the normalized
-- address (a repeat is recognised; without the key the table cannot be tested against a list of
-- addresses) and of the client IP, which is cleared 30 days after the signup, long after the 24
-- hour rate window needs it.
-- Platform-level table: no workspace policy, only the privileged signup transaction reads it.
-- Older images ignore it, so rolling back to one only stops the signup checks.
CREATE TABLE cloud_signups (
    email_hash text PRIMARY KEY,
    signup_workspace_id uuid REFERENCES workspaces(id) ON DELETE SET NULL,
    ip_hash text,
    created_at timestamptz NOT NULL
);

CREATE INDEX cloud_signups_ip_idx ON cloud_signups(ip_hash, created_at)
    WHERE ip_hash IS NOT NULL;

-- The app role gets every new table by default; this one is for the privileged path only.
SELECT secure_server_table('cloud_signups');
