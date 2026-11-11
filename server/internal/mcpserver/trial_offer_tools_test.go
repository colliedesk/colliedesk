package mcpserver_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (e *env) trialOffer(w workspace) string {
	e.t.Helper()
	_, token, err := store.New(e.pool).CreateTrialOffer(context.Background(), w.id, "ops", time.Now(), store.TrialOfferMaxValidity)
	if err != nil {
		e.t.Fatal(err)
	}
	return testOrigin + "/trial-offer?token=" + token
}

// An AI agent given the link previews the offer by name, accepts it with billing:manage in one call, and a
// spent link, another workspace's link or a token without billing:manage is refused with what to do.
func TestTrialOfferAcceptToolPreviewsAndAcceptsTheLink(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Offer Desk", "admin@offer.example.com")
	other := e.newWorkspace("Other Desk", "admin@other.example.com")
	link := e.trialOffer(w)

	preview := e.ok(e.connect(w.read, ""), "colliedesk_trial_offer_accept", map[string]any{"link": link, "preview": true})
	offer, _ := preview["offer"].(map[string]any)
	if offer["workspace_name"] != "Offer Desk" || offer["state"] != "open" || offer["can_accept"] != false || offer["refusal"] != "not_admin" {
		t.Fatalf("preview with a read token = %v", preview)
	}
	if next, _ := preview["next"].(string); !strings.Contains(next, "admin@offer.example.com") {
		t.Errorf("preview next = %q, want it to name who can accept", next)
	}
	refusedWithoutPermission(t, e, w.send, "colliedesk_trial_offer_accept", map[string]any{"link": link})
	if message := e.fails(e.connect(other.adminTier, ""), "colliedesk_trial_offer_accept", map[string]any{"link": link}); !strings.Contains(message, "another workspace") {
		t.Errorf("another workspace's admin: %q, want a refusal that says the offer is for another workspace", message)
	}

	session := e.connect(w.adminTier, "")
	out := e.ok(session, "colliedesk_trial_offer_accept", map[string]any{"link": link})
	accepted, _ := out["offer"].(map[string]any)
	ends, _ := out["trial_ends_at"].(string)
	if accepted["state"] != "accepted" || ends == "" {
		t.Fatalf("accept = %v", out)
	}
	if next, _ := out["next"].(string); !strings.Contains(next, ends) {
		t.Errorf("next = %q, want the new trial end %s", next, ends)
	}
	token := strings.TrimPrefix(link, testOrigin+"/trial-offer?token=")
	if message := e.fails(session, "colliedesk_trial_offer_accept", map[string]any{"link": token}); !strings.Contains(message, "already accepted") {
		t.Errorf("a spent link: %q, want a refusal that says it was already accepted", message)
	}
	if message := e.fails(session, "colliedesk_trial_offer_accept", map[string]any{"link": ""}); !strings.Contains(message, "link") {
		t.Errorf("an empty link: %q, want a message naming link", message)
	}
}
