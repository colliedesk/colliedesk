-- Journal rows written before the email envelope said "direct": mark the mail that never passed the
-- support mailbox, so it does not count as forwarded mail. A test message is the product's own message:
-- its fixed subject and opening line, from an agent of the workspace. A Gmail forwarding confirmation comes
-- from forwarding-noreply@google.com with "Forwarding Confirmation" in its subject. An answer to a platform
-- reply address left no mark in the journal and cannot be told apart.
UPDATE channel_inbound_events e SET raw = COALESCE(e.raw, '{}'::jsonb) || '{"direct": true}'::jsonb
FROM messages m
JOIN conversations c ON c.workspace_id = m.workspace_id AND c.id = m.conversation_id
JOIN contacts k ON k.workspace_id = c.workspace_id AND k.id = c.contact_id
WHERE m.workspace_id = e.workspace_id AND m.id = e.message_id AND m.kind = 'incoming'
    AND e.raw->>'direct' IS NULL
    AND (
        (c.subject = 'Test message from Colliedesk'
            AND starts_with(m.body, 'This is a test message you sent yourself from Colliedesk.')
            AND EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id = e.workspace_id AND lower(a.email) = lower(k.email)))
        OR (lower(k.email) = 'forwarding-noreply@google.com' AND c.subject ILIKE '%forwarding confirmation%')
    );
