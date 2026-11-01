// Package intake decides the gatherings of chat channels (docs/internals/channels.md "Intake"): a
// person's decision in the queue, AI's, and the rules' when AI does not decide in time. The store
// writes; this package says what to write. Nothing here names a channel type.
package intake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Actions a decision of a gathering takes.
const (
	Create = rules.Create
	Attach = rules.Attach
	Ignore = rules.Ignore
	Review = rules.Review
)

// Decision is what a person or AI decided of a gathering.
type Decision struct {
	Action string
	// ConversationID is the conversation an Attach adds the batch to.
	ConversationID string
	// ReviewReason says why a Review puts it in the queue.
	ReviewReason string
	// IgnoreReason says why an Ignore sets it aside, when the decider said.
	IgnoreReason                        string
	Command, Subject, ReplyKind, Reason string
	Confidence                          *float64
	CorrectsDecisionID                  string
}

// ConflictError is a decision that came after another: the gathering stands as Outcome.
type ConflictError struct{ Outcome store.GatheringOutcome }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("this gathering was already decided (%s by %s)", e.Outcome.State, e.Outcome.DecidedBy)
}

// ErrInvalid is a decision that cannot be made (an unknown action, an attach without a conversation).
var ErrInvalid = errors.New("invalid intake decision")

// Service decides gatherings.
type Service struct {
	store    *store.Store
	pipeline *pipeline.Pipeline
	now      func() time.Time
	logger   *slog.Logger
}

// New returns a Service.
func New(st *store.Store, p *pipeline.Pipeline, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: st, pipeline: p, now: now, logger: slog.Default()}
}

// Decide applies a person's (decider agent) or AI's decision. The same decision again changes nothing;
// another one once the gathering is decided is a ConflictError.
func (s *Service) Decide(ctx context.Context, workspaceID, gatheringID string, d Decision, decider string, actor store.ChannelActor) (store.GatheringOutcome, error) {
	apply := store.IntakeApply{Decider: decider, Actor: actor, Decision: &store.IntakeDecisionInput{Action: d.Action, TargetConversationID: d.ConversationID,
		Command: d.Command, Subject: d.Subject, ReplyKind: d.ReplyKind, Reason: d.Reason, IgnoreReason: d.IgnoreReason, Confidence: d.Confidence,
		CorrectsDecisionID: d.CorrectsDecisionID}}
	var err error
	// A card of messages a person took back to the queue holds no held events: its messages move.
	var detached *store.DetachedCard
	if d.Action == Create || d.Action == Attach {
		if detached, err = s.store.DetachedCardOf(ctx, workspaceID, gatheringID); err != nil {
			return store.GatheringOutcome{}, err
		}
	}
	switch d.Action {
	case Create:
		if d.ConversationID != "" {
			return store.GatheringOutcome{}, ErrInvalid
		}
		apply.ForceNew, apply.Command, apply.Subject = true, d.Command, d.Subject
		if detached != nil {
			_, err = s.store.DecideDetachedCard(ctx, workspaceID, gatheringID, "", d.Subject, apply, s.now().UTC())
		} else {
			_, err = s.pipeline.ApplyGathering(ctx, workspaceID, gatheringID, "", apply)
		}
	case Attach:
		if d.ConversationID == "" {
			return store.GatheringOutcome{}, ErrInvalid
		}
		if detached != nil {
			_, err = s.store.DecideDetachedCard(ctx, workspaceID, gatheringID, d.ConversationID, "", apply, s.now().UTC())
		} else {
			_, err = s.pipeline.ApplyGathering(ctx, workspaceID, gatheringID, d.ConversationID, apply)
		}
	case Ignore:
		err = s.store.SetGatheringState(ctx, workspaceID, gatheringID, Ignore, "", apply, s.now().UTC())
	case Review:
		if d.ReviewReason == "" {
			return store.GatheringOutcome{}, ErrInvalid
		}
		err = s.store.SetGatheringState(ctx, workspaceID, gatheringID, Review, d.ReviewReason, apply, s.now().UTC())
	default:
		return store.GatheringOutcome{}, ErrInvalid
	}
	if errors.Is(err, store.ErrGatheringDecided) {
		outcome, readErr := s.store.GatheringOutcomeOf(ctx, workspaceID, gatheringID)
		if readErr != nil {
			return outcome, readErr
		}
		if same(outcome, d, decider) {
			return outcome, nil
		}
		return outcome, &ConflictError{Outcome: outcome}
	}
	if err != nil {
		return store.GatheringOutcome{}, err
	}
	return s.store.GatheringOutcomeOf(ctx, workspaceID, gatheringID)
}

// same says a gathering already stands as d would leave it.
func same(o store.GatheringOutcome, d Decision, decider string) bool {
	if o.DecidedBy != decider {
		return false
	}
	switch d.Action {
	case Create:
		return o.State == store.GatheringCreated
	case Attach:
		return o.State == store.GatheringAttached && o.ConversationID == d.ConversationID
	case Ignore:
		return o.State == store.GatheringIgnored
	}
	return false
}

// Requeue takes a gathering that was set aside back to the queue; its decision stays in its history.
func (s *Service) Requeue(ctx context.Context, workspaceID, gatheringID string, actor store.ChannelActor) (store.GatheringOutcome, error) {
	err := s.store.SetGatheringState(ctx, workspaceID, gatheringID, Review, store.ReviewRequeued, store.IntakeApply{Decider: store.DecidedByAgent, Actor: actor}, s.now().UTC())
	if errors.Is(err, store.ErrGatheringDecided) {
		outcome, readErr := s.store.GatheringOutcomeOf(ctx, workspaceID, gatheringID)
		if readErr != nil {
			return outcome, readErr
		}
		if outcome.State == store.GatheringReview {
			return outcome, nil
		}
		return outcome, &ConflictError{Outcome: outcome}
	}
	if err != nil {
		return store.GatheringOutcome{}, err
	}
	return s.store.GatheringOutcomeOf(ctx, workspaceID, gatheringID)
}

// sweepBatch bounds one sweep.
const sweepBatch = 100

// Sweep applies the rules' decision to the gatherings AI did not decide in time (fallback): what they
// would open is replayed through the rules as they stand now, so a batch held while an earlier one of
// its sender waited too continues the conversation that one opened, as without AI; what they would hold
// for a person goes to the queue, and the chatter of a scope that opens only on a command goes to the
// queue too (an admin turned AI on so it is looked at). A gathering a person decided meanwhile is left as
// it is. It returns how many it applied.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	due, err := s.store.DueGatherings(ctx, s.now().UTC(), sweepBatch)
	if err != nil {
		return 0, err
	}
	applied := 0
	var errs []error
	for _, g := range due {
		apply := store.IntakeApply{Decider: store.DecidedByRules, Fallback: true}
		var err error
		switch g.RulesAction {
		case rules.Create:
			_, err = s.pipeline.ApplyGathering(ctx, g.WorkspaceID, g.ID, "", apply)
		case rules.Review:
			err = s.store.SetGatheringState(ctx, g.WorkspaceID, g.ID, Review, store.ReviewNoCommand, apply, s.now().UTC())
		default:
			err = s.store.SetGatheringState(ctx, g.WorkspaceID, g.ID, Review, store.ReviewAIUnavailable, apply, s.now().UTC())
		}
		switch {
		case errors.Is(err, store.ErrGatheringDecided):
		case err != nil:
			errs = append(errs, fmt.Errorf("gathering %s: %w", g.ID, err))
		default:
			applied++
		}
	}
	return applied, errors.Join(errs...)
}

// Run sweeps every interval until the context ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("intake sweep", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
