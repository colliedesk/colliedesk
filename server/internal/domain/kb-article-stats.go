package domain

import "time"

// KBArticleStats is how much one article was read on the help center and used in sent replies, per
// calendar day in the workspace's timezone, over the days [From, To).
type KBArticleStats struct {
	ArticleID string    `json:"article_id"`
	Timezone  string    `json:"timezone"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	FirstDay  string    `json:"first_day"`
	LastDay   string    `json:"last_day"`
	Views     int64     `json:"views"`
	// Uses are the sent replies that linked the article or were an AI draft quoting it, each reply once.
	Uses int64          `json:"uses"`
	Days []KBArticleDay `json:"days"`
	// Replies are the newest MaxKBArticleStatsReplies of those replies; RepliesTotal counts them all.
	Replies      []KBArticleUse `json:"replies"`
	RepliesTotal int64          `json:"replies_total"`
}

// KBArticleDay is one calendar day of an article's stats.
type KBArticleDay struct {
	Day   string `json:"day"`
	Views int64  `json:"views"`
	Uses  int64  `json:"uses"`
}

// KBArticleUse is one sent reply that used an article: Kinds says how (link, ai_source).
type KBArticleUse struct {
	ConversationID string    `json:"conversation_id"`
	DisplayRef     string    `json:"display_ref"`
	Subject        string    `json:"subject"`
	MessageID      string    `json:"message_id"`
	Kinds          []string  `json:"kinds"`
	At             time.Time `json:"at"`
}

// The bounds of an article's stats: the days read without a range, the longest range and the replies listed.
const (
	KBArticleStatsDefaultDays = 30
	MaxKBArticleStatsDays     = 366
	MaxKBArticleStatsReplies  = 50
)
