package mcpserver

import (
	"net/http"
	"net/url"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func (c *call) forReview(id string) {
	c.notFound = "No knowledge base review " + id + " in this workspace. List them with colliedesk_kb_reviews_list."
}

func kbArticleSendForReview(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ArticleID   string   `json:"article_id"`
		Locale      string   `json:"locale"`
		ReviewerIDs []string `json:"reviewer_ids"`
		Note        string   `json:"note"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	path := kbArticlePath(args.ArticleID)
	c.forArticle(args.ArticleID)
	if args.Locale != "" {
		path = kbTranslationPath(args.ArticleID, args.Locale)
		c.forTranslation(args.ArticleID, args.Locale)
	}
	if change := kbArticleFields(arguments, "article_id", "locale", "reviewer_ids", "note"); len(change) > 1 {
		if _, ok := change["updated_at"]; !ok {
			return nil, &toolError{"Give updated_at (from colliedesk_kb_article_get) with title, body, seo_title or seo_description, so a change someone saved since is not overwritten."}
		}
		if err := c.send(http.MethodPatch, path, change, nil); err != nil {
			return nil, err
		}
	} else if len(change) == 1 {
		return nil, &toolError{"updated_at goes with title, body, seo_title or seo_description; leave it out to send the text as it is."}
	}
	var review domain.KBReview
	if err := c.send(http.MethodPost, path+"/reviews", map[string]any{"reviewer_ids": args.ReviewerIDs, "note": args.Note}, &review); err != nil {
		return nil, err
	}
	return map[string]any{"review": kbReviewRow(review)}, nil
}

func kbReviewsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		State     string `json:"state"`
		Reviewer  string `json:"reviewer"`
		ArticleID string `json:"article_id"`
		Locale    string `json:"locale"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query, path := url.Values{}, "/api/v1/kb/reviews"
	if args.State != "" {
		query.Set("state", args.State)
	}
	switch {
	case args.Locale != "" && args.ArticleID == "":
		return nil, &toolError{"locale goes with article_id: give the article whose translation's reviews you want."}
	case args.ArticleID != "" && args.Reviewer != "":
		return nil, &toolError{"Give reviewer or article_id, not both."}
	case args.Locale != "":
		path = kbTranslationPath(args.ArticleID, args.Locale, "/reviews")
		c.forTranslation(args.ArticleID, args.Locale)
	case args.ArticleID != "":
		path = kbArticlePath(args.ArticleID, "/reviews")
		c.forArticle(args.ArticleID)
	case args.Reviewer != "":
		query.Set("reviewer", args.Reviewer)
	}
	var page struct {
		Reviews []domain.KBReview `json:"reviews"`
	}
	if err := c.get(path, query, &page); err != nil {
		return nil, err
	}
	rows := make([]kbReviewOut, 0, len(page.Reviews))
	for _, review := range page.Reviews {
		rows = append(rows, kbReviewRow(review))
	}
	return map[string]any{"reviews": rows}, nil
}

func kbReviewGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"review_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forReview(args.ID)
	var detail domain.KBReviewDetail
	if err := c.get("/api/v1/kb/reviews/"+url.PathEscape(args.ID), nil, &detail); err != nil {
		return nil, err
	}
	out := kbReviewDetailOut{kbReviewOut: kbReviewRow(detail.KBReview), Proposed: kbReviewContent(detail.Proposed)}
	out.TeamText.Diff = detail.Diff
	if out.TeamText.Diff == nil {
		out.TeamText.Diff = []domain.DiffLine{}
	}
	if detail.Live != nil {
		live := kbReviewContent(*detail.Live)
		out.Live = &live
	}
	return out, nil
}

func kbReviewApprove(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string   `json:"review_id"`
		IDs  []string `json:"review_ids"`
		Note string   `json:"note"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if (args.ID == "") == (len(args.IDs) == 0) {
		return nil, &toolError{"Give review_id, or review_ids to approve several; not both."}
	}
	if args.ID != "" {
		return kbReviewDecide(c, args.ID, "/approve", args.Note)
	}
	return kbReviewsApprove(c, args.IDs, args.Note)
}

func kbReviewRequestChanges(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string `json:"review_id"`
		Note string `json:"note"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return kbReviewDecide(c, args.ID, "/request-changes", args.Note)
}

func kbReviewWithdraw(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"review_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return kbReviewDecide(c, args.ID, "/withdraw", "")
}

func kbReviewDecide(c *call, id, action, note string) (any, error) {
	c.forReview(id)
	var body any
	if note != "" {
		body = map[string]any{"note": note}
	}
	var review domain.KBReview
	if err := c.send(http.MethodPost, "/api/v1/kb/reviews/"+url.PathEscape(id)+action, body, &review); err != nil {
		return nil, err
	}
	return map[string]any{"review": kbReviewRow(review)}, nil
}
