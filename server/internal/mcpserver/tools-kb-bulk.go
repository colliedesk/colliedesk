package mcpserver

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbBulkError is why one id of a bulk call was refused; the others still run.
type kbBulkError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func kbReviewsApprove(c *call, ids []string, note string) (any, error) {
	var answer struct {
		Results []struct {
			ID     string           `json:"id"`
			Review *domain.KBReview `json:"review"`
			Error  *kbBulkError     `json:"error"`
		} `json:"results"`
	}
	if err := c.send(http.MethodPost, "/api/v1/kb/reviews/approve", map[string]any{"review_ids": ids, "note": note}, &answer); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(answer.Results))
	for _, result := range answer.Results {
		row := map[string]any{"id": result.ID}
		if result.Review != nil {
			row["review"] = kbReviewRow(*result.Review)
		}
		if result.Error != nil {
			row["error"] = result.Error
		}
		results = append(results, row)
	}
	return map[string]any{"results": results}, nil
}

func kbArticlesPublish(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		IDs []string `json:"article_ids"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			ID      string         `json:"id"`
			Article *restKBArticle `json:"article"`
			Error   *kbBulkError   `json:"error"`
		} `json:"results"`
	}
	if err := c.send(http.MethodPost, "/api/v1/kb/articles/publish", map[string]any{"article_ids": args.IDs}, &answer); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(answer.Results))
	for _, result := range answer.Results {
		row := map[string]any{"id": result.ID}
		if result.Article != nil {
			row["article"] = kbArticleRow(*result.Article)
		}
		if result.Error != nil {
			row["error"] = result.Error
		}
		results = append(results, row)
	}
	return map[string]any{"results": results}, nil
}
