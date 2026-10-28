-- acme's active domain stopped proving its claim a day ago; beta's pending claim gets a known target.
UPDATE help_center_domains SET claim = 'acacacacacacacacacacacac', failing_since = '2026-10-21 08:05:00+00'
WHERE workspace_id = md5('ws-acme')::uuid;
UPDATE help_center_domains SET claim = 'bebebebebebebebebebebebe', pending_reason = 'proof_lost'
WHERE workspace_id = md5('ws-beta')::uuid;
