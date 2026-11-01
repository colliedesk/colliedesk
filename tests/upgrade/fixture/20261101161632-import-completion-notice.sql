UPDATE import_jobs SET notice_state='sent',notice_next_attempt_at='2026-10-01T12:00:00Z' WHERE kind='import' AND state='finished';
