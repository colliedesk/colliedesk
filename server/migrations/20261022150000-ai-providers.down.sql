-- Removing AI settings drops the providers' sealed keys with them; usage history goes too.
DROP TABLE ai_usage;
DROP TABLE ai_settings_events;
DROP TABLE ai_slots;
DROP TABLE ai_providers;
