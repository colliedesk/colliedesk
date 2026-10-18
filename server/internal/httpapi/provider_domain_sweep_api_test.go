package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/providerdomain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newSweeper(pool *pgxpool.Pool, serverURL string, clock *time.Time) *providerdomain.Sweeper {
	client := &resend.Client{APIKey: fakeResendKey, Base: serverURL}
	return providerdomain.New(store.New(pool), client, providerdomain.Options{Now: func() time.Time { return *clock }})
}

func trashCount(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	return queryString(t, pool, `SELECT count(*)::text FROM email_provider_domain_trash`)
}

func TestUnusedProviderDomainsAreDeletedInTheBackgroundNeverInTheRequest(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	adminA := createSession(t, api, service, "Churn Alpha", "admin@churn-alpha.example.com")
	adminB := createSession(t, api, service, "Churn Beta", "admin@churn-beta.example.com")
	_ = connectEmail(t, api, adminB, "support@churn-beta-brand.example.com")
	prove(t, api, dns, adminB)
	_ = verify(t, api, fake, adminB)
	betaDomain := fake.idOf("churn-beta-brand.example.com")

	for i := range 6 {
		putAddress(t, api, adminA, fmt.Sprintf("support@churn-%d.example.com", i))
		prove(t, api, dns, adminA)
		_ = verify(t, api, fake, adminA)
	}
	putAddress(t, api, adminA, "support@churn-final.example.com")
	if deletes := fake.called("DELETE /domains/" + fake.idOf("churn-0.example.com")); deletes != 0 {
		t.Fatalf("a request deleted a provider domain itself")
	}
	if len(fake.live()) != 7 || trashCount(t, pool) != "6" {
		t.Fatalf("before the sweep: live=%d trash=%s, want 7 and 6", len(fake.live()), trashCount(t, pool))
	}

	clock := time.Now()
	sweeper := newSweeper(pool, server.URL, &clock)
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if live := fake.live(); len(live) != 1 || live[0] != betaDomain || trashCount(t, pool) != "0" {
		t.Fatalf("after the sweep: live=%v trash=%s, want only B's", live, trashCount(t, pool))
	}
	prove(t, api, dns, adminA)
	_ = verify(t, api, fake, adminA)
	if len(fake.live()) != 2 {
		t.Fatalf("the new domain after the last change: %v", fake.live())
	}

	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, adminA, nil); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", response.Code)
	}
	clock = time.Now().Add(time.Second)
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if live := fake.live(); len(live) != 1 || live[0] != betaDomain {
		t.Fatalf("disconnecting A must remove A's domains and only those: %v (B's is %s)", live, betaDomain)
	}
}

func TestAFailedProviderDeleteIsKeptAndRetriedAndAnAlreadyGoneDomainIsForgotten(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Retry Delete", "admin@retry-delete.example.com")
	_ = connectEmail(t, api, admin, "support@retry-one.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)
	first := fake.idOf("retry-one.example.com")
	putAddress(t, api, admin, "support@retry-two.example.com")

	clock := time.Now()
	sweeper := newSweeper(pool, server.URL, &clock)
	fake.set(func(f *fakeResend) { f.failDelete = true })
	_ = sweeper.Sweep(context.Background())
	if kept := queryString(t, pool, `SELECT provider_domain_id||' '||attempts||' '||COALESCE(last_error,'') FROM email_provider_domain_trash`); kept[:len(first)+3] != first+" 1 " || len(kept) <= len(first)+3 {
		t.Fatalf("the id must stay with its attempt count and error: %q", kept)
	}
	fake.set(func(f *fakeResend) { f.failDelete = false })

	// It waits out a backoff before the next try, then finishes.
	before := fake.callCount()
	_ = sweeper.Sweep(context.Background())
	if fake.callCount() != before {
		t.Fatal("a failed delete must back off, not retry on the next tick")
	}
	clock = clock.Add(2 * time.Hour)
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.live()) != 0 || trashCount(t, pool) != "0" {
		t.Fatalf("retry: live=%v trash=%s", fake.live(), trashCount(t, pool))
	}

	// A domain removed in the Resend dashboard answers 404 and counts as gone.
	if _, err := pool.Exec(context.Background(), `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) SELECT workspace_id,'dom_already_gone' FROM email_channels`); err != nil {
		t.Fatal(err)
	}
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if trashCount(t, pool) != "0" {
		t.Fatalf("a 404 on delete must clear the record: %s left", trashCount(t, pool))
	}
}
