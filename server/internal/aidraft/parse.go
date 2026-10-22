package aidraft

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Actions the model may propose.
const (
	ActionAnswer  = "answer"
	ActionClarify = "clarify"
	ActionHandoff = "handoff"
)

// Confidence levels. The server decides the level; the model's own rating is one input of several.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Reasons a draft is not high confidence. Stored and shown, so values never change. A risk the
// model flagged is "risk:" followed by its name.
const (
	ReasonModelUnsure   = "model_unsure"
	ReasonNoKBSource    = "no_kb_source"
	ReasonUncitedClaim  = "uncited_claim"
	ReasonUnknownSource = "unknown_source"
	ReasonNotAnAnswer   = "not_an_answer"
	ReasonHandoff       = "handoff"
	// ReasonUnsourcedLink is a link or an email address in the reply that no source shown contains:
	// the model was told to use only the sources, so it came from the customer's text or nowhere.
	ReasonUnsourcedLink = "unsourced_link"
)

// linkPattern finds web addresses and email addresses in a reply.
var linkPattern = regexp.MustCompile(`(?i)\bhttps?://[^\s<>()\[\]"']+|\bwww\.[^\s<>()\[\]"']+|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`)

// unsourcedLinks returns the links and addresses of the reply that appear in no source.
func unsourcedLinks(reply string, docs []store.AIRetrievedDoc) []string {
	var missing []string
	for _, link := range linkPattern.FindAllString(reply, -1) {
		link = strings.TrimRight(link, ".,;:!?")
		found := false
		for _, doc := range docs {
			if strings.Contains(strings.ToLower(doc.Title+"\n"+doc.Text), strings.ToLower(link)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, link)
		}
	}
	return missing
}

// Risks the model is asked to flag. Any of them keeps a draft away from auto-answer.
var Risks = []string{
	"refund_or_billing_exception", // money back, a discount, a charge reversed, an exception to a policy
	"account_specific_data",       // the answer depends on this customer's own account, order or history
	"security_or_access",          // access to an account, 2FA, ownership, a suspected breach
	"legal_or_complaint",          // a legal threat, a chargeback, a formal complaint, personal data requests
	"angry_customer",              // the customer is upset or has written several times
	"outage_or_incident",          // something may be down or broken right now
	"needs_action_by_team",        // someone must do something the customer cannot (change, fix, escalate)
}

var priorities = []string{"low", "normal", "high", "urgent"}

// Source is one document a draft cites, as stored: its title and number come from the database.
type Source struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Number int64  `json:"number,omitempty"`
	Title  string `json:"title"`
	Locale string `json:"locale,omitempty"`
}

// Triage is a suggestion a person accepts or ignores.
type Triage struct {
	Tags     []string `json:"tags"`
	Priority string   `json:"priority,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// Result is a draft checked against what was shown to the model.
type Result struct {
	Action        string
	Body          string
	Confidence    string
	Reasons       []string
	HandoffReason string
	// Sources are the cited documents that were really shown, in the order first cited.
	Sources []Source
	// TopicArticleID is the first knowledge base article cited; empty when none is.
	TopicArticleID string
	Triage         *Triage
}

// answer is the JSON the model writes (Schema).
type answer struct {
	Action     string `json:"action"`
	Reply      string `json:"reply"`
	Confidence string `json:"confidence"`
	Claims     []struct {
		Text   string `json:"text"`
		Source string `json:"source"`
	} `json:"claims"`
	Risks         []string `json:"risks"`
	HandoffReason string   `json:"handoff_reason"`
	Triage        *struct {
		Tags     []string `json:"tags"`
		Priority string   `json:"priority"`
		Reason   string   `json:"reason"`
	} `json:"triage"`
}

// sourceIndex reads "S3" as the third document shown; anything else is -1.
func sourceIndex(label string, count int) int {
	label = strings.TrimSpace(label)
	if len(label) < 2 || (label[0] != 'S' && label[0] != 's') {
		return -1
	}
	n, err := strconv.Atoi(label[1:])
	if err != nil || n < 1 || n > count {
		return -1
	}
	return n - 1
}

// Parse reads the model's JSON answer and decides its confidence. docs are the sources in the order
// the prompt numbered them; tags are the workspace's tag names. A source the model names that was
// not shown, a claim without a source, a risk, or the model's own doubt keeps a draft below high.
func Parse(text string, docs []store.AIRetrievedDoc, tags []string) (Result, error) {
	var in answer
	if err := json.Unmarshal([]byte(text), &in); err != nil {
		return Result{}, fmt.Errorf("aidraft: the answer is not JSON: %w", err)
	}
	// PostgreSQL text cannot hold NUL; a model that writes one would otherwise fail every attempt.
	clean := func(text string) string { return strings.TrimSpace(strings.ReplaceAll(text, "\x00", "")) }
	result := Result{Action: in.Action, Body: clean(in.Reply), HandoffReason: clean(in.HandoffReason)}
	switch in.Action {
	case ActionAnswer, ActionClarify:
		if result.Body == "" {
			return Result{}, errors.New("aidraft: an answer without a reply")
		}
	case ActionHandoff:
	default:
		return Result{}, fmt.Errorf("aidraft: unknown action %q", in.Action)
	}
	var reasons []string
	add := func(reason string) {
		if !slices.Contains(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	cited := map[int]bool{}
	kbCited := false
	for _, claim := range in.Claims {
		if strings.TrimSpace(claim.Source) == "" {
			add(ReasonUncitedClaim)
			continue
		}
		index := sourceIndex(claim.Source, len(docs))
		if index < 0 {
			add(ReasonUnknownSource)
			continue
		}
		if !cited[index] {
			cited[index] = true
			doc := docs[index]
			source := Source{Kind: doc.Kind, Title: doc.Title, Locale: doc.KBLocale}
			if doc.Kind == store.AIRetrievedKBArticle {
				source.ID, source.Number = doc.KBArticleID, doc.Number
				kbCited = true
				if result.TopicArticleID == "" {
					result.TopicArticleID = doc.KBArticleID
				}
			} else {
				source.ID = doc.ConversationID
			}
			result.Sources = append(result.Sources, source)
		}
	}
	if in.Action == ActionHandoff {
		add(ReasonHandoff)
	} else if in.Action == ActionClarify {
		add(ReasonNotAnAnswer)
	}
	if in.Confidence != ConfidenceHigh {
		add(ReasonModelUnsure)
	}
	if !kbCited {
		add(ReasonNoKBSource)
	}
	if len(unsourcedLinks(result.Body, docs)) > 0 {
		add(ReasonUnsourcedLink)
	}
	for _, risk := range in.Risks {
		if slices.Contains(Risks, risk) {
			add("risk:" + risk)
		}
	}
	result.Reasons = reasons
	switch {
	case in.Action == ActionHandoff:
		result.Confidence = ConfidenceLow
	case len(reasons) == 0:
		result.Confidence = ConfidenceHigh
	case slices.Contains(reasons, ReasonUnknownSource) || len(result.Sources) == 0:
		result.Confidence = ConfidenceLow
	default:
		result.Confidence = ConfidenceMedium
	}
	result.Triage = cleanTriage(in.Triage, tags)
	return result, nil
}

// cleanTriage keeps the tags that exist in the workspace, spelled as the workspace spells them, once
// each, and a priority only when it is one Colliedesk knows.
func cleanTriage(in *struct {
	Tags     []string `json:"tags"`
	Priority string   `json:"priority"`
	Reason   string   `json:"reason"`
}, tags []string) *Triage {
	if in == nil {
		return nil
	}
	out := &Triage{Tags: []string{}, Reason: strings.TrimSpace(strings.ReplaceAll(in.Reason, "\x00", ""))}
	for _, name := range in.Tags {
		for _, tag := range tags {
			if strings.EqualFold(strings.TrimSpace(name), tag) && !slices.Contains(out.Tags, tag) {
				out.Tags = append(out.Tags, tag)
			}
		}
	}
	if slices.Contains(priorities, in.Priority) {
		out.Priority = in.Priority
	}
	if len(out.Tags) == 0 && out.Priority == "" {
		return nil
	}
	return out
}
