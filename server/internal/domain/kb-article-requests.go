package domain

import "errors"

// MaxKBArticleRequestText bounds a request's title and the one line a team member writes with it.
const MaxKBArticleRequestText = 300

var (
	// ErrKBArticleRequestNotFound means the conversation has asked for no article.
	ErrKBArticleRequestNotFound = errors.New("this conversation has not asked for an article")
	// ErrKBSourceConversationNotFound means the conversation an article is asked or made from is not
	// one of the workspace's.
	ErrKBSourceConversationNotFound = errors.New("the conversation was not found")
	// ErrKBSourceMessageNotFound means the reply an article is made from is not a message of the
	// conversation.
	ErrKBSourceMessageNotFound = errors.New("the message was not found in this conversation")
)

// KBArticleRequestInput is a conversation's request for an article. Title defaults to the
// conversation subject; GapID names an open gap to join instead of matching by words.
type KBArticleRequestInput struct {
	Title *string `json:"title"`
	Note  *string `json:"note"`
	GapID *string `json:"gap_id"`
}

// KBArticleRequest is a filed request and the gap it is in.
type KBArticleRequest struct {
	Gap             KBGap `json:"gap"`
	Created         bool  `json:"created"`
	AddedToExisting bool  `json:"added_to_existing"`
}

// KBArticleReplySource is the sent reply an article was made from: the conversation and the
// customer question just before the reply, masked.
type KBArticleReplySource struct {
	ConversationID string `json:"conversation_id"`
	DisplayRef     string `json:"display_ref"`
	Subject        string `json:"subject"`
	MessageID      string `json:"message_id"`
	Question       string `json:"question"`
}
