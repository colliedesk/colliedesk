package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// blocked reports whether the request is still waiting after a pause long enough for an unlocked
// code path to have finished.
func blocked(done <-chan *httptest.ResponseRecorder) (*httptest.ResponseRecorder, bool) {
	select {
	case response := <-done:
		return response, false
	case <-time.After(600 * time.Millisecond):
		return nil, true
	}
}

func (f teamFixture) workspaceTx(t *testing.T) pgx.Tx {
	t.Helper()
	tx, err := database.New(f.pool).BeginWorkspace(context.Background(), workspaceID(t, f.api, f.admin))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func TestAssigningWhileTheAgentIsBeingDeactivatedIsRefused(t *testing.T) {
	f := newTeamFixture(t)
	conversation := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Racing"})
	ws := workspaceID(t, f.api, f.admin)

	// A deactivation that has written the flag but not committed yet.
	tx := f.workspaceTx(t)
	if _, err := tx.Exec(context.Background(), `UPDATE agents SET deactivated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, f.annID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(f.api, newRequest(t, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": f.annID}, f.admin, nil))
	}()
	if response, waiting := blocked(done); !waiting {
		t.Fatalf("assignment finished with status %d while the deactivation was uncommitted; it must wait for the agent row", response.Code)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.Code != http.StatusBadRequest {
		t.Fatalf("assignment after the deactivation committed: status=%d body=%s, want 400", response.Code, response.Body.String())
	}
	if got := f.assignee(t, conversation); got != nil {
		t.Fatalf("assignee=%v, want the conversation unassigned", *got)
	}
}

func TestSessionCreatedByARacingRedeemIsRevokedByTheDeactivation(t *testing.T) {
	f := newTeamFixture(t)
	if _, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com"); err != nil {
		t.Fatal(err)
	}
	ws := workspaceID(t, f.api, f.admin)

	// A redeem that has locked the link and inserted its session but not committed.
	tx := f.workspaceTx(t)
	var linkID string
	if err := tx.QueryRow(context.Background(), `SELECT id::text FROM pairing_links WHERE workspace_id=$1 AND agent_id=$2 AND used_at IS NULL FOR UPDATE`, ws, f.annID).Scan(&linkID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at)
		VALUES($1,$2,decode(repeat('ab',32),'hex'),'agent','racing',now()+interval '1 day')`, ws, f.annID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(f.api, newRequest(t, http.MethodPatch, "/api/v1/agents/"+f.annID, map[string]any{"active": false}, f.admin, nil))
	}()
	if response, waiting := blocked(done); !waiting {
		t.Fatalf("deactivation finished with status %d while a redeem was in flight; it must wait for the link", response.Code)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.Code != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%s", response.Code, response.Body.String())
	}
	var revoked bool
	if err := f.pool.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL FROM sessions WHERE workspace_id=$1 AND label='racing'`, ws).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatalf("the session made by the racing redeem is still valid; it would work again after a reactivation")
	}
}

func TestDemotingRevokesThePersonsPendingAdminInvites(t *testing.T) {
	f := newTeamFixture(t)
	if code, failure := f.patch(t, f.admin, f.bobID, map[string]any{"role": "admin"}); code != http.StatusOK {
		t.Fatalf("promote status=%d error=%+v", code, failure)
	}
	_, adminInvite := createContractInvite(t, f.api, f.admin, "bob@team.example.com", "admin")
	_, agentInvite := createContractInvite(t, f.api, f.admin, "bob@team.example.com", "agent")
	if code, failure := f.patch(t, f.admin, f.bobID, map[string]any{"role": "agent"}); code != http.StatusOK {
		t.Fatalf("demote status=%d error=%+v", code, failure)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, adminInvite)}, "", nil).Code; got != http.StatusUnauthorized {
		t.Errorf("admin invite after the demotion status=%d, want 401", got)
	}
	if got := f.member(t, "bob@team.example.com").Role; got != "agent" {
		t.Fatalf("bob role=%q, want agent", got)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentInvite)}, "", nil).Code; got != http.StatusOK {
		t.Errorf("agent invite after the demotion status=%d, want 200: only admin-ceiling invites are revoked", got)
	}
}

func TestMalformedIdsAreAnsweredAsNotFoundAndInvalidTarget(t *testing.T) {
	f := newTeamFixture(t)
	if code, _ := f.patch(t, f.admin, "not-a-uuid", map[string]any{"role": "agent"}); code != http.StatusNotFound {
		t.Errorf("malformed agent id status=%d, want 404", code)
	}
	code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false, "reassign_to": "not-a-uuid"})
	if code != http.StatusBadRequest || failure.Error.Code != "invalid_reassign_target" {
		t.Errorf("malformed target status=%d error=%+v, want 400 invalid_reassign_target", code, failure.Error)
	}
}

func TestRedeemRacingADeactivationNeverLeavesALiveSession(t *testing.T) {
	f := newTeamFixture(t)
	link, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ws := workspaceID(t, f.api, f.admin)

	// A deactivation that holds the agent row and has not committed yet.
	tx := f.workspaceTx(t)
	if _, err := tx.Exec(context.Background(), `SELECT 1 FROM agents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, ws, f.annID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE agents SET deactivated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, f.annID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(f.api, newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil))
	}()
	if response, waiting := blocked(done); !waiting {
		t.Fatalf("redeem finished with status %d while the deactivation was uncommitted", response.Code)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.Code != http.StatusUnauthorized {
		t.Fatalf("redeem after the deactivation committed: status=%d body=%s, want 401", response.Code, response.Body.String())
	}
	var live int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE workspace_id=$1 AND agent_id=$2 AND revoked_at IS NULL AND label<>'Ann laptop'`, ws, f.annID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatalf("%d unrevoked sessions were created for a deactivated agent; they would work again after a reactivation", live)
	}
}

func TestDeactivationAndRedeemTogetherNeitherDeadlockNorLeakASession(t *testing.T) {
	f := newTeamFixture(t)
	ws := workspaceID(t, f.api, f.admin)
	for round := 0; round < 6; round++ {
		link, err := f.service.Pair(context.Background(), "team-desk", "bob@team.example.com")
		if err != nil {
			t.Fatal(err)
		}
		redeemed := make(chan int, 1)
		go func() {
			redeemed <- serve(f.api, newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)).Code
		}()
		if code, failure := f.patch(t, f.admin, f.bobID, map[string]any{"active": false}); code != http.StatusOK {
			t.Fatalf("round %d: deactivate status=%d error=%+v", round, code, failure)
		}
		if code := <-redeemed; code != http.StatusOK && code != http.StatusUnauthorized {
			t.Fatalf("round %d: redeem status=%d", round, code)
		}
		var live int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE workspace_id=$1 AND agent_id=$2 AND revoked_at IS NULL`, ws, f.bobID).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != 0 {
			t.Fatalf("round %d: %d live sessions for a deactivated agent", round, live)
		}
		if code, failure := f.patch(t, f.admin, f.bobID, map[string]any{"active": true}); code != http.StatusOK {
			t.Fatalf("round %d: reactivate status=%d error=%+v", round, code, failure)
		}
	}
}
