-- Onboarding answers of cloud sign-ups: one workspace answered all three, one answered none.
INSERT INTO workspace_onboarding (workspace_id, goal, role, source, created_at) VALUES
    (md5('ws-acme')::uuid, 'leave_helpdesk', 'support_lead', 'reddit', '2026-10-21 11:30:00+00'),
    (md5('ws-beta')::uuid, NULL, NULL, NULL, '2026-10-21 11:35:00+00');
