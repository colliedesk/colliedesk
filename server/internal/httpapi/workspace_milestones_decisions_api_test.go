package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/milestones"
	"github.com/jackc/pgx/v5"
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
	retriedAt := time.Date(2026, time.October, 9, 15, 30, 0, 0, time.UTC)
	f.clock.set(retriedAt)
	retry := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+messageID+"/retry", nil, f.adminA, nil)
	if retry.Code != http.StatusOK && retry.Code != http.StatusCreated {
		t.Fatalf("retry: status=%d body=%s", retry.Code, retry.Body.String())
	}
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply")
	if len(got) != 1 || got[0].Detail["message_id"] != messageID {
		t.Fatalf("first_reply after the retry = %+v, want one naming %s", got, messageID)
	}
	if !got[0].At.Equal(retriedAt) {
		t.Fatalf("first_reply at %s, want %s: the time the retry queued the reply, not when it was written", got[0].At, retriedAt)
	}
}

// recordReplyOf runs the reply rule on one message, as the server does after it stores the delivery.
func recordReplyOf(t *testing.T, f limitsFixture, messageID string) {
	t.Helper()
	tx, err := f.pool.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck
	if err := milestones.RecordReply(context.Background(), tx, f.workspaceA, messageID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A reply that has no delivery row at all (the channel type carries no reply) did not leave either: the rule
// asks for a delivery that is not failed, it does not only rule failed ones out.
func TestAReplyWithNoDeliveryRowIsNoFirstReplyAndOneQueuedLaterCountsAtThatTime(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := *f.receive(t, false, 1, "Order missing").ConversationID
	var messageID string
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body)
		SELECT $1,$2,(SELECT id FROM agents WHERE workspace_id=$1 ORDER BY created_at LIMIT 1),'outgoing','No delivery' RETURNING id::text`,
		f.workspaceA, conversation).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	recordReplyOf(t, f, messageID)
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply"); len(got) != 0 {
		t.Fatalf("a reply with no delivery row made a first_reply: %+v", got)
	}
	queuedAt := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO channel_outbox(workspace_id,message_id,channel_type,channel_id,status,created_at)
		VALUES($1,$2,'email',(SELECT id FROM channels WHERE workspace_id=$1 AND type='email' ORDER BY is_default DESC,created_at LIMIT 1),'queued',$3)`,
		f.workspaceA, messageID, queuedAt); err != nil {
		t.Fatal(err)
	}
	recordReplyOf(t, f, messageID)
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply")
	if len(got) != 1 || !got[0].At.Equal(queuedAt) {
		t.Fatalf("first_reply = %+v, want one at %s, when the delivery was queued", got, queuedAt)
	}
}
