package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// What the team reads of the help center's AI chat: the month's use against its limits, the chats and
// their transcripts. Transcripts are end users' messages, read like a conversation's; deleting one is
// an admin's. docs/internals/ai-chat.md records the decisions.

// Chat session states and where a chat started.
const (
	ChatOpen      = "open"
	ChatEnded     = "ended"
	ChatHandedOff = "handed_off"

	SurfaceHelpCenter = "help_center"
	SurfaceArticle    = "article"
	SurfaceWidget     = "widget"
)

// ChatTranscriptDays is how long a chat's texts are kept after its last turn; its numbers stay.
const ChatTranscriptDays = 90

// ErrChatNotFound means no chat with that id in this workspace.
var ErrChatNotFound = errors.New("no chat with that id in this workspace")

// ChatSessionSummary is one chat as the list shows it.
type ChatSessionSummary struct {
	ID           string     `json:"id"`
	ChannelID    string     `json:"channel_id"`
	ChannelName  string     `json:"channel_name"`
	Surface      string     `json:"surface"`
	Status       string     `json:"status"`
	Turns        int        `json:"turns"`
	FirstQ       *string    `json:"first_question"`
	NotHelpful   bool       `json:"not_helpful"`
	Conversation *string    `json:"conversation_id"`
	CreatedAt    time.Time  `json:"created_at"`
	LastTurnAt   time.Time  `json:"last_turn_at"`
	EndedAt      *time.Time `json:"ended_at"`
	// KeptUntil is when its texts are removed, unless a turn comes first.
	KeptUntil time.Time `json:"kept_until"`
}

// ChatSessionPage is a page of chats, newest activity first; Next continues it.
type ChatSessionPage struct {
	Items []ChatSessionSummary `json:"items"`
	Next  *string              `json:"next_cursor"`
}

// ChatSessionFilter narrows the list: one channel, one state, only chats rated not helpful.
type ChatSessionFilter struct {
	ChannelID  string
	Status     string
	NotHelpful bool
	// ConversationID: only the chat that handed off to this conversation.
	ConversationID string
	Cursor         string
	Limit          int
}

// ChatSource is an article an answer cited, as it was then.
type ChatSource struct {
	Ordinal   int     `json:"ordinal"`
	ArticleID *string `json:"article_id"`
	Number    int64   `json:"number"`
	Version   int     `json:"version"`
	Title     *string `json:"title"`
	Folder    *string `json:"folder"`
}

// ChatTurn is one question and answer of a transcript.
type ChatTurn struct {
	ID         string       `json:"id"`
	Seq        int          `json:"seq"`
	Question   *string      `json:"question"`
	Answer     *string      `json:"answer"`
	Status     string       `json:"status"`
	ErrorCode  *string      `json:"error_code"`
	Viewing    *string      `json:"viewing_article_id"`
	Sources    []ChatSource `json:"sources"`
	Feedback   *string      `json:"feedback"`
	CreatedAt  time.Time    `json:"created_at"`
	FinishedAt *time.Time   `json:"finished_at"`
}

// ChatSession is a chat with its transcript.
type ChatSession struct {
	ChatSessionSummary
	HistoryCutAt *time.Time `json:"history_cut_at"`
	Transcript   []ChatTurn `json:"transcript"`
}

const chatSummaryColumns = `s.id::text,s.channel_id::text,c.name,s.surface,s.status,
	(SELECT count(*) FROM ai_chat_turns t WHERE t.workspace_id=s.workspace_id AND t.session_id=s.id),
	(SELECT t.question FROM ai_chat_turns t WHERE t.workspace_id=s.workspace_id AND t.session_id=s.id ORDER BY t.seq LIMIT 1),
	EXISTS (SELECT 1 FROM ai_chat_feedback f JOIN ai_chat_turns t ON t.workspace_id=f.workspace_id AND t.id=f.turn_id
		WHERE f.workspace_id=s.workspace_id AND t.session_id=s.id AND f.rating='not_helpful'),
	s.conversation_id::text,s.created_at,s.last_turn_at,s.ended_at`

func scanChatSummary(row pgx.Row) (ChatSessionSummary, error) {
	var c ChatSessionSummary
	err := row.Scan(&c.ID, &c.ChannelID, &c.ChannelName, &c.Surface, &c.Status, &c.Turns, &c.FirstQ, &c.NotHelpful, &c.Conversation, &c.CreatedAt, &c.LastTurnAt, &c.EndedAt)
	c.CreatedAt, c.LastTurnAt, c.EndedAt = c.CreatedAt.UTC(), c.LastTurnAt.UTC(), utcPointer(c.EndedAt)
	c.KeptUntil = c.LastTurnAt.AddDate(0, 0, ChatTranscriptDays)
	return c, err
}

// ChatSessions lists the workspace's chats, newest activity first.
func (s *Service) ChatSessions(ctx context.Context, workspaceID string, filter ChatSessionFilter) (ChatSessionPage, error) {
	page := ChatSessionPage{Items: []ChatSessionSummary{}}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if (filter.ChannelID != "" && !isUUID(filter.ChannelID)) || (filter.ConversationID != "" && !isUUID(filter.ConversationID)) {
		return page, nil
	}
	var before *time.Time
	var beforeID string
	if filter.Cursor != "" {
		at, id, ok := decodeChatCursor(filter.Cursor)
		if !ok {
			return page, invalid("cursor is not one this list gave.")
		}
		before, beforeID = &at, id
	}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+chatSummaryColumns+` FROM ai_chat_sessions s JOIN channels c ON c.workspace_id=s.workspace_id AND c.id=s.channel_id
			WHERE s.workspace_id=$1 AND ($2='' OR s.channel_id::text=$2) AND ($3='' OR s.status=$3)
				AND (NOT $4 OR EXISTS (SELECT 1 FROM ai_chat_feedback f JOIN ai_chat_turns t ON t.workspace_id=f.workspace_id AND t.id=f.turn_id
					WHERE f.workspace_id=s.workspace_id AND t.session_id=s.id AND f.rating='not_helpful'))
				AND ($5::timestamptz IS NULL OR (s.last_turn_at,s.id::text)<($5,$6)) AND ($8='' OR s.conversation_id::text=$8)
			ORDER BY s.last_turn_at DESC,s.id::text DESC LIMIT $7`,
			workspaceID, filter.ChannelID, filter.Status, filter.NotHelpful, before, beforeID, filter.Limit+1, filter.ConversationID)
		if err != nil {
			return err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChatSessionSummary, error) { return scanChatSummary(row) })
		if err != nil {
			return err
		}
		if len(items) > filter.Limit {
			items = items[:filter.Limit]
			last := items[len(items)-1]
			next := encodeChatCursor(last.LastTurnAt, last.ID)
			page.Next = &next
		}
		page.Items = items
		return nil
	})
	return page, err
}

func encodeChatCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeChatCursor(cursor string) (time.Time, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	at, id, found := strings.Cut(string(raw), "|")
	when, err := time.Parse(time.RFC3339Nano, at)
	return when, id, found && err == nil && isUUID(id)
}

// ChatSession reads one chat with its transcript; another workspace's chat is ErrChatNotFound.
func (s *Service) ChatSession(ctx context.Context, workspaceID, id string) (ChatSession, error) {
	if !isUUID(id) {
		return ChatSession{}, ErrChatNotFound
	}
	var chat ChatSession
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+chatSummaryColumns+`,s.history_cut_at FROM ai_chat_sessions s
			JOIN channels c ON c.workspace_id=s.workspace_id AND c.id=s.channel_id WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id)
		var summary ChatSessionSummary
		err := row.Scan(&summary.ID, &summary.ChannelID, &summary.ChannelName, &summary.Surface, &summary.Status, &summary.Turns, &summary.FirstQ, &summary.NotHelpful,
			&summary.Conversation, &summary.CreatedAt, &summary.LastTurnAt, &summary.EndedAt, &chat.HistoryCutAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChatNotFound
		}
		if err != nil {
			return err
		}
		summary.CreatedAt, summary.LastTurnAt, summary.EndedAt = summary.CreatedAt.UTC(), summary.LastTurnAt.UTC(), utcPointer(summary.EndedAt)
		summary.KeptUntil = summary.LastTurnAt.AddDate(0, 0, ChatTranscriptDays)
		chat.ChatSessionSummary, chat.HistoryCutAt = summary, utcPointer(chat.HistoryCutAt)
		chat.Transcript, err = readChatTurns(ctx, tx, workspaceID, id)
		return err
	})
	return chat, err
}

// readChatTurns reads a chat's turns in order, each with its sources and feedback.
func readChatTurns(ctx context.Context, tx pgx.Tx, workspaceID, sessionID string) ([]ChatTurn, error) {
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.seq,t.question,t.answer,t.status,t.error_code,t.viewing_kb_article_id::text,f.rating,t.created_at,t.finished_at
		FROM ai_chat_turns t LEFT JOIN ai_chat_feedback f ON f.workspace_id=t.workspace_id AND f.turn_id=t.id
		WHERE t.workspace_id=$1 AND t.session_id=$2 ORDER BY t.seq`, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	turns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChatTurn, error) {
		var t ChatTurn
		err := row.Scan(&t.ID, &t.Seq, &t.Question, &t.Answer, &t.Status, &t.ErrorCode, &t.Viewing, &t.Feedback, &t.CreatedAt, &t.FinishedAt)
		t.CreatedAt, t.FinishedAt, t.Sources = t.CreatedAt.UTC(), utcPointer(t.FinishedAt), []ChatSource{}
		return t, err
	})
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i, turn := range turns {
		index[turn.ID] = i
	}
	sources, err := tx.Query(ctx, `SELECT o.turn_id::text,o.ordinal,o.kb_article_id::text,o.article_number,o.article_version,o.title,o.folder
		FROM ai_chat_sources o JOIN ai_chat_turns t ON t.workspace_id=o.workspace_id AND t.id=o.turn_id
		WHERE o.workspace_id=$1 AND t.session_id=$2 ORDER BY t.seq,o.ordinal`, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	defer sources.Close()
	for sources.Next() {
		var turnID string
		var source ChatSource
		if err := sources.Scan(&turnID, &source.Ordinal, &source.ArticleID, &source.Number, &source.Version, &source.Title, &source.Folder); err != nil {
			return nil, err
		}
		if i, ok := index[turnID]; ok {
			turns[i].Sources = append(turns[i].Sources, source)
		}
	}
	if turns == nil {
		turns = []ChatTurn{}
	}
	return turns, sources.Err()
}

// DeleteChatSession removes a chat and its transcript for good. A conversation it handed off keeps its
// own copy. The removal is recorded in the AI settings history, without its texts.
func (s *Service) DeleteChatSession(ctx context.Context, workspaceID string, actor Actor, id string) error {
	if !isUUID(id) {
		return ErrChatNotFound
	}
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var turns int
		var surface string
		err := tx.QueryRow(ctx, `DELETE FROM ai_chat_sessions s WHERE s.workspace_id=$1 AND s.id=$2
			RETURNING s.surface,(SELECT count(*) FROM ai_chat_turns t WHERE t.workspace_id=s.workspace_id AND t.session_id=s.id)`, workspaceID, id).Scan(&surface, &turns)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChatNotFound
		}
		if err != nil {
			return err
		}
		return addEvent(ctx, tx, workspaceID, "chat_deleted", actor, "", "", SlotChat, map[string]any{"chat_id": id, "turns": turns, "surface": surface}, s.now().UTC())
	})
}

// ChatUsage is a month of the chat against its limits. On the cloud's own provider the limits are the
// plan's (answers and spend) and spend shows as a share, never dollars; on a provider the workspace pays
// for, or a server without plan limits, the limit is the slot's own monthly spend, if any.
type ChatUsage struct {
	Month        string `json:"month"`
	Answers      int    `json:"answers"`
	AnswersToday int    `json:"answers_today"`
	// AnswerLimit and DayAnswerLimit are null where answers are not limited.
	AnswerLimit    *int `json:"answer_limit"`
	DayAnswerLimit *int `json:"day_answer_limit"`
	// SpentMicroUSD is null on the cloud's own provider; SpentPercent is of the month's limit, null without one.
	SpentMicroUSD *int64 `json:"spent_micro_usd"`
	LimitMicroUSD *int64 `json:"limit_micro_usd"`
	SpentPercent  *int   `json:"spent_percent"`
	// UnpricedModel is the chat's model when the server has no price for it (AI_SERVER_PRICES): its
	// spend is not counted (SpentMicroUSD is null) and no limit in dollars can be set.
	UnpricedModel *string `json:"unpriced_model"`
	Chats         int     `json:"chats"`
	HandedOff     int     `json:"handed_off"`
	Helpful       int     `json:"rated_helpful"`
	Rated         int     `json:"rated"`
	// Articles is how many published articles the chat may answer from; zero answers nothing.
	Articles int       `json:"articles"`
	ResetsAt time.Time `json:"resets_at"`
}

// chatLimits are the limits a workspace's chat runs under now.
type chatLimits struct {
	answers  *int
	microUSD *int64
	// planned: the cloud's own provider, where spend shows as a share of the plan's limit.
	planned bool
}

func (s *Service) chatLimits(ctx context.Context, tx pgx.Tx, workspaceID string, row slotRow, found bool) (chatLimits, error) {
	if found && row.usesServer && s.config.PlanLimits {
		access, err := store.WorkspaceAccess(ctx, tx, workspaceID, s.now())
		if err != nil {
			return chatLimits{}, err
		}
		limits := domain.ChatLimitsFor(access.Plan)
		if access.Locked {
			limits = domain.ChatLimits{}
		}
		return chatLimits{answers: &limits.Answers, microUSD: &limits.MicroUSD, planned: true}, nil
	}
	if found {
		return chatLimits{microUSD: row.monthlyCap}, nil
	}
	return chatLimits{}, nil
}

// ChatUsage reads the chat's use of the month starting at month.
func (s *Service) ChatUsage(ctx context.Context, workspaceID string, month time.Time) (ChatUsage, error) {
	month = monthStart(month)
	now := s.now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	usage := ChatUsage{Month: month.Format("2006-01"), ResetsAt: month.AddDate(0, 1, 0)}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		slots, err := readSlots(ctx, tx, workspaceID, false)
		if err != nil {
			return err
		}
		row, found := slots[SlotChat]
		limits, err := s.chatLimits(ctx, tx, workspaceID, row, found)
		if err != nil {
			return err
		}
		var spent int64
		err = tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM ai_chat_turns WHERE workspace_id=$1 AND status IN ('answered','no_source') AND created_at>=$2 AND created_at<$3),
				(SELECT count(*) FROM ai_chat_turns WHERE workspace_id=$1 AND status IN ('answered','no_source') AND created_at>=$4),
				(SELECT COALESCE(sum(CASE WHEN status='started' THEN COALESCE(reserved_micro_usd,0) ELSE COALESCE(cost_micro_usd,0) END),0)
					FROM ai_usage WHERE workspace_id=$1 AND feature='chat' AND started_at>=$2 AND started_at<$3),
				(SELECT count(*) FROM ai_chat_sessions WHERE workspace_id=$1 AND created_at>=$2 AND created_at<$3),
				(SELECT count(*) FROM ai_chat_sessions WHERE workspace_id=$1 AND status='handed_off' AND created_at>=$2 AND created_at<$3),
				(SELECT count(*) FILTER (WHERE f.rating='helpful') FROM ai_chat_feedback f WHERE f.workspace_id=$1 AND f.created_at>=$2 AND f.created_at<$3),
				(SELECT count(*) FROM ai_chat_feedback f WHERE f.workspace_id=$1 AND f.created_at>=$2 AND f.created_at<$3),
				(SELECT count(*) FROM kb_articles a JOIN help_centers h ON h.workspace_id=a.workspace_id AND h.enabled
					WHERE a.workspace_id=$1 AND a.status='published' AND a.deleted_at IS NULL AND NOT a.ai_excluded)`,
			workspaceID, month, month.AddDate(0, 1, 0), day).
			Scan(&usage.Answers, &usage.AnswersToday, &spent, &usage.Chats, &usage.HandedOff, &usage.Helpful, &usage.Rated, &usage.Articles)
		if err != nil {
			return err
		}
		if limits.answers != nil {
			dayLimit := *limits.answers / 10
			usage.AnswerLimit, usage.DayAnswerLimit = limits.answers, &dayLimit
		}
		if found && row.model != nil {
			if _, priced := s.config.Server.PriceOf(*row.model); !priced {
				usage.UnpricedModel = row.model
			}
		}
		if !limits.planned && usage.UnpricedModel == nil {
			usage.SpentMicroUSD = &spent
		}
		if limits.microUSD != nil {
			usage.LimitMicroUSD = limits.microUSD
			percent := 100
			if *limits.microUSD > 0 {
				percent = int(min(spent*100 / *limits.microUSD, 100))
			}
			usage.SpentPercent = &percent
			if limits.planned {
				usage.LimitMicroUSD = nil
			}
		}
		return nil
	})
	return usage, err
}
