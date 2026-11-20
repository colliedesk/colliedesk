package store

// kbMediaUses are the uses of library items by articles, as rows (workspace_id, article_id, media_id,
// current) a query aliases: the media an article's text names (current while its current text does),
// and the files it lists under its body, in its own list, a translation's or its unpublished draft's,
// which are always current. One article can use an item more than once, so count its articles
// distinct.
const kbMediaUses = `(SELECT workspace_id, article_id, media_id, current FROM kb_article_media
	UNION ALL SELECT workspace_id, article_id, media_id, true FROM kb_article_attachments
	UNION ALL SELECT d.workspace_id, d.article_id, u.media_id, true FROM kb_article_drafts d, unnest(d.attachment_ids) AS u(media_id))`
