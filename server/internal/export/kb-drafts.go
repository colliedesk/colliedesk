package export

import "time"

// KBArticleDraft is the unpublished changes of a published article (Locale nil) or of a published
// translation. CollectionID and ExtraCollectionIDs nil mean the draft keeps the article where it is
// listed now.
type KBArticleDraft struct {
	ArticleID          string    `json:"article_id"`
	Locale             *string   `json:"locale"`
	Title              string    `json:"title"`
	Slug               string    `json:"slug"`
	BodyMD             string    `json:"body_md"`
	SEOTitle           string    `json:"seo_title"`
	SEODescription     string    `json:"seo_description"`
	CollectionID       *string   `json:"collection_id"`
	ExtraCollectionIDs []string  `json:"extra_collection_ids"`
	UpdatedByAgentID   *string   `json:"updated_by_agent_id"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (w *Writer) KBArticleDraft(record KBArticleDraft) error {
	return w.record("kb-article-drafts.ndjson", record)
}
