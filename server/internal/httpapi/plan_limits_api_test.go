package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

func invite(t *testing.T, f limitsFixture, admin string, number int) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": fmt.Sprintf("teammate%d@limits-alpha.example.com", number), "role_ceiling": "agent"}, admin, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

type responseView struct {
	Code int
	Body string
}

func TestFreePlanCapsAgentsAtThreeCountingPendingInvites(t *testing.T) {
	f := newLimitsFixture(t, true)
	// The admin is seat 1; two pending invites fill seats 2 and 3.
	for number := 1; number <= 2; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	fourth := request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "teammate3@limits-alpha.example.com", "role_ceiling": "agent"}, f.adminA, nil)
	if fourth.Code != http.StatusPaymentRequired {
		t.Fatalf("fourth seat status=%d body=%s, want 402", fourth.Code, fourth.Body.String())
	}
	var failure apiError
	decode(t, fourth, &failure)
	if failure.Error.Code != "plan_limit_reached" || !strings.Contains(failure.Error.Message, "Free") ||
		!strings.Contains(failure.Error.Message, "3 agents") || !strings.Contains(failure.Error.Message, "Settings") {
		t.Fatalf("error=%+v, want the plan, the cap and the upgrade path named", failure.Error)
	}
	if got := f.usage(t, f.adminA).Usage.Agents; got != 1 {
		t.Fatalf("agents=%d, want 1: a refused invite must not add anyone", got)
	}

	// The way out: revoking a pending invite frees its seat.
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/invites", nil, f.adminA, nil), &list)
	if len(list.Items) != 2 {
		t.Fatalf("pending invites=%d, want 2 (the refused one is not stored)", len(list.Items))
	}
	if got := request(t, f.api, http.MethodDelete, "/api/v1/auth/invites/"+list.Items[0].ID, nil, f.adminA, nil).Code; got != http.StatusNoContent {
		t.Fatalf("revoke status=%d", got)
	}
	if got := invite(t, f, f.adminA, 3); got.Code != http.StatusCreated {
		t.Fatalf("invite after a revoke status=%d body=%s, want 201", got.Code, got.Body)
	}
}

func TestExpiredAndUsedInvitesDoNotHoldASeat(t *testing.T) {
	f := newLimitsFixture(t, true)
	for number := 1; number <= 2; number++ {
		invite(t, f, f.adminA, number)
	}
	f.clock.set(midMonth.Add(time.Hour)) // past the 15 minute life of the first two links
	for number := 3; number <= 4; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d after the old ones expired: status=%d body=%s", number, got.Code, got.Body)
		}
	}
}

func TestRedeemingAnInviteOverTheCapIsRefusedAndTheLinkSurvives(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_redeem")
	var urls []string
	for number := 1; number <= 3; number++ {
		_, url := createContractInvite(t, f.api, f.adminA, fmt.Sprintf("seat%d@limits-alpha.example.com", number), "agent")
		urls = append(urls, url)
	}
	// Downgrade: the subscription ends. The workspace now has 1 agent and 3 pending invites.
	ended := time.Date(2026, time.September, 15, 11, 0, 0, 0, time.UTC)
	f.billing.mustDeliver(t, "evt_end", subscription{ID: "sub_redeem", Workspace: f.workspaceA, Product: productMonthly,
		Status: "canceled", PeriodEnd: ended, EndedAt: &ended, ModifiedAt: f.clock.Now().Add(time.Minute)}.event("subscription.revoked"))
	if plan := f.usage(t, f.adminA).Plan; plan != "free" {
		t.Fatalf("plan=%s, want free after the subscription ended", plan)
	}

	refused := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, urls[0])}, "", nil)
	if refused.Code != http.StatusPaymentRequired {
		t.Fatalf("redeem over the cap status=%d body=%s, want 402", refused.Code, refused.Body.String())
	}
	if got := f.usage(t, f.adminA).Usage.Agents; got != 1 {
		t.Fatalf("agents=%d, want 1: existing agents stay and none is added", got)
	}

	// Upgrading again lets the same, still unused link work.
	f.upgrade(t, f.workspaceA, "sub_redeem_again")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, urls[0])}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem after upgrade status=%d, want 200", got)
	}
}

func TestPaidPlansAllowTenAgents(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_paid_agents")
	for number := 1; number <= 9; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("paid invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	eleventh := invite(t, f, f.adminA, 10)
	if eleventh.Code != http.StatusPaymentRequired || !strings.Contains(eleventh.Body, "10 agents") {
		t.Fatalf("eleventh seat status=%d body=%s, want 402 naming the 10 agent cap", eleventh.Code, eleventh.Body)
	}
}

func TestSelfHostedHasNoAgentOrConversationLimits(t *testing.T) {
	f := newLimitsFixture(t, false)
	for number := 1; number <= 12; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("self-hosted invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	f.seedConversations(t, f.workspaceA, 100, f.clock.Now().Add(-time.Hour), false)
	result := f.receive(t, false, 1, "Over the free quota")
	if result.Status != "created" {
		t.Fatalf("result=%+v", result)
	}
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Over+the+free+quota"); len(subjects) != 1 {
		t.Fatalf("self-hosted conversation 101 hidden: %v", subjects)
	}
	if held := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE held_at IS NOT NULL`); held != "0" {
		t.Fatalf("held=%s, want 0 when billing is disabled", held)
	}
}

func TestInboundConversationOverTheMonthlyQuotaIsStoredHeldAndHidden(t *testing.T) {
	f := newLimitsFixture(t, true)
	for number := 1; number <= 100; number++ {
		if got := f.receive(t, false, number, fmt.Sprintf("Quota %d", number)); got.Status != "created" {
			t.Fatalf("conversation %d: %+v", number, got)
		}
	}
	if held := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE held_at IS NOT NULL`); held != "0" {
		t.Fatalf("held=%s after exactly 100 conversations, want 0", held)
	}
	over := f.receive(t, false, 101, "Held 101")
	if over.Status != "created" || over.ConversationID == nil || over.MessageID == nil {
		t.Fatalf("101st conversation must still be accepted and stored: %+v", over)
	}
	if body := queryString(t, f.pool, `SELECT body FROM messages WHERE id=$1`, *over.MessageID); !strings.Contains(body, "Held 101") {
		t.Fatalf("held message body=%q, want it stored in full", body)
	}
	if heldAt := queryString(t, f.pool, `SELECT COALESCE(held_at::text,'') FROM conversations WHERE id=$1`, *over.ConversationID); heldAt == "" {
		t.Fatal("the 101st conversation is not marked held")
	}

	if subjects := f.inboxSubjects(t, f.adminA, ""); len(subjects) != 100 || contains(subjects, "Held 101") {
		t.Fatalf("inbox shows %d conversations, held one visible=%v; want exactly the first 100", len(subjects), contains(subjects, "Held 101"))
	}
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Held+101"); len(subjects) != 0 {
		t.Fatalf("search finds the held conversation: %v", subjects)
	}
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Please+help+with+Held"); len(subjects) != 0 {
		t.Fatalf("search by message body finds the held conversation: %v", subjects)
	}

	// Every agent, not only an admin, learns from the list that conversations are waiting.
	if got := f.listedHeldCount(t, f.adminA); got != 1 {
		t.Fatalf("held_count on the list=%d, want 1", got)
	}
	if got := f.listedHeldCount(t, f.adminB); got != 0 {
		t.Fatalf("workspace B list held_count=%d, want 0", got)
	}
	view := f.usage(t, f.adminA)
	if view.Usage.ConversationsThisMonth != 101 || view.Usage.HeldConversations != 1 {
		t.Fatalf("usage=%+v, want 101 received this month and 1 held", view.Usage)
	}

	// Replies on conversations that already exist always work: an agent on the held one, and the
	// customer on a visible one, even though the month is over quota.
	if got := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+*over.ConversationID, nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("direct read of a held conversation status=%d, want 200", got)
	}
	agentReply := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+*over.ConversationID+"/messages",
		map[string]any{"kind": "note", "body": "Seen, will answer on upgrade."}, f.adminA, nil)
	if agentReply.Code != http.StatusCreated {
		t.Fatalf("agent note on a held conversation status=%d body=%s", agentReply.Code, agentReply.Body.String())
	}
	first := queryString(t, f.pool, `SELECT id::text FROM conversations WHERE subject='Quota 1'`)
	customerReply := inboundOK(t, f.api, f.secretA, mail([]string{
		"From: customer1@customer.example", "To: support@limits-alpha.example.com", "Subject: Re: Quota 1",
		"Message-ID: <limits-reply@customer.example>", "In-Reply-To: <limits-1-false@customer.example>",
	}, "Following up."))
	if customerReply.Status != "threaded" || *customerReply.ConversationID != first {
		t.Fatalf("customer reply on a visible conversation: %+v, want threaded onto %s", customerReply, first)
	}
	if subjects := f.inboxSubjects(t, f.adminA, ""); len(subjects) != 100 {
		t.Fatalf("inbox=%d after the replies, want 100", len(subjects))
	}
}

func TestHeldConversationsBecomeVisibleWhenTheMonthEnds(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.seedConversations(t, f.workspaceA, 100, midMonth.Add(-time.Hour), false)
	held := f.receive(t, false, 1, "Held in September")
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Held+in+September"); len(subjects) != 0 {
		t.Fatalf("held conversation visible in its own month: %v", subjects)
	}

	// Hand-written calendar: 23:59:59 on 30 September is still September; 00:00:00 on 1 October is
	// the first instant of the next month, which has a fresh quota.
	f.clock.set(time.Date(2026, time.September, 30, 23, 59, 59, 0, time.UTC))
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Held+in+September"); len(subjects) != 0 {
		t.Fatalf("held conversation visible one second before the month ends: %v", subjects)
	}
	f.clock.set(time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC))
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Held+in+September"); len(subjects) != 1 {
		t.Fatalf("held conversation still hidden in the next month: %v", subjects)
	}
	if got := f.usage(t, f.adminA).Usage; got.HeldConversations != 0 || got.ConversationsThisMonth != 0 {
		t.Fatalf("usage=%+v on 1 October, want a clean month", got)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+*held.ConversationID, nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
	// The new month starts with a full quota: the next conversation is not held.
	f.receive(t, false, 2, "Fresh in October")
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Fresh+in+October"); len(subjects) != 1 {
		t.Fatalf("first conversation of a new month hidden: %v", subjects)
	}
}

func TestUpgradeReleasesHeldConversationsAndPaidTrafficIsNeverHeld(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.seedConversations(t, f.workspaceA, 100, f.clock.Now().Add(-time.Hour), false)
	f.receive(t, false, 1, "Waiting for upgrade")
	f.receive(t, false, 2, "Also waiting")
	if got := f.usage(t, f.adminA).Usage.HeldConversations; got != 2 {
		t.Fatalf("held=%d, want 2 before the upgrade", got)
	}

	f.upgrade(t, f.workspaceA, "sub_release")
	if subjects := f.inboxSubjects(t, f.adminA, ""); !contains(subjects, "Waiting for upgrade") || !contains(subjects, "Also waiting") {
		t.Fatalf("held conversations not released on upgrade: %d visible", len(subjects))
	}
	if got := f.usage(t, f.adminA).Usage.HeldConversations; got != 0 {
		t.Fatalf("held=%d after the upgrade, want 0", got)
	}
	if got := f.listedHeldCount(t, f.adminA); got != 0 {
		t.Fatalf("held_count on the list=%d after the upgrade, want 0", got)
	}
	f.receive(t, false, 3, "Paid and over 100")
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Paid+and+over"); len(subjects) != 1 {
		t.Fatalf("paid workspace held a conversation: %v", subjects)
	}
}

func TestDowngradeKeepsExistingConversationsVisible(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_down")
	f.seedConversations(t, f.workspaceA, 150, f.clock.Now().Add(-time.Hour), false)
	f.receive(t, false, 1, "Received while paid")
	before := len(f.inboxSubjects(t, f.adminA, ""))

	ended := f.clock.Now().Add(-time.Minute)
	f.billing.mustDeliver(t, "evt_down", subscription{ID: "sub_down", Workspace: f.workspaceA, Product: productMonthly,
		Status: "canceled", PeriodEnd: ended, EndedAt: &ended, ModifiedAt: f.clock.Now().Add(time.Minute)}.event("subscription.revoked"))
	if got := len(f.inboxSubjects(t, f.adminA, "")); got != before {
		t.Fatalf("downgrade changed the inbox from %d to %d conversations", before, got)
	}
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Received+while+paid"); len(subjects) != 1 {
		t.Fatalf("a conversation received while paid disappeared after the downgrade: %v", subjects)
	}
	// Only a new one over the quota is held: this month already has 151.
	f.receive(t, false, 2, "After the downgrade")
	if subjects := f.inboxSubjects(t, f.adminA, "&search=After+the+downgrade"); len(subjects) != 0 {
		t.Fatalf("new conversation over quota after downgrade is visible: %v", subjects)
	}
}

func TestImportedConversationsNeitherCountNorAreHeld(t *testing.T) {
	f := newLimitsFixture(t, true)
	// An import writes tickets with their original, old or new, timestamps and an origin. A year of
	// history stamped this month must not use up the quota.
	f.seedConversations(t, f.workspaceA, 500, f.clock.Now().Add(-time.Hour), true)
	if got := f.usage(t, f.adminA).Usage; got.ConversationsThisMonth != 0 || got.HeldConversations != 0 {
		t.Fatalf("usage=%+v, want imported conversations ignored", got)
	}
	f.receive(t, false, 1, "First real one")
	if subjects := f.inboxSubjects(t, f.adminA, "&search=First+real+one"); len(subjects) != 1 {
		t.Fatalf("first real conversation after an import was held: %v", subjects)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE held_at IS NOT NULL`); got != "0" {
		t.Fatalf("held=%s", got)
	}
}

func TestWorkspaceQuotaNeverAffectsAnotherWorkspace(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.seedConversations(t, f.workspaceA, 100, f.clock.Now().Add(-time.Hour), false)
	f.receive(t, false, 1, "A is over")
	f.receive(t, true, 1, "B is fine")

	if got := f.usage(t, f.adminB).Usage; got.ConversationsThisMonth != 1 || got.HeldConversations != 0 {
		t.Fatalf("workspace B usage=%+v, want its own 1 conversation and none held", got)
	}
	if subjects := f.inboxSubjects(t, f.adminB, ""); len(subjects) != 1 || subjects[0] != "B is fine" {
		t.Fatalf("workspace B inbox=%v", subjects)
	}
	if got := f.usage(t, f.adminA).Usage; got.HeldConversations != 1 {
		t.Fatalf("workspace A usage=%+v, want 1 held", got)
	}
	// Upgrading A releases only A's held conversations.
	f.receive(t, true, 2, "B second")
	f.upgrade(t, f.workspaceB, "sub_b_only")
	if got := f.usage(t, f.adminA).Usage.HeldConversations; got != 1 {
		t.Fatalf("an upgrade of B released A's held conversations: held=%d", got)
	}
	// Agent cap is per workspace too.
	for number := 1; number <= 2; number++ {
		invite(t, f, f.adminA, number)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "b-teammate@limits-bravo.example.com", "role_ceiling": "agent"}, f.adminB, nil).Code; got != http.StatusCreated {
		t.Fatalf("workspace B invite status=%d, want 201 while A's seats are full", got)
	}
}

func TestHeldConversationsStayOutOfTheContactView(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.seedConversations(t, f.workspaceA, 100, f.clock.Now().Add(-time.Hour), false)
	f.receive(t, false, 7, "Hidden from contact")
	contactID := queryString(t, f.pool, `SELECT id::text FROM contacts WHERE email='customer7@customer.example'`)
	var contact struct {
		Conversations []struct{ Subject string } `json:"conversations"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/contacts/"+contactID, nil, f.adminA, nil), &contact)
	if len(contact.Conversations) != 0 {
		t.Fatalf("contact view lists held conversations: %+v", contact.Conversations)
	}
}

func TestConcurrentInvitesAndDeliveriesNeverPassTheLimits(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.seedConversations(t, f.workspaceA, 98, f.clock.Now().Add(-time.Hour), false)
	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for number := 1; number <= 8; number++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- invite(t, f, f.adminA, number).Code
		}()
	}
	for number := 1; number <= 6; number++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.receive(t, false, number, fmt.Sprintf("Race %d", number))
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	for code := range statuses {
		if code == http.StatusCreated {
			accepted++
		}
	}
	if accepted != 2 {
		t.Fatalf("%d invites accepted on a free workspace with 1 agent, want exactly 2", accepted)
	}
	if got := f.usage(t, f.adminA).Usage; got.ConversationsThisMonth != 104 || got.HeldConversations != 4 {
		t.Fatalf("usage=%+v, want 104 received and exactly 4 held (98 seeded + 2 within the quota)", got)
	}
}

// A slow upload of one delivery must not queue the other new conversations of the workspace, or a
// few large attachments would time out the mail webhook; the quota still comes out exact.
func TestSlowUploadDoesNotBlockOtherNewConversationsAndQuotaStaysExact(t *testing.T) {
	gate := &gatedStorage{entered: make(chan struct{}), release: make(chan struct{})}
	f := newLimitsFixtureWithStorage(t, true, func(inner attachments.Storage) attachments.Storage {
		gate.Storage = inner
		return gate
	})
	f.seedConversations(t, f.workspaceA, 99, f.clock.Now().Add(-time.Hour), false)

	firstDone := make(chan inboundResult, 1)
	go func() { firstDone <- f.receive(t, false, 1, "Slow upload") }()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first delivery never reached attachment storage")
	}
	secondDone := make(chan inboundResult, 1)
	go func() { secondDone <- f.receive(t, false, 2, "Quick one") }()
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		close(gate.release)
		t.Fatal("a delivery waited for another delivery's upload")
	}
	close(gate.release)
	<-firstDone

	// 99 seeded + the quick one (100th, visible) + the slow one (101st, held).
	if subjects := f.inboxSubjects(t, f.adminA, "&search=Quick+one"); len(subjects) != 1 {
		t.Fatalf("the delivery inside the quota is hidden: %v", subjects)
	}
	if got := f.usage(t, f.adminA).Usage; got.ConversationsThisMonth != 101 || got.HeldConversations != 1 {
		t.Fatalf("usage=%+v, want 101 received and exactly 1 held", got)
	}
}

func TestReinvitingAnExistingAgentOrResendingAPendingInviteTakesNoSeat(t *testing.T) {
	f := newLimitsFixture(t, true)
	// Seat 2 is an agent, seat 3 a pending invite: the workspace is at the cap of 3.
	_, agentURL := createContractInvite(t, f.api, f.adminA, "agent@limits-alpha.example.com", "agent")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentURL)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem status=%d", got)
	}
	if got := invite(t, f, f.adminA, 1); got.Code != http.StatusCreated {
		t.Fatalf("pending invite status=%d", got.Code)
	}

	again := func(email string) int {
		return request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
			map[string]any{"email": email, "role_ceiling": "admin"}, f.adminA, nil).Code
	}
	if got := again("agent@limits-alpha.example.com"); got != http.StatusCreated {
		t.Fatalf("role change for an existing agent at the cap status=%d, want 201", got)
	}
	if got := again("Teammate1@limits-alpha.example.com"); got != http.StatusCreated {
		t.Fatalf("re-sending a pending invite status=%d, want 201 (same person, same seat)", got)
	}
	if got := f.usage(t, f.adminA).Usage; got.Agents != 2 || got.PendingInvites != 1 {
		t.Fatalf("usage=%+v, want 2 agents and 1 pending invite (one person, not one per link)", got)
	}
	if got := again("someone-new@limits-alpha.example.com"); got != http.StatusPaymentRequired {
		t.Fatalf("a genuinely new person at the cap status=%d, want 402", got)
	}
}

func TestPaidPlanLimitMessageSaysToContactUsInsteadOfUpgrade(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_message")
	for number := 1; number <= 9; number++ {
		invite(t, f, f.adminA, number)
	}
	refused := invite(t, f, f.adminA, 10)
	if refused.Code != http.StatusPaymentRequired || !strings.Contains(refused.Body, "contact us for more than 10 agents") ||
		strings.Contains(strings.ToLower(refused.Body), "upgrade") {
		t.Fatalf("status=%d body=%s, want a contact-us message without an upgrade offer", refused.Code, refused.Body)
	}
}

// Creating a conversation through the API takes the same workspace quota lock as inbound mail, so
// the count a delivery reads never misses a conversation that is being written.
func TestCreatingAConversationWaitsForTheQuotaLock(t *testing.T) {
	f := newLimitsFixture(t, true)
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Lock", "email": "lock@customer.example"})
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "conversation-quota:"+f.workspaceA); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		done <- request(t, f.api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Locked"}, f.adminA, nil).Code
	}()
	select {
	case code := <-done:
		t.Fatalf("conversation created (status %d) while another writer held the quota lock", code)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != http.StatusCreated {
			t.Fatalf("status=%d after the lock was released, want 201", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conversation creation never finished")
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
