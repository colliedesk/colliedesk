package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// connectEmail gives the workspace an email channel with the sender name.
func (e *trialEnv) connectEmail(address, senderName string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,sender_name)
		VALUES($1,$2,'token',$3)`, e.workspaceID, address, senderName); err != nil {
		e.t.Fatal(err)
	}
}

func (e *trialEnv) senderName() string {
	e.t.Helper()
	var name string
	if err := e.pool.QueryRow(context.Background(), `SELECT sender_name FROM email_channels WHERE workspace_id=$1`, e.workspaceID).Scan(&name); err != nil {
		e.t.Fatal(err)
	}
	return name
}

// apiSenderName reads the sender name the way the web app does: GET /api/v1/channels/email as the admin.
func (e *trialEnv) apiSenderName(slug, adminEmail string) string {
	e.t.Helper()
	previous := stderr
	stderr = &bytes.Buffer{}
	defer func() { stderr = previous }()
	token := strings.TrimSpace(e.mustRun("token", "create", "--workspace", slug, "--email", adminEmail, "--scope", "admin", "--label", "sender name check"))
	response := tokenAPICall(e.t, e, http.MethodGet, "/api/v1/channels/email", token, "")
	var channel struct {
		Channel struct {
			SenderName string `json:"sender_name"`
		} `json:"channel"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &channel) != nil {
		e.t.Fatalf("GET /channels/email: status=%d body=%s", response.Code, response.Body.String())
	}
	return channel.Channel.SenderName
}

// The hosted cloud refuses a brand in a sender name through the API; the operator sets it for the
// brand's genuine owner, and only for the named workspace.
func TestEmailSenderNameSetsABrandNameOnlyForTheNamedWorkspace(t *testing.T) {
	if _, denied := domain.DeniedBrand("Colliedesk Support"); !denied {
		t.Fatal("setup: the test name must be one the hosted cloud refuses")
	}
	env := newTrialEnv(t, "Sender Desk")
	other := env.withWorkspace("Sender Other")
	env.connectEmail("support@colliedesk.example.com", "Sender Desk")
	other.connectEmail("help@other.example.com", "Other Help")

	output := env.mustRun("email", "sender-name", "--by", "ops", "sender-desk", "  Colliedesk Support  ")
	for _, want := range []string{"sender-desk", `"Sender Desk"`, `"Colliedesk Support"`, "ops"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output must name the workspace, the old and new name and the operator (missing %q):\n%s", want, output)
		}
	}
	if got := env.apiSenderName("sender-desk", "admin@senderdesk.example.com"); got != "Colliedesk Support" {
		t.Fatalf("the API returns sender name %q, want the trimmed name the operator set", got)
	}
	if got := other.senderName(); got != "Other Help" {
		t.Fatalf("another workspace's sender name changed to %q", got)
	}
}

func TestEmailSenderNameRejectsBadInput(t *testing.T) {
	env := newTrialEnv(t, "Sender Bad")
	env.connectEmail("support@bad.example.com", "Sender Bad")
	bare := env.withWorkspace("Sender Bare")
	for name, args := range map[string][]string{
		"no arguments":       {"email", "sender-name"},
		"no name":            {"email", "sender-name", "sender-bad"},
		"empty name":         {"email", "sender-name", "sender-bad", ""},
		"only spaces":        {"email", "sender-name", "sender-bad", "   "},
		"line feed":          {"email", "sender-name", "sender-bad", "Acme\nBcc: x@example.com"},
		"carriage return":    {"email", "sender-name", "sender-bad", "Acme\rSupport"},
		"direction override": {"email", "sender-name", "sender-bad", "Acme ‮Support"},
		"too long":           {"email", "sender-name", "sender-bad", strings.Repeat("a", 101)},
		"two names":          {"email", "sender-name", "sender-bad", "Acme", "Support"},
		"unknown workspace":  {"email", "sender-name", "nobody", "Acme Support"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := env.senderName(); got != "Sender Bad" {
		t.Fatalf("a rejected command changed the sender name to %q", got)
	}

	_, err := bare.run("email", "sender-name", "sender-bare", "Acme Support")
	if err == nil || !strings.Contains(err.Error(), "no email channel") {
		t.Fatalf("a workspace without an email channel: err=%v, want a refusal that says it has no email channel", err)
	}
	var channels int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM email_channels WHERE workspace_id=$1`, bare.workspaceID).Scan(&channels); err != nil || channels != 0 {
		t.Fatalf("the command created a channel for a workspace without one: count=%d err=%v", channels, err)
	}
}
