package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Gate says whether AI may act on its own on a channel's scope, for an action. A gate the scope's
// readiness does not open keeps the action a suggestion: at auto, AI suggests and the rules apply.
type Gate interface {
	Open(ctx context.Context, q GateQuery) bool
}

// GateQuery is the action AI would take on a card of a scope, with the model it ran and the actions an
// admin opened by hand.
type GateQuery struct {
	WorkspaceID, ChannelID, Scope, Action, Model string
	Overrides                                    []string
}

type closedGate struct{}

func (closedGate) Open(context.Context, GateQuery) bool { return false }

type openGate struct{}

func (openGate) Open(context.Context, GateQuery) bool { return true }

// OpenGate opens every gate: for tests and evaluation, never a default.
var OpenGate Gate = openGate{}

// readinessGate opens an action's gate when the scope's readiness for the model and the prompt's
// Version does (people's explicit labels and offline evaluations), or an admin opened it by hand.
type readinessGate struct {
	store *store.Store
	now   func() time.Time
}

func (g readinessGate) Open(ctx context.Context, q GateQuery) bool {
	readiness, err := g.store.IntakeReadinessOf(ctx, q.WorkspaceID, q.ChannelID, q.Scope, q.Model, prompt.Version, q.Overrides, g.now().UTC())
	return err == nil && readiness.Open(q.Action)
}

// WithGate returns the service deciding with gate; nil opens a gate only when the scope's readiness does.
func (s *Service) WithGate(gate Gate) *Service {
	copied := *s
	copied.gate = gate
	if gate == nil {
		copied.gate = readinessGate{store: s.store, now: s.now}
	}
	return &copied
}

// AIHandler decides intake cards: register it on the AI worker for the intake kind.
func (s *Service) AIHandler() ai.Handler { return ai.HandlerFunc(s.runAI) }

// Errors of an answer that is not a decision, as intake_decisions.error_code names them.
const (
	codeTargetInvalid = "target_invalid"
	codeInvalidAnswer = "invalid_answer"
	codeNoSubject     = "no_subject"
)

// runAI reads a card, asks the model and applies its decision when everything allows (docs/internals/
// channels.md "AI decides"); otherwise it records the suggestion and, for a card that waits at auto, has
// the rules decide it now.
func (s *Service) runAI(ctx context.Context, run *ai.Run) error {
	workspaceID, gatheringID := run.Task.WorkspaceID, run.Task.GatheringID
	snap, err := s.store.IntakeSnapshotOf(ctx, workspaceID, gatheringID, s.now().UTC())
	if errors.Is(err, store.ErrGatheringNotFound) {
		return run.Record(ctx, func(pgx.Tx) error { return nil })
	}
	if err != nil {
		return err
	}
	g := snap.Gathering
	if g.State != store.GatheringDeciding && g.State != store.GatheringReview && g.State != store.GatheringApplied {
		return run.Record(ctx, func(pgx.Tx) error { return nil })
	}
	// Turned off since the card was queued: nothing reaches the model, and the card is the rules' now.
	if snap.Level != levelAuto && snap.Level != levelSuggest {
		return &ai.TaskError{Code: ai.CodeAutomationOff, Cancelled: true}
	}
	var options ai.IntakeOptions
	if err := json.Unmarshal(snap.Options, &options); err != nil {
		return fmt.Errorf("read intake options: %w", err)
	}
	options = options.Effective()
	images, skipped, err := s.cardImages(ctx, run, snap, options)
	if err != nil {
		return err
	}
	built := prompt.Build(snapshotFor(snap, options, images, skipped))
	request := aiprovider.Request{System: built.System, Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: built.User, Images: built.Images}},
		MaxOutput: prompt.MaxOutput, Schema: prompt.Schema}

	if s.breaker.open(workspaceID, s.now()) {
		return &ai.TaskError{Code: CodePaused}
	}
	started := s.now()
	var answer prompt.Answer
	var response aiprovider.Response
	code := ""
	for attempt := 0; attempt < 2; attempt++ {
		if response, err = run.Metered(ctx, request); err != nil {
			// Only the provider's own failures count: not a held allowance, not a stopping process.
			var providerErr *aiprovider.Error
			if errors.As(err, &providerErr) && ctx.Err() == nil {
				s.breaker.fail(workspaceID, s.now())
			}
			return err
		}
		answer, err = built.Parse(response.JSON)
		if !errors.Is(err, prompt.ErrUnknownLabel) {
			break
		}
	}
	if err != nil {
		s.breaker.fail(workspaceID, s.now())
	} else {
		s.breaker.succeed(workspaceID)
	}
	switch {
	case errors.Is(err, prompt.ErrUnknownLabel):
		code = codeTargetInvalid
	case err != nil:
		code = codeInvalidAnswer
	case answer.Action == prompt.ActionCreate && strings.TrimSpace(answer.Subject) == "":
		code = codeNoSubject
	}
	latency := int(s.now().Sub(started).Milliseconds())
	manifest, _ := json.Marshal(map[string]any{"sources": built.Manifest, "images_sent": len(built.Images), "images_withheld": built.ImagesWithheld})
	// The model asked for, as readiness matches it with evaluations (a provider may answer under another id).
	detail := store.AIDecisionDetail{RunnerUpConversationID: answer.RunnerUpID, ErrorCode: code, Manifest: manifest, TaskID: run.Task.ID,
		Model: run.Binding.Model, PromptVersion: prompt.Version, LatencyMS: &latency, ItemsSeen: snap.EventIDs}
	if detail.ItemsSeen == nil {
		detail.ItemsSeen = []string{}
	}
	if answer.RunnerUpID != "" {
		detail.RunnerUpConfidence = &answer.RunnerUpConfidence
	}
	confidence := answer.Confidence
	decision := Decision{Action: answer.Action, ConversationID: answer.ConversationID, Command: answer.Command, Subject: answer.Subject,
		ReplyKind: answer.ReplyKind, Reason: answer.Reason, IgnoreReason: answer.IgnoreReason, Confidence: &confidence}
	if code != "" {
		// Nothing it could act on: the row says what failed; the card gets the rules' decision.
		decision = Decision{Action: rules.Review, Reason: ""}
	}

	reason := s.notApplied(ctx, workspaceID, run.Binding.Model, snap, options, built, decision, code)
	if reason == "" {
		applied, err := s.applyAI(ctx, workspaceID, gatheringID, decision, detail)
		switch {
		case errors.Is(err, store.ErrNotACandidate):
			// The conversation it named stopped being one the card may join (closed, merged, gone).
			reason = "target_changed"
		case err != nil:
			return err
		case applied:
			return run.Record(ctx, func(pgx.Tx) error { return nil })
		default:
			// Decided first: by the rules past its time (a person's says so when it is recorded).
			reason = "too_old"
		}
	}
	detail.NotAppliedReason = reason
	waiting := false
	err = run.Record(ctx, func(tx pgx.Tx) error {
		input := store.IntakeDecisionInput{Action: decision.Action, TargetConversationID: decision.ConversationID, Command: decision.Command,
			Subject: decision.Subject, ReplyKind: decision.ReplyKind, Reason: decision.Reason, IgnoreReason: decision.IgnoreReason, Confidence: decision.Confidence,
			AIDecisionDetail: detail}
		if code != "" {
			input.Confidence = nil
		}
		var err error
		waiting, err = store.RecordAIDecision(ctx, tx, workspaceID, gatheringID, input, s.now().UTC())
		return err
	})
	if err != nil || !waiting || snap.Level != levelAuto {
		return err
	}
	// At auto the card waited for AI; what it did not do, the rules do now.
	err = s.applyRules(ctx, store.DueGathering{WorkspaceID: workspaceID, ID: gatheringID, RulesAction: g.Rules.Action, RulesTarget: deref(g.Rules.TargetConversationID)},
		store.IntakeApply{Decider: store.DecidedByRules, Fallback: code != ""}, reviewReasonFor(reason))
	if errors.Is(err, store.ErrGatheringDecided) {
		return nil
	}
	return err
}

const (
	levelAuto    = "auto"
	levelSuggest = "suggest"
)

// notApplied is why AI's decision is not applied on its own, "" when it is: only at auto, on a card that
// waits for it, through an open gate, sure enough, about what it could read without a credential, soon
// enough, and for attach the one conversation of the customer it could mean. Whether that conversation is
// still one the card may join is checked when the decision is applied (store.ErrNotACandidate).
func (s *Service) notApplied(ctx context.Context, workspaceID, model string, snap store.IntakeSnapshot, options ai.IntakeOptions, built prompt.Context, d Decision, code string) string {
	g := snap.Gathering
	threshold := map[string]float64{prompt.ActionCreate: *options.CreateThreshold, prompt.ActionAttach: *options.AttachThreshold, prompt.ActionIgnore: *options.IgnoreThreshold}
	switch {
	case code != "":
		return "error"
	case snap.Level != levelAuto || g.State != store.GatheringDeciding:
		return "level_suggest"
	case built.Credential:
		return "credential"
	case !hasContent(snap) && len(built.Images) == 0:
		return "no_content"
	case s.now().Sub(g.ClosedAt) > time.Duration(*options.AutoMaxAgeSeconds)*time.Second:
		return "too_old"
	case !s.gate.Open(ctx, GateQuery{WorkspaceID: workspaceID, ChannelID: g.ChannelID, Scope: g.Scope, Action: d.Action, Model: model,
		Overrides: options.GateOverride}):
		return "gate_closed"
	case d.Confidence == nil || *d.Confidence < threshold[d.Action]:
		return "below_threshold"
	case d.Action == prompt.ActionAttach && *options.AttachRequiresSingleCandidate && customersConversations(snap) > 1:
		return "several_candidates"
	}
	return ""
}

// applyAI applies AI's decision through Decide; false when someone decided the card first.
func (s *Service) applyAI(ctx context.Context, workspaceID, gatheringID string, d Decision, detail store.AIDecisionDetail) (bool, error) {
	d.Detail = detail
	_, err := s.Decide(ctx, workspaceID, gatheringID, d, store.DecidedByAI, store.ChannelActor{})
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		return false, nil
	}
	return err == nil, err
}

// reviewReasonFor is why a card the rules would have kept waits for a person instead.
func reviewReasonFor(notApplied string) string {
	switch notApplied {
	case "gate_closed":
		return "gate_closed"
	case "credential":
		return "credential"
	case "too_old":
		return "too_old"
	case "error":
		return store.ReviewAIUnavailable
	}
	return "ai_unsure"
}

func hasContent(snap store.IntakeSnapshot) bool {
	for _, item := range snap.Items {
		if strings.TrimSpace(item.Body) != "" {
			return true
		}
	}
	return false
}

// customersConversations counts the open conversations offered that the card's customer reported.
func customersConversations(snap store.IntakeSnapshot) int {
	n := 0
	for _, candidate := range snap.Candidates {
		if !candidate.Resolved && (snap.Contact == nil || candidate.ContactID == snap.Contact.ID) {
			n++
		}
	}
	return n
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// snapshotFor turns what the store read into what the prompt builder reads: people by their identity on
// the channel, the channel's options, what the workspace agreed to send.
func snapshotFor(snap store.IntakeSnapshot, options ai.IntakeOptions, images []aiprovider.Image, imagesSkipped string) prompt.Snapshot {
	location, err := time.LoadLocation(snap.Timezone)
	if err != nil {
		location = time.UTC
	}
	consented := func(class string) bool {
		for _, c := range snap.Consents {
			if c == class {
				return true
			}
		}
		return false
	}
	out := prompt.Snapshot{Location: location, Sources: options.Sources, Skipped: map[string]string{}, RedactKeywords: options.RedactKeywords,
		ContactLabels: prompt.ContactLabels{Account: options.ContactLabels.Account, ID: options.ContactLabels.ID, Patterns: maskPatterns(options.RedactPatterns)},
		Instructions:  *options.Instructions}
	g := snap.Gathering
	if g.Place != nil {
		out.Place = prompt.Place{OneToOne: g.Place.Kind != "group"}
		if g.Place.Name != nil {
			out.Place.Name = *g.Place.Name
		}
	}
	quote := func(item store.IntakeSnapshotItem) *prompt.Quote {
		if item.QuoteExternalID == nil {
			return nil
		}
		quoted, ok := snap.Quoted[*item.QuoteExternalID]
		if !ok {
			return nil
		}
		return &prompt.Quote{ID: quoted.ID, At: quoted.OccurredAt, From: personOf(quoted.ChatItem), Text: quoted.Body, Media: mediaOf(quoted.ChatItem),
			ConversationID: quoted.ConversationID}
	}
	itemOf := func(item store.IntakeSnapshotItem) prompt.Item {
		return prompt.Item{ID: item.ID, At: item.OccurredAt, From: personOf(item.ChatItem), Text: item.Body, Media: mediaOf(item.ChatItem), Quote: quote(item),
			ConversationID: item.ConversationID}
	}
	for _, item := range snap.Before {
		out.Before = append(out.Before, itemOf(item))
	}
	for _, item := range snap.Items {
		out.Incoming = append(out.Incoming, itemOf(item))
	}
	for _, c := range snap.Candidates {
		out.Candidates = append(out.Candidates, prompt.Candidate{ConversationID: c.ConversationID, Status: c.Status, UpdatedAt: c.UpdatedAt, Subject: c.Subject,
			LastMessage: c.LastMessage, Reporter: prompt.Person{Key: "contact:" + c.ContactID, Name: c.ContactName}})
	}
	if snap.Contact != nil {
		if consented("contact_profile") {
			out.Contact = &prompt.Contact{Person: prompt.Person{Key: "contact:" + snap.Contact.ID, Name: snap.Contact.Name}, Company: snap.Contact.Organization,
				OpenConversations: snap.Contact.OpenConversations}
		} else {
			out.Skipped[prompt.SourceContact] = "consent"
		}
	}
	out.Images = images
	if imagesSkipped != "" {
		out.Skipped[prompt.SourceImages] = imagesSkipped
	}
	for _, command := range snap.Commands {
		out.Commands = append(out.Commands, prompt.Command{Key: command})
	}
	return out
}

func maskPatterns(patterns []ai.IntakeRedactPattern) []prompt.MaskPattern {
	out := make([]prompt.MaskPattern, len(patterns))
	for i, p := range patterns {
		out[i] = prompt.MaskPattern{Pattern: p.Pattern, Mask: p.Mask}
	}
	return out
}

// cardImages reads the card's pictures when AI may see them: the images source on, the workspace agreed
// to send pictures, and the model (and its fallback) reads them. Otherwise it says why not.
func (s *Service) cardImages(ctx context.Context, run *ai.Run, snap store.IntakeSnapshot, options ai.IntakeOptions) ([]aiprovider.Image, string, error) {
	if !options.Sources[prompt.SourceImages] {
		return nil, "", nil
	}
	if !slices.Contains(snap.Consents, "images") {
		return nil, "consent", nil
	}
	taker, ok := run.Binding.Provider.(aiprovider.ImageTaker)
	if !ok || !taker.TakesImages(ctx, run.Binding.Model) || run.Binding.FallbackModel != "" && !taker.TakesImages(ctx, run.Binding.FallbackModel) {
		return nil, "model_text_only", nil
	}
	stored, err := s.store.IntakeImagesOf(ctx, run.Task.WorkspaceID, run.Task.GatheringID, aiprovider.MaxImages, aiprovider.MaxImageBytes)
	if err != nil {
		return nil, "", err
	}
	images := make([]aiprovider.Image, len(stored))
	for i, image := range stored {
		images[i] = aiprovider.Image{MediaType: image.ContentType, Data: image.Data}
	}
	return images, "", nil
}

// personOf is who wrote an item: a customer by their contact (so they are one person across the place)
// or identity on the channel, the team as one of its people or the channel's bot.
func personOf(item store.ChatItem) prompt.Person {
	name := deref(item.Sender.Name)
	team := item.Role == channels.RoleStaff || item.Role == channels.RoleBot
	switch {
	case !team && item.ContactID != nil:
		return prompt.Person{Key: "contact:" + *item.ContactID, Name: name}
	case item.AgentID != nil:
		return prompt.Person{Key: "agent:" + *item.AgentID, Name: name, Team: true}
	}
	return prompt.Person{Key: deref(item.Sender.Kind) + ":" + deref(item.Sender.Value), Name: name, Team: team}
}

// mediaOf names the kinds of files that came with an item.
func mediaOf(item store.ChatItem) []string {
	var kinds []string
	for _, file := range item.Files {
		switch {
		case strings.HasPrefix(file.ContentType, "image/"):
			kinds = append(kinds, "image")
		case strings.HasPrefix(file.ContentType, "video/"):
			kinds = append(kinds, "video")
		default:
			kinds = append(kinds, "file")
		}
	}
	return kinds
}
