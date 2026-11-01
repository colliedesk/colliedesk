package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/jackc/pgx/v5"
)

// Task states and the reasons a task did not run.
const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"

	CodeFeatureOff        = "feature_off"
	CodeAutomaticLimit    = "automatic_limit"
	CodeWorkspaceLocked   = "workspace_locked"
	CodeAllowanceReached  = "ai_allowance_reached"
	CodeNoProgress        = "no_progress"
	CodeNoHandler         = "no_handler"
	CodeInternal          = "internal"
	CodeCancelledByPerson = "cancelled"
	// An automatic draft is not written when it would not be used or may not be: someone replied
	// (answered), the conversation closed, the automatic mode went off, the day's or the
	// conversation's automatic drafts are used, or they spent their half of the allowance.
	CodeAnswered         = "answered"
	CodeConversationShut = "conversation_closed"
	CodeAutomationOff    = "automation_off"
	CodeDraftLimit       = "draft_limit"
	// CodeIntakeLimit: the day's intake decisions (intake_per_day) are used.
	CodeIntakeLimit       = "intake_limit"
	CodeConversationCap   = "conversation_cap"
	CodeAllowanceReserved = "allowance_reserved"
	// A newer automatic draft for the conversation waits in the queue; this one is not tried again.
	CodeSuperseded = "superseded"

	// codeMinuteFull is never stored: such a task waits for the next minute.
	codeMinuteFull = "minute_full"
)

// ErrClaimLost means the task was cancelled or leased again while it ran: its result is not recorded.
var ErrClaimLost = errors.New("the AI task was cancelled or taken over while it ran")

// Default automatic limits: what a flood of new tickets may start in a workspace.
const (
	DefaultAutomaticPerDay    = 300
	DefaultAutomaticPerMinute = 30
	// DefaultAutomaticDraftsPerDay bounds the drafts written by themselves in a UTC day; a draft costs
	// about ten triage suggestions.
	DefaultAutomaticDraftsPerDay = 100
	// DefaultAutomaticIntakePerDay bounds intake's decisions of a UTC day, so a busy group cannot
	// spend the day's triage and drafts.
	DefaultAutomaticIntakePerDay = 300
	// automaticDraftsPerConversation bounds the automatic drafts of one conversation in 24 hours, so a
	// back-and-forth the mail loop guard lets through cannot spend all day.
	automaticDraftsPerConversation = 5
)

// Task is one queued piece of AI work.
type Task struct {
	ID             string
	WorkspaceID    string
	Kind           string
	ConversationID string
	// GatheringID is the intake card an intake task decides.
	GatheringID string
	Automatic   bool
	Actor       Actor
	Attempts    int
	CreatedAt   time.Time
}

// NewTask is what Enqueue needs.
type NewTask struct {
	Kind           string
	ConversationID string
	Automatic      bool
	Actor          Actor
}

// Enqueue adds a task in tx, the caller's workspace-scoped transaction, so the task and what it
// belongs to (a draft) are written together. It never calls a provider.
func (s *Service) Enqueue(ctx context.Context, tx pgx.Tx, workspaceID string, task NewTask) (string, error) {
	if !slices.Contains(Slots, task.Kind) {
		return "", fmt.Errorf("unknown AI task kind %q", task.Kind)
	}
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO ai_tasks(workspace_id,kind,conversation_id,automatic,requested_by_agent_id,requested_by_token_id,state,next_run_at,created_at)
		VALUES($1,$2,$3,$4,$5,$6,'queued',$7,$7) RETURNING id::text`, workspaceID, task.Kind, nullable(task.ConversationID), task.Automatic,
		nullable(task.Actor.AgentID), nullable(task.Actor.TokenID), s.now().UTC()).Scan(&id)
	if err == nil {
		s.wake()
	}
	return id, err
}

// CancelTask stops a task that has not finished; a worker running it can no longer record a result.
// A call already sent still leaves its usage row.
func (s *Service) CancelTask(ctx context.Context, tx pgx.Tx, workspaceID, id string) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE ai_tasks SET state='cancelled',error_code=$3,claim_token=NULL,lease_until=NULL,finished_at=$4
		WHERE workspace_id=$1 AND id=$2 AND state IN ('queued','running')`, workspaceID, id, CodeCancelledByPerson, s.now().UTC())
	return tag.RowsAffected() == 1, err
}

func (s *Service) wake() {
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}

// Binding is a feature resolved to the provider that runs it now.
type Binding struct {
	Provider aiprovider.Provider
	Model    string
	// FallbackModel is intake's, empty when it has none: the same provider's model a request goes to
	// when Model fails or answers what cannot be read.
	FallbackModel string
	Effort        aiprovider.Effort
	call          Call
}

// ErrFeatureOff means the feature is off, or on but without its provider or model.
var ErrFeatureOff = errors.New("this AI feature is off")

// Resolve finds the provider, model and effort a feature uses now, with the key opened.
func (s *Service) Resolve(ctx context.Context, workspaceID, slot string) (Binding, error) {
	var binding Binding
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		slots, err := readSlots(ctx, tx, workspaceID, false)
		if err != nil {
			return err
		}
		row, found := slots[slot]
		if !found || !s.view(slot, row, true).Active || row.model == nil {
			return ErrFeatureOff
		}
		binding.Model, binding.Effort = *row.model, aiprovider.Effort(row.effort)
		if row.fallbackModel != nil {
			binding.FallbackModel = *row.fallbackModel
		}
		if row.usesServer {
			server := s.config.Server
			if !slices.Contains(server.ModelsFor(slot), binding.FallbackModel) {
				binding.FallbackModel = ""
			}
			binding.call = Call{WorkspaceID: workspaceID, BilledTo: BilledToServer, Kind: server.Kind, Label: server.Label}
			binding.Provider, err = s.build(Spec{Kind: server.Kind, BaseURL: server.BaseURL, APIKey: server.APIKey, Server: true})
			return err
		}
		stored, err := loadProvider(ctx, tx, workspaceID, *row.providerID, false)
		if err != nil {
			return err
		}
		spec, err := s.spec(workspaceID, stored)
		if err != nil {
			return err
		}
		binding.call = Call{WorkspaceID: workspaceID, BilledTo: BilledToWorkspaceKey, ProviderID: stored.ID, Kind: spec.Kind, Label: stored.Label}
		binding.Provider, err = s.build(spec)
		return err
	})
	return binding, err
}

// Run is what a handler gets for one attempt of a task: the task, its feature's binding, and the one
// way to call the provider for it.
type Run struct {
	Task    Task
	Binding Binding
	service *Service
	claim   claimedTask
}

// Record writes the task's result and marks it done in one transaction of its workspace, only while
// this attempt still holds the task: a person who cancelled meanwhile never sees a late result. It
// returns ErrClaimLost when the task is no longer this attempt's.
func (r *Run) Record(ctx context.Context, write func(pgx.Tx) error) error {
	ctx = context.WithoutCancel(ctx)
	return r.service.db.InWorkspace(ctx, r.claim.workspaceID, func(tx pgx.Tx) error {
		var held bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_tasks WHERE workspace_id=$1 AND id=$2 AND claim_token=$3 FOR UPDATE)`,
			r.claim.workspaceID, r.claim.id, r.claim.token).Scan(&held); err != nil {
			return err
		}
		if !held {
			return ErrClaimLost
		}
		if err := write(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET state='done',error_code=NULL,claim_token=NULL,lease_until=NULL,finished_at=$3 WHERE workspace_id=$1 AND id=$2`,
			r.claim.workspaceID, r.claim.id, r.service.now().UTC())
		return err
	})
}

// Metered sends a request for this task through the metered layer, with the feature's model and
// effort unless the request names them. A request on the feature's model goes once more to its
// fallback model, when it has one, after an error that another model may not have (a timeout, a
// refused rate, the provider busy or down, the model gone) or an answer that cannot be read.
func (r *Run) Metered(ctx context.Context, request aiprovider.Request) (aiprovider.Response, error) {
	if request.Model == "" {
		request.Model = r.Binding.Model
	}
	if request.Effort == "" {
		request.Effort = r.Binding.Effort
	}
	call := r.Binding.call
	call.Feature, call.TaskID, call.Automatic, call.Actor = r.Task.Kind, r.Task.ID, r.Task.Automatic, r.Task.Actor
	fallback := r.Binding.FallbackModel
	if fallback == "" || request.Model != r.Binding.Model || fallback == request.Model {
		return r.service.Metered(ctx, call, r.Binding.Provider, request)
	}
	// The feature's model gets part of the attempt, so its fallback has time left to answer.
	first, cancel := context.WithTimeout(ctx, fallbackAfter)
	response, err := r.service.Metered(first, call, r.Binding.Provider, request)
	cancel()
	if !worthFallback(request, response, err) || ctx.Err() != nil {
		return response, err
	}
	request.Model = fallback
	return r.service.Metered(ctx, call, r.Binding.Provider, request)
}

// Ask sends one request of a feature on binding outside the task queue, metered like any call (an
// operator's evaluation): asked by a person, never automatic.
func (s *Service) Ask(ctx context.Context, binding Binding, feature string, request aiprovider.Request) (aiprovider.Response, error) {
	if request.Model == "" {
		request.Model = binding.Model
	}
	if request.Effort == "" {
		request.Effort = binding.Effort
	}
	call := binding.call
	call.Feature = feature
	return s.Metered(ctx, call, binding.Provider, request)
}

// IntakeOptionsOf is a chat scope's intake options as the decider reads them (the scope's, else the
// channel's), with the defaults filled in.
func (s *Service) IntakeOptionsOf(ctx context.Context, workspaceID, channelID, scope string) (IntakeOptions, error) {
	var options IntakeOptions
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT options FROM channel_ai_levels WHERE workspace_id=$1 AND channel_id=$2::uuid AND slot='intake' AND scope IN ($3,'')
			ORDER BY scope=$3 DESC LIMIT 1`, workspaceID, channelID, scope).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &options)
	})
	return options.Effective(), err
}

// fallbackAfter is how long a request waits for the feature's model before its fallback model.
const fallbackAfter = 35 * time.Second

func worthFallback(request aiprovider.Request, response aiprovider.Response, err error) bool {
	var providerErr *aiprovider.Error
	switch {
	case err == nil:
		return request.Schema != nil && response.JSON == ""
	case errors.As(err, &providerErr):
		return providerErr.Retryable() || providerErr.Code == aiprovider.ErrorModelNotFound || providerErr.Code == aiprovider.ErrorInvalidResponse
	}
	return false
}

// Handler runs one kind of task. It returns nil when the task is done; a retryable provider error
// queues it again, any other error fails it.
type Handler interface {
	Run(ctx context.Context, run *Run) error
}

// HandlerFunc lets a function be a Handler.
type HandlerFunc func(ctx context.Context, run *Run) error

func (f HandlerFunc) Run(ctx context.Context, run *Run) error { return f(ctx, run) }

// AutomaticLimits are a workspace's automatic limits and, when today's is used up, until when
// automatic tasks wait.
type AutomaticLimits struct {
	PerDay      int        `json:"per_day"`
	PerMinute   int        `json:"per_minute"`
	UsedToday   int        `json:"used_today"`
	PausedUntil *time.Time `json:"paused_until"`
	// DraftPerDay bounds the automatic drafts of a UTC day, inside PerDay; DraftsToday counts them.
	// Automatic drafts pause until the next day past DraftPerDay (DraftsPausedUntil), and until the
	// next month once they spent half of the server's allowance (DraftsReservedUntil): the other half
	// stays for drafts people ask for and for triage.
	DraftPerDay         int        `json:"draft_per_day"`
	DraftsToday         int        `json:"drafts_today"`
	DraftsPausedUntil   *time.Time `json:"drafts_paused_until"`
	DraftsReservedUntil *time.Time `json:"drafts_reserved_until"`
	// IntakePerDay bounds intake's decisions of a UTC day, inside PerDay; IntakeToday counts them, and
	// past it intake waits until the next day (IntakePausedUntil).
	IntakePerDay      int        `json:"intake_per_day"`
	IntakeToday       int        `json:"intake_today"`
	IntakePausedUntil *time.Time `json:"intake_paused_until"`
}

func dayStart(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}

// automaticLimits reads the limits and counts the automatic tasks that started today and in the last
// minute. Tasks count, not calls: retrying a task does not use more of the limit.
func (s *Service) automaticLimits(ctx context.Context, tx pgx.Tx, workspaceID string) (AutomaticLimits, int, error) {
	limits := AutomaticLimits{PerDay: DefaultAutomaticPerDay, PerMinute: DefaultAutomaticPerMinute, DraftPerDay: DefaultAutomaticDraftsPerDay,
		IntakePerDay: DefaultAutomaticIntakePerDay}
	err := tx.QueryRow(ctx, `SELECT per_day,per_minute,draft_per_day,intake_per_day FROM ai_automatic_limits WHERE workspace_id=$1`, workspaceID).
		Scan(&limits.PerDay, &limits.PerMinute, &limits.DraftPerDay, &limits.IntakePerDay)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AutomaticLimits{}, 0, err
	}
	now := s.now().UTC()
	var minute int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE started_at>=$2), count(*) FILTER (WHERE started_at>$3), count(*) FILTER (WHERE started_at>=$2 AND kind='draft'),
			count(*) FILTER (WHERE started_at>=$2 AND kind='intake')
		FROM ai_tasks WHERE workspace_id=$1 AND automatic AND started_at>=LEAST($2,$3)`, workspaceID, dayStart(now), now.Add(-time.Minute)).
		Scan(&limits.UsedToday, &minute, &limits.DraftsToday, &limits.IntakeToday); err != nil {
		return AutomaticLimits{}, 0, err
	}
	next := dayStart(now).Add(24 * time.Hour)
	if limits.UsedToday >= limits.PerDay {
		limits.PausedUntil = &next
	}
	if limits.DraftsToday >= limits.DraftPerDay || limits.UsedToday >= limits.PerDay {
		limits.DraftsPausedUntil = &next
	}
	if limits.IntakeToday >= limits.IntakePerDay || limits.UsedToday >= limits.PerDay {
		limits.IntakePausedUntil = &next
	}
	reserved, err := s.automaticDraftsReserved(ctx, tx, workspaceID)
	if err != nil {
		return AutomaticLimits{}, 0, err
	}
	if reserved {
		month := monthStart(now).AddDate(0, 1, 0)
		limits.DraftsReservedUntil = &month
	}
	return limits, minute, nil
}

// automaticDraftsReserved says whether automatic drafts spent half of this month's allowance of the
// server's provider (what their calls cost, or hold while running).
func (s *Service) automaticDraftsReserved(ctx context.Context, tx pgx.Tx, workspaceID string) (bool, error) {
	_, limit, limited, err := s.allowanceLeft(ctx, tx, workspaceID)
	if err != nil || !limited {
		return false, err
	}
	var spent int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN status='started' THEN COALESCE(reserved_micro_usd,0) ELSE COALESCE(cost_micro_usd,0) END),0)
		FROM ai_usage WHERE workspace_id=$1 AND billed_to='server' AND feature='draft' AND automatic AND started_at>=$2`, workspaceID, monthStart(s.now())).Scan(&spent); err != nil {
		return false, err
	}
	return spent*2 >= limit, nil
}

// SetAutomaticLimits changes how many automatic tasks the workspace runs a day and a minute, and how
// many of them a day are drafts and intake decisions (nil: kept).
func (s *Service) SetAutomaticLimits(ctx context.Context, workspaceID string, actor Actor, perDay, perMinute int, draftPerDay, intakePerDay *int) (AutomaticLimits, error) {
	if perDay < 0 || perDay > 10000 || perMinute < 0 || perMinute > 1000 || (draftPerDay != nil && (*draftPerDay < 0 || *draftPerDay > 10000)) ||
		(intakePerDay != nil && (*intakePerDay < 0 || *intakePerDay > 10000)) {
		return AutomaticLimits{}, invalid("per_day is 0 to 10000, per_minute 0 to 1000, draft_per_day and intake_per_day 0 to 10000.")
	}
	now := s.now().UTC()
	var limits AutomaticLimits
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		before, _, err := s.automaticLimits(ctx, tx, workspaceID)
		if err != nil {
			return err
		}
		drafts, intake := before.DraftPerDay, before.IntakePerDay
		if draftPerDay != nil {
			drafts = *draftPerDay
		}
		if intakePerDay != nil {
			intake = *intakePerDay
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ai_automatic_limits(workspace_id,per_day,per_minute,draft_per_day,intake_per_day,updated_by,updated_at) VALUES($1,$2,$3,$4,$7,$5,$6)
			ON CONFLICT (workspace_id) DO UPDATE SET per_day=EXCLUDED.per_day,per_minute=EXCLUDED.per_minute,draft_per_day=EXCLUDED.draft_per_day,
				intake_per_day=EXCLUDED.intake_per_day,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`,
			workspaceID, perDay, perMinute, drafts, nullable(actor.AgentID), now, intake); err != nil {
			return err
		}
		if limits, _, err = s.automaticLimits(ctx, tx, workspaceID); err != nil {
			return err
		}
		if before.PerDay == perDay && before.PerMinute == perMinute && before.DraftPerDay == drafts && before.IntakePerDay == intake {
			return nil
		}
		return addEvent(ctx, tx, workspaceID, "automatic_limits_changed", actor, "", "", "", map[string]any{
			"from": map[string]any{"per_day": before.PerDay, "per_minute": before.PerMinute, "draft_per_day": before.DraftPerDay, "intake_per_day": before.IntakePerDay},
			"to":   map[string]any{"per_day": perDay, "per_minute": perMinute, "draft_per_day": drafts, "intake_per_day": intake}}, now)
	})
	return limits, err
}
