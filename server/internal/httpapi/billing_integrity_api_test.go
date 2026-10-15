package httpapi_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two checkouts can leave two subscriptions. Cancelling the spare must not take the paid plan from
// the workspace while the other one still charges.
func TestCancellingASpareSubscriptionKeepsThePaidPlan(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	spare := activeTeam(f.workspaceA, "sub_spare", now)
	f.mustDeliver(t, "msg_spare", spare.event("subscription.active"))
	kept := activeTeam(f.workspaceA, "sub_kept", now.Add(time.Minute))
	f.mustDeliver(t, "msg_kept", kept.event("subscription.active"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" || got.ActiveSubscriptions != 2 {
		t.Fatalf("billing=%+v, want team with 2 active subscriptions so an admin can see the duplicate", got)
	}

	// The spare is cancelled: still paid until its period ends, and newer than the other one.
	spare.Status, spare.CancelAtPeriodEnd, spare.ModifiedAt = "canceled", true, now.Add(2*time.Minute)
	f.mustDeliver(t, "msg_spare_cancel", spare.event("subscription.canceled"))
	// Its period then ends and Polar revokes it.
	ended := now.Add(-time.Hour)
	spare.PeriodEnd, spare.EndedAt, spare.ModifiedAt = ended, &ended, now.Add(3*time.Minute)
	f.mustDeliver(t, "msg_spare_revoked", spare.event("subscription.revoked"))

	got := getBilling(t, f.api, f.adminA)
	if got.Plan != "team" || got.Status != "active" || got.ActiveSubscriptions != 1 {
		t.Fatalf("billing=%+v, want the workspace on team through the subscription that still charges", got)
	}
	kept.Status, kept.PeriodEnd, kept.EndedAt, kept.ModifiedAt = "canceled", ended, &ended, now.Add(4*time.Minute)
	f.mustDeliver(t, "msg_kept_revoked", kept.event("subscription.revoked"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "free" || got.ActiveSubscriptions != 0 {
		t.Fatalf("billing=%+v, want free once no subscription is left", got)
	}
}

// subscription.created (incomplete) and subscription.active for a new subscription can arrive at
// the same moment. Whatever the interleaving, the newer state wins.
func TestConcurrentFirstEventsLeaveTheNewestState(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	const workspaces = 24
	type pair struct {
		token           string
		created, active []byte
	}
	pairs := make([]pair, workspaces)
	for index := range pairs {
		token := createSession(t, f.api, f.service, fmt.Sprintf("Race %d", index), fmt.Sprintf("admin@race-%d.example.com", index))
		created := subscription{ID: "sub_race", Workspace: workspaceID(t, f.api, token), Product: productMonthly, Status: "incomplete",
			PeriodEnd: now.Add(time.Hour), ModifiedAt: now}
		active := created
		active.Status, active.ModifiedAt = "active", now.Add(time.Second)
		pairs[index] = pair{token, created.event("subscription.created"), active.event("subscription.active")}
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	for index, item := range pairs {
		for kind, body := range [][]byte{item.created, item.active} {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				if response := f.deliver(t, fmt.Sprintf("msg_race_%d_%d", index, kind), body); response.Code != http.StatusOK {
					t.Errorf("webhook status=%d body=%s", response.Code, response.Body.String())
				}
			}()
		}
	}
	close(start)
	group.Wait()
	for index, item := range pairs {
		if got := getBilling(t, f.api, item.token); got.Plan != "team" || got.Status != "active" {
			t.Errorf("workspace %d billing=%+v, want team/active: the older event overwrote the newer", index, got)
		}
	}
}

func TestPolarWebhookStoresAnUnknownFutureStatusAsNotPaid(t *testing.T) {
	f := newBillingFixture(t)
	sub := activeTeam(f.workspaceA, "sub_future", time.Now())
	sub.Status = "some_future_status"
	if response := f.deliver(t, "msg_future", sub.event("subscription.updated")); response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 2xx: a 5xx would make Polar retry forever", response.Code, response.Body.String())
	}
	if got := getBilling(t, f.api, f.adminA); got.Plan != "free" || got.Status != "unknown" {
		t.Fatalf("billing=%+v, want free with status unknown", got)
	}
}

func TestPolarWebhookLogsEventsItCannotAttachToAWorkspace(t *testing.T) {
	f := newBillingFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	now := time.Now()
	unknownProduct := activeTeam(f.workspaceA, "sub_odd_product", now)
	unknownProduct.Product = "prod-not-ours"
	f.mustDeliver(t, "msg_log_1", activeTeam("", "sub_no_external", now).event("subscription.active"))
	f.mustDeliver(t, "msg_log_2", unknownProduct.event("subscription.active"))
	f.mustDeliver(t, "msg_log_3", activeTeam("6f0f3b0e-2d1c-4a55-9d2a-0c5e8b7a1f33", "sub_no_workspace", now).event("subscription.active"))
	output := logs.String()
	for _, want := range []string{"sub_no_external", "sub_odd_product", "sub_no_workspace", "subscription.active", "level=WARN"} {
		if !strings.Contains(output, want) {
			t.Errorf("log lacks %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "@") {
		t.Errorf("log carries an email address:\n%s", output)
	}
}
