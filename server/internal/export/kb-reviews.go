package export

import "time"

// KBReview is one review of an article's text (Locale nil) or of one translation's. ContentHash is the
// SHA-256 of the exact text the reviewers were given; an approval holds only while the text still has
// that hash. A request or a decision made through an AI agent's token names that token.
type KBReview struct {
	ID                    string     `json:"id"`
	ArticleID             string     `json:"article_id"`
	Locale                *string    `json:"locale"`
	ContentHash           string     `json:"content_hash"`
	State                 string     `json:"state"`
	Note                  string     `json:"note"`
	RequestedByAgentID    *string    `json:"requested_by_agent_id"`
	RequestedByTokenID    *string    `json:"requested_by_token_id"`
	RequestedByTokenLabel *string    `json:"requested_by_token_label"`
	DecidedByAgentID      *string    `json:"decided_by_agent_id"`
	DecidedByTokenID      *string    `json:"decided_by_token_id"`
	DecidedByTokenLabel   *string    `json:"decided_by_token_label"`
	DecidedAt             *time.Time `json:"decided_at"`
	DecisionNote          string     `json:"decision_note"`
	SupersedesReviewID    *string    `json:"supersedes_review_id"`
	// ApprovesPending: the review is approved and its text is still the one waiting to be published.
	ApprovesPending bool `json:"approves_pending"`
	// AuthorEmail is the email that tells the author the review was decided: none, queued, sending,
	// sent, rejected or off.
	AuthorEmail string    `json:"author_email"`
	CreatedAt   time.Time `json:"created_at"`
}

// KBReviewReviewer is one person asked to review: when the email reached them and when they opened it.
type KBReviewReviewer struct {
	ReviewID   string     `json:"review_id"`
	AgentID    string     `json:"agent_id"`
	Email      string     `json:"email"`
	NotifiedAt *time.Time `json:"notified_at"`
	SeenAt     *time.Time `json:"seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (w *Writer) KBReview(record KBReview) error {
	return w.record("kb-reviews.ndjson", record)
}

func (w *Writer) KBReviewReviewer(record KBReviewReviewer) error {
	return w.record("kb-review-reviewers.ndjson", record)
}
