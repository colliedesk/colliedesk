package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

func activeTeam(workspace, id string, now time.Time) subscription {
	return subscription{ID: id, Workspace: workspace, Product: productMonthly, Status: "active",
		PeriodEnd: now.Add(30 * 24 * time.Hour), ModifiedAt: now}
}

func TestPolarWebhookRejectsBadSignaturesAndWritesNothing(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	body := activeTeam(f.workspaceA, "sub_bad", now).event("subscription.active")
	stamp := now.Unix()
	cases := map[string]*http.Request{
		"no signature headers": func() *http.Request {
			req := webhookRequest("", stamp, "", body)
			req.Header.Del("webhook-timestamp")
			return req
		}(),
		"no signature":               webhookRequest("msg_1", stamp, "", body),
		"wrong signature":            webhookRequest("msg_2", stamp, "v1,"+"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", body),
		"signature for another body": webhookRequest("msg_3", stamp, sign("msg_3", stamp, []byte(`{}`)), body),
		"signed with another id":     webhookRequest("msg_4", stamp, sign("msg_other", stamp, body), body),
		"stale timestamp":            webhookRequest("msg_5", stamp-3600, sign("msg_5", stamp-3600, body), body),
		"future timestamp":           webhookRequest("msg_6", stamp+3600, sign("msg_6", stamp+3600, body), body),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if response := serve(f.api, req); response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s, want 401", response.Code, response.Body.String())
			}
		})
	}
	if rows := rowCount(t, f.pool, "workspace_billing") + rowCount(t, f.pool, "billing_webhook_events"); rows != 0 {
		t.Fatalf("rejected webhooks wrote %d rows", rows)
	}
	if got := getBilling(t, f.api, f.adminA); got.Plan != "free" {
		t.Fatalf("plan=%q after rejected webhooks, want free", got.Plan)
	}
}

func TestPolarWebhookActivatesTheWorkspaceNamedByExternalID(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	sub := activeTeam(f.workspaceA, "sub_a", now)
	f.mustDeliver(t, "msg_activate", sub.event("subscription.active"))

	got := getBilling(t, f.api, f.adminA)
	if got.Plan != "team" || got.Status != "active" || got.CancelAtPeriodEnd {
		t.Fatalf("billing=%+v, want team/active", got)
	}
	if got.CurrentPeriodEnd == nil || !got.CurrentPeriodEnd.Equal(sub.PeriodEnd) {
		t.Fatalf("period end=%v, want %v", got.CurrentPeriodEnd, sub.PeriodEnd)
	}
	if got.Limits.Agents != 10 || got.Limits.ConversationsPerMonth != nil {
		t.Fatalf("limits=%+v, want 10 agents and no conversation cap", got.Limits)
	}
}

func TestPolarWebhookMapsProductsToPlans(t *testing.T) {
	for product, plan := range map[string]string{productMonthly: "team", productYearly: "team", productFounding: "founding"} {
		t.Run(plan+" "+product, func(t *testing.T) {
			f := newBillingFixture(t)
			now := time.Now()
			sub := activeTeam(f.workspaceA, "sub_"+product, now)
			sub.Product = product
			f.mustDeliver(t, "msg_"+product, sub.event("subscription.active"))
			if got := getBilling(t, f.api, f.adminA); got.Plan != plan || got.Limits.Agents != 10 {
				t.Fatalf("billing=%+v, want %s with 10 agents", got, plan)
			}
		})
	}
}

func TestPolarWebhookStatusMapping(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	past, future := now.Add(-48*time.Hour), now.Add(10*24*time.Hour)
	cases := []struct {
		name       string
		event      string
		status     string
		periodEnd  time.Time
		cancelEnd  bool
		endedAt    *time.Time
		wantPlan   string
		wantStatus string
	}{
		{"active", "subscription.active", "active", future, false, nil, "team", "active"},
		{"trialing", "subscription.created", "trialing", future, false, nil, "team", "trialing"},
		{"past due stays paid and is flagged", "subscription.past_due", "past_due", future, false, nil, "team", "past_due"},
		{"cancelled keeps access until the period ends", "subscription.canceled", "canceled", future, true, nil, "team", "canceled"},
		{"cancelled after the period ended", "subscription.canceled", "canceled", past, true, nil, "free", "canceled"},
		{"revoked", "subscription.revoked", "canceled", future, false, &past, "free", "canceled"},
		{"unpaid", "subscription.updated", "unpaid", future, false, nil, "free", "unpaid"},
		{"incomplete", "subscription.created", "incomplete", future, false, nil, "free", "incomplete"},
		{"incomplete expired", "subscription.updated", "incomplete_expired", future, false, nil, "free", "incomplete_expired"},
		{"paused", "subscription.updated", "paused", future, false, nil, "free", "paused"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			f := newBillingFixture(t)
			sub := subscription{ID: "sub_map", Workspace: f.workspaceA, Product: productMonthly, Status: item.status,
				PeriodEnd: item.periodEnd, CancelAtPeriodEnd: item.cancelEnd, EndedAt: item.endedAt, ModifiedAt: now}
			f.mustDeliver(t, "msg_map", sub.event(item.event))
			got := getBilling(t, f.api, f.adminA)
			if got.Plan != item.wantPlan || got.Status != item.wantStatus || got.CancelAtPeriodEnd != item.cancelEnd {
				t.Fatalf("billing=%+v, want plan=%s status=%s cancel_at_period_end=%v", got, item.wantPlan, item.wantStatus, item.cancelEnd)
			}
			if item.wantPlan == "free" && (got.Limits.Agents != 3 || got.Limits.ConversationsPerMonth == nil || *got.Limits.ConversationsPerMonth != 100) {
				t.Fatalf("free limits=%+v, want 3 agents and 100 conversations a month", got.Limits)
			}
		})
	}
}

func TestPolarWebhookReplayDoesNotWriteTwice(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	f.mustDeliver(t, "msg_replay", activeTeam(f.workspaceA, "sub_r", now).event("subscription.active"))

	// The same webhook-id carrying a different, validly signed body must not be applied.
	cancelled := activeTeam(f.workspaceA, "sub_r", now.Add(time.Minute))
	cancelled.Status, cancelled.PeriodEnd = "canceled", now.Add(-time.Hour)
	response := f.deliver(t, "msg_replay", cancelled.event("subscription.canceled"))
	if response.Code != http.StatusOK {
		t.Fatalf("replay status=%d, want 2xx so Polar stops retrying", response.Code)
	}
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" || got.Status != "active" {
		t.Fatalf("billing=%+v after replay, want the first write kept", got)
	}
	if events := rowCount(t, f.pool, "billing_webhook_events"); events != 1 {
		t.Fatalf("webhook event rows=%d, want 1", events)
	}
}

func TestPolarWebhookNeverCrossesWorkspaces(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	f.mustDeliver(t, "msg_cross", activeTeam(f.workspaceA, "sub_x", now).event("subscription.active"))

	if got := getBilling(t, f.api, f.adminB); got.Plan != "free" || got.Status != "none" || got.CurrentPeriodEnd != nil {
		t.Fatalf("workspace B sees billing=%+v from an event for A", got)
	}
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" {
		t.Fatalf("workspace A plan=%q, want team", got.Plan)
	}
	var owner string
	if err := f.pool.QueryRow(t.Context(), `SELECT workspace_id::text FROM workspace_billing`).Scan(&owner); err != nil || owner != f.workspaceA {
		t.Fatalf("billing row owner=%q err=%v, want workspace A", owner, err)
	}

	// The same webhook-id used for B is a different event: dedup is per workspace.
	f.mustDeliver(t, "msg_cross", activeTeam(f.workspaceB, "sub_y", now).event("subscription.active"))
	if got := getBilling(t, f.api, f.adminB); got.Plan != "team" {
		t.Fatalf("workspace B plan=%q, want team after its own event", got.Plan)
	}
}

func TestPolarWebhookForUnknownWorkspaceCreatesNothing(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	for name, workspace := range map[string]string{"unknown id": "6f0f3b0e-2d1c-4a55-9d2a-0c5e8b7a1f33", "not a uuid": "workspace-one", "no external id": ""} {
		t.Run(name, func(t *testing.T) {
			response := f.deliver(t, "msg_unknown_"+name, activeTeam(workspace, "sub_u", now).event("subscription.active"))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s, want 2xx", response.Code, response.Body.String())
			}
		})
	}
	if rows := rowCount(t, f.pool, "workspace_billing") + rowCount(t, f.pool, "billing_webhook_events"); rows != 0 {
		t.Fatalf("unknown workspaces created %d rows", rows)
	}
}

func TestPolarWebhookIgnoresUnknownProductsAndOtherEvents(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	other := activeTeam(f.workspaceA, "sub_other", now)
	other.Product = "prod-not-ours"
	f.mustDeliver(t, "msg_product", other.event("subscription.active"))
	f.mustDeliver(t, "msg_checkout", []byte(`{"type":"checkout.created","timestamp":"2026-10-15T00:00:00Z","data":{"id":"co_1"}}`))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "free" || got.Status != "none" {
		t.Fatalf("billing=%+v, want untouched free", got)
	}
	if rows := rowCount(t, f.pool, "workspace_billing"); rows != 0 {
		t.Fatalf("workspace_billing rows=%d, want 0", rows)
	}
}

func TestPolarWebhookRejectsAMalformedSignedBody(t *testing.T) {
	f := newBillingFixture(t)
	if response := f.deliver(t, "msg_garbage", []byte(`not json`)); response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", response.Code)
	}
}

func TestPolarWebhookIgnoresOutOfOrderEvents(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	newer := activeTeam(f.workspaceA, "sub_o", now)
	f.mustDeliver(t, "msg_newer", newer.event("subscription.active"))

	// An older update for the same subscription arrives late and must not roll the state back.
	older := activeTeam(f.workspaceA, "sub_o", now.Add(-time.Hour))
	older.Status, older.CancelAtPeriodEnd = "past_due", true
	f.mustDeliver(t, "msg_older", older.event("subscription.past_due"))
	if got := getBilling(t, f.api, f.adminA); got.Status != "active" || got.CancelAtPeriodEnd {
		t.Fatalf("billing=%+v, want the newer state kept", got)
	}

	later := activeTeam(f.workspaceA, "sub_o", now.Add(time.Hour))
	later.CancelAtPeriodEnd = true
	f.mustDeliver(t, "msg_later", later.event("subscription.updated"))
	if got := getBilling(t, f.api, f.adminA); !got.CancelAtPeriodEnd {
		t.Fatalf("billing=%+v, want the later update applied", got)
	}
}

func TestPolarWebhookOldSubscriptionDoesNotEndANewerPaidOne(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	f.mustDeliver(t, "msg_new_sub", activeTeam(f.workspaceA, "sub_new", now).event("subscription.active"))

	old := subscription{ID: "sub_old", Workspace: f.workspaceA, Product: productMonthly, Status: "canceled",
		PeriodEnd: now.Add(-24 * time.Hour), ModifiedAt: now.Add(time.Minute)}
	ended := now.Add(-24 * time.Hour)
	old.EndedAt = &ended
	f.mustDeliver(t, "msg_old_sub", old.event("subscription.revoked"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" || got.Status != "active" {
		t.Fatalf("billing=%+v, want the active subscription kept", got)
	}

	// Once the current subscription ends, a new subscription replaces the record.
	f.mustDeliver(t, "msg_end_new", subscription{ID: "sub_new", Workspace: f.workspaceA, Product: productMonthly, Status: "canceled",
		PeriodEnd: now.Add(-time.Hour), ModifiedAt: now.Add(2 * time.Minute), EndedAt: &ended}.event("subscription.revoked"))
	f.mustDeliver(t, "msg_resub", subscription{ID: "sub_again", Workspace: f.workspaceA, Product: productYearly, Status: "active",
		PeriodEnd: now.Add(365 * 24 * time.Hour), ModifiedAt: now.Add(3 * time.Minute)}.event("subscription.active"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" || got.Status != "active" {
		t.Fatalf("billing=%+v, want the new subscription", got)
	}
}
