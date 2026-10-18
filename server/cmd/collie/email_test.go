package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (e *trialEnv) pause(reason string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE workspaces SET email_paused_at=now(),email_pause_reason=$2 WHERE id=$1`, e.workspaceID, reason); err != nil {
		e.t.Fatal(err)
	}
}

func (e *trialEnv) paused() bool {
	e.t.Helper()
	var paused bool
	if err := e.pool.QueryRow(context.Background(), `SELECT email_paused_at IS NOT NULL FROM workspaces WHERE id=$1`, e.workspaceID).Scan(&paused); err != nil {
		e.t.Fatal(err)
	}
	return paused
}

func TestEmailResumeClearsOnlyTheNamedWorkspacesPause(t *testing.T) {
	env := newTrialEnv(t, "Pause Desk")
	other := env.withWorkspace("Pause Other")
	env.pause("3 spam complaints on 100 replies in the last 7 days.")
	other.pause("4 spam complaints on 200 replies in the last 7 days.")

	output := env.mustRun("email", "resume", "pause-desk")
	if env.paused() {
		t.Fatal("email resume left the workspace paused")
	}
	if !other.paused() {
		t.Fatal("resuming one workspace cleared another's pause")
	}
	if !strings.Contains(output, "pause-desk") || !strings.Contains(output, "3 spam complaints") {
		t.Fatalf("output must name the workspace and the reason that was lifted:\n%s", output)
	}
}

func TestEmailResumeOnAWorkspaceThatIsNotPausedSaysSo(t *testing.T) {
	env := newTrialEnv(t, "Pause Idle")
	output := env.mustRun("email", "resume", "pause-idle")
	if !strings.Contains(output, "not paused") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestEmailResumeRejectsBadInput(t *testing.T) {
	env := newTrialEnv(t, "Pause Bad")
	env.pause("3 spam complaints on 100 replies in the last 7 days.")
	for name, args := range map[string][]string{
		"no workspace":      {"email", "resume"},
		"two workspaces":    {"email", "resume", "pause-bad", "other"},
		"unknown workspace": {"email", "resume", "nobody"},
		"unknown verb":      {"email", "pause", "pause-bad"},
		"no verb":           {"email"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if !env.paused() {
		t.Fatal("a rejected command cleared the pause")
	}
}

// holdDomain makes the workspace the provider-verified holder of the domain.
func (e *trialEnv) holdDomain(domain, providerID string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,provider_domain_id,domain_verified_at)
		VALUES($1,$2,'token',$3,now())`, e.workspaceID, "support@"+domain, providerID); err != nil {
		e.t.Fatal(err)
	}
}

func (e *trialEnv) tryVerifyDomain(domain, providerID string) error {
	_, err := e.pool.Exec(context.Background(), `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,provider_domain_id,domain_verified_at)
		VALUES($1,$2,'token',$3,now())`, e.workspaceID, "support@"+domain, providerID)
	return err
}

func (e *trialEnv) trashed(providerID string) bool {
	e.t.Helper()
	var found bool
	if err := e.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM email_provider_domain_trash WHERE provider_domain_id=$1)`, providerID).Scan(&found); err != nil {
		e.t.Fatal(err)
	}
	return found
}

func TestEmailReleaseDomainClearsTheHolderSoTheOwnerCanVerify(t *testing.T) {
	holder := newTrialEnv(t, "Holder Desk")
	owner := holder.withWorkspace("Owner Desk")
	holder.holdDomain("shared.example.com", "dom_holder")
	if err := owner.tryVerifyDomain("shared.example.com", "dom_owner"); err == nil {
		t.Fatal("setup: a second workspace must not be able to hold the same verified domain")
	}

	output := holder.mustRun("email", "release-domain", "--by", "ops", "Shared.Example.com")
	if !strings.Contains(output, "holder-desk") || !strings.Contains(output, "shared.example.com") {
		t.Fatalf("output must name the workspace and the domain that were released:\n%s", output)
	}
	if !holder.trashed("dom_holder") {
		t.Fatal("the holder's provider domain must be queued for deletion")
	}
	var providerID *string
	var verified *time.Time
	if err := holder.pool.QueryRow(context.Background(), `SELECT provider_domain_id,domain_verified_at FROM email_channels WHERE workspace_id=$1`, holder.workspaceID).Scan(&providerID, &verified); err != nil {
		t.Fatal(err)
	}
	if providerID != nil || verified != nil {
		t.Fatalf("the holder's channel must lose the provider domain and the verification: %v %v", providerID, verified)
	}
	if err := owner.tryVerifyDomain("shared.example.com", "dom_owner"); err != nil {
		t.Fatalf("after the release the owner must be able to verify: %v", err)
	}
}

func TestEmailReleaseDomainOnADomainNobodyHoldsSaysSo(t *testing.T) {
	env := newTrialEnv(t, "Release Idle")
	env.holdDomain("held.example.com", "dom_held")
	output := env.mustRun("email", "release-domain", "other.example.com")
	if !strings.Contains(output, "No workspace holds other.example.com") {
		t.Fatalf("output:\n%s", output)
	}
	if env.trashed("dom_held") {
		t.Fatal("an unrelated domain was released")
	}
}

func TestEmailReleaseDomainRejectsBadInput(t *testing.T) {
	env := newTrialEnv(t, "Release Bad")
	env.holdDomain("keep.example.com", "dom_keep")
	for name, args := range map[string][]string{
		"no domain":    {"email", "release-domain"},
		"two domains":  {"email", "release-domain", "a.example.com", "b.example.com"},
		"an address":   {"email", "release-domain", "support@keep.example.com"},
		"not a domain": {"email", "release-domain", "localhost"},
		"empty":        {"email", "release-domain", ""},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if env.trashed("dom_keep") {
		t.Fatal("a rejected command released a domain")
	}
}
