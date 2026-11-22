package domain

import (
	"errors"
	"slices"
	"time"
)

const (
	// KBGapRankDays is the window of the rank: distinct askers in the last 30 days.
	KBGapRankDays = 30
	// MaxKBGapSignalsShown bounds the signals one gap's detail lists, newest first.
	MaxKBGapSignalsShown = 100
	// DefaultKBGapsListed and MaxKBGapsListed bound one page of the gaps list.
	DefaultKBGapsListed = 50
	MaxKBGapsListed     = 200
)

// KBGapStatuses are the states of a gap; KBGapKinds the kinds of its signals.
var (
	KBGapStatuses = []string{"open", "fixed", "dismissed"}
	KBGapKinds    = []string{"search_no_result", "vote_no", "chat_handoff", "chat_unanswered", "recurring_topic"}
)

// ErrKBGapNotFound means the gap is not one of the workspace's.
var ErrKBGapNotFound = errors.New("knowledge gap not found")

// KBGapArticle names the article that answers a gap.
type KBGapArticle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// KBGap is what readers asked for and did not find, grouped by their words.
type KBGap struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Status             string            `json:"status"`
	Kinds              []string          `json:"kinds"`
	Askers             int               `json:"askers"`
	RecentAskers       int               `json:"recent_askers"`
	FirstSeen          time.Time         `json:"first_seen"`
	LastSeen           time.Time         `json:"last_seen"`
	FixedByArticle     *KBGapArticle     `json:"fixed_by_article"`
	FixedAutomatically bool              `json:"fixed_automatically"`
	StatusChangedBy    *SavedReplyPerson `json:"status_changed_by"`
	StatusChangedAt    *time.Time        `json:"status_changed_at"`
}

// KBGapSignal is one thing a reader could not find; which reader is never part of it.
type KBGapSignal struct {
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// KBGapDetail is a gap with its newest signals.
type KBGapDetail struct {
	KBGap
	SignalCount int           `json:"signal_count"`
	Signals     []KBGapSignal `json:"signals"`
}

// KBGapFilter picks the gaps listed: Status (default open), Kind (any when empty) and Limit.
type KBGapFilter struct {
	Status, Kind string
	Limit        int
}

// Clean fills the defaults and refuses an unknown status, kind or limit.
func (f KBGapFilter) Clean() (KBGapFilter, error) {
	if f.Status == "" {
		f.Status = "open"
	}
	if !slices.Contains(KBGapStatuses, f.Status) {
		return f, invalidKB("status must be open, fixed or dismissed")
	}
	if f.Kind != "" && !slices.Contains(KBGapKinds, f.Kind) {
		return f, invalidKB("kind must be search_no_result, vote_no, chat_handoff, chat_unanswered or recurring_topic")
	}
	if f.Limit == 0 {
		f.Limit = DefaultKBGapsListed
	}
	if f.Limit < 1 || f.Limit > MaxKBGapsListed {
		return f, invalidKB("limit must be from 1 to %d", MaxKBGapsListed)
	}
	return f, nil
}

// KBGapChange is a PATCH of a gap: a status, an article that fixes it, or both.
type KBGapChange struct {
	Status           *string `json:"status"`
	FixedByArticleID *string `json:"fixed_by_article_id"`
}

// Clean resolves the status the change sets: an article alone means fixed; open and dismissed take no article.
func (c KBGapChange) Clean() (status, articleID string, err error) {
	if c.FixedByArticleID != nil {
		articleID = *c.FixedByArticleID
		status = "fixed"
	}
	if c.Status != nil {
		status = *c.Status
	}
	switch {
	case status == "":
		return "", "", invalidKB("send status, fixed_by_article_id or both")
	case !slices.Contains(KBGapStatuses, status):
		return "", "", invalidKB("status must be open, fixed or dismissed")
	case status != "fixed" && articleID != "":
		return "", "", invalidKB("only a fixed gap has an article: leave out fixed_by_article_id, or send status fixed")
	}
	return status, articleID, nil
}
