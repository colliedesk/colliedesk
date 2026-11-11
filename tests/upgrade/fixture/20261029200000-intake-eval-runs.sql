-- beta: an operator scored the group scope of beta's chat channel on a labelled dataset. Values are fixture data.
INSERT INTO intake_eval_runs (id, workspace_id, channel_id, scope, model, prompt_version, dataset_sha256, counts, run_by_agent_id, run_by_token_id, created_at)
    SELECT md5('eval-beta-1')::uuid, workspace_id, channel_id, 'group', 'claude-haiku-5-5', 'intake-2026-10-26.2',
        '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
        '{"create": {"labels": 40, "correct": 37, "severe": 0, "light": 3}, "attach": {"labels": 25, "correct": 24, "severe": 1, "light": 0}, "ignore": {"labels": 18, "correct": 18, "severe": 0, "light": 0}}',
        md5('agent-beta-admin')::uuid, md5('token-beta-1')::uuid, '2026-10-26 09:00:00+00'
    FROM intake_gatherings WHERE id = md5('gathering-beta-1')::uuid;
