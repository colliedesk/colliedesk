package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

const migrationRequestPath = "/api/v1/billing/migration-request"

type migrationRequestView struct {
	FreshdeskAddress string `json:"freshdesk_address"`
	Times            string `json:"times"`
	RequestedAt      string `json:"requested_at"`
}

// newMigrationFixture is a billing fixture whose server can send mail through a recording relay.
func newMigrationFixture(t *testing.T) (billingFixture, *recordingMailer) {
	t.Helper()
	mailer := &recordingMailer{}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.example.com"
		config.AlertMailer = mailer
	})
	return f, mailer
}

func askForMigration(t *testing.T, f billingFixture, token string, body map[string]any, headers map[string]string) migrationResponse {
	t.Helper()
	req := newRequest(t, http.MethodPost, migrationRequestPath, body, token, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return migrationResponse{serve(f.api, req)}
}

type migrationResponse struct{ *httptest.ResponseRecorder }

func (r migrationResponse) fieldError(t *testing.T, field string) {
	t.Helper()
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s, want 422", r.Code, r.Body.String())
	}
	var body struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	decode(t, r.ResponseRecorder, &body)
	if body.Error.Code != "validation_failed" || body.Error.Fields[field] == "" {
		t.Fatalf("error=%+v, want validation_failed with fields.%s", body.Error, field)
	}
}

func billingMigrationRequestedAt(t *testing.T, f billingFixture, token string) *time.Time {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/billing", nil, token, nil)
	var view struct {
		MigrationRequestedAt *time.Time `json:"migration_requested_at"`
	}
	decode(t, response, &view)
	if !strings.Contains(response.Body.String(), `"migration_requested_at"`) {
		t.Fatalf("GET /billing has no migration_requested_at: %s", response.Body.String())
	}
	return view.MigrationRequestedAt
}

func TestAdminAsksForAMigrationAndSupportGetsTheDetails(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	patch := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Ada Admin"}, f.adminA, nil)
	if patch.Code != http.StatusOK {
		t.Fatalf("set display name: %d %s", patch.Code, patch.Body.String())
	}
	f.mustDeliver(t, "msg_founding", subscription{ID: "sub_mig", Workspace: f.workspaceA, Product: productFounding, Status: "active",
		PeriodEnd: time.Now().Add(30 * 24 * time.Hour), ModifiedAt: time.Now()}.event("subscription.active"))
	if got := billingMigrationRequestedAt(t, f, f.adminA); got != nil {
		t.Fatalf("migration_requested_at=%v before any request, want null", got)
	}

	req := newRequest(t, http.MethodPost, migrationRequestPath, map[string]any{"freshdesk_address": "https://Acme.Freshdesk.com/helpdesk", "times": "Tue 10:00-12:00 UTC"}, f.adminA, nil)
	response := serve(f.api, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, migrationRequestPath, map[string]any{"freshdesk_address": "https://Acme.Freshdesk.com/helpdesk", "times": "Tue 10:00-12:00 UTC"}, f.adminA, nil), response)
	var answer migrationRequestView
	decode(t, response, &answer)
	if answer.FreshdeskAddress != "acme.freshdesk.com" || answer.Times != "Tue 10:00-12:00 UTC" || answer.RequestedAt == "" {
		t.Fatalf("answer=%+v", answer)
	}

	sent := mailer.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}
	if sent[0].From != "noreply@mail.example.com" || len(sent[0].To) != 1 || sent[0].To[0] != "support@colliedesk.com" {
		t.Fatalf("from=%q to=%v", sent[0].From, sent[0].To)
	}
	message, err := netmail.ReadMessage(strings.NewReader(sent[0].Message))
	if err != nil {
		t.Fatal(err)
	}
	if got := message.Header.Get("Subject"); got != "Freshdesk migration request: Billing Alpha" {
		t.Errorf("subject=%q", got)
	}
	if got := message.Header.Get("Reply-To"); !strings.Contains(got, "admin@billing-alpha.example.com") {
		t.Errorf("Reply-To=%q, want the admin's address", got)
	}
	raw := sent[0].Message
	for _, want := range []string{"Billing Alpha", "billing-alpha", f.workspaceA, "founding", "acme.freshdesk.com", "Tue 10:00-12:00 UTC", "Ada Admin", "admin@billing-alpha.example.com", answer.RequestedAt} {
		if !strings.Contains(raw, want) {
			t.Errorf("mail lacks %q:\n%s", want, raw)
		}
	}

	got := billingMigrationRequestedAt(t, f, f.adminA)
	asked, err := time.Parse(time.RFC3339, answer.RequestedAt)
	if err != nil || got == nil || !got.Equal(asked) {
		t.Fatalf("migration_requested_at=%v, want %s", got, answer.RequestedAt)
	}
}

func TestMigrationRequestFromANonAdminOrAnAgentTokenSendsNothingUntilApproved(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	body := map[string]any{"freshdesk_address": "acme.freshdesk.com"}
	if got := askForMigration(t, f, f.agentTokenA, body, nil).Code; got != http.StatusForbidden {
		t.Fatalf("a team agent: status=%d, want 403", got)
	}
	token := issueToken(t, f.api, f.adminA, "billing helper", "admin").Token
	created := accepted(t, f.api, http.MethodPost, migrationRequestPath, body, token)
	if len(mailer.all()) != 0 || billingMigrationRequestedAt(t, f, f.adminA) != nil {
		t.Fatal("the agent's call sent mail or stored a request before a person approved")
	}
	approve := request(t, f.api, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/approve", nil, f.adminA, nil)
	if approve.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approve.Code, approve.Body.String())
	}
	if len(mailer.all()) != 1 || billingMigrationRequestedAt(t, f, f.adminA) == nil {
		t.Fatalf("approval sent %d mails, want 1 and a stored request", len(mailer.all()))
	}
}

func TestMigrationRequestNormalizesTheFreshdeskAddress(t *testing.T) {
	for _, address := range []string{"https://Acme.Freshdesk.com/helpdesk", "  acme.freshdesk.com ", "ACME.FRESHDESK.COM", "http://acme.freshdesk.com/a/b?x=1", "acme.freshdesk.com/"} {
		f, mailer := newMigrationFixture(t)
		response := askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": address}, nil)
		var answer migrationRequestView
		decode(t, response.ResponseRecorder, &answer)
		if response.Code != http.StatusOK || answer.FreshdeskAddress != "acme.freshdesk.com" || len(mailer.all()) != 1 {
			t.Errorf("%q: status=%d answer=%+v mails=%d, want 200 acme.freshdesk.com and one mail", address, response.Code, answer, len(mailer.all()))
		}
	}
}

func TestMigrationRequestRefusesAnythingButOneFreshdeskHost(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	bad := []string{"freshdesk.com", "acme.example.com", "evil.com/acme.freshdesk.com", "acme.freshdesk.com.evil.com", "user:pw@acme.freshdesk.com",
		"acme.freshdesk.com:8080", "10.0.0.1", "-a.freshdesk.com", "a-.freshdesk.com", "a..freshdesk.com", "a.b.freshdesk.com", "", "   ",
		strings.Repeat("a", 300) + ".freshdesk.com", strings.Repeat("a", 64) + ".freshdesk.com", "[::1]", "ac me.freshdesk.com", "https://"}
	for _, address := range bad {
		askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": address}, nil).fieldError(t, "freshdesk_address")
	}
	askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "acme.freshdesk.com", "times": strings.Repeat("x", 501)}, nil).fieldError(t, "times")
	if len(mailer.all()) != 0 || billingMigrationRequestedAt(t, f, f.adminA) != nil {
		t.Fatal("a refused request sent mail or stored a row")
	}
	ok := askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "acme.freshdesk.com", "times": strings.Repeat("x", 500)}, nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("500 characters of times: status=%d %s", ok.Code, ok.Body.String())
	}
}

func TestMigrationRequestCannotInjectAMailHeaderOrLine(t *testing.T) {
	mailer := &recordingMailer{}
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Billing = fake.config()
		config.Email.MailFromDomain = "mail.example.com"
		config.AlertMailer = mailer
	})
	admin := createSession(t, api, service, "Evil\r\nBcc: victim@evil.example", "admin@injection.example.com")
	f := billingFixture{api: api, pool: pool, adminA: admin}
	response := askForMigration(t, f, admin, map[string]any{"freshdesk_address": "acme.freshdesk.com", "times": "noon\r\nBcc: victim@evil.example\x00"}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d %s", response.Code, response.Body.String())
	}
	sent := mailer.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails", len(sent))
	}
	head, body, _ := strings.Cut(sent[0].Message, "\r\n\r\n")
	if strings.Contains(strings.ToLower(head), "\r\nbcc:") {
		t.Fatalf("a header was injected:\n%s", head)
	}
	if strings.Contains(body, "\nBcc: victim@evil.example") || strings.Contains(sent[0].Message, "\x00") {
		t.Fatalf("a line was injected into the body:\n%s", body)
	}
}

func TestMigrationRequestsAreLimitedToFiveAnHourPerWorkspace(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	body := map[string]any{"freshdesk_address": "acme.freshdesk.com"}
	for i := range 5 {
		if got := askForMigration(t, f, f.adminA, body, nil).Code; got != http.StatusOK {
			t.Fatalf("request %d: status=%d, want 200", i+1, got)
		}
		askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "nope.example.com"}, nil).fieldError(t, "freshdesk_address")
	}
	sixth := askForMigration(t, f, f.adminA, body, nil)
	if sixth.Code != http.StatusTooManyRequests || sixth.Header().Get("Retry-After") == "" || !strings.Contains(sixth.Body.String(), "rate_limited") {
		t.Fatalf("sixth: status=%d retry-after=%q body=%s, want 429 rate_limited with Retry-After", sixth.Code, sixth.Header().Get("Retry-After"), sixth.Body.String())
	}
	if len(mailer.all()) != 5 {
		t.Fatalf("sent %d mails, want 5", len(mailer.all()))
	}
	if got := askForMigration(t, f, f.adminB, body, nil).Code; got != http.StatusOK {
		t.Fatalf("another workspace: status=%d, want 200", got)
	}
}

func TestMigrationRequestWithAnIdempotencyKeySendsOneMail(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	body := map[string]any{"freshdesk_address": "acme.freshdesk.com"}
	headers := map[string]string{"Idempotency-Key": "press-1"}
	first := askForMigration(t, f, f.adminA, body, headers)
	second := askForMigration(t, f, f.adminA, body, headers)
	var firstView, secondView migrationRequestView
	decode(t, first.ResponseRecorder, &firstView)
	decode(t, second.ResponseRecorder, &secondView)
	if first.Code != http.StatusOK || second.Code != http.StatusOK || firstView != secondView || firstView.RequestedAt == "" {
		t.Fatalf("first=%d %s second=%d %s, want the same 200 answer", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if len(mailer.all()) != 1 {
		t.Fatalf("sent %d mails for one press, want 1", len(mailer.all()))
	}
	other := askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "other.freshdesk.com"}, headers)
	if other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), "idempotency_key_reused") {
		t.Fatalf("same key, other input: status=%d %s", other.Code, other.Body.String())
	}
}

func TestMigrationRequestStoresNothingWhenTheMailCannotBeSent(t *testing.T) {
	f, mailer := newMigrationFixture(t)
	mailer.setFail(true)
	failed := askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "acme.freshdesk.com"}, nil)
	if failed.Code != http.StatusBadGateway || !strings.Contains(failed.Body.String(), "mail_failed") {
		t.Fatalf("status=%d %s, want 502 mail_failed", failed.Code, failed.Body.String())
	}
	if billingMigrationRequestedAt(t, f, f.adminA) != nil || rowCount(t, f.pool, "workspace_migration_requests") != 0 {
		t.Fatal("a failed send stored a request")
	}

	bare := newBillingFixture(t)
	unavailable := askForMigration(t, bare, bare.adminA, map[string]any{"freshdesk_address": "acme.freshdesk.com"}, nil)
	if unavailable.Code != http.StatusServiceUnavailable || !strings.Contains(unavailable.Body.String(), "mail_unavailable") {
		t.Fatalf("no mailer: status=%d %s, want 503 mail_unavailable", unavailable.Code, unavailable.Body.String())
	}
	if rowCount(t, bare.pool, "workspace_migration_requests") != 0 {
		t.Fatal("an unavailable mailer stored a request")
	}
}

func TestAMigrationRequestOfOneWorkspaceIsInvisibleToAnother(t *testing.T) {
	f, _ := newMigrationFixture(t)
	if got := askForMigration(t, f, f.adminA, map[string]any{"freshdesk_address": "acme.freshdesk.com"}, nil).Code; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
	if billingMigrationRequestedAt(t, f, f.adminA) == nil {
		t.Fatal("workspace A does not see its own request")
	}
	if got := billingMigrationRequestedAt(t, f, f.adminB); got != nil {
		t.Fatalf("workspace B sees migration_requested_at=%v", got)
	}

	ctx := context.Background()
	var role string
	if err := f.pool.QueryRow(ctx, `SELECT roles[1]::text FROM pg_policies WHERE schemaname=current_schema() AND tablename='workspace_migration_requests' AND policyname='workspace_isolation'`).Scan(&role); err != nil {
		t.Fatalf("workspace_migration_requests has no workspace_isolation policy: %v", err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true), set_config('app.workspace_id',$2,true)`, role, f.workspaceB); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_migration_requests WHERE workspace_id=$1`, f.workspaceA).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("scoped to B, %d migration requests of A are visible, want 0", visible)
	}
}

func TestBillingSwitchedOffStillReportsAnEmptyMigrationRequest(t *testing.T) {
	f := newBillingFixtureWith(t, func(config *httpapi.Config) {
		config.SwitchedOff = map[string]bool{serverconfig.FeatureBilling: true}
	})
	response := request(t, f.api, http.MethodGet, "/api/v1/billing", nil, f.adminA, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"migration_requested_at":null`) {
		t.Fatalf("status=%d body=%s, want migration_requested_at null", response.Code, response.Body.String())
	}
}
