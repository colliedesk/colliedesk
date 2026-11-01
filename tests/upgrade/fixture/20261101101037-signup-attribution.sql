-- acme signed up from a Meta ad for a TikTok cross-post: every utm key and both click IDs, with the time the
-- browser first saw them. beta came from no link and keeps all of them empty.
UPDATE workspace_onboarding SET utm_source = 'meta', utm_medium = 'paid_social', utm_campaign = 'launch.2026',
    utm_content = 'post-07', utm_term = 'freshdesk-alternative', ttclid = 'E.C.P.acme', fbclid = 'IwAR_acme-1',
    click_id_seen_at = '2026-10-21 11:20:00+00'
WHERE workspace_id = md5('ws-acme')::uuid;
