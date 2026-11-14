package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The log keeps whether a workspace was the team's own sign-up, so a report can leave it out after the
// workspace and its trial record are gone.
func TestASignUpRecordsWhetherItIsOnTheTeamsInternalList(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("@team.example", "")
	if err != nil {
		t.Fatal(err)
	}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.InternalSignups = internal })
	for _, tc := range []struct {
		email, name, slug string
		want              any
	}{
		{"dev@team.example", "Team Test", "team-test", true},
		{"grace@example.com", "Hopper Help", "hopper-help", false},
	} {
		wantResponse(t, f.signUp(t, tc.email, tc.name, defaultPeer), http.StatusCreated, "")
		workspace := queryString(t, f.pool, `SELECT id::text FROM workspaces WHERE slug=$1`, tc.slug)
		got := onlyKind(milestonesOf(t, f.pool, workspace), "signed_up")
		if len(got) != 1 || got[0].Detail["internal"] != tc.want {
			t.Fatalf("signed_up for %s = %+v, want detail internal=%v", tc.slug, got, tc.want)
		}
	}
}

// A reply that was saved but never queued (the channel is off) did not leave Colliedesk. The first
// reply is the one that does: here, the retry that queues it.
func TestAReplySavedButNotQueuedIsNoFirstReplyUntilItsRetryQueuesIt(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := *f.receive(t, false, 1, "Order missing").ConversationID
	if _, err := f.pool.Exec(t.Context(), `UPDATE channels SET enabled=false, disabled_at=now() WHERE workspace_id=$1`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	saved := f.reply(t, f.adminA, conversation)
	if saved.Code != http.StatusCreated {
		t.Fatalf("reply with the channel off: status=%d body=%s", saved.Code, saved.Body)
	}
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply"); len(got) != 0 {
		t.Fatalf("a reply saved with its channel off made a first_reply: %+v", got)
	}
	messageID := queryString(t, f.pool, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='outgoing'`, f.workspaceA)
	if _, err := f.pool.Exec(t.Context(), `UPDATE channels SET enabled=true, disabled_at=NULL WHERE workspace_id=$1`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	retry := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+messageID+"/retry", nil, f.adminA, nil)
	if retry.Code != http.StatusOK && retry.Code != http.StatusCreated {
		t.Fatalf("retry: status=%d body=%s", retry.Code, retry.Body.String())
	}
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply")
	if len(got) != 1 || got[0].Detail["message_id"] != messageID {
		t.Fatalf("first_reply after the retry = %+v, want one naming %s", got, messageID)
	}
}
