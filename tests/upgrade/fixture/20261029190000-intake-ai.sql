-- beta: Bea's ignore of Minh's fourth batch followed what AI suggested. Values are fixture data.
INSERT INTO intake_decisions (id, workspace_id, gathering_id, decider, basis, action, reply_kind, confidence, reason, applied, not_applied_reason, ignore_reason,
        items_seen, manifest, model, prompt_version, created_at)
    SELECT md5('decision-beta-4')::uuid, workspace_id, id, 'ai', 'model', 'ignore', 'acknowledgement', 0.930, 'Says the team already answered it.', false,
        'gate_closed', 'handled_in_chat', '{}', '{"sources": [{"source": "incoming", "runes": 40, "items": 1}]}', 'claude-haiku-5-5', 'intake-2026-10-26.1',
        '2026-10-25 12:30:30+00'
    FROM intake_gatherings WHERE id = md5('gathering-beta-4')::uuid;
UPDATE intake_decisions SET follows_decision_id = md5('decision-beta-4')::uuid WHERE id = md5('decision-beta-3')::uuid;
