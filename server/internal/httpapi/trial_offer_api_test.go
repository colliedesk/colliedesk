package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type trialOfferView struct {
	ID          string   `json:"id"`
	State       string   `json:"state"`
	Workspace   string   `json:"workspace_name"`
	ExpiresAt   string   `json:"expires_at"`
	AcceptedAt  *string  `json:"accepted_at"`
	AcceptedBy  *string  `json:"accepted_by"`
	TrialEndsAt string   `json:"trial_ends_at"`
	Plan        string   `json:"plan"`
	ExtendsTo   string   `json:"extends_to"`
	CanAccept   bool     `json:"can_accept"`
	Refusal     *string  `json:"refusal"`
	AcceptBy    []string `json:"accept_by"`
}

type trialOfferAcceptedView struct {
	Offer               trialOfferView `json:"offer"`
	PreviousTrialEndsAt string         `json:"previous_trial_ends_at"`
	TrialEndsAt         string         `json:"trial_ends_at"`
	Extended            bool           `json:"extended"`
}

type trialOfferFixture struct {
	limitsFixture
	funnel *recordingFunnel
}

func newTrialOfferFixture(t *testing.T) trialOfferFixture {
	t.Helper()
	funnel := &recordingFunnel{}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.Funnel = funnel })
	return trialOfferFixture{limitsFixture: f, funnel: funnel}
}

// offer makes an offer for the workspace as the operator command does, at the fixture's clock.
func (f trialOfferFixture) offer(t *testing.T, workspace string, valid time.Duration) (store.TrialOffer, string) {
	t.Helper()
	offer, token, err := store.New(f.pool).CreateTrialOffer(context.Background(), workspace, "ops", f.clock.Now(), valid)
	if err != nil {
		t.Fatal(err)
	}
	return offer, token
}

func (f trialOfferFixture) read(t *testing.T, session, token string) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/trial-offer?token="+url.QueryEscape(token), nil, session, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func (f trialOfferFixture) accept(t *testing.T, session, token string) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/trial-offer/accept", map[string]any{"token": token}, session, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func (f trialOfferFixture) trialEndOf(t *testing.T, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT to_char(trial_ends_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') FROM workspaces WHERE id=$1`, workspace)
}

func decodeView[T any](t *testing.T, got *responseView, status int, into *T) {
	t.Helper()
	if got.Code != status {
		t.Fatalf("status=%d body=%s, want %d", got.Code, got.Body, status)
	}
	if err := json.Unmarshal([]byte(got.Body), into); err != nil {
		t.Fatalf("decode %s: %v", got.Body, err)
	}
}

func wantRefused(t *testing.T, got *responseView, status int, code string) {
	t.Helper()
	if got.Code != status || !strings.Contains(got.Body, `"code":"`+code+`"`) {
		t.Fatalf("status=%d body=%s, want %d %s", got.Code, got.Body, status, code)
	}
}

func TestTrialOfferReadChangesNothingAndAcceptReopensALockedWorkspace(t *testing.T) {
	f := newTrialOfferFixture(t)
	f.clock.set(time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC))
	offer, token := f.offer(t, f.workspaceA, 30*24*time.Hour)

	var read trialOfferView
	decodeView(t, f.read(t, f.adminA, token), http.StatusOK, &read)
	if read.ID != offer.ID || read.State != "open" || read.Workspace != "Limits Alpha" || read.Plan != "none" || !read.CanAccept || read.Refusal != nil ||
		read.TrialEndsAt != "2026-10-15T09:00:00Z" || read.ExpiresAt != "2026-11-19T09:00:00Z" || read.ExtendsTo != "2026-11-03T09:00:00Z" {
		t.Fatalf("read of an open offer in a locked workspace = %+v", read)
	}
	if len(read.AcceptBy) != 1 || read.AcceptBy[0] != "admin@limits-alpha.example.com" {
		t.Fatalf("accept_by = %v, want the admin", read.AcceptBy)
	}
	// Reading twice, as a mail scanner and then the person, changes nothing.
	f.read(t, f.adminA, token)
	if end := f.trialEndOf(t, f.workspaceA); end != "2026-10-15T09:00:00Z" {
		t.Fatalf("reading moved the trial end to %s", end)
	}
	wantLocked(t, f.postNote(t, f.adminA, "00000000-0000-0000-0000-000000000001"), "a write before accepting")

	f.clock.set(time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC))
	var accepted trialOfferAcceptedView
	decodeView(t, f.accept(t, f.adminA, token), http.StatusOK, &accepted)
	if !accepted.Extended || accepted.TrialEndsAt != "2026-11-04T12:00:00Z" || accepted.PreviousTrialEndsAt != "2026-10-15T09:00:00Z" ||
		accepted.Offer.State != "accepted" || accepted.Offer.AcceptedBy == nil || *accepted.Offer.AcceptedBy != "admin@limits-alpha.example.com" ||
		accepted.Offer.AcceptedAt == nil || *accepted.Offer.AcceptedAt != "2026-10-21T12:00:00Z" || accepted.Offer.CanAccept {
		t.Fatalf("accept = %+v", accepted)
	}
	if end := f.trialEndOf(t, f.workspaceA); end != "2026-11-04T12:00:00Z" {
		t.Fatalf("trial end after accepting = %s", end)
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("after accepting the workspace is still closed: %d %s", response.Code, response.Body.String())
	}
	by := queryString(t, f.pool, `SELECT changed_by FROM workspace_trial_changes WHERE workspace_id=$1 AND new_ends_at='2026-11-04T12:00:00Z'`, f.workspaceA)
	if !strings.Contains(by, offer.ID) || !strings.Contains(by, "admin@limits-alpha.example.com") {
		t.Fatalf("audit changed_by = %q, want the offer id and who accepted", by)
	}
	if events := f.funnel.named("trial_offer_accepted"); len(events) != 1 || events[0].Workspace != f.workspaceA {
		t.Fatalf("funnel trial_offer_accepted = %+v, want one for workspace A", events)
	}
}

func TestTrialOfferNoLongerOpenIsRefusedAndSaysWhy(t *testing.T) {
	f := newTrialOfferFixture(t)
	f.clock.set(time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC))
	_, expiring := f.offer(t, f.workspaceA, 24*time.Hour)
	revokedOffer, revoked := f.offer(t, f.workspaceA, 30*24*time.Hour)
	if _, err := store.New(f.pool).RevokeTrialOffer(context.Background(), revokedOffer.ID, "ops", f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	_, spent := f.offer(t, f.workspaceA, 30*24*time.Hour)
	if got := f.accept(t, f.adminA, spent); got.Code != http.StatusOK {
		t.Fatalf("first accept: %d %s", got.Code, got.Body)
	}
	f.clock.set(time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC))
	before := f.trialEndOf(t, f.workspaceA)
	for _, c := range []struct{ token, state, code string }{
		{expiring, "expired", "trial_offer_expired"},
		{revoked, "revoked", "trial_offer_revoked"},
		{spent, "accepted", "trial_offer_accepted"},
	} {
		var read trialOfferView
		decodeView(t, f.read(t, f.adminA, c.token), http.StatusOK, &read)
		if read.State != c.state || read.CanAccept || read.Refusal == nil || *read.Refusal != c.state {
			t.Errorf("%s offer read = %+v", c.state, read)
		}
		got := f.accept(t, f.adminA, c.token)
		wantRefused(t, got, http.StatusConflict, c.code)
		if !strings.Contains(got.Body, `"trial_ends_at":"2026-11-03T09:00:00Z"`) || !strings.Contains(got.Body, "support@colliedesk.com") {
			t.Errorf("%s refusal does not give the trial end and support: %s", c.state, got.Body)
		}
	}
	if after := f.trialEndOf(t, f.workspaceA); after != before {
		t.Fatalf("a refused accept moved the trial end %s -> %s", before, after)
	}
}

func TestTrialOfferNeedsAnAdminAndNamesThem(t *testing.T) {
	f := newTrialOfferFixture(t)
	agent := joinWithRole(t, f.api, f.adminA, "agent@limits-alpha.example.com", "agent")
	_, token := f.offer(t, f.workspaceA, 30*24*time.Hour)
	var read trialOfferView
	decodeView(t, f.read(t, agent, token), http.StatusOK, &read)
	if read.CanAccept || read.Refusal == nil || *read.Refusal != "not_admin" || len(read.AcceptBy) != 1 || read.AcceptBy[0] != "admin@limits-alpha.example.com" {
		t.Fatalf("agent's read = %+v, want not_admin naming the admin", read)
	}
	wantRefused(t, f.accept(t, agent, token), http.StatusForbidden, "permission_missing")
	if end := f.trialEndOf(t, f.workspaceA); end != "2026-10-15T09:00:00Z" {
		t.Fatalf("an agent's accept moved the trial end to %s", end)
	}
}

func TestTrialOfferOfAnotherWorkspaceIsRefusedWithoutNamingIt(t *testing.T) {
	f := newTrialOfferFixture(t)
	_, token := f.offer(t, f.workspaceA, 30*24*time.Hour)
	for name, got := range map[string]*responseView{"read": f.read(t, f.adminB, token), "accept": f.accept(t, f.adminB, token)} {
		wantRefused(t, got, http.StatusForbidden, "trial_offer_other_workspace")
		if strings.Contains(got.Body, "Alpha") || strings.Contains(got.Body, "alpha") {
			t.Errorf("%s names the other workspace: %s", name, got.Body)
		}
	}
	for _, got := range []*responseView{f.read(t, f.adminA, token+"x"), f.accept(t, f.adminA, "nothing")} {
		wantRefused(t, got, http.StatusNotFound, "trial_offer_not_found")
	}
	if a, b := f.trialEndOf(t, f.workspaceA), f.trialEndOf(t, f.workspaceB); a != "2026-10-15T09:00:00Z" || b != "2026-10-15T09:00:00Z" {
		t.Fatalf("trial ends A=%s B=%s, want both unchanged", a, b)
	}
	var read trialOfferView
	decodeView(t, f.read(t, f.adminA, token), http.StatusOK, &read)
	if read.State != "open" {
		t.Fatalf("after workspace B tried, the offer is %s", read.State)
	}
}

func TestTrialOfferIsRefusedWhenTheWorkspaceHasAPlan(t *testing.T) {
	f := newTrialOfferFixture(t)
	f.upgrade(t, f.workspaceA, "sub_offer_plan")
	_, token := f.offer(t, f.workspaceA, 30*24*time.Hour)
	var read trialOfferView
	decodeView(t, f.read(t, f.adminA, token), http.StatusOK, &read)
	if read.Plan != "team" || read.CanAccept || read.Refusal == nil || *read.Refusal != "has_plan" {
		t.Fatalf("read with a plan = %+v", read)
	}
	wantRefused(t, f.accept(t, f.adminA, token), http.StatusConflict, "trial_offer_has_plan")
	if end := f.trialEndOf(t, f.workspaceA); end != "2026-10-15T09:00:00Z" {
		t.Fatalf("trial end changed to %s", end)
	}
	decodeView(t, f.read(t, f.adminA, token), http.StatusOK, &read)
	if read.State != "open" {
		t.Fatalf("a refused accept spent the offer: %s", read.State)
	}
}

func TestTrialOfferNeverShortensALaterTrialEnd(t *testing.T) {
	f := newTrialOfferFixture(t)
	later := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)
	if _, err := store.New(f.pool).SetTrialEnd(context.Background(), f.workspaceA, later, "ops", f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	_, token := f.offer(t, f.workspaceA, 30*24*time.Hour)
	var read trialOfferView
	decodeView(t, f.read(t, f.adminA, token), http.StatusOK, &read)
	if read.ExtendsTo != "2026-12-31T00:00:00Z" {
		t.Fatalf("extends_to = %s, want the later end kept", read.ExtendsTo)
	}
	var accepted trialOfferAcceptedView
	decodeView(t, f.accept(t, f.adminA, token), http.StatusOK, &accepted)
	if accepted.Extended || accepted.TrialEndsAt != "2026-12-31T00:00:00Z" || accepted.Offer.State != "accepted" {
		t.Fatalf("accept with a later end = %+v", accepted)
	}
	if end := f.trialEndOf(t, f.workspaceA); end != "2026-12-31T00:00:00Z" {
		t.Fatalf("trial end = %s, want 2026-12-31T00:00:00Z kept", end)
	}
}
