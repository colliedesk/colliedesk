package mcpserver

// A published article keeps its changes in one draft: colliedesk_kb_article_update writes it while the
// help center shows the live text, and these tools publish or discard it. A published translation
// keeps its own draft the same way (colliedesk_kb_translation_update); the same tools, given its
// locale, publish or discard that one.

const (
	// kbArticleDraftSchema is the draft on an article's answer, null without one.
	kbArticleDraftSchema = `{"type":["object","null"],"description":"Unpublished changes, shaped like the article; colliedesk_kb_article_draft_publish puts them live.",` +
		`"properties":{"team_text":{"type":"object"},"slug":{"type":"string"},"collection_ids":{"type":"array"},"show_contents":{"type":"boolean"},"updated_by":{"type":["string","null"]},"attachments":{"type":"array"}}}`
	// kbArticleDraftResultOutput is the article after a publish or a discard, or the translation when a
	// locale was given, without its text: read it with colliedesk_kb_article_get or colliedesk_kb_translation_get.
	kbArticleDraftResultOutput = `{"type":"object","description":"article, or translation when locale was given.","properties":{"article":` + kbArticleRowSchema +
		`,"translation":` + kbTranslationSummarySchema + `}}`
	// kbDraftLocaleProperty names the translation whose draft a call publishes or discards.
	kbDraftLocaleProperty = `"locale":{"type":"string","minLength":2,"maxLength":35,"pattern":"^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$",` +
		`"description":"The language of a published translation, such as fr, to act on its draft instead of the article's; its updated_at then comes from colliedesk_kb_translation_get."}`
	kbArticleDraftInput = `{"type":"object","additionalProperties":false,"required":["article_id","updated_at"],"properties":{` + kbArticleIDProperty + `,` + kbUpdatedAtProperty + `,` +
		kbDraftLocaleProperty + `}}`
)

func kbArticleDraftPublishSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_draft_publish",
		Title: "Publish the unpublished changes of a knowledge base article",
		Description: `Put the draft of a published article (its draft, from colliedesk_kb_article_get) on the help center: its title, text, slug, collections and search fields become the live ones, as a new version. ` +
			`With locale, publish the draft of that published translation instead (from colliedesk_kb_translation_get). ` +
			`To undo, restore the version before with colliedesk_kb_article_version_restore (it becomes the draft), then publish it here. Refused when there is no draft.` + "\n\n" + kbTextNotice,
		REST:   []string{"POST /api/v1/kb/articles/{id}/draft/publish", "POST /api/v1/kb/articles/{id}/translations/{locale}/draft/publish"},
		Input:  kbArticleDraftInput,
		Output: kbArticleDraftResultOutput,
		run:    kbArticleDraftPublish,
	}
}

func kbArticleDraftDiscardSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_draft_discard",
		Title: "Discard the unpublished changes of a knowledge base article",
		Description: `Drop the draft of a published article, or with locale of that published translation: the live text, its updated_at and its versions stay as they were. ` +
			`Each draft save is a version, so colliedesk_kb_article_version_restore brings the changes back into a draft. Refused when there is no draft.` + "\n\n" + kbTextNotice,
		REST:   []string{"DELETE /api/v1/kb/articles/{id}/draft", "DELETE /api/v1/kb/articles/{id}/translations/{locale}/draft"},
		Input:  kbArticleDraftInput,
		Output: kbArticleDraftResultOutput,
		run:    kbArticleDraftDiscard,
	}
}
