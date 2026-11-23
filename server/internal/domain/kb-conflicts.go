package domain

import "time"

// The bounds of an article's conflicts as the team reads them: how many are listed and how much of
// each reply.
const (
	MaxKBArticleConflictsListed = 100
	KBConflictExcerptRunes      = 300
)

// KBArticleConflicts are the sent replies the workspace's small model found to contradict one
// article. Open counts the ones no editor cleared; the article may be out of date while it is above 0.
type KBArticleConflicts struct {
	ArticleID string              `json:"article_id"`
	Open      int                 `json:"open"`
	Conflicts []KBArticleConflict `json:"conflicts"`
}

// KBArticleConflict is one reply that contradicts the article. Reason is the model's masked sentence
// quoting both sides; ClearedAt, ClearedBy and ClearedWithToken say who cleared it and when.
type KBArticleConflict struct {
	ID               string            `json:"id"`
	Reason           string            `json:"reason"`
	CreatedAt        time.Time         `json:"created_at"`
	Reply            KBConflictReply   `json:"reply"`
	ClearedAt        *time.Time        `json:"cleared_at"`
	ClearedBy        *SavedReplyPerson `json:"cleared_by"`
	ClearedWithToken *SavedReplyToken  `json:"cleared_with_token"`
}

// KBConflictReply is the reply a conflict names: its masked start on one line, who sent it and where.
// Conversation is nil for a reader who may not read conversations.
type KBConflictReply struct {
	MessageID    string                  `json:"message_id"`
	Excerpt      string                  `json:"excerpt"`
	Author       *SavedReplyPerson       `json:"author"`
	SentAt       time.Time               `json:"sent_at"`
	Conversation *KBConflictConversation `json:"conversation"`
}

// KBConflictConversation names the conversation a conflicting reply was sent on.
type KBConflictConversation struct {
	ID         string `json:"id"`
	DisplayRef string `json:"display_ref"`
	Subject    string `json:"subject"`
}
