package mcpserver

import (
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

type kbReviewText struct {
	ArticleTitle string            `json:"article_title"`
	Note         string            `json:"note"`
	DecisionNote string            `json:"decision_note"`
	Diff         []domain.DiffLine `json:"diff,omitempty"`
}

type kbReviewOut struct {
	ID                  string                   `json:"id"`
	ArticleID           string                   `json:"article_id"`
	Locale              *string                  `json:"locale"`
	State               string                   `json:"state"`
	Current             bool                     `json:"current"`
	Reviewers           []domain.KBReviewer      `json:"reviewers"`
	RequestedBy         *domain.SavedReplyPerson `json:"requested_by"`
	WrittenByAgentToken *tokenOut                `json:"written_by_agent_token"`
	DecidedBy           *domain.SavedReplyPerson `json:"decided_by"`
	DecidedByAgentToken *tokenOut                `json:"decided_by_agent_token"`
	DecidedAt           *time.Time               `json:"decided_at"`
	SupersedesReviewID  *string                  `json:"supersedes_review_id"`
	CreatedAt           time.Time                `json:"created_at"`
	TeamText            kbReviewText             `json:"team_text"`
}

type kbReviewContentOut struct {
	Slug          string     `json:"slug"`
	CollectionIDs []string   `json:"collection_ids"`
	TeamText      kbTeamText `json:"team_text"`
}

type kbReviewDetailOut struct {
	kbReviewOut
	Live     *kbReviewContentOut `json:"live"`
	Proposed kbReviewContentOut  `json:"proposed"`
}

// reviewToken names the token someone acted through, and the person it acts as.
func reviewToken(token *domain.SavedReplyToken, person *domain.SavedReplyPerson) *tokenOut {
	if token == nil {
		return nil
	}
	out := &tokenOut{Label: token.Label}
	if person != nil {
		out.ActsAs = person.Name
	}
	return out
}

func kbReviewRow(review domain.KBReview) kbReviewOut {
	reviewers := review.Reviewers
	if reviewers == nil {
		reviewers = []domain.KBReviewer{}
	}
	return kbReviewOut{ID: review.ID, ArticleID: review.ArticleID, Locale: review.Locale, State: review.State, Current: review.Current, Reviewers: reviewers,
		RequestedBy: review.RequestedBy, WrittenByAgentToken: reviewToken(review.RequestedByToken, review.RequestedBy),
		DecidedBy: review.DecidedBy, DecidedByAgentToken: reviewToken(review.DecidedByToken, review.DecidedBy), DecidedAt: review.DecidedAt,
		SupersedesReviewID: review.SupersedesReviewID, CreatedAt: review.CreatedAt,
		TeamText: kbReviewText{ArticleTitle: review.ArticleTitle, Note: review.Note, DecisionNote: review.DecisionNote}}
}

func kbReviewContent(content domain.KBContent) kbReviewContentOut {
	ids := content.CollectionIDs
	if ids == nil {
		ids = []string{}
	}
	return kbReviewContentOut{Slug: content.Slug, CollectionIDs: ids, TeamText: kbTeamText{content.Title, content.BodyMD, content.SEOTitle, content.SEODescription}}
}
