-- How a collection lists its articles: by the position set by hand (manual, what every
-- collection did until now), by title, newest first or most recently updated first. The
-- positions stay stored under every order, so switching back to manual brings the hand
-- order back.
ALTER TABLE kb_collections ADD COLUMN article_order text NOT NULL DEFAULT 'manual'
	CHECK (article_order IN ('manual','title','newest','updated'));
