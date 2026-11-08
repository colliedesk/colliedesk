-- The operator console asks, on each status, whether the server's AI provider is out of credit: its calls
-- since the last one that answered. This keeps that a short range scan of the server's own calls.
CREATE INDEX ai_usage_server_finished ON ai_usage(finished_at) WHERE billed_to = 'server' AND status IN ('ok', 'error');
