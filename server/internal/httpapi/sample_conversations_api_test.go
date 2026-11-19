package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const samplePeer = "203.0.113.180:40000"

const aiAppConsentNext = "/oauth/authorize?client_id=https%3A%2F%2Fclaude.ai%2Fclient.json&response_type=code&state=abc"

func newSampleSignupFixture(t *testing.T, funnel *recordingFunnel) signupFixture {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return newSignupFixture(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.Funnel = funnel
	})
}

// A team that signs up from an AI app's connector works the inbox only through that app, and an empty inbox
// gives the app nothing to show. Such a sign-up starts with three sample conversations, tagged sample and
// written from the person's own address: a reply reaches only them, and none counts as a customer's message.
func TestAnAIAppSignupStartsWithSampleConversationsFromThePersonsOwnAddress(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSampleSignupFixture(t, funnel)
	wantResponse(t, f.signUpWith(t, "owner@samples-acme.example.com", samplePeer,
		map[string]any{"workspace_name": "Samples Acme", "next": aiAppConsentNext}), http.StatusCreated, "")
	workspace := queryString(t, f.pool, `SELECT id::text FROM workspaces WHERE name='Samples Acme'`)

	if got := queryString(t, f.pool, `SELECT count(*)::text FROM conversations c
		JOIN conversation_tags ct ON ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id
		JOIN tags g ON g.workspace_id=ct.workspace_id AND g.id=ct.tag_id AND g.name='sample'
		WHERE c.workspace_id=$1 AND c.status='open'`, workspace); got != "3" {
		t.Fatalf("open conversations tagged sample = %s, want 3", got)
	}
	if got := queryString(t, f.pool, `SELECT string_agg(DISTINCT k.email, ',') FROM conversations c
		JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id WHERE c.workspace_id=$1`, workspace); got != "owner@samples-acme.example.com" {
		t.Fatalf("sample conversations come from %q, want only the person's own address", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM workspace_events WHERE workspace_id=$1 AND kind='first_customer_message'`, workspace); got != "0" {
		t.Fatalf("first_customer_message recorded %s times, want 0: a sample is not a customer writing in", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM channel_outbox WHERE workspace_id=$1`, workspace); got != "0" {
		t.Fatalf("emails queued at sign-up = %s, want 0: adding samples sends nothing", got)
	}
	events := funnel.named("sample_conversations_added")
	if len(events) != 1 || events[0].Workspace != workspace || events[0].Properties["count"] != 3 {
		t.Fatalf("sample_conversations_added events = %+v, want one with count 3 for the workspace", events)
	}
}

// A sign-up on the web keeps the empty inbox, which explains the next step itself.
func TestAWebSignupStartsWithAnEmptyInbox(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSampleSignupFixture(t, funnel)
	wantResponse(t, f.signUpWith(t, "owner@web-acme.example.com", samplePeer,
		map[string]any{"workspace_name": "Web Acme", "next": "/inbox"}), http.StatusCreated, "")
	workspace := queryString(t, f.pool, `SELECT id::text FROM workspaces WHERE name='Web Acme'`)

	if got := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, workspace); got != "0" {
		t.Fatalf("conversations after a web sign-up = %s, want 0", got)
	}
	if events := funnel.named("sample_conversations_added"); len(events) != 0 {
		t.Fatalf("sample_conversations_added events = %+v, want none for a web sign-up", events)
	}
}

// Samples are a head start, never a condition: a server that cannot store them still opens the workspace.
func TestAnAIAppSignupThatCannotAddSamplesStillStarts(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	wantResponse(t, f.signUpWith(t, "owner@no-samples.example.com", samplePeer,
		map[string]any{"workspace_name": "No Samples", "next": aiAppConsentNext}), http.StatusCreated, "")
	workspace := queryString(t, f.pool, `SELECT id::text FROM workspaces WHERE name='No Samples'`)

	if got := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, workspace); got != "0" {
		t.Fatalf("conversations = %s, want 0 when samples cannot be stored", got)
	}
	if events := funnel.named("sample_conversations_added"); len(events) != 0 {
		t.Fatalf("sample_conversations_added events = %+v, want none when nothing was added", events)
	}
}
