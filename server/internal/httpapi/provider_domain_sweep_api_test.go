package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/providerdomain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newSweeper(pool *pgxpool.Pool, serverURL string, clock *time.Time) *providerdomain.Sweeper {
	client := &resend.Client{APIKey: fakeResendKey, Base: serverURL}
	return providerdomain.New(store.New(pool), client, providerdomain.Options{Now: func() time.Time { return *clock }, Delay: time.Nanosecond})
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
	if _, err := pool.Exec(context.Background(), `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) SELECT workspace_id,'dom_already_gone' FROM email_domains`); err != nil {
		t.Fatal(err)
	}
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if trashCount(t, pool) != "0" {
		t.Fatalf("a 404 on delete must clear the record: %s left", trashCount(t, pool))
	}
}

func TestAProviderDomainThatStaysUnverifiedForAWeekIsReleasedAndTheTenantIsToldToVerifyAgain(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	slow := createSession(t, api, service, "Expire Slow", "admin@expire-slow.example.com")
	done := createSession(t, api, service, "Expire Done", "admin@expire-done.example.com")
	_ = connectEmail(t, api, slow, "support@expire-slow.example.com")
	_ = connectEmail(t, api, done, "support@expire-done.example.com")
	for _, admin := range []string{slow, done} {
		prove(t, api, dns, admin)
		_ = verify(t, api, fake, admin)
	}
	fake.setStatus("expire-done.example.com", "verified")
	_ = verify(t, api, fake, done)
	slowDomain, doneDomain := fake.idOf("expire-slow.example.com"), fake.idOf("expire-done.example.com")
	if _, err := pool.Exec(context.Background(), `UPDATE email_domains SET provider_domain_created_at=now()-interval '8 days'`); err != nil {
		t.Fatal(err)
	}

	clock := time.Now().Add(time.Minute)
	if err := newSweeper(pool, server.URL, &clock).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if live := fake.live(); len(live) != 1 || live[0] != doneDomain {
		t.Fatalf("only the verified domain may stay at the provider: %v (slow was %s)", live, slowDomain)
	}
	settings := providerState(t, api, slow)
	if len(settings.Channel.DNSRecords) != 0 || settings.Channel.DNSRecordsExpiredAt == nil {
		t.Fatalf("the tenant must be told the records expired: %+v", settings.Channel)
	}
	if providerState(t, api, done).Channel.DomainVerifiedAt == nil {
		t.Fatal("a verified domain must not expire")
	}

	// Verifying again starts over with a new domain and clears the notice.
	_ = verify(t, api, fake, slow)
	settings = providerState(t, api, slow)
	if len(settings.Channel.DNSRecords) != 3 || settings.Channel.DNSRecordsExpiredAt != nil || len(fake.live()) != 2 {
		t.Fatalf("after verifying again: %+v live=%v", settings.Channel, fake.live())
	}
}

func TestTwoSweepersNeverDeleteTheSameDomainTwice(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Claim Desk", "admin@claim-desk.example.com")
	workspace := workspaceID(t, api, admin)
	fake.set(func(f *fakeResend) { f.deleteDelay = 150 * time.Millisecond })
	for i := range 6 {
		id := fmt.Sprintf("dom_claim_%d", i)
		fake.set(func(f *fakeResend) { f.domains[id] = &fakeResendDomain{ID: id, Name: id} })
		if _, err := pool.Exec(context.Background(), `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) VALUES($1,$2)`, workspace, id); err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Now().Add(time.Minute)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = newSweeper(pool, server.URL, &clock).Sweep(context.Background())
		}()
	}
	wg.Wait()
	for i := range 6 {
		if n := fake.called(fmt.Sprintf("DELETE /domains/dom_claim_%d", i)); n != 1 {
			t.Errorf("domain %d was deleted %d times by two sweepers, want once", i, n)
		}
	}
	if len(fake.live()) != 0 || trashCount(t, pool) != "0" {
		t.Fatalf("after the sweeps: live=%v trash=%s", fake.live(), trashCount(t, pool))
	}
}

func TestADomainThatWasVerifiedOnceIsNeverReleasedByTheUnverifiedTimeout(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Once Verified", "admin@once-verified.example.com")
	_ = connectEmail(t, api, admin, "support@once-verified.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)
	fake.setStatus("once-verified.example.com", "verified")
	if response := verify(t, api, fake, admin); response.Code != http.StatusOK {
		t.Fatalf("setup: status=%d", response.Code)
	}
	id := fake.idOf("once-verified.example.com")

	// Resend later reads the domain as pending (a temporary DNS problem): sending falls back, nothing is released.
	fake.setStatus("once-verified.example.com", "pending")
	if response := verify(t, api, fake, admin); response.Code != http.StatusConflict {
		t.Fatalf("re-check status=%d, want 409", response.Code)
	}
	if providerState(t, api, admin).Channel.DomainVerifiedAt != nil {
		t.Fatal("setup: the domain should read as not verified now")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE email_domains SET provider_domain_created_at=now()-interval '30 days'`); err != nil {
		t.Fatal(err)
	}

	clock := time.Now().Add(time.Minute)
	if err := newSweeper(pool, server.URL, &clock).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if live := fake.live(); len(live) != 1 || live[0] != id {
		t.Fatalf("a domain that was verified once must stay at the provider: %v", live)
	}
	if trashCount(t, pool) != "0" || queryString(t, pool, `SELECT COALESCE(provider_domain_id,'') FROM email_domains`) != id {
		t.Fatal("the channel must keep its provider domain and nothing may be queued")
	}

	fake.setStatus("once-verified.example.com", "verified")
	if response := verify(t, api, fake, admin); response.Code != http.StatusOK {
		t.Fatalf("restoring status=%d, want 200", response.Code)
	}
	if providerState(t, api, admin).Channel.DomainVerifiedAt == nil {
		t.Fatal("the next verify must restore verification")
	}
}
