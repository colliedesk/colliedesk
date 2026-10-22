package domain

import (
	"errors"
	"time"
)

// Presence says who has a conversation open (docs/internals/presence.md). A browser tab renews its
// row every PresenceHeartbeat while visible; a row not renewed before it expires is gone.
const (
	PresenceHeartbeat = 5 * time.Second
	// PresenceHiddenHeartbeat is asked of a tab in the background that holds a reply not sent yet;
	// browsers run timers of such tabs rarely, so its row lives PresenceWritingTTL.
	PresenceHiddenHeartbeat = 30 * time.Second
	PresenceViewingTTL      = 15 * time.Second
	PresenceWritingTTL      = 150 * time.Second
	// PresenceTokenTTL is how long an agent token stays listed after it announced itself: an AI agent
	// works in steps, not on a timer.
	PresenceTokenTTL = 2 * time.Minute
	// PresenceMaxPerAgent caps the rows of one person (tabs and tokens); the oldest go first.
	PresenceMaxPerAgent = 20
)

// ErrPresenceConversationNotFound is a conversation that is not in the caller's workspace.
var ErrPresenceConversationNotFound = errors.New("conversation not found")

// ConversationViewer is one person, or one agent token acting as a person, with a conversation open.
// WritingSince and EditedAt are set while a reply not sent yet is open in one of their tabs.
type ConversationViewer struct {
	AgentID      string        `json:"agent_id"`
	Name         string        `json:"name"`
	Writing      bool          `json:"writing"`
	WritingSince *time.Time    `json:"writing_since"`
	EditedAt     *time.Time    `json:"edited_at"`
	AgentToken   *MessageToken `json:"agent_token"`
}
