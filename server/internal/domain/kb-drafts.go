package domain

import (
	"errors"
	"time"
)

// KBArticleDrafted is the version kind of a saved draft: unpublished changes to a published article or
// translation. The help center keeps showing the published text.
const KBArticleDrafted = "draft"

// ErrKBDraftNotFound: the article (or translation) has no unpublished changes to publish or discard.
var ErrKBDraftNotFound = errors.New("knowledge base draft not found")

// KBArticleDraft is the unpublished changes of a published article: every field as it will be once
// published. UpdatedAt is when the draft was saved last; the article's UpdatedAt is the token to send back.
type KBArticleDraft struct {
	Title          string            `json:"title"`
	Slug           string            `json:"slug"`
	BodyMD         string            `json:"body_md"`
	SEOTitle       string            `json:"seo_title"`
	SEODescription string            `json:"seo_description"`
	CollectionID   string            `json:"collection_id"`
	CollectionIDs  []string          `json:"collection_ids"`
	ShowContents   bool              `json:"show_contents"`
	Attachments    KBAttachments     `json:"attachments"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	UpdatedBy      *SavedReplyPerson `json:"updated_by"`
}

// AsDrafted is the article as it will be once its draft is published; without a draft it is the article.
// UpdatedAt stays the article's: the token every write checks.
func (a KBArticle) AsDrafted() KBArticle {
	if a.Draft == nil {
		return a
	}
	draft := *a.Draft
	a.Title, a.Slug, a.BodyMD, a.SEOTitle, a.SEODescription = draft.Title, draft.Slug, draft.BodyMD, draft.SEOTitle, draft.SEODescription
	a.CollectionID, a.CollectionIDs, a.ShowContents = draft.CollectionID, draft.CollectionIDs, draft.ShowContents
	a.Attachments = draft.Attachments
	return a
}
