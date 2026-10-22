package aidraft_test

import (
	"slices"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/aidraft"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// sources numbers the retrieved documents as the prompt shows them: S1, S2, ...
var sources = []store.AIRetrievedDoc{
	{Kind: store.AIRetrievedKBArticle, KBArticleID: "a1", Number: 12, Title: "Refund policy", Text: "Annual plans are refundable within 30 days."},
	{Kind: store.AIRetrievedConversation, ConversationID: "c1", Title: "Refund question", Text: "Customer: refund?\n\nReply: Yes within 30 days."},
	{Kind: store.AIRetrievedKBArticle, KBArticleID: "a2", Number: 13, Title: "Export invoices", Text: "Select invoices, then Export > ZIP."},
}

var tags = []string{"billing", "export", "bug"}

func parse(t *testing.T, answer string) aidraft.Result {
	t.Helper()
	result, err := aidraft.Parse(answer, sources, tags)
	if err != nil {
		t.Fatalf("parse %s: %v", answer, err)
	}
	return result
}

func TestAGroundedConfidentAnswerIsHighAndTakesItsTopicFromTheFirstArticleCited(t *testing.T) {
	result := parse(t, `{"action":"answer","reply":"Select the invoices, then Export > ZIP.","confidence":"high",
		"claims":[{"text":"Export > ZIP exports invoices","source":"S3"}],"risks":[],
		"triage":{"tags":["export"],"priority":"normal","reason":"How-to question"}}`)
	if result.Confidence != aidraft.ConfidenceHigh || len(result.Reasons) != 0 {
		t.Fatalf("confidence = %s %v, want high with no reasons", result.Confidence, result.Reasons)
	}
	if result.TopicArticleID != "a2" {
		t.Errorf("topic = %q, want the export article a2", result.TopicArticleID)
	}
	if len(result.Sources) != 1 || result.Sources[0].ID != "a2" || result.Sources[0].Number != 13 || result.Sources[0].Title != "Export invoices" {
		t.Errorf("sources = %+v, want the export article as stored", result.Sources)
	}
	if result.Triage == nil || !slices.Equal(result.Triage.Tags, []string{"export"}) || result.Triage.Priority != "normal" {
		t.Errorf("triage = %+v", result.Triage)
	}
}

func TestAnAnswerIsNotHighWhenTheModelOrTheChecksDoubtIt(t *testing.T) {
	cases := map[string]struct {
		answer string
		reason string
	}{
		"model unsure": {`{"action":"answer","reply":"Yes.","confidence":"medium","claims":[{"text":"x","source":"S1"}],"risks":[]}`, aidraft.ReasonModelUnsure},
		"a claim without a source": {`{"action":"answer","reply":"Yes, and we also give credit.","confidence":"high",
			"claims":[{"text":"refundable within 30 days","source":"S1"},{"text":"we give credit","source":""}],"risks":[]}`, aidraft.ReasonUncitedClaim},
		"a source that was never shown": {`{"action":"answer","reply":"Yes.","confidence":"high","claims":[{"text":"x","source":"S9"}],"risks":[]}`, aidraft.ReasonUnknownSource},
		"only a past conversation":      {`{"action":"answer","reply":"Yes.","confidence":"high","claims":[{"text":"x","source":"S2"}],"risks":[]}`, aidraft.ReasonNoKBSource},
		"no claims at all":              {`{"action":"answer","reply":"Sure!","confidence":"high","claims":[],"risks":[]}`, aidraft.ReasonNoKBSource},
		"a risk":                        {`{"action":"answer","reply":"Yes.","confidence":"high","claims":[{"text":"x","source":"S1"}],"risks":["refund_or_billing_exception"]}`, "risk:refund_or_billing_exception"},
		"a question back":               {`{"action":"clarify","reply":"Which plan are you on?","confidence":"high","claims":[],"risks":[]}`, aidraft.ReasonNotAnAnswer},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			result := parse(t, test.answer)
			if result.Confidence == aidraft.ConfidenceHigh {
				t.Fatalf("confidence high, want lower")
			}
			if !slices.Contains(result.Reasons, test.reason) {
				t.Errorf("reasons = %v, want %s", result.Reasons, test.reason)
			}
		})
	}
}

func TestAHandoffIsLowAndKeepsItsReason(t *testing.T) {
	result := parse(t, `{"action":"handoff","reply":"","confidence":"high","claims":[],"risks":["account_specific_data"],"handoff_reason":"Needs the customer's invoice history."}`)
	if result.Action != aidraft.ActionHandoff || result.Confidence != aidraft.ConfidenceLow || result.HandoffReason != "Needs the customer's invoice history." {
		t.Fatalf("result = %+v", result)
	}
}

func TestTriageKeepsOnlyExistingTagsAndAKnownPriority(t *testing.T) {
	result := parse(t, `{"action":"answer","reply":"Yes.","confidence":"high","claims":[{"text":"x","source":"S1"}],"risks":[],
		"triage":{"tags":["Billing","made-up","billing"],"priority":"critical","reason":"Refund"}}`)
	if result.Triage == nil || !slices.Equal(result.Triage.Tags, []string{"billing"}) || result.Triage.Priority != "" {
		t.Fatalf("triage = %+v, want only the existing tag billing (once, as the workspace spells it) and no priority", result.Triage)
	}
}

func TestAnAnswerThatIsNotJSONIsAnError(t *testing.T) {
	if _, err := aidraft.Parse("Sure, here is a reply.", sources, tags); err == nil {
		t.Fatal("parsed text that is not JSON")
	}
	if _, err := aidraft.Parse(`{"action":"answer","reply":"","confidence":"high","claims":[],"risks":[]}`, sources, tags); err == nil {
		t.Fatal("parsed an answer without a reply")
	}
}
