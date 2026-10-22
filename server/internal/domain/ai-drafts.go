package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

// AI drafts (docs/internals/ai-drafts.md).

// AIDraft statuses. Stored and returned, so values never change.
const (
	AIDraftQueued     = "queued"     // waiting for the worker
	AIDraftReady      = "ready"      // written; a person may use, edit or discard it
	AIDraftFailed     = "failed"     // the provider failed for good; error_code says why
	AIDraftSuperseded = "superseded" // the customer wrote again, or a person asked for a new one
	AIDraftUsed       = "used"       // a person sent a reply from it
	AIDraftDiscarded  = "discarded"  // a person discarded it
	AIDraftScheduled  = "scheduled"  // auto-answer sends it at auto_send_at unless someone stops it
	AIDraftAutoSent   = "auto_sent"  // auto-answer sent it
	AIDraftCancelled  = "cancelled"  // a scheduled answer was stopped
)

// Outcomes of a reviewed draft.
const (
	AIOutcomeSentUnchanged = "sent_unchanged"
	AIOutcomeSentEdited    = "sent_edited"
	AIOutcomeDiscarded     = "discarded"
	AIOutcomeAutoSent      = "auto_sent"
	AIOutcomeAutoCancelled = "auto_cancelled"
)

// AIAcceptedSimilarity is how close a sent reply must be to its draft for the draft to count as
// right in its topic's record: wording changes pass, a changed fact or a rewritten reply does not.
const AIAcceptedSimilarity = 0.85

// AISettings are a workspace's AI switches. Available is whether this server has an AI provider at all.
type AISettings struct {
	Available              bool       `json:"available"`
	DraftsEnabled          bool       `json:"drafts_enabled"`
	AutoAnswerEnabled      bool       `json:"auto_answer_enabled"`
	AutoAnswerDelaySeconds int        `json:"auto_answer_delay_seconds"`
	AutoAnswerMinReviewed  int        `json:"auto_answer_min_reviewed"`
	AutoAnswerMinAccuracy  int        `json:"auto_answer_min_accuracy"`
	UpdatedAt              *time.Time `json:"updated_at"`
}

// DefaultAISettings are the settings of a workspace that never changed them.
func DefaultAISettings(available bool) AISettings {
	return AISettings{Available: available, DraftsEnabled: true, AutoAnswerDelaySeconds: 300, AutoAnswerMinReviewed: 10, AutoAnswerMinAccuracy: 90}
}

// AITopicRecord is how drafts resting on one knowledge base article did with people.
type AITopicRecord struct {
	ArticleID     string `json:"article_id"`
	ArticleNumber int64  `json:"article_number"`
	Title         string `json:"title"`
	Reviewed      int    `json:"reviewed"`
	Accepted      int    `json:"accepted"`
	// Eligible says auto-answer may send this topic's high-confidence drafts on its own.
	Eligible bool `json:"eligible"`
}

// AutoAnswerEligible says whether a topic's record earns auto-answer under the settings.
func AutoAnswerEligible(settings AISettings, reviewed, accepted int) bool {
	if !settings.AutoAnswerEnabled || reviewed < settings.AutoAnswerMinReviewed || reviewed == 0 {
		return false
	}
	return accepted*100 >= settings.AutoAnswerMinAccuracy*reviewed
}

// AIDraftSource is a document a draft cites, as stored.
type AIDraftSource struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Number int64  `json:"number,omitempty"`
	Title  string `json:"title"`
	Locale string `json:"locale,omitempty"`
}

// AITriage is a suggested tag set and priority.
type AITriage struct {
	Tags     []string `json:"tags"`
	Priority string   `json:"priority,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// AIDraft is one draft of a reply to a conversation.
type AIDraft struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	MessageID         *string         `json:"message_id"`
	Status            string          `json:"status"`
	Action            *string         `json:"action"`
	Body              *string         `json:"body"`
	Confidence        *string         `json:"confidence"`
	ConfidenceReasons []string        `json:"confidence_reasons"`
	HandoffReason     *string         `json:"handoff_reason"`
	TopicArticleID    *string         `json:"topic_article_id"`
	Sources           []AIDraftSource `json:"sources"`
	Triage            *AITriage       `json:"triage"`
	TriageAppliedAt   *time.Time      `json:"triage_applied_at"`
	Outcome           *string         `json:"outcome"`
	Similarity        *float64        `json:"similarity"`
	AutoSendAt        *time.Time      `json:"auto_send_at"`
	ErrorCode         *string         `json:"error_code"`
	Model             *string         `json:"model"`
	CreatedAt         time.Time       `json:"created_at"`
	ReadyAt           *time.Time      `json:"ready_at"`
}

// AIDraftResult is what the worker writes into a draft.
type AIDraftResult struct {
	Action            string
	Body              string
	Confidence        string
	ConfidenceReasons []string
	HandoffReason     string
	TopicArticleID    string
	Sources           []AIDraftSource
	Triage            *AITriage
	Model             string
}

// SourcesJSON encodes sources for storage; nil is an empty list.
func SourcesJSON(sources []AIDraftSource) json.RawMessage {
	if sources == nil {
		sources = []AIDraftSource{}
	}
	encoded, _ := json.Marshal(sources)
	return encoded
}

var (
	// ErrAIUnavailable means the server has no AI provider configured.
	ErrAIUnavailable = errors.New("this server has no AI provider configured")
	// ErrAIDraftsOff means the workspace turned drafts off.
	ErrAIDraftsOff = errors.New("AI drafts are turned off for this workspace")
	// ErrAIDraftNotFound is a draft that is not on the conversation in the caller's workspace.
	ErrAIDraftNotFound = errors.New("AI draft not found")
	// ErrAIDraftState is an action the draft's status does not allow (discarding a sent draft).
	ErrAIDraftState = errors.New("the AI draft can no longer be changed this way")
)

// ReplySimilarity says how close two replies are, 0 to 1, by the words they share in order (longest
// common subsequence of words over the longer reply). Case, punctuation and spacing do not count.
func ReplySimilarity(a, b string) float64 {
	x, y := replyWords(a), replyWords(b)
	if len(x) == 0 && len(y) == 0 {
		return 1
	}
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	// Two rows of the LCS table are enough.
	previous, current := make([]int, len(y)+1), make([]int, len(y)+1)
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			switch {
			case x[i-1] == y[j-1]:
				current[j] = previous[j-1] + 1
			case previous[j] >= current[j-1]:
				current[j] = previous[j]
			default:
				current[j] = current[j-1]
			}
		}
		previous, current = current, previous
	}
	longer := max(len(x), len(y))
	return float64(previous[len(y)]) / float64(longer)
}

func replyWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}
