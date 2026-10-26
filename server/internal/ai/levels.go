package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Levels: what a feature does by itself on a channel (docs/internals/ai.md "Levels per channel").
// A person can always ask by hand while the feature is on; a level only decides the AI's own work.
const (
	LevelOff     = "off"
	LevelSuggest = "suggest"
	LevelAuto    = "auto"
)

// Levels lists the levels from least to most the AI does by itself.
var Levels = []string{LevelOff, LevelSuggest, LevelAuto}

// SlotIntake decides whether a chat message opens a conversation, joins one, or is not a request.
const SlotIntake = "intake"

// Why a channel's level is off although a level was set.
const OffWorkspaceOff = "workspace_off"

// Where a channel's effective level comes from.
const (
	LevelFromScope     = "scope"
	LevelFromChannel   = "channel"
	LevelFromWorkspace = "workspace"
)

func levelRank(level string) int { return slices.Index(Levels, level) }

// levelAllowed says whether a feature may run at a level: a draft is never sent by AI, and intake
// runs by itself only on a channel's scope (its gate is the channel's), never as a workspace default.
func levelAllowed(slot, level string, onChannel bool) bool {
	switch {
	case levelRank(level) < 0:
		return false
	case level != LevelAuto:
		return true
	case slot == SlotTriage:
		return true
	case slot == SlotIntake:
		return onChannel
	}
	return false
}

// ChannelRef is what the AI settings need of a channel and its type's definition.
type ChannelRef struct {
	ID, Name, Type string
	// Scopes are the type's Definition.Scopes; IntakeScopes those of them intake runs on.
	Scopes, IntakeScopes []string
}

// ScopesShown are the scopes a channel's AI levels are set on: the whole channel (”) and its type's.
func (c ChannelRef) ScopesShown() []string { return append([]string{""}, c.Scopes...) }

// SlotsOf lists the features a scope of the channel has, in display order: triage and drafts
// everywhere, intake only on a scope the type declares (a chat's).
func (c ChannelRef) SlotsOf(scope string) []string {
	if scope != "" && slices.Contains(c.IntakeScopes, scope) {
		return []string{SlotTriage, SlotDraft, SlotIntake}
	}
	return []string{SlotTriage, SlotDraft}
}

// ChannelLevel is one level a channel sets apart from the workspace's default.
type ChannelLevel struct {
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Scope       string `json:"scope"`
	Slot        string `json:"slot"`
	Level       string `json:"level"`
	// EffectiveLevel is what runs: off while the workspace has the feature off (OffReason says so).
	EffectiveLevel string          `json:"effective_level"`
	OffReason      *string         `json:"off_reason"`
	Options        json.RawMessage `json:"options"`
	UpdatedBy      *string         `json:"updated_by"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ChannelAI is a channel's AI levels, one entry per scope and feature it has.
type ChannelAI struct {
	ChannelID string           `json:"channel_id"`
	Scopes    []ChannelAIScope `json:"scopes"`
}

// ChannelAIScope is one scope of a channel (” is the whole channel) and the features it has.
type ChannelAIScope struct {
	Scope string          `json:"scope"`
	Slots []ChannelAISlot `json:"slots"`
}

// ChannelAISlot is a feature's level on one scope of a channel. Level is this scope's own, null when
// it follows (Source says from where); EffectiveLevel is what runs.
type ChannelAISlot struct {
	Slot           string  `json:"slot"`
	Level          *string `json:"level"`
	EffectiveLevel string  `json:"effective_level"`
	Source         string  `json:"source"`
	OffReason      *string `json:"off_reason"`
	// Options are intake's settings on this scope, with the defaults filled in; null for other features.
	Options *IntakeOptions `json:"options"`
	// Lowered is the server's last lowering of this scope's level, while no person changed it since.
	Lowered *Lowered `json:"lowered"`
}

// Lowered says when and why the server lowered a level: model_changed or recipient_changed.
type Lowered struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// ChannelLevelInput sets one feature's level on a scope of a channel. A nil Level removes the
// scope's own level, so it follows the channel's, then the workspace's.
type ChannelLevelInput struct {
	Scope   string
	Level   *string
	Options json.RawMessage
}

var (
	// ErrRaiseNeedsApproval means the change lets the AI do more by itself, and the caller may not
	// do that without a person's approval (an agent token).
	ErrRaiseNeedsApproval = errors.New("this change lets AI do more by itself; a person approves it")
	// ErrLevelNotAllowed means the feature cannot run at that level there.
	ErrLevelNotAllowed = errors.New("the feature cannot run at that level here")
)

// IntakeNotSupportedError refuses intake on a scope its type does not declare (email, web forms);
// Available lists the features the scope has.
type IntakeNotSupportedError struct{ Available []string }

func (e *IntakeNotSupportedError) Error() string {
	return "intake runs only on the chat scopes a channel type declares"
}

// IntakeOptions are intake's settings on a channel's scope. A field left out takes its default.
type IntakeOptions struct {
	CreateThreshold *float64 `json:"create_threshold,omitempty"`
	AttachThreshold *float64 `json:"attach_threshold,omitempty"`
	IgnoreThreshold *float64 `json:"ignore_threshold,omitempty"`
	// AttachRequiresSingleCandidate: never attach by itself while the customer has two or more open
	// conversations the AI hesitates between.
	AttachRequiresSingleCandidate *bool `json:"attach_requires_single_candidate,omitempty"`
	// AutoMaxAgeSeconds: a batch older than this when it is decided is only suggested.
	AutoMaxAgeSeconds *int `json:"auto_max_age_seconds,omitempty"`
	// ReopenWindowDays: a resolved conversation is a candidate this long after its last change.
	ReopenWindowDays *int `json:"reopen_window_days,omitempty"`
	// Sources turns context sources on or off by name; a source left out keeps its default.
	Sources map[string]bool `json:"sources,omitempty"`
	// Instructions are the channel's own words added to the prompt.
	Instructions *string `json:"instructions,omitempty"`
	// RedactKeywords are words that mark a secret in the channel's language (a password, a code).
	RedactKeywords []string `json:"redact_keywords,omitempty"`
	// ContactLabels are words of the channel's language that name a bank account or an identity
	// document before its number, which is masked like the ones the frame's English words name.
	ContactLabels *IntakeContactLabels `json:"contact_labels,omitempty"`
	// RedactPatterns are the channel's own shapes of an identifier no label names (RE2 syntax), each
	// masked as its kind before AI reads a message.
	RedactPatterns []IntakeRedactPattern `json:"redact_patterns,omitempty"`
	// GateOverride opens the gate of actions (create, attach, ignore) by hand, before readiness does,
	// with GateOverrideReason saying why.
	GateOverride       []string `json:"gate_override,omitempty"`
	GateOverrideReason *string  `json:"gate_override_reason,omitempty"`
}

// maxOverrideReason bounds why an admin opened a gate by hand.
const maxOverrideReason = 500

// IntakeRedactPattern is one of a channel's patterns and what it masks: phone, id_number,
// account_number or hidden.
type IntakeRedactPattern struct {
	Pattern string `json:"pattern"`
	Mask    string `json:"mask"`
}

// Bounds of a channel's redact patterns: they run on every message intake reads.
const (
	maxRedactPatterns       = 20
	maxRedactPatternRunes   = 200
	maxRedactPatternProgram = 2000
)

var redactMasks = []string{"phone", "id_number", "account_number", "hidden"}

// emptyMatchProbes hold each kind of character a message has: letters, digits, spaces, punctuation.
var emptyMatchProbes = []string{"", "a", "1", " ", ".", "call me at 0912 345 678, ok?", "Ünïcödé đơn 12"}

// checkRedactPattern refuses a pattern that does not compile, is too long or too large once compiled,
// or matches the empty string (it would mask between every character). n is its place in the list, as the
// settings page numbers it.
func checkRedactPattern(n int, p IntakeRedactPattern) error {
	if !slices.Contains(redactMasks, p.Mask) {
		return invalid("Pattern %d is shown as phone, id_number, account_number or hidden.", n)
	}
	if runes := utf8.RuneCountInString(p.Pattern); runes == 0 || runes > maxRedactPatternRunes {
		return invalid("Pattern %d is 1 to %d characters.", n, maxRedactPatternRunes)
	}
	parsed, err := syntax.Parse(p.Pattern, syntax.Perl)
	if err != nil {
		return invalid("Pattern %d is not a valid RE2 expression: %s", n, p.Pattern)
	}
	program, err := syntax.Compile(parsed.Simplify())
	if err != nil || len(program.Inst) > maxRedactPatternProgram {
		return invalid("Pattern %d is too large: %s", n, p.Pattern)
	}
	compiled, err := regexp.Compile(p.Pattern)
	if err != nil {
		return invalid("Pattern %d is not a valid RE2 expression: %s", n, p.Pattern)
	}
	// A pattern that can match no character at all (an empty string, a word boundary) would put its mask
	// between letters: it is tried on texts of each kind of character.
	for _, probe := range emptyMatchProbes {
		for _, span := range compiled.FindAllStringIndex(probe, -1) {
			if span[0] == span[1] {
				return invalid("Pattern %d can match nothing at all, so it would mask between letters: %s", n, p.Pattern)
			}
		}
	}
	return nil
}

// IntakeContactLabels are the words that name a bank account (Account) or an identity document (ID).
type IntakeContactLabels struct {
	Account []string `json:"account,omitempty"`
	ID      []string `json:"id,omitempty"`
}

// IntakeSources are the context sources intake may read, and whether each is on by default.
var IntakeSources = map[string]bool{
	"incoming": true, "thread_window": true, "open_conversations": true, "corrections": false,
	"contact": true, "kb": true, "images": true,
}

// Defaults of a new channel's intake (docs/user/ai.md): attaching a new request to an old
// conversation hides it, so attaching needs more confidence than creating.
const (
	defaultCreateThreshold   = 0.85
	defaultAttachThreshold   = 0.90
	defaultIgnoreThreshold   = 0.95
	defaultAutoMaxAgeSeconds = 2 * 60 * 60
	defaultReopenWindowDays  = 7
	maxInstructions          = 4000
	// maxIntakeOptionsBytes is what channel_ai_levels.options holds (CHECK in the migration).
	maxIntakeOptionsBytes = 16384
)

// Effective returns the options with every default filled in.
func (o IntakeOptions) Effective() IntakeOptions {
	number := func(value *float64, fallback float64) *float64 {
		if value != nil {
			return value
		}
		return &fallback
	}
	whole := func(value *int, fallback int) *int {
		if value != nil {
			return value
		}
		return &fallback
	}
	single := true
	if o.AttachRequiresSingleCandidate != nil {
		single = *o.AttachRequiresSingleCandidate
	}
	sources := map[string]bool{}
	for name, on := range IntakeSources {
		sources[name] = on
	}
	for name, on := range o.Sources {
		sources[name] = on
	}
	instructions := ""
	if o.Instructions != nil {
		instructions = *o.Instructions
	}
	keywords := o.RedactKeywords
	if keywords == nil {
		keywords = []string{}
	}
	patterns := o.RedactPatterns
	if patterns == nil {
		patterns = []IntakeRedactPattern{}
	}
	override := o.GateOverride
	if override == nil {
		override = []string{}
	}
	reason := ""
	if o.GateOverrideReason != nil {
		reason = *o.GateOverrideReason
	}
	labels := IntakeContactLabels{Account: []string{}, ID: []string{}}
	if o.ContactLabels != nil {
		labels.Account = append(labels.Account, o.ContactLabels.Account...)
		labels.ID = append(labels.ID, o.ContactLabels.ID...)
	}
	return IntakeOptions{CreateThreshold: number(o.CreateThreshold, defaultCreateThreshold), AttachThreshold: number(o.AttachThreshold, defaultAttachThreshold),
		IgnoreThreshold: number(o.IgnoreThreshold, defaultIgnoreThreshold), AttachRequiresSingleCandidate: &single,
		AutoMaxAgeSeconds: whole(o.AutoMaxAgeSeconds, defaultAutoMaxAgeSeconds), ReopenWindowDays: whole(o.ReopenWindowDays, defaultReopenWindowDays),
		Sources: sources, Instructions: &instructions, RedactKeywords: keywords, ContactLabels: &labels, RedactPatterns: patterns,
		GateOverride: override, GateOverrideReason: &reason}
}

var keywordPattern = regexp.MustCompile(`^\S(.{0,78}\S)?$`)

// parseIntakeOptions reads and checks options as a client sends them; unknown fields are refused.
func parseIntakeOptions(raw json.RawMessage) (IntakeOptions, error) {
	var options IntakeOptions
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return options, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil {
		return options, invalid("options are intake's settings: create_threshold, attach_threshold, ignore_threshold, attach_requires_single_candidate, auto_max_age_seconds, reopen_window_days, sources, instructions, redact_keywords, contact_labels, redact_patterns, gate_override and gate_override_reason.")
	}
	// The settings page shows a refusal as it comes, so a threshold is named as the page names it.
	for name, value := range map[string]*float64{"open a conversation": options.CreateThreshold, "add to a conversation": options.AttachThreshold, "set a message aside as not a request": options.IgnoreThreshold} {
		if value != nil && (*value < 0.5 || *value > 0.99) {
			return options, invalid("The threshold to %s is between 0.5 and 0.99.", name)
		}
	}
	if value := options.AutoMaxAgeSeconds; value != nil && (*value < 60 || *value > 24*60*60) {
		return options, invalid("auto_max_age_seconds is between 60 and 86400.")
	}
	if value := options.ReopenWindowDays; value != nil && (*value < 0 || *value > 90) {
		return options, invalid("reopen_window_days is between 0 and 90.")
	}
	for name, on := range options.Sources {
		if _, known := IntakeSources[name]; !known {
			return options, invalid("sources names %s.", strings.Join(sortedKeys(IntakeSources), ", "))
		}
		if name == "incoming" && !on {
			return options, invalid("The incoming messages are what intake decides on; they cannot be turned off.")
		}
	}
	if options.Instructions != nil && utf8.RuneCountInString(*options.Instructions) > maxInstructions {
		return options, invalid("instructions are at most %d characters.", maxInstructions)
	}
	if encoded, err := json.Marshal(options); err == nil && jsonbTextBytes(encoded) > maxIntakeOptionsBytes {
		return options, invalid("Intake's settings of a scope are at most 16 KiB together; shorten the instructions or the redact keywords.")
	}
	if len(options.RedactKeywords) > 100 {
		return options, invalid("redact_keywords holds at most 100 words.")
	}
	for _, word := range options.RedactKeywords {
		if !keywordPattern.MatchString(word) {
			return options, invalid("Each of redact_keywords is 1 to 80 characters without spaces at its ends.")
		}
	}
	for i, action := range options.GateOverride {
		if !slices.Contains(store.IntakeActions, action) || slices.Contains(options.GateOverride[:i], action) {
			return options, invalid("gate_override names actions once each: create, attach, ignore.")
		}
	}
	reason := ""
	if options.GateOverrideReason != nil {
		reason = strings.TrimSpace(*options.GateOverrideReason)
	}
	if len(options.GateOverride) > 0 && reason == "" {
		return options, invalid("Opening a gate by hand needs gate_override_reason: why AI may act before its labels show it is ready.")
	}
	if utf8.RuneCountInString(reason) > maxOverrideReason {
		return options, invalid("gate_override_reason is at most %d characters.", maxOverrideReason)
	}
	if len(options.RedactPatterns) > maxRedactPatterns {
		return options, invalid("At most %d patterns to mask.", maxRedactPatterns)
	}
	for i, pattern := range options.RedactPatterns {
		if err := checkRedactPattern(i+1, pattern); err != nil {
			return options, err
		}
	}
	if labels := options.ContactLabels; labels != nil {
		if len(labels.Account) > 50 || len(labels.ID) > 50 {
			return options, invalid("contact_labels holds at most 50 words for account and 50 for id.")
		}
		for _, word := range append(slices.Clone(labels.Account), labels.ID...) {
			if !keywordPattern.MatchString(word) {
				return options, invalid("Each of contact_labels is 1 to 80 characters without spaces at its ends.")
			}
		}
	}
	return options, nil
}

// jsonbTextBytes is how long PostgreSQL writes encoded as jsonb text, which the CHECK on
// channel_ai_levels.options measures: a space after every ',' and ':' between values.
func jsonbTextBytes(encoded []byte) int {
	size, inString, escaped := len(encoded), false, false
	for _, b := range encoded {
		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		case !inString && (b == ',' || b == ':'):
			size++
		}
	}
	return size
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// optionsLoosen says whether going from before to after lets intake do more by itself: a lower
// threshold, attaching among several candidates, older batches, a longer reopen window, a source
// turned on, fewer words redacted or masked, a gate opened by hand, or other instructions. Only a change that tightens needs no approval.
func optionsLoosen(before, after IntakeOptions) bool {
	b, a := before.Effective(), after.Effective()
	if *a.CreateThreshold < *b.CreateThreshold || *a.AttachThreshold < *b.AttachThreshold || *a.IgnoreThreshold < *b.IgnoreThreshold {
		return true
	}
	if *b.AttachRequiresSingleCandidate && !*a.AttachRequiresSingleCandidate {
		return true
	}
	if *a.AutoMaxAgeSeconds > *b.AutoMaxAgeSeconds || *a.ReopenWindowDays > *b.ReopenWindowDays {
		return true
	}
	for name, on := range a.Sources {
		if on && !b.Sources[name] {
			return true
		}
	}
	for _, word := range b.RedactKeywords {
		if !slices.Contains(a.RedactKeywords, word) {
			return true
		}
	}
	for _, word := range b.ContactLabels.Account {
		if !slices.Contains(a.ContactLabels.Account, word) {
			return true
		}
	}
	for _, word := range b.ContactLabels.ID {
		if !slices.Contains(a.ContactLabels.ID, word) {
			return true
		}
	}
	for _, pattern := range b.RedactPatterns {
		if !slices.Contains(a.RedactPatterns, pattern) {
			return true
		}
	}
	for _, action := range a.GateOverride {
		if !slices.Contains(b.GateOverride, action) {
			return true
		}
	}
	return *a.Instructions != *b.Instructions
}

// levelRow is one row of channel_ai_levels.
type levelRow struct {
	scope, slot, level string
	options            json.RawMessage
	updatedBy          *string
	updatedAt          time.Time
}

func readChannelLevels(ctx context.Context, tx pgx.Tx, workspaceID, channelID string, lock bool) (map[[2]string]levelRow, error) {
	query := `SELECT scope,slot,level,options,updated_by::text,updated_at FROM channel_ai_levels WHERE workspace_id=$1 AND channel_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, query, workspaceID, channelID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (levelRow, error) {
		var r levelRow
		return r, row.Scan(&r.scope, &r.slot, &r.level, &r.options, &r.updatedBy, &r.updatedAt)
	})
	if err != nil {
		return nil, err
	}
	levels := map[[2]string]levelRow{}
	for _, r := range list {
		levels[[2]string{r.scope, r.slot}] = r
	}
	return levels, nil
}

// configuredLevel is what a scope of a channel asks of a feature, before the workspace's consent:
// the scope's row, the channel's, then the workspace's default. It is ai_level() without its first
// test, so a change can be compared while the feature is off.
func configuredLevel(levels map[[2]string]levelRow, slots map[string]slotRow, scope, slot string) (string, string) {
	if row, ok := levels[[2]string{scope, slot}]; ok {
		if scope == "" {
			return row.level, LevelFromChannel
		}
		return row.level, LevelFromScope
	}
	if row, ok := levels[[2]string{"", slot}]; ok && scope != "" {
		return row.level, LevelFromChannel
	}
	if row, ok := slots[slot]; ok {
		return row.defaultLevel, LevelFromWorkspace
	}
	return LevelOff, LevelFromWorkspace
}

func intakeOptionsOf(levels map[[2]string]levelRow, scope string) IntakeOptions {
	var options IntakeOptions
	if row, ok := levels[[2]string{scope, SlotIntake}]; ok {
		_ = json.Unmarshal(row.options, &options)
	}
	return options
}

// ChannelAI reads a channel's levels on each of its scopes, with what runs and why.
func (s *Service) ChannelAI(ctx context.Context, workspaceID string, channel ChannelRef) (ChannelAI, error) {
	var result ChannelAI
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		result, err = s.channelAI(ctx, tx, workspaceID, channel)
		return err
	})
	return result, err
}

func (s *Service) channelAI(ctx context.Context, tx pgx.Tx, workspaceID string, channel ChannelRef) (ChannelAI, error) {
	result := ChannelAI{ChannelID: channel.ID, Scopes: []ChannelAIScope{}}
	slots, err := readSlots(ctx, tx, workspaceID, false)
	if err != nil {
		return result, err
	}
	levels, err := readChannelLevels(ctx, tx, workspaceID, channel.ID, false)
	if err != nil {
		return result, err
	}
	lowered, err := lastLowerings(ctx, tx, workspaceID, channel.ID)
	if err != nil {
		return result, err
	}
	for _, scope := range channel.ScopesShown() {
		entry := ChannelAIScope{Scope: scope, Slots: []ChannelAISlot{}}
		for _, slot := range channel.SlotsOf(scope) {
			level, source := configuredLevel(levels, slots, scope, slot)
			item := ChannelAISlot{Slot: slot, EffectiveLevel: level, Source: source}
			if row, ok := levels[[2]string{scope, slot}]; ok {
				own := row.level
				item.Level = &own
			}
			if row, ok := slots[slot]; !ok || !row.enabled {
				// The workspace has not agreed to send this feature's data anywhere: nothing runs.
				reason := OffWorkspaceOff
				item.EffectiveLevel, item.OffReason = LevelOff, &reason
			}
			if slot == SlotIntake {
				options := intakeOptionsOf(levels, scope).Effective()
				item.Options = &options
			}
			item.Lowered = lowered[[2]string{scope, slot}]
			entry.Slots = append(entry.Slots, item)
		}
		result.Scopes = append(result.Scopes, entry)
	}
	return result, nil
}

// CheckChannelLevel refuses a level that could never be set on the channel, before anything is
// filed or stored: a scope the channel lacks, intake where its type declares none, a level the feature
// never runs at, options that are not intake's. It returns intake's options as given.
func CheckChannelLevel(channel ChannelRef, slot string, input ChannelLevelInput) (IntakeOptions, error) {
	var options IntakeOptions
	// The help center's chat has no level per channel: it is on or off for the workspace.
	if !slices.Contains(Slots, slot) || slot == SlotChat {
		return options, ErrNotFound
	}
	if !slices.Contains(channel.ScopesShown(), input.Scope) {
		names := make([]string, 0, len(channel.Scopes))
		for _, scope := range channel.Scopes {
			names = append(names, "'"+scope+"'")
		}
		if len(names) == 0 {
			return options, invalid("This channel has no scopes: send scope ''.")
		}
		return options, invalid("scope is '' (the whole channel) or one of %s.", strings.Join(names, ", "))
	}
	if !slices.Contains(channel.SlotsOf(input.Scope), slot) {
		return options, &IntakeNotSupportedError{Available: channel.SlotsOf(input.Scope)}
	}
	if input.Level != nil && !levelAllowed(slot, *input.Level, true) {
		return options, ErrLevelNotAllowed
	}
	if slot != SlotIntake && len(bytes.TrimSpace(input.Options)) > 0 && string(bytes.TrimSpace(input.Options)) != "null" {
		return options, invalid("options are intake's only.")
	}
	options, err := parseIntakeOptions(input.Options)
	if err != nil {
		return options, err
	}
	if input.Level == nil && len(bytes.TrimSpace(input.Options)) > 0 && string(bytes.TrimSpace(input.Options)) != "null" {
		return options, invalid("options go with a level; a scope that follows the channel follows its options too.")
	}
	return options, nil
}

// SetChannelLevel sets a feature's level on a scope of a channel, or removes it (nil Level). When
// the change lets the AI do more by itself (a scope's level rises, or intake's options loosen) and
// mayRaise is false, nothing changes and it returns ErrRaiseNeedsApproval: an agent token's raise
// waits for a person. Lowering is the caller's to do. Every change is in the AI history and in the
// channel's.
func (s *Service) SetChannelLevel(ctx context.Context, workspaceID string, actor Actor, channel ChannelRef, slot string, input ChannelLevelInput, mayRaise bool) (ChannelAI, error) {
	var result ChannelAI
	options, err := CheckChannelLevel(channel, slot, input)
	if err != nil {
		return result, err
	}
	now := s.now().UTC()
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		// The order SetSlot takes them: a recipient change lowering the channels meanwhile is seen.
		slots, err := readSlots(ctx, tx, workspaceID, true)
		if err != nil {
			return err
		}
		levels, err := readChannelLevels(ctx, tx, workspaceID, channel.ID, true)
		if err != nil {
			return err
		}
		next := map[[2]string]levelRow{}
		for key, row := range levels {
			next[key] = row
		}
		key := [2]string{input.Scope, slot}
		current, had := levels[key]
		encoded := json.RawMessage(`{}`)
		if slot == SlotIntake && input.Level != nil {
			if len(bytes.TrimSpace(input.Options)) == 0 && had {
				// Options left out keep what the scope had.
				_ = json.Unmarshal(current.options, &options)
			}
			if encoded, err = json.Marshal(options); err != nil {
				return err
			}
		}
		if input.Level == nil {
			delete(next, key)
		} else {
			next[key] = levelRow{scope: input.Scope, slot: slot, level: *input.Level, options: encoded}
		}
		raises := false
		for _, scope := range channel.ScopesShown() {
			if !slices.Contains(channel.SlotsOf(scope), slot) {
				continue
			}
			before, _ := configuredLevel(levels, slots, scope, slot)
			after, _ := configuredLevel(next, slots, scope, slot)
			raises = raises || levelRank(after) > levelRank(before)
		}
		if slot == SlotIntake && optionsLoosen(intakeOptionsOf(levels, input.Scope), intakeOptionsOf(next, input.Scope)) {
			raises = true
		}
		if raises && !mayRaise {
			return ErrRaiseNeedsApproval
		}
		updatedBy := nullable(actor.AgentID)
		if input.Level == nil {
			if !had {
				result, err = s.channelAI(ctx, tx, workspaceID, channel)
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM channel_ai_levels WHERE workspace_id=$1 AND channel_id=$2 AND scope=$3 AND slot=$4`,
				workspaceID, channel.ID, input.Scope, slot); err != nil {
				return err
			}
		} else {
			if had && current.level == *input.Level && bytes.Equal(normalJSON(current.options), normalJSON(encoded)) {
				result, err = s.channelAI(ctx, tx, workspaceID, channel)
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level,options,updated_by,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (workspace_id,channel_id,scope,slot) DO UPDATE SET level=EXCLUDED.level,
				options=EXCLUDED.options,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`,
				workspaceID, channel.ID, input.Scope, slot, *input.Level, encoded, updatedBy, now); err != nil {
				return err
			}
			if slot == SlotTriage && *input.Level == LevelAuto {
				// Applying by itself needs parts to apply; the workspace's, or the default ones.
				if _, err := tx.Exec(ctx, `UPDATE ai_slots SET auto_parts=$3 WHERE workspace_id=$1 AND slot=$2 AND auto_parts='{}'`,
					workspaceID, slot, defaultAutoParts); err != nil {
					return err
				}
			}
		}
		var before, after any
		if had {
			before = current.level
		}
		if input.Level != nil {
			after = *input.Level
		}
		optionsChanged := slot == SlotIntake && !bytes.Equal(normalJSON(current.options), normalJSON(encoded))
		if err := recordChannelLevel(ctx, tx, workspaceID, actor, channel, input.Scope, slot, before, after, optionsChanged, "", now); err != nil {
			return err
		}
		result, err = s.channelAI(ctx, tx, workspaceID, channel)
		return err
	})
	return result, err
}

func normalJSON(raw json.RawMessage) []byte {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return []byte("{}")
	}
	if m, ok := value.(map[string]any); ok && len(m) == 0 {
		return []byte("{}")
	}
	out, _ := json.Marshal(value)
	return out
}

// recordChannelLevel writes a level change to the AI history and to the channel's. reason is empty
// for a person's change, else why the server changed it (recipient_changed, model_changed).
func recordChannelLevel(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, channel ChannelRef, scope, slot string, before, after any,
	optionsChanged bool, reason string, now time.Time) error {
	detail := map[string]any{"channel_id": channel.ID, "channel_name": channel.Name, "scope": scope, "slot": slot,
		"before": before, "after": after, "options_changed": optionsChanged}
	if reason != "" {
		detail["reason"] = reason
	}
	if err := addEvent(ctx, tx, workspaceID, "channel_level_changed", actor, "", "", slot, detail, now); err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string]any{"scope": scope, "slot": slot, "before": before, "after": after, "options_changed": optionsChanged, "reason": nullable(reason)})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO channel_events(workspace_id,channel_id,channel_type,channel_name,kind,actor_agent_id,actor_token_id,detail,created_at)
		VALUES($1,$2,$3,$4,'ai_changed',$5,$6,$7,$8)`, workspaceID, channel.ID, channel.Type, channel.Name, nullable(actor.AgentID), nullable(actor.TokenID), encoded, now)
	return err
}

// lowerChannelLevels takes back what channels let a feature do by itself when the admin's consent is
// recorded anew (the feature moved to another provider, or was turned on again; reason
// recipient_changed), or when intake moves to another model, whose gate it has not passed
// (model_changed): auto becomes suggest, and a draft written by itself becomes a draft asked for by
// hand, as the workspace's level does.
func lowerChannelLevels(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, slot, reason string, now time.Time) error {
	lowered := LevelSuggest
	from := LevelAuto
	if slot == SlotDraft {
		lowered, from = LevelOff, LevelSuggest
	}
	rows, err := tx.Query(ctx, `UPDATE channel_ai_levels l SET level=$4,updated_by=$5,updated_at=$6
		FROM channels c WHERE l.workspace_id=$1 AND l.slot=$2 AND l.level=$3 AND c.workspace_id=l.workspace_id AND c.id=l.channel_id
		RETURNING l.channel_id::text,c.name,c.type,l.scope`, workspaceID, slot, from, lowered, nullable(actor.AgentID), now)
	if err != nil {
		return err
	}
	type change struct {
		channel ChannelRef
		scope   string
	}
	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (change, error) {
		var c change
		return c, row.Scan(&c.channel.ID, &c.channel.Name, &c.channel.Type, &c.scope)
	})
	if err != nil {
		return err
	}
	for _, c := range changes {
		if err := recordChannelLevel(ctx, tx, workspaceID, actor, c.channel, c.scope, slot, from, lowered, false, reason, now); err != nil {
			return err
		}
	}
	return nil
}

// lastLowerings is, per scope and feature of a channel, the server's lowering of its level when it is
// the last change of it (a person's change since clears it).
func lastLowerings(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (map[[2]string]*Lowered, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (detail->>'scope',detail->>'slot') detail->>'scope',detail->>'slot',detail->>'reason',created_at
		FROM channel_events WHERE workspace_id=$1 AND channel_id=$2::uuid AND kind='ai_changed'
		ORDER BY detail->>'scope',detail->>'slot',created_at DESC,id DESC`, workspaceID, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lowered := map[[2]string]*Lowered{}
	for rows.Next() {
		var scope, slot string
		var reason *string
		var at time.Time
		if err := rows.Scan(&scope, &slot, &reason, &at); err != nil {
			return nil, err
		}
		if reason != nil && *reason != "" {
			lowered[[2]string{scope, slot}] = &Lowered{At: at.UTC(), Reason: *reason}
		}
	}
	return lowered, rows.Err()
}

// channelLevels lists the levels channels set, for Settings → Built-in AI.
func channelLevels(ctx context.Context, tx pgx.Tx, workspaceID string, slots map[string]slotRow) ([]ChannelLevel, error) {
	rows, err := tx.Query(ctx, `SELECT l.channel_id::text,c.name,l.scope,l.slot,l.level,l.options,l.updated_by::text,l.updated_at,
			ai_level(l.workspace_id,l.channel_id,NULLIF(l.scope,''),l.slot)
		FROM channel_ai_levels l JOIN channels c ON c.workspace_id=l.workspace_id AND c.id=l.channel_id
		WHERE l.workspace_id=$1 AND c.deleted_at IS NULL ORDER BY lower(c.name),c.id,l.scope,l.slot`, workspaceID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChannelLevel, error) {
		var l ChannelLevel
		err := row.Scan(&l.ChannelID, &l.ChannelName, &l.Scope, &l.Slot, &l.Level, &l.Options, &l.UpdatedBy, &l.UpdatedAt, &l.EffectiveLevel)
		l.UpdatedAt = l.UpdatedAt.UTC()
		if l.Slot != SlotIntake {
			l.Options = nil
		}
		if row, ok := slots[l.Slot]; !ok || !row.enabled {
			reason := OffWorkspaceOff
			l.OffReason = &reason
		}
		return l, err
	})
	if list == nil {
		list = []ChannelLevel{}
	}
	return list, err
}

// SourceConsent is an admin's agreement that intake may send a kind of context besides the
// conversation's text: images, the contact's profile, an integration's data (external:<name>).
type SourceConsent struct {
	SourceClass string    `json:"source_class"`
	ConsentedBy *string   `json:"consented_by"`
	ConsentedAt time.Time `json:"consented_at"`
}

var sourceClassPattern = regexp.MustCompile(`^(images|contact_profile|external:[a-z0-9_]{1,40})$`)

// ErrSourceClass refuses a source class that is not images, contact_profile or external:<name>.
var ErrSourceClass = errors.New("a source class is images, contact_profile or external:<name>")

func sourceConsents(ctx context.Context, tx pgx.Tx, workspaceID string) ([]SourceConsent, error) {
	rows, err := tx.Query(ctx, `SELECT source_class,consented_by::text,consented_at FROM ai_source_consents WHERE workspace_id=$1 ORDER BY source_class`, workspaceID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SourceConsent, error) {
		var c SourceConsent
		err := row.Scan(&c.SourceClass, &c.ConsentedBy, &c.ConsentedAt)
		c.ConsentedAt = c.ConsentedAt.UTC()
		return c, err
	})
	if list == nil {
		list = []SourceConsent{}
	}
	return list, err
}

// SetSourceConsent records an admin's consent for a source class; giving it again changes nothing.
func (s *Service) SetSourceConsent(ctx context.Context, workspaceID string, actor Actor, class string) (SourceConsent, error) {
	if !sourceClassPattern.MatchString(class) {
		return SourceConsent{}, ErrSourceClass
	}
	now := s.now().UTC()
	var consent SourceConsent
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO ai_source_consents(workspace_id,source_class,consented_by,consented_at) VALUES($1,$2,$3,$4)
			ON CONFLICT DO NOTHING`, workspaceID, class, nullable(actor.AgentID), now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if err := addEvent(ctx, tx, workspaceID, "source_consent_changed", actor, "", "", SlotIntake,
				map[string]any{"source_class": class, "consented": true}, now); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `SELECT source_class,consented_by::text,consented_at FROM ai_source_consents WHERE workspace_id=$1 AND source_class=$2`,
			workspaceID, class).Scan(&consent.SourceClass, &consent.ConsentedBy, &consent.ConsentedAt)
	})
	consent.ConsentedAt = consent.ConsentedAt.UTC()
	return consent, err
}

// RemoveSourceConsent takes a consent back; intake stops sending that class from its next decision.
// It reports whether there was one.
func (s *Service) RemoveSourceConsent(ctx context.Context, workspaceID string, actor Actor, class string) (bool, error) {
	if !sourceClassPattern.MatchString(class) {
		return false, ErrSourceClass
	}
	removed := false
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ai_source_consents WHERE workspace_id=$1 AND source_class=$2`, workspaceID, class)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		removed = true
		return addEvent(ctx, tx, workspaceID, "source_consent_changed", actor, "", "", SlotIntake,
			map[string]any{"source_class": class, "consented": false}, s.now().UTC())
	})
	return removed, err
}
