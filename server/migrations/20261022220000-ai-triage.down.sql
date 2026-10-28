-- Suggestions are only suggestions; what a person accepted stays on the conversations.
DROP TRIGGER ai_triage_on_first_message ON messages;
DROP FUNCTION ai_triage_on_first_message();
DROP TABLE ai_triage_suggestions;
