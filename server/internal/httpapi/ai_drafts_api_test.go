package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/aidraft"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type aiDraftView struct {
	ID                string   `json:"id"`
	ConversationID    string   `json:"conversation_id"`
	Status            string   `json:"status"`
	Action            *string  `json:"action"`
	Body              *string  `json:"body"`
	Confidence        *string  `json:"confidence"`
	ConfidenceReasons []string `json:"confidence_reasons"`
	TopicArticleID    *string  `json:"topic_article_id"`
	Sources           []struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Number int64  `json:"number"`
		Title  string `json:"title"`
	} `json:"sources"`
	Triage *struct {
		Tags     []string `json:"tags"`
		Priority string   `json:"priority"`
	} `json:"triage"`
	TriageAppliedAt *string  `json:"triage_applied_at"`
	Outcome         *string  `json:"outcome"`
	Similarity      *float64 `json:"similarity"`
	AutoSendAt      *string  `json:"auto_send_at"`
	ErrorCode       *string  `json:"error_code"`
}

type aiDraftAnswer struct {
	Draft     *aiDraftView `json:"draft"`
	Available bool         `json:"available"`
	Enabled   bool         `json:"drafts_enabled"`
}

// aiDesk is a workspace on a server with an AI provider: one published refund article, the email
// channel and the tags billing and bug. The worker answers from fake, a provider without a network.
type aiDesk struct {
	api     http.Handler
	service *auth.Service
	pool    *pgxpool.Pool
	admin   string
	secret  string
	article kbArticleView
	fake    *aiprovider.Fake
	worker  *aidraft.Worker
}

func newAIDesk(t *testing.T, name string) aiDesk {
	t.Helper()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.AI = true })
	fake := &aiprovider.Fake{}
	desk := aiDesk{api: api, service: service, pool: pool, fake: fake,
		worker: &aidraft.Worker{Store: store.New(pool).WithAI(true), Provider: fake, Settings: aidraft.Settings{Model: "test-model", Effort: aiprovider.EffortLow}}}
	return desk.sibling(t, name)
}

// sibling is another workspace on the same server and worker.
func (d aiDesk) sibling(t *testing.T, name string) aiDesk {
	t.Helper()
	domainName := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".example.com"
	d.admin = createSession(t, d.api, d.service, name, "admin@"+domainName)
	d.secret = connectEmail(t, d.api, d.admin, "support@"+domainName)
	for _, tag := range []string{"billing", "bug"} {
		create(t, d.api, d.admin, "/api/v1/tags", map[string]any{"name": tag})
	}
	collection := kbCollection(t, d.api, d.admin, map[string]any{"name": "Billing"}).ID
	d.article = kbArticle(t, d.api, d.admin, map[string]any{"collection_id": collection, "title": "Refund policy",
		"body_md": "Annual plans are refundable within 30 days. The refund reaches your card in 5 to 10 business days.", "status": "published"})
	return d
}

// highAnswer is a grounded, confident answer citing S1 (the first source shown).
func highAnswer(reply string) aiprovider.Response {
	text := fmt.Sprintf(`{"action":"answer","reply":%q,"claims":[{"text":"refunds within 30 days","source":"S1"}],"confidence":"high","risks":[],
		"handoff_reason":"","triage":{"tags":["billing"],"priority":"high","reason":"Refund question"}}`, reply)
	usage := aiprovider.Usage{Reported: true, InputTokens: 1200, OutputTokens: 150}
	return aiprovider.Response{Text: text, JSON: text, Stop: aiprovider.StopDone, Model: "test-model", Usage: usage, Hops: []aiprovider.Hop{{Model: "test-model", Usage: usage}}}
}

func (d aiDesk) email(t *testing.T, from, subject, body string) string {
	t.Helper()
	result := inboundOK(t, d.api, d.secret, mail([]string{"From: " + from, "Subject: " + subject, fmt.Sprintf("Message-ID: <%s-%s@customer.example>", strings.ReplaceAll(subject, " ", "-"), strings.Split(from, "@")[0])}, body))
	if result.ConversationID == nil {
		t.Fatalf("inbound mail opened no conversation: %+v", result)
	}
	return *result.ConversationID
}

func (d aiDesk) run(t *testing.T) int {
	t.Helper()
	buildAIRetrieval(t, d.pool)
	written, err := d.worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("AI worker: %v", err)
	}
	return written
}

func (d aiDesk) draft(t *testing.T, token, conversationID string) aiDraftAnswer {
	t.Helper()
	var answer aiDraftAnswer
	response := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/ai-draft", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET ai-draft status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, response, &answer)
	return answer
}

func TestACustomerEmailGetsAnAIDraftFromTheKnowledgeBaseAndNeverAnotherWorkspacesText(t *testing.T) {
	desk := newAIDesk(t, "Draft Alpha")
	other := desk.sibling(t, "Draft Beta")
	kbArticle(t, other.api, other.admin, map[string]any{"collection_id": kbCollection(t, other.api, other.admin, map[string]any{"name": "Help"}).ID,
		"title": "Refund rules of Beta", "body_md": "CANARY-BETA-7731 refunds are never given.", "status": "published"})
	desk.fake.Script(aiprovider.FakeReply{Response: highAnswer("Annual plans are refundable within 30 days.")})

	conversation := desk.email(t, "olivia@customer.example", "Refund please", "We bought the annual plan 12 days ago. Can we get a refund?")
	if before := desk.draft(t, desk.admin, conversation); before.Draft == nil || before.Draft.Status != "queued" || !before.Available || !before.Enabled {
		t.Fatalf("before the worker: %+v, want a queued draft", before)
	}
	if written := desk.run(t); written != 1 {
		t.Fatalf("worker wrote %d drafts, want 1", written)
	}

	got := desk.draft(t, desk.admin, conversation).Draft
	if got == nil || got.Status != "ready" || got.Body == nil || *got.Body != "Annual plans are refundable within 30 days." || got.Confidence == nil || *got.Confidence != "high" {
		t.Fatalf("draft = %+v, want a ready high-confidence answer", got)
	}
	if len(got.Sources) != 1 || got.Sources[0].ID != desk.article.ID || got.Sources[0].Number != desk.article.Number || got.Sources[0].Title != "Refund policy" {
		t.Errorf("sources = %+v, want the refund article as stored", got.Sources)
	}
	if got.TopicArticleID == nil || *got.TopicArticleID != desk.article.ID || got.Triage == nil || got.Triage.Priority != "high" || len(got.Triage.Tags) != 1 || got.Triage.Tags[0] != "billing" {
		t.Errorf("topic %v triage %+v", got.TopicArticleID, got.Triage)
	}
	requests := desk.fake.Requests()
	if len(requests) != 1 {
		t.Fatalf("provider got %d requests, want 1", len(requests))
	}
	prompt := requests[0].Messages[0].Text
	if !strings.Contains(prompt, "Annual plans are refundable within 30 days") || !strings.Contains(prompt, "12 days ago") {
		t.Errorf("prompt lacks the article or the customer's message:\n%s", prompt)
	}
	if strings.Contains(prompt, "CANARY-BETA-7731") || strings.Contains(requests[0].System, "CANARY-BETA-7731") {
		t.Fatal("another workspace's article reached the prompt")
	}
	var rows, input int64
	if err := desk.pool.QueryRow(context.Background(), `SELECT count(*), COALESCE(sum(input_tokens),0) FROM ai_usage WHERE ai_draft_id=$1 AND feature='draft' AND model='test-model'`,
		got.ID).Scan(&rows, &input); err != nil || rows != 1 || input != 1200 {
		t.Errorf("usage rows=%d input=%d err=%v, want one metered call of 1200 input tokens", rows, input, err)
	}
	// Another workspace cannot read the draft, nor the conversation's.
	if response := request(t, desk.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/ai-draft", nil, other.admin, nil); response.Code != http.StatusNotFound {
		t.Errorf("another workspace read the draft: status=%d", response.Code)
	}
	if response := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-draft/"+got.ID+"/discard", nil, other.admin, nil); response.Code != http.StatusNotFound {
		t.Errorf("another workspace discarded the draft: status=%d", response.Code)
	}
}

func TestWithoutAnAIProviderNothingIsQueuedAndTheDraftRouteSaysWhy(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "No AI", "admin@no-ai.example.com")
	secret := connectEmail(t, api, admin, "support@no-ai.example.com")
	result := inboundOK(t, api, secret, mail([]string{"From: a@customer.example", "Subject: Help", "Message-ID: <no-ai@customer.example>"}, "Help me."))
	var queued int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ai_drafts`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("drafts queued without a provider: %d %v", queued, err)
	}
	var answer aiDraftAnswer
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+*result.ConversationID+"/ai-draft", nil, admin, nil), &answer)
	if answer.Available || answer.Draft != nil {
		t.Errorf("answer = %+v, want unavailable and no draft", answer)
	}
	response := request(t, api, http.MethodPost, "/api/v1/conversations/"+*result.ConversationID+"/ai-draft", nil, admin, nil)
	if response.Code != http.StatusConflict || errorCode(t, response) != "ai_unavailable" {
		t.Errorf("asking for a draft: status=%d body=%s, want 409 ai_unavailable", response.Code, response.Body.String())
	}
}

func TestSendingAReplyFromADraftRecordsHowMuchThePersonChangedIt(t *testing.T) {
	desk := newAIDesk(t, "Draft Use")
	desk.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		return highAnswer("Annual plans are refundable within 30 days. The refund reaches your card in 5 to 10 business days."), nil
	}
	first := desk.email(t, "a@customer.example", "Refund one", "Refund?")
	second := desk.email(t, "b@customer.example", "Refund two", "Refund?")
	desk.run(t)
	for _, test := range []struct {
		conversation, body, outcome string
	}{
		{first, "Annual plans are refundable within 30 days. The refund reaches your card in 5 to 10 business days.", "sent_unchanged"},
		{second, "Good news: annual plans are refundable within 30 days. I have started yours.", "sent_edited"},
	} {
		draft := desk.draft(t, desk.admin, test.conversation).Draft
		response := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+test.conversation+"/messages",
			map[string]any{"kind": "outgoing", "body": test.body, "ai_draft_id": draft.ID}, desk.admin, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("send from draft: status=%d body=%s", response.Code, response.Body.String())
		}
		var message struct {
			AIDraftID *string `json:"ai_draft_id"`
		}
		decode(t, response, &message)
		if message.AIDraftID == nil || *message.AIDraftID != draft.ID {
			t.Errorf("reply names draft %v, want %s", message.AIDraftID, draft.ID)
		}
		after := desk.draft(t, desk.admin, test.conversation).Draft
		if after.Status != "used" || after.Outcome == nil || *after.Outcome != test.outcome || after.Similarity == nil {
			t.Errorf("%s: draft after sending = %+v, want used %s", test.conversation, after, test.outcome)
		}
	}
	// A reply that names another workspace's draft, or a made-up one, is refused and nothing is sent.
	response := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+first+"/messages",
		map[string]any{"kind": "outgoing", "body": "Hi", "ai_draft_id": "00000000-0000-4000-8000-000000000000"}, desk.admin, nil)
	if response.Code != http.StatusNotFound {
		t.Errorf("reply with an unknown draft: status=%d, want 404", response.Code)
	}
}

func TestATriageSuggestionIsAppliedByAPersonAndADiscardCountsAgainstTheTopic(t *testing.T) {
	desk := newAIDesk(t, "Draft Triage")
	desk.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		return highAnswer("Refundable within 30 days."), nil
	}
	conversation := desk.email(t, "a@customer.example", "Refund", "Refund?")
	desk.run(t)
	draft := desk.draft(t, desk.admin, conversation).Draft
	var applied struct {
		Draft        aiDraftView `json:"draft"`
		Conversation struct {
			Priority string `json:"priority"`
			Tags     []struct {
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"conversation"`
	}
	response := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-draft/"+draft.ID+"/apply-triage", nil, desk.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("apply triage status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, response, &applied)
	if applied.Conversation.Priority != "high" || len(applied.Conversation.Tags) != 1 || applied.Conversation.Tags[0].Name != "billing" || applied.Draft.TriageAppliedAt == nil {
		t.Errorf("after apply: %+v", applied)
	}
	response = request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-draft/"+draft.ID+"/discard", nil, desk.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("discard status=%d body=%s", response.Code, response.Body.String())
	}
	if after := desk.draft(t, desk.admin, conversation).Draft; after.Status != "discarded" || after.Outcome == nil || *after.Outcome != "discarded" {
		t.Errorf("after discard: %+v", after)
	}
	var settings struct {
		Topics []struct {
			ArticleID string `json:"article_id"`
			Reviewed  int    `json:"reviewed"`
			Accepted  int    `json:"accepted"`
		} `json:"topics"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/workspace/ai", nil, desk.admin, nil), &settings)
	if len(settings.Topics) != 1 || settings.Topics[0].ArticleID != desk.article.ID || settings.Topics[0].Reviewed != 1 || settings.Topics[0].Accepted != 0 {
		t.Errorf("topics = %+v, want the refund article with one reviewed, none accepted", settings.Topics)
	}
	// A person can ask for a new draft; the old one is superseded.
	response = request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-draft", nil, desk.admin, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("new draft status=%d body=%s", response.Code, response.Body.String())
	}
	if again := desk.draft(t, desk.admin, conversation).Draft; again.Status != "queued" || again.ID == draft.ID {
		t.Errorf("after asking again: %+v", again)
	}
}

func TestAutoAnswerSendsOnlyTopicsPeopleApprovedAndAnyoneCanStopIt(t *testing.T) {
	desk := newAIDesk(t, "Auto Answer")
	desk.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		return highAnswer("Annual plans are refundable within 30 days."), nil
	}
	patch := func(body map[string]any, token string, want int) {
		t.Helper()
		if response := request(t, desk.api, http.MethodPatch, "/api/v1/workspace/ai", body, token, nil); response.Code != want {
			t.Fatalf("PATCH ai settings status=%d body=%s, want %d", response.Code, response.Body.String(), want)
		}
	}
	agent := agentSession(t, desk.api, desk.admin, "agent@auto-answer.example.com")
	patch(map[string]any{"auto_answer_enabled": true}, agent, http.StatusForbidden)
	patch(map[string]any{"auto_answer_enabled": true, "auto_answer_min_reviewed": 3, "auto_answer_delay_seconds": 0}, desk.admin, http.StatusOK)

	// Three drafts on the refund article, sent by people unchanged: the topic earns auto-answer.
	for i := range 3 {
		conversation := desk.email(t, fmt.Sprintf("c%d@customer.example", i), fmt.Sprintf("Refund %d", i), "Refund?")
		desk.run(t)
		draft := desk.draft(t, desk.admin, conversation).Draft
		if draft.Status != "ready" {
			t.Fatalf("draft %d status %s before the topic earned auto-answer, want ready", i, draft.Status)
		}
		request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
			map[string]any{"kind": "outgoing", "body": *draft.Body, "ai_draft_id": draft.ID}, desk.admin, nil)
	}

	// With a delay, the next one is scheduled, and anyone can stop it: it stays a draft for a person.
	patch(map[string]any{"auto_answer_delay_seconds": 3600}, desk.admin, http.StatusOK)
	stopped := desk.email(t, "d@customer.example", "Refund stop", "Refund?")
	desk.run(t)
	draft := desk.draft(t, desk.admin, stopped).Draft
	if draft.Status != "scheduled" || draft.AutoSendAt == nil {
		t.Fatalf("draft after the topic earned auto-answer = %+v, want scheduled", draft)
	}
	if response := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+stopped+"/ai-draft/"+draft.ID+"/cancel-auto-answer", nil, agent, nil); response.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", response.Code, response.Body.String())
	}
	if after := desk.draft(t, desk.admin, stopped).Draft; after.Status != "ready" || after.AutoSendAt != nil {
		t.Errorf("after cancel: %+v, want ready", after)
	}

	// A customer who writes again before it goes out stops it too, and gets a new draft.
	again := desk.email(t, "e@customer.example", "Refund again", "Refund?")
	desk.run(t)
	first := desk.draft(t, desk.admin, again).Draft
	if first.Status != "scheduled" {
		t.Fatalf("second scheduled draft = %+v", first)
	}
	inboundOK(t, desk.api, desk.secret, mail([]string{"From: e@customer.example", "Subject: Re: Refund again", "Message-ID: <follow-up-e@customer.example>",
		"In-Reply-To: <Refund-again-e@customer.example>"}, "Also, how long does it take?"))
	var status, outcome string
	if err := desk.pool.QueryRow(context.Background(), `SELECT status, COALESCE(outcome,'') FROM ai_drafts WHERE id=$1`, first.ID).Scan(&status, &outcome); err != nil ||
		status != "cancelled" || outcome != "auto_cancelled" {
		t.Errorf("scheduled answer after the customer wrote again: %s %s %v, want cancelled auto_cancelled", status, outcome, err)
	}
	desk.run(t)
	if next := desk.draft(t, desk.admin, again).Draft; next.ID == first.ID || next.Status != "scheduled" {
		t.Fatalf("draft for the follow-up = %+v, want a new scheduled one", next)
	}
	// Turning auto-answer off puts every scheduled answer back in a person's hands.
	patch(map[string]any{"auto_answer_enabled": false}, desk.admin, http.StatusOK)
	if after := desk.draft(t, desk.admin, again).Draft; after.Status != "ready" {
		t.Errorf("scheduled answer after auto-answer was turned off: %s, want ready", after.Status)
	}
	patch(map[string]any{"auto_answer_enabled": true, "auto_answer_delay_seconds": 0}, desk.admin, http.StatusOK)

	// Otherwise it is sent on its own, names its draft, has no author, and the conversation waits for the customer.
	sent := desk.email(t, "f@customer.example", "Refund sent", "Refund?")
	desk.run(t) // writes the draft and schedules it (delay 0)
	desk.run(t) // sends what is due
	for _, message := range timelineWithDraft(t, desk.api, desk.admin, sent) {
		if message.Kind == "outgoing" {
			if message.AIDraftID == nil || message.AuthorAgentID != nil || message.Body != "Annual plans are refundable within 30 days." {
				t.Errorf("auto answer = %+v", message)
			}
		}
	}
	if after := desk.draft(t, desk.admin, sent).Draft; after.Status != "auto_sent" {
		t.Errorf("sent draft status = %s, want auto_sent", after.Status)
	}
	if err := desk.pool.QueryRow(context.Background(), `SELECT status FROM conversations WHERE id=$1`, sent).Scan(&status); err != nil || status != "pending" {
		t.Errorf("conversation status = %s %v, want pending", status, err)
	}
	// Auto-answer never earns its own record: the topic still counts the three reviewed by people.
	var settings struct {
		Topics []struct {
			Reviewed int  `json:"reviewed"`
			Accepted int  `json:"accepted"`
			Eligible bool `json:"eligible"`
		} `json:"topics"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/workspace/ai", nil, desk.admin, nil), &settings)
	if len(settings.Topics) != 1 || settings.Topics[0].Reviewed != 3 || settings.Topics[0].Accepted != 3 || !settings.Topics[0].Eligible {
		t.Errorf("topics = %+v", settings.Topics)
	}
}

func TestAProviderFailureIsRetriedThenRecordedOnTheDraft(t *testing.T) {
	desk := newAIDesk(t, "Draft Failure")
	desk.fake.Script(
		aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded, Status: 529}},
		aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}},
	)
	conversation := desk.email(t, "a@customer.example", "Refund", "Refund?")
	desk.run(t)
	if draft := desk.draft(t, desk.admin, conversation).Draft; draft.Status != "queued" || draft.ErrorCode == nil || *draft.ErrorCode != "overloaded" {
		t.Fatalf("after a retryable failure: %+v, want queued with overloaded", draft)
	}
	if _, err := desk.pool.Exec(context.Background(), `UPDATE ai_drafts SET next_attempt_at=now()`); err != nil {
		t.Fatal(err)
	}
	desk.run(t)
	if draft := desk.draft(t, desk.admin, conversation).Draft; draft.Status != "failed" || draft.ErrorCode == nil || *draft.ErrorCode != "auth" {
		t.Fatalf("after a failure that is not retryable: %+v, want failed with auth", draft)
	}
	var rows int
	if err := desk.pool.QueryRow(context.Background(), `SELECT count(*) FROM ai_usage WHERE error_code IS NOT NULL`).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("metered failed calls = %d %v, want 2", rows, err)
	}
}

type timelineDraftMessage struct {
	Kind          string  `json:"kind"`
	Body          string  `json:"body"`
	AuthorAgentID *string `json:"author_agent_id"`
	AIDraftID     *string `json:"ai_draft_id"`
}

func timelineWithDraft(t *testing.T, api http.Handler, token, conversationID string) []timelineDraftMessage {
	t.Helper()
	var list struct {
		Items []timelineDraftMessage `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil), &list)
	return list.Items
}
