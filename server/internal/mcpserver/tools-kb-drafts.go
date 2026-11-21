package mcpserver

import (
	"net/http"
	"net/url"
)

// restKBDraft is the unpublished changes of a published article, as the API answers them.
type restKBDraft struct {
	Title          string             `json:"title"`
	Slug           string             `json:"slug"`
	BodyMD         string             `json:"body_md"`
	SEOTitle       string             `json:"seo_title"`
	SEODescription string             `json:"seo_description"`
	CollectionID   string             `json:"collection_id"`
	CollectionIDs  []string           `json:"collection_ids"`
	ShowContents   bool               `json:"show_contents"`
	UpdatedAt      string             `json:"updated_at"`
	UpdatedBy      *restPerson        `json:"updated_by"`
	Attachments    []restKBAttachment `json:"attachments"`
	Tags           []string           `json:"tags"`
}

// kbArticleDraftOut is what publishing the draft would put live; the live text stays in team_text.
type kbArticleDraftOut struct {
	TeamText      kbTeamText        `json:"team_text"`
	Slug          string            `json:"slug"`
	CollectionIDs []string          `json:"collection_ids"`
	ShowContents  bool              `json:"show_contents"`
	UpdatedBy     *string           `json:"updated_by"`
	Attachments   []kbAttachmentOut `json:"attachments"`
	Tags          []string          `json:"tags"`
}

// withDraft adds an article's draft to its view. updated_at stays the article's, the one every change sends back.
func (out *kbArticleOut) withDraft(article restKBArticle) {
	out.HasDraft = article.HasDraft
	draft := article.Draft
	if draft == nil {
		return
	}
	ids := draft.CollectionIDs
	if len(ids) == 0 {
		ids = []string{draft.CollectionID}
	}
	out.Draft = &kbArticleDraftOut{TeamText: kbTeamText{draft.Title, draft.BodyMD, draft.SEOTitle, draft.SEODescription},
		Slug: draft.Slug, CollectionIDs: ids, ShowContents: draft.ShowContents, Attachments: kbAttachmentsView(draft.Attachments), Tags: kbTags(draft.Tags)}
	if draft.UpdatedBy != nil {
		out.Draft.UpdatedBy = &draft.UpdatedBy.Name
	}
}

// forArticleDraft words a 404 of a draft call: the article is not in this workspace, or it has no draft.
func (c *call) forArticleDraft(id string) {
	c.notFound = "No knowledge base article " + id + " with a draft in this workspace: either it has no unpublished changes " +
		"(its published text is the current one) or it is not here. Read it with colliedesk_kb_article_get, or find it with colliedesk_kb_articles_search."
}

func kbArticleDraftPublish(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"article_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticleDraft(args.ID)
	var published restKBArticle
	if err := c.send(http.MethodPost, kbArticlePath(args.ID, "/draft/publish"), map[string]any{"updated_at": args.UpdatedAt}, &published); err != nil {
		return nil, err
	}
	return map[string]any{"article": kbArticleRow(published)}, nil
}

func kbArticleDraftDiscard(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"article_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticleDraft(args.ID)
	var kept restKBArticle
	if err := c.send(http.MethodDelete, kbArticlePath(args.ID, "/draft?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, &kept); err != nil {
		return nil, err
	}
	return map[string]any{"article": kbArticleRow(kept)}, nil
}
