package mcpserver

// A published article keeps its changes in one draft: colliedesk_kb_article_update writes it while the
// help center shows the live text, and these tools publish or discard it.

const (
	// kbArticleDraftSchema is the draft on an article's answer, null without one.
	kbArticleDraftSchema = `{"type":["object","null"],"description":"Unpublished changes, shaped like the article; colliedesk_kb_article_draft_publish puts them live.",` +
		`"properties":{"team_text":{"type":"object"},"slug":{"type":"string"},"collection_ids":{"type":"array"},"show_contents":{"type":"boolean"},"updated_by":{"type":["string","null"]},"attachments":{"type":"array"}}}`
	// kbArticleDraftResultOutput is the article after a publish or a discard, without its text: read it
	// with colliedesk_kb_article_get.
	kbArticleDraftResultOutput = `{"type":"object","required":["article"],"properties":{"article":` + kbArticleRowSchema + `}}`
)

func kbArticleDraftPublishSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_draft_publish",
		Title: "Publish the unpublished changes of a knowledge base article",
		Description: `Put the draft of a published article (its draft, from colliedesk_kb_article_get) on the help center: its title, text, slug, collections and search fields become the live ones, as a new version. ` +
			`To undo, restore the version before with colliedesk_kb_article_version_restore (it becomes the draft), then publish it here. Refused when there is no draft.` + "\n\n" + kbTextNotice,
		REST:   []string{"POST /api/v1/kb/articles/{id}/draft/publish"},
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id","updated_at"],"properties":{` + kbArticleIDProperty + `,` + kbUpdatedAtProperty + `}}`,
		Output: kbArticleDraftResultOutput,
		run:    kbArticleDraftPublish,
	}
}

func kbArticleDraftDiscardSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_draft_discard",
		Title: "Discard the unpublished changes of a knowledge base article",
		Description: `Drop the draft of a published article: the live text, its updated_at and its versions stay as they were. ` +
			`Each draft save is a version, so colliedesk_kb_article_version_restore brings the changes back into a draft. Refused when there is no draft.` + "\n\n" + kbTextNotice,
		REST:   []string{"DELETE /api/v1/kb/articles/{id}/draft"},
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id","updated_at"],"properties":{` + kbArticleIDProperty + `,` + kbUpdatedAtProperty + `}}`,
		Output: kbArticleDraftResultOutput,
		run:    kbArticleDraftDiscard,
	}
}
