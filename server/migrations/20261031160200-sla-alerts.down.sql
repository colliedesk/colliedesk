CREATE OR REPLACE FUNCTION conversations_sla_open() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.sla_applies IS NULL THEN
        SELECT COALESCE(bool_and(t.enabled AND NEW.channel NOT IN ('imported', 'problem_report')
                AND (t.channel_ids IS NULL OR NEW.channel_id = ANY(t.channel_ids))
                AND COALESCE(NEW.created_at, now()) >= t.enabled_at), false)
            INTO NEW.sla_applies FROM response_targets t WHERE t.workspace_id = NEW.workspace_id;
    END IF;
    NEW.sla_stale := NEW.sla_applies;
    RETURN NEW;
END
$$;
DROP INDEX conversations_sla_warn;
ALTER TABLE conversations DROP COLUMN sla_alert_since;
ALTER TABLE agents DROP COLUMN sla_alert_email;
DROP TABLE conversation_sla_alerts;
