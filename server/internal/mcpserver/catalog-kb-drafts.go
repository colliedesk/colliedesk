package mcpserver

// A published article keeps its changes in one draft: colliedesk_kb_article_update writes it while the
// help center shows the live text, and these tools publish or discard it.

const (
	// kbArticleDraftSchema is the draft on an article's answer, null without one.
	kbArticleDraftSchema = `{"type":["object","null"],"description":"Unpublished changes: what colliedesk_kb_article_draft_publish puts live; team_text stays the live text.",` +
		`"properties":{"team_text":` + kbTeamTextSchema + `,"slug":{"type":"string"},"collection_ids":{"type":"array","items":{"type":"string"}},"updated_by":{"type":["string","null"]}}}`
	// kbArticleDraftResultOutput is the article after a publish or a discard, without its text: read it
	// with colliedesk_kb_article_get.
	kbArticleDraftResultOutput = `{"type":"object","required":["article"],"properties":{"article":` + kbArticleRowSchema + `}}`
	kbDraftUpdatedAtProperty   = `"updated_at":{"type":"string","format":"date-time","maxLength":40,"description":"The article's updated_at as you last read it: the draft's. If someone saved the draft since, nothing changes and you are told to read it again."}`
)

func kbArticleDraftPublishSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_draft_publish",
		Title: "Publish the unpublished changes of a knowledge base article",
		Description: `Put the draft of a published article (its draft, from colliedesk_kb_article_get) on the help center: its title, text, slug, collections and search fields become the live ones, as a new version. ` +
			`Undo with colliedesk_kb_article_version_restore and the version before. Refused when there is no draft.` + "\n\n" + kbTextNotice,
		REST:   []string{"POST /api/v1/kb/articles/{id}/draft/publish"},
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id","updated_at"],"properties":{` + kbArticleIDProperty + `,` + kbDraftUpdatedAtProperty + `}}`,
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
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id","updated_at"],"properties":{` + kbArticleIDProperty + `,` + kbDraftUpdatedAtProperty + `}}`,
		Output: kbArticleDraftResultOutput,
		run:    kbArticleDraftDiscard,
	}
}
