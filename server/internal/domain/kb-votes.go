package domain

import "time"

// MaxKBVoteMissingChars is the longest "What was missing?" line a reader may leave with a "No".
const MaxKBVoteMissingChars = 500

// KBVoteCount is what an article's public page says: Helpful of Total readers found it helpful.
// Votes an import brought in count until the team first resets the article's votes.
type KBVoteCount struct {
	Helpful int
	Total   int
}

// KBArticleVotes are the votes of one article as the team reads them. Helpful and NotHelpful are the
// counts the public page shows; Imported is the part of them an import brought in (zero once reset).
type KBArticleVotes struct {
	ArticleID  string               `json:"article_id"`
	Helpful    int                  `json:"helpful"`
	NotHelpful int                  `json:"not_helpful"`
	Imported   KBVoteTally          `json:"imported"`
	Votes      []KBArticleVote      `json:"votes"`
	Resets     []KBArticleVoteReset `json:"resets"`
}

// KBVoteTally is a pair of counts.
type KBVoteTally struct {
	Helpful    int `json:"helpful"`
	NotHelpful int `json:"not_helpful"`
}

// KBArticleVote is one reader's current vote. Contact names a signed-in reader; nil is a reader the
// help center does not know. Missing is the masked "What was missing?" line of a "No", or nil.
type KBArticleVote struct {
	Helpful   bool           `json:"helpful"`
	Missing   *string        `json:"missing"`
	Contact   *KBVoteContact `json:"contact"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// KBVoteContact is the signed-in reader who voted.
type KBVoteContact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// KBArticleVoteReset is one reset of an article's votes: the counts the page showed just before it,
// and who reset them.
type KBArticleVoteReset struct {
	ID         string            `json:"id"`
	Helpful    int               `json:"helpful"`
	NotHelpful int               `json:"not_helpful"`
	ResetBy    *SavedReplyPerson `json:"reset_by"`
	Token      *SavedReplyToken  `json:"token"`
	CreatedAt  time.Time         `json:"created_at"`
}
