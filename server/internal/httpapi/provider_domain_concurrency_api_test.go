package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestManyVerifiesAtOnceDoNotStall(t *testing.T) {
	const workspaces = 40 // more than the test pool has connections
	fake, server := newFakeResend(t)
	fake.set(func(f *fakeResend) { f.createDelay = 300 * time.Millisecond })
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	tokens := make([]string, workspaces)
	for i := range tokens {
		tokens[i] = createSession(t, api, service, fmt.Sprintf("Many %d", i), fmt.Sprintf("admin@many-%d.example.com", i))
		putAddress(t, api, tokens[i], fmt.Sprintf("support@many-%d.example.com", i))
		prove(t, api, dns, tokens[i])
	}
	fake.advance(time.Minute)

	done := make(chan int, workspaces)
	for _, token := range tokens {
		go func() { done <- request(t, api, http.MethodPost, verifyDomainPath, nil, token, nil).Code }()
	}
	for range tokens {
		select {
		case code := <-done:
			if code != http.StatusConflict {
				t.Errorf("verify status=%d, want 409 (domain created, not yet verified)", code)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("verifies did not finish: the server stalled on its own connection pool")
		}
	}
	if live := fake.live(); len(live) != workspaces {
		t.Fatalf("live provider domains = %d, want one per workspace (%d)", len(live), workspaces)
	}
}

func TestAVerifyThatOutlivesAnAddressChangeCannotBringBackADeletedDomain(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Interleave", "admin@interleave.example.com")
	putAddress(t, api, admin, "support@interleave.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)
	first := fake.idOf("interleave.example.com")
	fake.setStatus("interleave.example.com", "verified")
	fake.set(func(f *fakeResend) {
		f.onVerify = func() {
			// While the check waits on Resend the admin moves to another domain and back.
			putAddress(t, api, admin, "support@elsewhere.example.com")
			putAddress(t, api, admin, "support@interleave.example.com")
		}
		f.domains[first].Status = "pending"
	})

	response := verify(t, api, fake, admin)
	fake.set(func(f *fakeResend) { f.onVerify = nil })
	if response.Code == http.StatusOK {
		t.Fatalf("a check that raced an address change must not report success: %s", response.Body.String())
	}
	if got := queryString(t, pool, `SELECT COALESCE(provider_domain_id,'')||COALESCE(verified_at::text,'') FROM email_domains`); got != "" {
		t.Fatalf("the stale check wrote back %q", got)
	}
	if queryString(t, pool, `SELECT count(*)::text FROM email_provider_domain_trash WHERE provider_domain_id=$1`, first) != "1" {
		t.Fatal("the replaced domain must be queued for deletion")
	}
}

func evasionConfig(fake *fakeResend, server interface{ Close() }, mailer *recordingMailer, serverURL string) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		pauseConfig(mailer)(config)
		config.Email.FromPolicy = "verified"
		config.Email.ResendAPIKey = fakeResendKey
		config.Email.ResendAPIBase = serverURL
		config.Now = fake.now
	}
}

func TestOnlyOneWorkspaceCanHoldAVerifiedDomainSoItsComplaintsStayCounted(t *testing.T) {
	fake, server := newFakeResend(t)
	mailer := &recordingMailer{}
	api, service, pool, dns := testAPIWithConfig(t, evasionConfig(fake, server, mailer, server.URL))
	adminA := createSession(t, api, service, "Evade One", "admin@evade-one.example.com")
	adminB := createSession(t, api, service, "Evade Two", "admin@evade-two.example.com")
	secretA := connectEmail(t, api, adminA, "support@evade.example.com")
	_ = connectEmail(t, api, adminB, "support@evade.example.com")
	for _, admin := range []string{adminA, adminB} {
		prove(t, api, dns, admin)
		_ = verify(t, api, fake, admin)
	}
	fake.setStatus("evade.example.com", "verified") // the newest copy, B's, is the one Resend verifies
	responseB := verify(t, api, fake, adminB)
	if responseB.Code != http.StatusOK {
		t.Fatalf("setup: B verify status=%d body=%s", responseB.Code, responseB.Body.String())
	}
	// A's copy later verifies too, so the two now claim one name.
	for id, domain := range fake.domainsByID() {
		if id != fake.idOf("evade.example.com") {
			fake.setStatusByID(domain, "verified")
		}
	}
	response := verify(t, api, fake, adminA)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "email_domain_unavailable") {
		t.Fatalf("A claims a name B has verified: status=%d body=%s, want 409 email_domain_unavailable", response.Code, response.Body.String())
	}
	if providerState(t, api, adminA).Channel.DomainVerifiedAt != nil {
		t.Fatal("A must not be verified for a name another workspace holds")
	}

	replies := sentReplies(t, api, pool, adminB, connectEmail(t, api, adminB, "support@evade.example.com"), "evade", 3)
	_ = replies
	_ = secretA
	from := `"Evade" <support@evade.example.com>`
	for i := range 3 {
		complainFrom(t, api, from, fmt.Sprintf("<evade-%d@amazonses.com>", i), fmt.Sprintf("msg_evade_%d", i))
	}
	if pauseState(t, api, adminB).SendingPause == nil {
		t.Fatal("the holder of the verified name must be paused by complaints about it")
	}
}

func TestWhenResendNoLongerVerifiesTheDomainTheWorkspaceStopsSendingAsIt(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Unverify", "admin@unverify.example.com")
	_ = connectEmail(t, api, admin, "support@unverify.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)
	fake.setStatus("unverify.example.com", "verified")
	if response := verify(t, api, fake, admin); response.Code != http.StatusOK {
		t.Fatalf("setup: status=%d", response.Code)
	}
	fake.setStatus("unverify.example.com", "failed") // the customer removed the DNS records

	if response := verify(t, api, fake, admin); response.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409", response.Code)
	}
	if providerState(t, api, admin).Channel.DomainVerifiedAt != nil {
		t.Fatal("a domain Resend no longer verifies must not stay verified here")
	}
}

func TestAnAmbiguousVerifiedSenderCountsTheComplaintForEveryMatch(t *testing.T) {
	fake, server := newFakeResend(t)
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, evasionConfig(fake, server, mailer, server.URL))
	adminA := createSession(t, api, service, "Both One", "admin@both-one.example.com")
	adminB := createSession(t, api, service, "Both Two", "admin@both-two.example.com")
	secretA := connectEmail(t, api, adminA, "support@both.example.com")
	secretB := connectEmail(t, api, adminB, "support@both.example.com")
	_ = sentReplies(t, api, pool, adminA, secretA, "bothA", 3)
	_ = sentReplies(t, api, pool, adminB, secretB, "bothB", 3)
	// Rows verified before the one-holder rule existed: remove the index to build that state.
	if _, err := pool.Exec(context.Background(), `DROP INDEX IF EXISTS email_domains_provider_verified_key`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE email_domains SET provider_domain_id='dom_'||workspace_id::text,verified_at=now()`); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		complainFrom(t, api, `"Both" <support@both.example.com>`, fmt.Sprintf("<both-%d@amazonses.com>", i), fmt.Sprintf("msg_both_%d", i))
	}
	if pauseState(t, api, adminA).SendingPause == nil || pauseState(t, api, adminB).SendingPause == nil {
		t.Fatal("with two matches the complaint must count for both, not for neither")
	}
}
