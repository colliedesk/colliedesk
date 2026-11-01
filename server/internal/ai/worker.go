package ai

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// WorkerOptions tunes the worker; zero values take the defaults.
type WorkerOptions struct {
	// Interval is how often the worker looks for due tasks when nothing wakes it (5 s).
	Interval time.Duration
	// Lease is how long a claimed task belongs to this worker (3 min): longer than the longest call
	// (90 s) plus closing its usage row, so a task is never run twice at once.
	Lease time.Duration
	// MaxRunning caps the provider calls of the whole server (8) and PerWorkspace those of one
	// workspace (2), so one busy workspace cannot hold every slot.
	MaxRunning   int
	PerWorkspace int
	// MaxAttempts is how many times a task is tried before it fails (3).
	MaxAttempts int
	// Timeouts bound one attempt of each kind (triage 30 s, draft 90 s, intake 75 s: its model and,
	// when that fails, its fallback model).
	Timeouts map[string]time.Duration
}

func (o WorkerOptions) withDefaults() WorkerOptions {
	if o.Interval <= 0 {
		o.Interval = 5 * time.Second
	}
	if o.Lease <= 0 {
		o.Lease = 3 * time.Minute
	}
	if o.MaxRunning <= 0 {
		o.MaxRunning = 8
	}
	if o.PerWorkspace <= 0 {
		o.PerWorkspace = 2
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.Timeouts == nil {
		o.Timeouts = map[string]time.Duration{SlotTriage: 30 * time.Second, SlotDraft: 90 * time.Second, SlotIntake: 75 * time.Second}
	}
	return o
}

// Worker runs queued AI tasks in `collie serve`. It claims due tasks of every workspace under
// PrivilegeAIJobClaim, then reads, calls and records each in its own workspace's transactions,
// never holding one across a provider call.
type Worker struct {
	service  *Service
	db       *database.DB
	options  WorkerOptions
	handlers map[string]Handler

	mu      sync.Mutex
	running int
	done    sync.WaitGroup
}

// NewWorker makes a worker for the service's tasks; Handle registers what runs each kind.
func NewWorker(service *Service, db *database.DB, options WorkerOptions) *Worker {
	return &Worker{service: service, db: db, options: options.withDefaults(), handlers: map[string]Handler{}}
}

// Handle registers the handler of a task kind. A task of a kind without a handler fails.
func (w *Worker) Handle(kind string, handler Handler) { w.handlers[kind] = handler }

// Run ticks until ctx ends, then waits for the tasks it started.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("AI worker tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			w.done.Wait()
			return
		case <-ticker.C:
		case <-w.service.wakeup:
		}
	}
}

// Tick closes usage rows a dead process left started, then starts the due tasks there is room for.
// Tests call it and Wait instead of Run.
func (w *Worker) Tick(ctx context.Context) error {
	if err := w.sweep(ctx); err != nil {
		return err
	}
	w.mu.Lock()
	room := w.options.MaxRunning - w.running
	w.mu.Unlock()
	if room <= 0 {
		return nil
	}
	claimed, err := w.claim(ctx, room)
	if err != nil {
		return err
	}
	for _, item := range claimed {
		w.mu.Lock()
		w.running++
		w.mu.Unlock()
		w.done.Add(1)
		go func() {
			defer w.done.Done()
			defer func() {
				w.mu.Lock()
				w.running--
				w.mu.Unlock()
			}()
			w.run(ctx, item)
		}()
	}
	return nil
}

// Wait blocks until every task this worker started has ended.
func (w *Worker) Wait() { w.done.Wait() }

type claimedTask struct {
	id, workspaceID, token string
}

// claim leases due tasks: people's tasks before automatic ones, then oldest first, and no more per
// workspace than PerWorkspace minus the tasks it already has running under a live lease (counted in
// the database, so it holds while two processes overlap during a deploy). One workspace's backlog
// therefore never holds back another's. A task found running had a worker that died; it is leased
// again and its attempt counts.
func (w *Worker) claim(ctx context.Context, limit int) ([]claimedTask, error) {
	now := w.service.now().UTC()
	var out []claimedTask
	err := w.db.Privileged(ctx, database.PrivilegeAIJobClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH running AS (
				SELECT workspace_id, count(*) AS n FROM ai_tasks WHERE state='running' AND lease_until>$1 GROUP BY workspace_id),
			ranked AS (
				SELECT t.id, t.automatic, t.next_run_at,
					row_number() OVER (PARTITION BY t.workspace_id ORDER BY t.automatic, t.automatic AND t.kind='draft', t.next_run_at) + COALESCE(r.n, 0) AS place
				FROM ai_tasks t LEFT JOIN running r ON r.workspace_id=t.workspace_id
				WHERE t.state IN ('queued','running') AND t.next_run_at<=$1 AND (t.lease_until IS NULL OR t.lease_until<=$1)),
			due AS (
				SELECT t.id FROM ai_tasks t JOIN ranked k ON k.id=t.id WHERE k.place<=$4
				ORDER BY k.automatic, k.automatic AND t.kind='draft', k.next_run_at LIMIT $2 FOR UPDATE OF t SKIP LOCKED)
			UPDATE ai_tasks t SET state='running', claim_token=gen_random_uuid(), lease_until=$1+$3::interval, attempts=t.attempts+1
			FROM due WHERE t.id=due.id RETURNING t.id::text, t.workspace_id::text, t.claim_token::text`, now, limit, w.options.Lease.String(), w.options.PerWorkspace)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimedTask, error) {
			var item claimedTask
			return item, row.Scan(&item.id, &item.workspaceID, &item.token)
		})
		return err
	})
	return out, err
}

// release queues a task again at at without counting the attempt: the process is stopping, or the
// workspace used its automatic runs of this minute.
func (w *Worker) release(ctx context.Context, item claimedTask, at time.Time) {
	ctx = context.WithoutCancel(ctx)
	err := w.db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		if done, err := w.endIfReplaced(ctx, tx, item); done || err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET state='queued',claim_token=NULL,lease_until=NULL,attempts=greatest(attempts-1,0),next_run_at=$4
			WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`, item.workspaceID, item.id, item.token, at)
		return err
	})
	if err != nil {
		slog.Error("AI worker could not give back a task", "task", item.id, "error", err)
	}
}

// sweep closes usage rows left started longer than a lease and a margin: the process making the call
// died. They count as failed calls of unknown outcome.
func (w *Worker) sweep(ctx context.Context) error {
	now := w.service.now().UTC()
	return w.db.Privileged(ctx, database.PrivilegeAIJobClaim, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE ai_usage SET status='error',error_code='unknown',finished_at=$1
			WHERE status='started' AND started_at<$2`, now, now.Add(-w.options.Lease-2*time.Minute)); err != nil {
			return err
		}
		// A ready draft or suggestion nobody decided on for a week expired (a suggestion partly applied
		// counts as partly accepted, one that named nothing stays without an outcome); 90 days after a draft ended, its text and prompt are erased and
		// its numbers stay.
		if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET outcome='expired',decided_at=$1 WHERE status='ready' AND outcome IS NULL AND ready_at<$2`,
			now, now.Add(-draftExpiry)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET outcome=CASE WHEN `+triageAppliedSome+` THEN 'partly_accepted' ELSE 'expired' END,decided_at=$1
			WHERE status='ready' AND outcome IS NULL AND ready_at<$2 AND (tag_ids<>'{}' OR priority IS NOT NULL OR assignee_agent_id IS NOT NULL)`,
			now, now.Add(-triageExpiry)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET text=NULL,prompt='',missing_info=NULL,warnings='[]',text_cleared_at=$1
			WHERE text_cleared_at IS NULL AND (decided_at IS NOT NULL OR status<>'ready') AND COALESCE(decided_at,created_at)<$2`, now, now.Add(-draftTextRetention)); err != nil {
			return err
		}
		// A suggestion's reason, the only generated text it holds, goes 90 days after it ended too.
		// An open one has expired long before (one naming nothing never does, so it goes by its age).
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET reason=NULL WHERE reason IS NOT NULL AND COALESCE(decided_at,created_at)<$1`,
			now.Add(-draftTextRetention)); err != nil {
			return err
		}
		return sweepChats(ctx, tx, now)
	})
}

// chatSessionKept is how long a chat's numbers stay after its last turn; its texts go after
// ChatTranscriptDays. A transfer code is kept a day after it expired.
const chatSessionKept = 400 * 24 * time.Hour

// sweepChats erases the texts of chats whose last turn is ChatTranscriptDays old (questions, answers,
// the titles of what they cited), removes chats a year old and transfer codes a day after they expired.
func sweepChats(ctx context.Context, tx pgx.Tx, now time.Time) error {
	expired := now.AddDate(0, 0, -ChatTranscriptDays)
	if _, err := tx.Exec(ctx, `UPDATE ai_chat_turns t SET question=NULL,answer=NULL FROM ai_chat_sessions s
		WHERE s.workspace_id=t.workspace_id AND s.id=t.session_id AND s.last_turn_at<$1 AND (t.question IS NOT NULL OR t.answer IS NOT NULL)`, expired); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_chat_sources o SET title=NULL,folder=NULL FROM ai_chat_turns t JOIN ai_chat_sessions s ON s.workspace_id=t.workspace_id AND s.id=t.session_id
		WHERE t.workspace_id=o.workspace_id AND t.id=o.turn_id AND s.last_turn_at<$1 AND (o.title IS NOT NULL OR o.folder IS NOT NULL)`, expired); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_chat_turns SET status='failed',error_code='interrupted',finished_at=$1 WHERE status='streaming' AND created_at<$2`,
		now, now.Add(-ChatInterruptedAfter)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ai_chat_sessions WHERE last_turn_at<$1`, now.Add(-chatSessionKept)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM ai_chat_transfers WHERE expires_at<$1`, now.Add(-24*time.Hour))
	return err
}

// run is one attempt of one task. Every write names the claim token: a task cancelled or leased again
// meanwhile ignores what this attempt would record.
// A task that only waits for other calls to give back what they hold tries again after heldRetry,
// for at most heldWait from when it was queued.
const (
	heldRetry = 5 * time.Second
	heldWait  = 10 * time.Minute
)

func (w *Worker) run(ctx context.Context, item claimedTask) {
	task, err := w.load(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Error("AI worker could not read a task", "task", item.id, "error", err)
		return
	}
	stop := func(state, code string) { w.finish(ctx, item, state, code) }
	if task.Attempts > w.options.MaxAttempts {
		stop(TaskFailed, CodeNoProgress)
		return
	}
	handler, ok := w.handlers[task.Kind]
	if !ok {
		stop(TaskFailed, CodeNoHandler)
		return
	}
	if code, err := w.admit(ctx, item, task); err != nil {
		slog.Error("AI worker could not admit a task", "task", item.id, "error", err)
		w.retry(ctx, item, task, &aiprovider.Error{Code: aiprovider.ErrorUnavailable})
		return
	} else if code == codeMinuteFull {
		// A burst past the per-minute limit waits for the next minute; only the daily limit drops work.
		w.release(ctx, item, w.service.now().UTC().Truncate(time.Minute).Add(time.Minute))
		return
	} else if code != "" {
		stop(TaskCancelled, code)
		return
	}
	binding, err := w.service.Resolve(ctx, task.WorkspaceID, task.Kind)
	if errors.Is(err, ErrFeatureOff) {
		stop(TaskCancelled, CodeFeatureOff)
		return
	}
	if err != nil {
		slog.Error("AI worker could not resolve a feature", "task", item.id, "error", err)
		stop(TaskFailed, CodeInternal)
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, w.options.Timeouts[task.Kind])
	err = handler.Run(runCtx, &Run{Task: task, Binding: binding, service: w.service, claim: item})
	cancel()
	var providerErr *aiprovider.Error
	switch {
	case err != nil && ctx.Err() != nil:
		// The process is stopping (a deploy): the task goes back to the queue at once and this
		// attempt does not count, though its call, if one went out, keeps its usage row.
		w.release(ctx, item, w.service.now().UTC())
	case errors.Is(err, ErrClaimLost):
		// Cancelled or leased again while it ran: what it would record is no longer wanted.
	case err == nil:
		stop(TaskDone, "")
	case errors.Is(err, ErrAllowanceReached):
		stop(TaskFailed, CodeAllowanceReached)
	case errors.Is(err, ErrAllowanceHeld) && w.service.now().Sub(task.CreatedAt) < heldWait:
		// Room is held by calls still running: try again shortly, without counting an attempt. A call
		// that never finishes gives its hold back when the sweep closes its row.
		w.release(ctx, item, w.service.now().UTC().Add(heldRetry))
	case errors.Is(err, ErrAllowanceHeld):
		stop(TaskFailed, CodeAllowanceReached)
	case errors.As(err, new(*TaskError)):
		var taskErr *TaskError
		errors.As(err, &taskErr)
		stop(TaskFailed, taskErr.Code)
	case errors.As(err, &providerErr) && providerErr.Retryable() && task.Attempts < w.options.MaxAttempts && ctx.Err() == nil:
		w.retry(ctx, item, task, providerErr)
	case errors.As(err, &providerErr):
		stop(TaskFailed, string(providerErr.Code))
	default:
		slog.Error("AI task failed", "task", item.id, "kind", task.Kind, "error", err)
		stop(TaskFailed, CodeInternal)
	}
}

func (w *Worker) load(ctx context.Context, item claimedTask) (Task, error) {
	task := Task{ID: item.id, WorkspaceID: item.workspaceID}
	err := w.db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		var conversation, agent, token *string
		if err := tx.QueryRow(ctx, `SELECT kind,conversation_id::text,automatic,requested_by_agent_id::text,requested_by_token_id::text,attempts,created_at
			FROM ai_tasks WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`, item.workspaceID, item.id, item.token).
			Scan(&task.Kind, &conversation, &task.Automatic, &agent, &token, &task.Attempts, &task.CreatedAt); err != nil {
			return err
		}
		task.ConversationID, task.Actor.AgentID, task.Actor.TokenID = deref(conversation), deref(agent), deref(token)
		return nil
	})
	return task, err
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// admit decides whether the task may call a provider now: not in a locked workspace, and an automatic
// task not past the workspace's limits. A task admitted for the first time is marked started, which
// is what the limits count.
func (w *Worker) admit(ctx context.Context, item claimedTask, task Task) (string, error) {
	var code string
	now := w.service.now().UTC()
	err := w.db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		if w.service.config.PlanLimits {
			access, err := store.WorkspaceAccess(ctx, tx, item.workspaceID, now)
			if err != nil {
				return err
			}
			if access.Locked {
				code = CodeWorkspaceLocked
				return nil
			}
		}
		var started bool
		if err := tx.QueryRow(ctx, `SELECT started_at IS NOT NULL FROM ai_tasks WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`,
			item.workspaceID, item.id, item.token).Scan(&started); err != nil {
			return err
		}
		if started {
			// A try again of an automatic draft still asks whether it is wanted; its limits counted at its start.
			if task.Automatic && task.Kind == SlotDraft {
				var err error
				code, err = w.service.automaticDraftWanted(ctx, tx, item.workspaceID, task)
				return err
			}
			return nil
		}
		if task.Automatic {
			// One automatic admission at a time per workspace, so two workers cannot both take the last place.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-automatic:"+item.workspaceID); err != nil {
				return err
			}
			limits, lastMinute, err := w.service.automaticLimits(ctx, tx, item.workspaceID)
			if err != nil {
				return err
			}
			switch {
			case limits.UsedToday >= limits.PerDay:
				code = CodeAutomaticLimit
				return nil
			case lastMinute >= limits.PerMinute:
				code = codeMinuteFull
				return nil
			}
			if task.Kind == SlotDraft {
				if code, err = w.service.admitAutomaticDraft(ctx, tx, item.workspaceID, task, limits, now); err != nil || code != "" {
					return err
				}
			}
			if task.Kind == SlotIntake && limits.IntakeToday >= limits.IntakePerDay {
				code = CodeIntakeLimit
				return nil
			}
		}
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET started_at=$4 WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`, item.workspaceID, item.id, item.token, now)
		return err
	})
	return code, err
}

// admitAutomaticDraft decides whether an automatic draft is still worth its call, and may be paid for:
// the automatic mode still on, nobody replied since, the conversation open, under the day's and the
// conversation's automatic drafts, and (on the server's provider) under half of the allowance.
// automaticDraftWanted is the part of admitAutomaticDraft a try again asks too: the mode still on,
// the conversation open, nobody replied since.
func (s *Service) automaticDraftWanted(ctx context.Context, tx pgx.Tx, workspaceID string, task Task) (string, error) {
	var on bool
	var status string
	var answered bool
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT ai_level($1,c.channel_id,c.channel_scope,'draft')='suggest' FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2),false),
			(SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2),
			COALESCE((SELECT kind<>'incoming' FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind IN ('incoming','outgoing')
				ORDER BY created_at DESC,id DESC LIMIT 1),false)`, workspaceID, task.ConversationID).Scan(&on, &status, &answered)
	switch {
	case err != nil:
		return "", err
	case !on:
		return CodeAutomationOff, nil
	case status == "resolved" || status == "closed":
		return CodeConversationShut, nil
	case answered:
		return CodeAnswered, nil
	}
	return "", nil
}

func (s *Service) admitAutomaticDraft(ctx context.Context, tx pgx.Tx, workspaceID string, task Task, limits AutomaticLimits, now time.Time) (string, error) {
	if code, err := s.automaticDraftWanted(ctx, tx, workspaceID, task); code != "" || err != nil {
		return code, err
	}
	var server bool
	var recent int
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT uses_server FROM ai_slots WHERE workspace_id=$1 AND slot='draft'),false),
			(SELECT count(*) FROM ai_tasks WHERE workspace_id=$1 AND conversation_id=$2 AND kind='draft' AND automatic AND started_at>$3)`,
		workspaceID, task.ConversationID, now.Add(-24*time.Hour)).Scan(&server, &recent)
	switch {
	case err != nil:
		return "", err
	case limits.DraftsToday >= limits.DraftPerDay:
		return CodeDraftLimit, nil
	case recent >= automaticDraftsPerConversation:
		return CodeConversationCap, nil
	}
	if server {
		reserved, err := s.automaticDraftsReserved(ctx, tx, workspaceID)
		if err != nil || reserved {
			return CodeAllowanceReserved, err
		}
	}
	return "", nil
}

// retry queues the task again after the provider's retry-after, or a backoff that doubles each attempt.
func (w *Worker) retry(ctx context.Context, item claimedTask, task Task, providerErr *aiprovider.Error) {
	wait := time.Duration(1<<min(task.Attempts, 6)) * 5 * time.Second
	if providerErr.RetryAfter > wait {
		wait = providerErr.RetryAfter
	}
	ctx = context.WithoutCancel(ctx)
	err := w.db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		if done, err := w.endIfReplaced(ctx, tx, item); done || err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET state='queued',claim_token=NULL,lease_until=NULL,error_code=$4,next_run_at=$5
			WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`, item.workspaceID, item.id, item.token, string(providerErr.Code), w.service.now().UTC().Add(wait))
		return err
	})
	if err != nil {
		slog.Error("AI worker could not queue a task again", "task", item.id, "error", err)
	}
}

// endIfReplaced ends an automatic draft that would go back to the queue while a newer one for the same
// conversation already waits there: the newer one answers the newest message (one waits at a time).
func (w *Worker) endIfReplaced(ctx context.Context, tx pgx.Tx, item claimedTask) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE ai_tasks t SET state='cancelled',error_code=$4,claim_token=NULL,lease_until=NULL,finished_at=$5
		WHERE t.workspace_id=$1 AND t.id=$2 AND t.claim_token=$3 AND t.kind='draft' AND t.automatic
			AND EXISTS (SELECT 1 FROM ai_tasks o WHERE o.workspace_id=t.workspace_id AND o.conversation_id=t.conversation_id AND o.kind='draft'
				AND o.automatic AND o.state='queued' AND o.id<>t.id)`, item.workspaceID, item.id, item.token, CodeSuperseded, w.service.now().UTC())
	return err == nil && tag.RowsAffected() == 1, err
}

func (w *Worker) finish(ctx context.Context, item claimedTask, state, code string) {
	ctx = context.WithoutCancel(ctx)
	err := w.db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET state=$4,error_code=$5,claim_token=NULL,lease_until=NULL,finished_at=$6
			WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`, item.workspaceID, item.id, item.token, state, nullable(code), w.service.now().UTC())
		return err
	})
	if err != nil {
		slog.Error("AI worker could not record a task", "task", item.id, "error", err)
	}
}
