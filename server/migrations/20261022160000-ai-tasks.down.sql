-- The queue only holds work in progress; the usage rows keep their counts without a task.
DELETE FROM ai_settings_events WHERE kind = 'automatic_limits_changed';
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_kind_check;
ALTER TABLE ai_settings_events ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed',
    'provider_key_changed', 'provider_removed', 'slot_changed'));
DROP TABLE ai_automatic_limits;
DROP INDEX ai_usage_started;
DROP INDEX ai_usage_task;
ALTER TABLE ai_usage DROP COLUMN cost_micro_usd;
ALTER TABLE ai_usage DROP COLUMN automatic;
ALTER TABLE ai_usage DROP COLUMN task_id;
DROP TABLE ai_tasks;
