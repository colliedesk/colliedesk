package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// The end user's side of the help center's AI chat: one browser's chat (found by the hash of its
// cookie's token, on the channel it started on), its turns, feedback, pre-warm and an admin's preview.
// The HTTP side (cookies, Origin, NDJSON) is the API's; docs/internals/ai-chat.md records the decisions.

const (
	// ChatQuestionRunes is the longest question.
	ChatQuestionRunes = 1000
	// ChatTurnsPerSession is how many questions one chat holds.
	ChatTurnsPerSession = 50
	// ChatKeptFor is how long a chat continues after its last turn.
	ChatKeptFor = 30 * 24 * time.Hour
	// chatAnswerTimeout bounds one answer, whether or not the end user still waits for it.
	chatAnswerTimeout = 60 * time.Second
	// chatStoreTimeout bounds storing an answer, after its own time.
	chatStoreTimeout = 10 * time.Second
	// ChatInterruptedAfter: a turn still streaming this long after it began was cut (a crash, a deploy);
	// the sweep marks it failed so it is not read back as on its way for ever.
	ChatInterruptedAfter = 5 * time.Minute
	// Answers streaming at once: per workspace and per process.
	chatPerWorkspace = 3
	chatPerProcess   = 32
	// chatWarmEvery: a workspace's cache read or written this recently needs no pre-warm.
	chatWarmEvery = 4 * time.Minute
	// chatRelated is how many articles a paused chat offers instead of an answer.
	chatRelated = 3
)

var (
	// ErrChatFull means the chat holds ChatTurnsPerSession questions: the end user starts a new one.
	ErrChatFull = errors.New("this chat is full")
	// ErrChatBusy means too many answers are streaming: try again in a moment.
	ErrChatBusy = errors.New("too many chat answers at once")
	// ErrChatTurnNotFound means no turn with that id in the browser's chat.
	ErrChatTurnNotFound = errors.New("no such turn in this chat")
)

// Turn statuses.
const (
	ChatStreaming = "streaming"
	ChatAnswered  = "answered"
	ChatNoSource  = "no_source"
	ChatOverLimit = "over_limit"
	ChatFailed    = "failed"
)

// chatGate counts the answers streaming at once.
type chatGate struct {
	mu        sync.Mutex
	total     int
	workspace map[string]int
}

func newChatGate() *chatGate { return &chatGate{workspace: map[string]int{}} }

func (g *chatGate) enter(workspaceID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.total >= chatPerProcess || g.workspace[workspaceID] >= chatPerWorkspace {
		return false
	}
	g.total++
	g.workspace[workspaceID]++
	return true
}

func (g *chatGate) leave(workspaceID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.total--
	if g.workspace[workspaceID]--; g.workspace[workspaceID] <= 0 {
		delete(g.workspace, workspaceID)
	}
}

// ChatSurface is what a chat needs of its help chat channel and help center.
type ChatSurface struct {
	ChannelID     string
	WorkspaceName string
	Instructions  string
}

// ChatBrowser names one browser's chat: the hash of its cookie's token on a channel.
type ChatBrowser struct {
	ChannelID string
	TokenHash []byte
}

// ChatSourceView is an article an answer cites, as the end user sees it.
type ChatSourceView struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Folder string `json:"folder"`
}

// ChatTurnView is a turn as the end user's browser reads it back.
type ChatTurnView struct {
	ID        string           `json:"id"`
	Question  *string          `json:"question"`
	Answer    *string          `json:"answer"`
	Status    string           `json:"status"`
	ErrorCode *string          `json:"error_code"`
	Sources   []ChatSourceView `json:"sources"`
	Feedback  *string          `json:"feedback"`
	Redacted  bool             `json:"redacted"`
	CreatedAt time.Time        `json:"created_at"`
}

// ChatSessionView is a browser's chat.
type ChatSessionView struct {
	ID string `json:"-"`
	// HandedOffAt is the last hand-off, nil before one.
	HandedOffAt *time.Time     `json:"-"`
	Status      string         `json:"status"`
	IdleHours   int            `json:"idle_hours"`
	Turns       []ChatTurnView `json:"turns"`
}

// ChatEvents receive one answer as it is written. A call after the end user left is harmless.
type ChatEvents interface {
	Turn(id string, created bool)
	Delta(text string)
	Sources(sources []ChatArticleRef)
	Done(status string, links []string, redacted bool)
	Error(code string)
}

// ChatActive reports whether the workspace's chat feature can answer now (on, with its provider and model).
func (s *Service) ChatActive(ctx context.Context, workspaceID string) (bool, error) {
	features, err := s.ActiveFeatures(ctx, workspaceID)
	return features[SlotChat], err
}

// ChatActiveIn is ChatActive read in the caller's transaction, which is in the workspace.
func (s *Service) ChatActiveIn(ctx context.Context, tx pgx.Tx, workspaceID string) (bool, error) {
	slots, err := readSlots(ctx, tx, workspaceID, false)
	if err != nil {
		return false, err
	}
	row, found := slots[SlotChat]
	return found && s.view(SlotChat, row, true).Active, nil
}

// findChat is the open chat of a browser, kept ChatKeptFor after its last turn; an ended one is gone.
func (s *Service) findChat(ctx context.Context, tx pgx.Tx, workspaceID string, browser ChatBrowser, lock bool) (id, status string, last time.Time, found bool, err error) {
	if len(browser.TokenHash) != 32 || !isUUID(browser.ChannelID) {
		return "", "", time.Time{}, false, nil
	}
	query := `SELECT id::text,status,last_turn_at FROM ai_chat_sessions WHERE workspace_id=$1 AND channel_id=$2 AND token_hash=$3 AND status<>'ended' AND last_turn_at>$4`
	if lock {
		query += ` FOR UPDATE`
	}
	err = tx.QueryRow(ctx, query, workspaceID, browser.ChannelID, browser.TokenHash, s.now().UTC().Add(-ChatKeptFor)).Scan(&id, &status, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", time.Time{}, false, nil
	}
	return id, status, last, err == nil, err
}

// ChatSessionOf reads a browser's chat; found is false without one.
func (s *Service) ChatSessionOf(ctx context.Context, workspaceID string, browser ChatBrowser) (ChatSessionView, bool, error) {
	var view ChatSessionView
	var found bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, status, last, ok, err := s.findChat(ctx, tx, workspaceID, browser, false)
		if err != nil || !ok {
			return err
		}
		found, view.ID, view.Status, view.IdleHours = true, id, status, int(s.now().UTC().Sub(last).Hours())
		if err := tx.QueryRow(ctx, `SELECT handed_off_at FROM ai_chat_sessions WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&view.HandedOffAt); err != nil {
			return err
		}
		turns, err := readChatTurns(ctx, tx, workspaceID, id)
		view.Turns = make([]ChatTurnView, 0, len(turns))
		for _, turn := range turns {
			view.Turns = append(view.Turns, turnView(turn))
		}
		return err
	})
	return view, found, err
}

func turnView(turn ChatTurn) ChatTurnView {
	view := ChatTurnView{ID: turn.ID, Question: turn.Question, Answer: turn.Answer, Status: turn.Status, ErrorCode: turn.ErrorCode, Feedback: turn.Feedback,
		CreatedAt: turn.CreatedAt, Sources: []ChatSourceView{}}
	if turn.Answer != nil {
		view.Redacted = strings.Contains(*turn.Answer, ChatHidden)
	}
	for _, source := range turn.Sources {
		item := ChatSourceView{Number: source.Number}
		if source.Title != nil {
			item.Title = *source.Title
		}
		if source.Folder != nil {
			item.Folder = *source.Folder
		}
		view.Sources = append(view.Sources, item)
	}
	return view
}

// ChatTurnOf reads one turn of a browser's chat (to recover an answer whose stream broke).
func (s *Service) ChatTurnOf(ctx context.Context, workspaceID string, browser ChatBrowser, turnID string) (ChatTurnView, error) {
	var view ChatTurnView
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, _, _, ok, err := s.findChat(ctx, tx, workspaceID, browser, false)
		if err != nil {
			return err
		}
		if !ok || !isUUID(turnID) {
			return ErrChatTurnNotFound
		}
		turns, err := readChatTurns(ctx, tx, workspaceID, id)
		for _, turn := range turns {
			if turn.ID == turnID {
				view = turnView(turn)
				return err
			}
		}
		if err != nil {
			return err
		}
		return ErrChatTurnNotFound
	})
	return view, err
}

// SetChatFeedback rates an answer of the browser's chat; an empty rating takes the rating back.
func (s *Service) SetChatFeedback(ctx context.Context, workspaceID string, browser ChatBrowser, turnID, rating string) error {
	if rating != "" && rating != "helpful" && rating != "not_helpful" {
		return invalid("rating is helpful or not_helpful.")
	}
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, _, _, ok, err := s.findChat(ctx, tx, workspaceID, browser, false)
		if err != nil {
			return err
		}
		var answered bool
		if ok && isUUID(turnID) {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ai_chat_turns WHERE workspace_id=$1 AND session_id=$2 AND id=$3 AND status IN ('answered','no_source'))`,
				workspaceID, id, turnID).Scan(&answered); err != nil {
				return err
			}
		}
		if !answered {
			return ErrChatTurnNotFound
		}
		if rating == "" {
			_, err = tx.Exec(ctx, `DELETE FROM ai_chat_feedback WHERE workspace_id=$1 AND turn_id=$2`, workspaceID, turnID)
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ai_chat_feedback(workspace_id,turn_id,rating,created_at,updated_at) VALUES($1,$2,$3,$4,$4)
			ON CONFLICT (workspace_id,turn_id) DO UPDATE SET rating=EXCLUDED.rating,updated_at=EXCLUDED.updated_at`, workspaceID, turnID, rating, s.now().UTC())
		return err
	})
}

// EndChat ends the browser's chat ("New chat"); the team keeps its transcript.
func (s *Service) EndChat(ctx context.Context, workspaceID string, browser ChatBrowser) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, _, _, ok, err := s.findChat(ctx, tx, workspaceID, browser, true)
		if err != nil || !ok {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE ai_chat_sessions SET status='ended',ended_at=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, id, s.now().UTC())
		return err
	})
}

// ChatTurnInput is one question of a browser.
type ChatTurnInput struct {
	Surface  ChatSurface
	Browser  ChatBrowser
	Started  string // where a new chat starts: SurfaceHelpCenter, SurfaceArticle or SurfaceWidget
	Question string
	// ViewingArticle is the number of the article the end user reads, when they ask about it.
	ViewingArticle int64
}

// chatTurnStart is what the first transaction of a turn decided.
type chatTurnStart struct {
	sessionID, turnID string
	created           bool
	history           []ChatHistoryTurn
	viewing           string
}

// ChatTurn answers one question of a browser, streaming the answer to events. A refusal before the
// answer starts is an error (ErrFeatureOff, ErrChatBusy, ErrChatFull, InvalidError); once the turn is
// stored, every outcome is an event, and the answer is finished and stored even when ctx ends: the
// end user reads it back with ChatTurnOf.
func (s *Service) ChatTurn(ctx context.Context, workspaceID string, in ChatTurnInput, events ChatEvents) error {
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return invalid("Write a question.")
	}
	if len([]rune(question)) > ChatQuestionRunes {
		return &InvalidError{Message: fmt.Sprintf("A question is at most %d characters.", ChatQuestionRunes)}
	}
	binding, err := s.Resolve(ctx, workspaceID, SlotChat)
	if err != nil {
		return err
	}
	if !s.chats.enter(workspaceID) {
		return ErrChatBusy
	}
	defer s.chats.leave(workspaceID)
	start, err := s.startChatTurn(ctx, workspaceID, in, question)
	if err != nil {
		return err
	}
	events.Turn(start.turnID, start.created)
	// The answer goes on without the end user: it is stored and metered either way.
	timeout := s.config.ChatAnswerTimeout
	if timeout <= 0 {
		timeout = chatAnswerTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return s.answerChatTurn(ctx, workspaceID, binding, in, question, start, events)
}

func (s *Service) startChatTurn(ctx context.Context, workspaceID string, in ChatTurnInput, question string) (chatTurnStart, error) {
	var start chatTurnStart
	now := s.now().UTC()
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, _, _, found, err := s.findChat(ctx, tx, workspaceID, in.Browser, true)
		if err != nil {
			return err
		}
		if !found {
			surface := in.Started
			if surface != SurfaceArticle && surface != SurfaceWidget {
				surface = SurfaceHelpCenter
			}
			// A chat that ended or expired under the same token gives its token up first.
			if _, err := tx.Exec(ctx, `UPDATE ai_chat_sessions SET token_hash=sha256(token_hash||id::text::bytea) WHERE workspace_id=$1 AND channel_id=$2 AND token_hash=$3`,
				workspaceID, in.Browser.ChannelID, in.Browser.TokenHash); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface,created_at,last_turn_at) VALUES($1,$2,$3,$4,$5,$5) RETURNING id::text`,
				workspaceID, in.Browser.ChannelID, in.Browser.TokenHash, surface, now).Scan(&id); err != nil {
				return err
			}
			start.created = true
		}
		start.sessionID = id
		rows, err := tx.Query(ctx, `SELECT COALESCE(question,''),COALESCE(answer,''),status FROM ai_chat_turns WHERE workspace_id=$1 AND session_id=$2 ORDER BY seq`, workspaceID, id)
		if err != nil {
			return err
		}
		turns := 0
		for rows.Next() {
			var q, a, status string
			if err := rows.Scan(&q, &a, &status); err != nil {
				rows.Close()
				return err
			}
			turns++
			if status == ChatAnswered || status == ChatNoSource {
				start.history = append(start.history, ChatHistoryTurn{Question: q, Answer: a})
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if turns >= ChatTurnsPerSession {
			return ErrChatFull
		}
		if in.ViewingArticle > 0 {
			if start.viewing, err = store.ChatKBArticleIDOf(ctx, tx, workspaceID, in.ViewingArticle); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,viewing_kb_article_id,status,created_at)
			VALUES($1,$2,$3,$4,$5,'streaming',$6) RETURNING id::text`, workspaceID, id, turns+1, question, nullable(start.viewing), now).Scan(&start.turnID)
	})
	return start, err
}

// chatOutcome is what a turn stores at its end.
type chatOutcome struct {
	status, errorCode, answer, model string
	ttft                             *int
	usage                            aiprovider.Usage
	sources                          []ChatArticleRef
	cut                              bool
}

func (s *Service) answerChatTurn(ctx context.Context, workspaceID string, binding Binding, in ChatTurnInput, question string, start chatTurnStart, events ChatEvents) error {
	outcome := chatOutcome{model: binding.Model}
	prompt, err := s.ChatPrompt(ctx, binding, workspaceID, ChatPromptInput{WorkspaceName: in.Surface.WorkspaceName, Instructions: in.Surface.Instructions,
		History: start.history, Question: question, ViewingArticleID: start.viewing})
	switch {
	case err != nil:
		outcome.status, outcome.errorCode = ChatFailed, "model_failed"
		events.Error(outcome.errorCode)
	case len(prompt.Articles) == 0:
		// No article to answer from: said at once, nothing is sent.
		outcome.status = ChatNoSource
		events.Sources(nil)
		events.Done(ChatNoSource, nil, false)
	default:
		outcome = s.streamChat(ctx, workspaceID, binding, prompt, events)
		outcome.cut = prompt.Cut > 0
	}
	// The answer is stored even once its time ran out: the outcome says so, and the store has its own.
	store, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatStoreTimeout)
	defer cancel()
	if outcome.status == ChatOverLimit {
		related, err := s.chatRelated(store, workspaceID, question)
		if err != nil {
			slog.Error("ai chat: related articles", "workspace_id", workspaceID, "error", err)
		}
		outcome.sources = related
		events.Sources(related)
		events.Done(ChatOverLimit, nil, false)
	}
	return s.finishChatTurn(store, workspaceID, start, prompt, outcome)
}

// streamChat calls the provider and hands the answer out sentence by sentence, checked.
func (s *Service) streamChat(ctx context.Context, workspaceID string, binding Binding, prompt ChatPrompt, events ChatEvents) chatOutcome {
	outcome := chatOutcome{model: binding.Model}
	filter := newChatFilter(prompt.Sent)
	var sentences chatSentences
	var answer strings.Builder
	var links []string
	redacted := false
	started := time.Now()
	send := func(parts []string) {
		for _, part := range parts {
			text, hidden, allowed := filter.check(part)
			redacted = redacted || hidden
			links = append(links, allowed...)
			answer.WriteString(text)
			events.Delta(text)
		}
	}
	call := binding.call
	call.Feature = FeatureChat
	response, err := s.MeteredStream(ctx, call, binding.Provider, prompt.Request, func(text string) {
		if outcome.ttft == nil {
			ms := int(time.Since(started).Milliseconds())
			outcome.ttft = &ms
		}
		send(sentences.add(text, time.Now()))
	})
	outcome.usage = response.Usage
	if response.Model != "" {
		outcome.model = response.Model
	}
	switch {
	case errors.Is(err, ErrChatLimit):
		outcome.status = ChatOverLimit
		return outcome
	case err != nil:
		send(sentences.rest())
		outcome.answer, outcome.status, outcome.errorCode = answer.String(), ChatFailed, "model_failed"
		if aiprovider.CodeOf(err) == aiprovider.ErrorTimeout || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			outcome.errorCode = "timeout"
		}
		events.Error(outcome.errorCode)
		return outcome
	}
	send(sentences.rest())
	outcome.answer = answer.String()
	for _, number := range chatCited(outcome.answer) {
		if ref, ok := prompt.Articles[number]; ok {
			outcome.sources = append(outcome.sources, ref)
		}
	}
	outcome.status = ChatAnswered
	if len(outcome.sources) == 0 {
		outcome.status = ChatNoSource
	}
	events.Sources(outcome.sources)
	events.Done(outcome.status, links, redacted)
	// Only a hint for the pre-warm: a failure to note it costs one more warm call at most.
	_ = s.noteChatCache(ctx, workspaceID, prompt.Fingerprint, binding.Model, s.now().UTC())
	return outcome
}

// chatRelated are the articles that best match a question the chat could not answer (paused).
func (s *Service) chatRelated(ctx context.Context, workspaceID, question string) ([]ChatArticleRef, error) {
	var related []ChatArticleRef
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		docs, err := store.AIRetrieve(ctx, tx, workspaceID, store.AIRetrievalQuery{Text: question, KBLimit: chatRelated})
		if err != nil {
			return err
		}
		all, err := store.ChatKBArticles(ctx, tx, workspaceID, false)
		known := map[string]store.ChatKBArticle{}
		for _, a := range all {
			known[a.ID] = a
		}
		for _, doc := range docs {
			if a, ok := known[doc.KBArticleID]; ok && doc.Kind == store.AIRetrievedKBArticle && len(related) < chatRelated {
				related = append(related, refOf(a))
			}
		}
		return err
	})
	return related, err
}

// finishChatTurn stores the outcome, its sources, and the chat's last turn.
func (s *Service) finishChatTurn(ctx context.Context, workspaceID string, start chatTurnStart, prompt ChatPrompt, outcome chatOutcome) error {
	now := s.now().UTC()
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var input, cacheRead, cacheWrite, output *int64
		if outcome.usage.Reported {
			input, cacheRead, cacheWrite, output = &outcome.usage.InputTokens, &outcome.usage.CacheReadTokens, &outcome.usage.CacheWriteTokens, &outcome.usage.OutputTokens
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_chat_turns SET answer=$3,status=$4,error_code=$5,model=$6,ttft_ms=$7,input_tokens=$8,cache_read_tokens=$9,
				cache_creation_tokens=$10,output_tokens=$11,finished_at=$12 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, start.turnID, nullable(outcome.answer), outcome.status, nullable(outcome.errorCode), nullable(outcome.model), outcome.ttft,
			input, cacheRead, cacheWrite, output, now); err != nil {
			return err
		}
		for i, source := range outcome.sources {
			if _, err := tx.Exec(ctx, `INSERT INTO ai_chat_sources(workspace_id,turn_id,ordinal,kb_article_id,article_number,article_version,title,folder)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, workspaceID, start.turnID, i+1, source.ID, source.Number, source.Version, source.Title, source.Folder); err != nil {
				return err
			}
		}
		cut := any(nil)
		if outcome.cut {
			cut = now
		}
		_, err := tx.Exec(ctx, `UPDATE ai_chat_sessions SET last_turn_at=$3,history_cut_at=COALESCE($4,history_cut_at) WHERE workspace_id=$1 AND id=$2`,
			workspaceID, start.sessionID, now, cut)
		return err
	})
}

// ChatPrewarm writes the cache of the workspace's articles before the end user's first question, so
// it answers sooner. It is skipped when the cache was read or written in the last minutes or the
// provider keeps none; it creates no chat and counts toward the chat's limits.
func (s *Service) ChatPrewarm(ctx context.Context, workspaceID string, surface ChatSurface) error {
	binding, err := s.Resolve(ctx, workspaceID, SlotChat)
	if err != nil || !aiprovider.Caches(binding.Provider) {
		return err
	}
	prompt, err := s.ChatPrompt(ctx, binding, workspaceID, ChatPromptInput{WorkspaceName: surface.WorkspaceName, Instructions: surface.Instructions, Question: "?"})
	if err != nil || !prompt.Whole {
		return err
	}
	var recent bool
	if err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(last_cache_at>$4,false) FROM ai_chat_kb_snapshots WHERE workspace_id=$1 AND fingerprint=$2 AND model=$3`,
			workspaceID, prompt.Fingerprint, binding.Model, s.now().UTC().Add(-chatWarmEvery)).Scan(&recent)
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if recent {
		return nil
	}
	request := prompt.Request
	request.Warm, request.MaxOutput = true, 0
	call := binding.call
	call.Feature = FeatureChat
	timeout := s.config.ChatAnswerTimeout
	if timeout <= 0 {
		timeout = chatAnswerTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if _, err := s.Metered(ctx, call, binding.Provider, request); err != nil {
		if errors.Is(err, ErrChatLimit) {
			return nil
		}
		return err
	}
	return s.noteChatCache(ctx, workspaceID, prompt.Fingerprint, binding.Model, s.now().UTC())
}

// ChatPreviewResult is an admin's trial answer.
type ChatPreviewResult struct {
	Answer   string           `json:"answer"`
	Status   string           `json:"status"`
	Sources  []ChatSourceView `json:"sources"`
	Redacted bool             `json:"redacted"`
	// Whole says the AI read every article (else the best excerpts); Articles is how many it read.
	Whole    bool `json:"whole"`
	Articles int  `json:"articles"`
	// TookMS is from the question to the end of the answer.
	TookMS int `json:"took_ms"`
}

// chatCollector gathers an answer's events into a ChatPreviewResult.
type chatCollector struct {
	result ChatPreviewResult
	answer strings.Builder
	failed string
}

func (c *chatCollector) Turn(string, bool) {}
func (c *chatCollector) Delta(text string) { c.answer.WriteString(text) }
func (c *chatCollector) Error(code string) { c.failed = code }
func (c *chatCollector) Sources(sources []ChatArticleRef) {
	for _, source := range sources {
		c.result.Sources = append(c.result.Sources, ChatSourceView{Number: source.Number, Title: source.Title, Folder: source.Folder})
	}
}
func (c *chatCollector) Done(status string, _ []string, redacted bool) {
	c.result.Status, c.result.Redacted = status, redacted
}

// ChatPreviewError means the provider failed the preview's answer; Code says how.
type ChatPreviewError struct{ Code string }

func (e *ChatPreviewError) Error() string { return "the AI chat preview failed: " + e.Code }

// ChatPreview answers a question as the chat would, for an admin trying it: nothing is stored but the
// call's usage, which counts toward the chat's limits.
func (s *Service) ChatPreview(ctx context.Context, workspaceID string, actor Actor, surface ChatSurface, question string) (ChatPreviewResult, error) {
	question = strings.TrimSpace(question)
	if question == "" || len([]rune(question)) > ChatQuestionRunes {
		return ChatPreviewResult{}, &InvalidError{Message: fmt.Sprintf("Write a question of at most %d characters.", ChatQuestionRunes)}
	}
	binding, err := s.Resolve(ctx, workspaceID, SlotChat)
	if err != nil {
		return ChatPreviewResult{}, err
	}
	if !s.chats.enter(workspaceID) {
		return ChatPreviewResult{}, ErrChatBusy
	}
	defer s.chats.leave(workspaceID)
	started := time.Now()
	binding.call.Actor = actor
	prompt, err := s.ChatPrompt(ctx, binding, workspaceID, ChatPromptInput{WorkspaceName: surface.WorkspaceName, Instructions: surface.Instructions, Question: question})
	if err != nil {
		return ChatPreviewResult{}, err
	}
	collector := &chatCollector{result: ChatPreviewResult{Sources: []ChatSourceView{}, Whole: prompt.Whole, Articles: len(prompt.Articles)}}
	if len(prompt.Articles) == 0 {
		collector.result.Status = ChatNoSource
	} else {
		outcome := s.streamChat(ctx, workspaceID, binding, prompt, collector)
		if outcome.status == ChatOverLimit {
			related, err := s.chatRelated(ctx, workspaceID, question)
			if err != nil {
				return ChatPreviewResult{}, err
			}
			collector.Sources(related)
			collector.result.Status = ChatOverLimit
		}
	}
	if collector.failed != "" {
		return ChatPreviewResult{}, &ChatPreviewError{Code: collector.failed}
	}
	collector.result.Answer, collector.result.TookMS = collector.answer.String(), int(time.Since(started).Milliseconds())
	return collector.result, nil
}

// ChatTransferFor is how long a code that moves a chat to the help center's page works.
const ChatTransferFor = 2 * time.Minute

// CreateChatTransfer makes a one-time code that moves the browser's chat (in a site's frame) to the help
// center's own page; found is false without a chat. Only the code's hash is stored.
func (s *Service) CreateChatTransfer(ctx context.Context, workspaceID string, browser ChatBrowser, codeHash []byte) (bool, error) {
	var found bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		id, _, _, ok, err := s.findChat(ctx, tx, workspaceID, browser, false)
		if err != nil || !ok {
			return err
		}
		found = true
		_, err = tx.Exec(ctx, `INSERT INTO ai_chat_transfers(workspace_id,session_id,code_hash,expires_at) VALUES($1,$2,$3,$4)`,
			workspaceID, id, codeHash, s.now().UTC().Add(ChatTransferFor))
		return err
	})
	return found, err
}

// RedeemChatTransfer uses a code once, within its time, on the channel it was made on: the chat moves to
// pageTokenHash (the page's own cookie) and the frame's token no longer finds it, so a link sent to
// someone else never shares one chat between two browsers. ok is false for a code that is unknown,
// used, expired or of another channel.
func (s *Service) RedeemChatTransfer(ctx context.Context, workspaceID, channelID string, codeHash, pageTokenHash []byte) (bool, error) {
	var ok bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var sessionID string
		err := tx.QueryRow(ctx, `UPDATE ai_chat_transfers t SET used_at=$4 FROM ai_chat_sessions s
			WHERE t.workspace_id=$1 AND t.code_hash=$2 AND t.used_at IS NULL AND t.expires_at>$4
				AND s.workspace_id=t.workspace_id AND s.id=t.session_id AND s.channel_id=$3 AND s.status<>'ended'
			RETURNING t.session_id::text`, workspaceID, codeHash, channelID, s.now().UTC()).Scan(&sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ok = true
		_, err = tx.Exec(ctx, `UPDATE ai_chat_sessions SET token_hash=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, sessionID, pageTokenHash)
		return err
	})
	return ok, err
}

// ChatHandedOff records that a chat's "Talk to a person" opened (or joined) a conversation: the chat
// says so in its transcript and links the conversation. The chat goes on; its texts stay its own.
func (s *Service) ChatHandedOff(ctx context.Context, workspaceID, sessionID, conversationID string) error {
	if !isUUID(sessionID) || !isUUID(conversationID) {
		return nil
	}
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE ai_chat_sessions SET status='handed_off',conversation_id=$3,handed_off_at=$4 WHERE workspace_id=$1 AND id=$2 AND status<>'ended'`,
			workspaceID, sessionID, conversationID, s.now().UTC())
		return err
	})
}
