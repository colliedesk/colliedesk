package aidraft

import (
	"context"
	"errors"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// defaultLease holds a claimed draft away from other workers while its calls run: longer than the
	// chooser's and the draft's timeouts together, so a live call is never claimed twice.
	defaultLease = 4 * time.Minute
	// defaultTimeout bounds one call; drafts take 3 to 10 seconds.
	defaultTimeout = 90 * time.Second
	// retryBase is the wait before a retryable failure is tried again, times the attempt.
	retryBase = 30 * time.Second
	// Error codes of a draft that are not the provider's.
	errorInvalidAnswer = "invalid_answer"
	errorRefused       = "refused"
)

// Worker writes the queued drafts of every workspace and sends the answers auto-answer scheduled.
type Worker struct {
	Store    *store.Store
	Provider aiprovider.Provider
	Settings Settings
	// Lease and Timeout default to defaultLease and defaultTimeout.
	Lease   time.Duration
	Timeout time.Duration
	// LogError hears failures that are not a draft's own (the database); nil drops them.
	LogError func(error)
	// MaxPerRun is how many drafts one workspace gets in a RunOnce before the next workspace's turn;
	// 0 writes every draft that is due.
	MaxPerRun int
}

func (w *Worker) logError(err error) {
	if w.LogError != nil && err != nil && !errors.Is(err, context.Canceled) {
		w.LogError(err)
	}
}

// Run works every interval until ctx ends.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce sends the scheduled answers that are due and writes every draft that is due, workspace by
// workspace, and returns how many drafts it wrote.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	workspaces, err := w.Store.AIDraftWorkspacesDue(ctx)
	if err != nil {
		return 0, err
	}
	written := 0
	var failed []error
	for _, workspaceID := range workspaces {
		if _, err := w.Store.SendDueAutoAnswers(ctx, workspaceID); err != nil {
			failed = append(failed, err)
		}
		for turn := 0; ctx.Err() == nil && (w.MaxPerRun == 0 || turn < w.MaxPerRun); turn++ {
			job, err := w.Store.ClaimAIDraft(ctx, workspaceID, w.lease())
			if err != nil {
				failed = append(failed, err)
				break
			}
			if job == nil {
				break
			}
			if err := w.write(ctx, job); err != nil {
				failed = append(failed, err)
				break
			}
			written++
		}
	}
	return written, errors.Join(failed...)
}

func (w *Worker) timeout() time.Duration {
	if w.Timeout > 0 {
		return w.Timeout
	}
	return defaultTimeout
}

func (w *Worker) lease() time.Duration {
	if w.Lease > 0 {
		return w.Lease
	}
	return defaultLease
}

// JobInput turns a claimed job into what the prompt is written from.
func JobInput(job *store.AIDraftJob) Input {
	in := Input{WorkspaceName: job.WorkspaceName, Subject: job.Subject, Sources: job.Sources, Tags: job.Tags}
	for _, turn := range job.Turns {
		from := FromTeam
		if turn.FromCustomer {
			from = FromCustomer
		}
		in.Turns = append(in.Turns, Turn{From: from, Name: turn.Name, Text: turn.Body})
	}
	return in
}

// Draft makes one call for the input and checks the answer. The response is returned with any error
// that came after the call, so the caller meters it.
func Draft(ctx context.Context, provider aiprovider.Provider, in Input, settings Settings) (Result, aiprovider.Response, error) {
	response, err := provider.Complete(ctx, BuildRequest(in, settings))
	if err != nil {
		return Result{}, response, err
	}
	if response.Stop == aiprovider.StopRefused {
		return Result{}, response, errDraftRefused
	}
	text := response.JSON
	if text == "" {
		text = response.Text
	}
	result, err := Parse(text, in.Sources, in.Tags)
	if err != nil {
		return Result{}, response, errDraftInvalid
	}
	return result, response, nil
}

var (
	errDraftRefused = errors.New("aidraft: the model declined")
	errDraftInvalid = errors.New("aidraft: the answer could not be read")
)

// write drafts one job: every attempt is metered, a failure is recorded on the draft.
func (w *Worker) write(ctx context.Context, job *store.AIDraftJob) error {
	in := JobInput(job)
	if err := w.choose(ctx, job, &in); err != nil {
		return errors.Join(err, w.Store.FailAIDraft(ctx, job.WorkspaceID, job.DraftID, job.Attempt, "store_error", true, retryBase))
	}
	call, cancel := context.WithTimeout(ctx, w.timeout())
	result, response, err := Draft(call, w.Provider, in, w.Settings)
	cancel()
	code := ""
	retryable, retryAfter := false, retryBase*time.Duration(job.Attempt)
	var providerErr *aiprovider.Error
	switch {
	case errors.As(err, &providerErr):
		code, retryable = string(providerErr.Code), providerErr.Retryable()
		if providerErr.RetryAfter > retryAfter {
			retryAfter = providerErr.RetryAfter
		}
	case errors.Is(err, errDraftRefused):
		code = errorRefused
	case errors.Is(err, errDraftInvalid):
		code, retryable = errorInvalidAnswer, true
	case err != nil:
		code, retryable = string(aiprovider.CodeOf(err)), true
	}
	if meterErr := w.meter(ctx, job, "draft", w.Settings.Model, response, code); meterErr != nil {
		return errors.Join(meterErr, w.Store.FailAIDraft(ctx, job.WorkspaceID, job.DraftID, job.Attempt, "store_error", true, retryBase))
	}
	if err != nil {
		return w.Store.FailAIDraft(ctx, job.WorkspaceID, job.DraftID, job.Attempt, code, retryable, retryAfter)
	}
	_, err = w.Store.FinishAIDraft(ctx, job.WorkspaceID, job.DraftID, job.Attempt, toDomain(result, response.Model))
	if errors.Is(err, domain.ErrAIDraftNotFound) {
		// The conversation was deleted, or another worker claimed the draft again: nothing to write.
		return nil
	}
	if err != nil {
		// Tried again until the draft has used its attempts, never forever.
		return errors.Join(err, w.Store.FailAIDraft(ctx, job.WorkspaceID, job.DraftID, job.Attempt, "store_error", true, retryBase))
	}
	return nil
}

// meter stores one row per model that ran in the attempt (a fallback bills two), or one row with the
// requested model and the error when nothing answered.
func (w *Worker) meter(ctx context.Context, job *store.AIDraftJob, feature, model string, response aiprovider.Response, code string) error {
	if len(response.Hops) == 0 {
		return w.Store.RecordAIUsage(ctx, job.WorkspaceID, store.AIUsage{DraftID: job.DraftID, Feature: feature, Model: model, ErrorCode: code})
	}
	for _, hop := range response.Hops {
		usage := store.AIUsage{DraftID: job.DraftID, Feature: feature, Model: hop.Model, Reported: hop.Usage.Reported, InputTokens: hop.Usage.InputTokens,
			OutputTokens: hop.Usage.OutputTokens, CacheReadTokens: hop.Usage.CacheReadTokens, CacheWriteTokens: hop.Usage.CacheWriteTokens, ErrorCode: code}
		if err := w.Store.RecordAIUsage(ctx, job.WorkspaceID, usage); err != nil {
			return err
		}
	}
	return nil
}

func toDomain(result Result, model string) domain.AIDraftResult {
	out := domain.AIDraftResult{Action: result.Action, Body: result.Body, Confidence: result.Confidence, ConfidenceReasons: result.Reasons,
		HandoffReason: result.HandoffReason, TopicArticleID: result.TopicArticleID, Model: model}
	for _, source := range result.Sources {
		out.Sources = append(out.Sources, domain.AIDraftSource(source))
	}
	if result.Triage != nil {
		out.Triage = &domain.AITriage{Tags: result.Triage.Tags, Priority: result.Triage.Priority, Reason: result.Triage.Reason}
	}
	return out
}
