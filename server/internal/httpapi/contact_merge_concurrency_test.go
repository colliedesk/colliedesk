package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// A merge moves many conversations in one statement while mail lands on the same conversations in one
// statement whose trigger refreshes last_public. The conversations are stored and dated so the merge
// would meet them in the reverse of the trigger's (workspace_id, id) order; both sides must still finish,
// every conversation must move, and last_public must name the newest message.
func TestAMergeAndMailOnTheSameConversationsNeverDeadlock(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Merge Race", "admin@merge-race.example.com")
	kept := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Kept", "email": "kept@merge-race.example"})
	duplicate := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Duplicate", "email": "duplicate@merge-race.example"})
	workspace := queryString(t, pool, `SELECT workspace_id::text FROM contacts WHERE id=$1`, kept)
	ctx := context.Background()

	ids := strings.Split(queryString(t, pool, `SELECT string_agg(gen_random_uuid()::text,',') FROM generate_series(1,150)`), ",")
	slices.Sort(ids)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Highest id first on disk and most recently updated, so any scan of the contact's conversations
	// locks them from the highest id down, while the trigger locks from the lowest id up.
	for index := len(ids) - 1; index >= 0; index-- {
		if _, err := pool.Exec(ctx, `INSERT INTO conversations(id,workspace_id,contact_id,subject,updated_at) VALUES($1,$2,$3,$4,$5)`,
			ids[index], workspace, duplicate, fmt.Sprint("Race ", index), base.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	mail := func(kind string, at time.Time) error {
		_, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,created_at)
			SELECT $1,id,$3,'race','{}'::jsonb,$4 FROM unnest($2::uuid[]) AS id`, workspace, ids, kind, at)
		return err
	}
	race := func(change func() int, kind string, at time.Time) (int, error) {
		start := make(chan struct{})
		var wait sync.WaitGroup
		var status int
		var mailErr error
		wait.Add(2)
		go func() { defer wait.Done(); <-start; status = change() }()
		go func() { defer wait.Done(); <-start; mailErr = mail(kind, at) }()
		close(start)
		wait.Wait()
		return status, mailErr
	}
	expect := func(round int, owner, kind string) {
		t.Helper()
		got := queryString(t, pool, `SELECT count(*) FILTER (WHERE contact_id=$2)||'/'||count(*) FILTER (WHERE last_public_kind=$3)
			FROM conversations WHERE id=ANY($1::uuid[])`, ids, owner, kind)
		if want := fmt.Sprintf("%d/%d", len(ids), len(ids)); got != want {
			t.Fatalf("round %d: on the contact/last_public %s = %s, want %s", round, kind, got, want)
		}
	}

	at := base.Add(24 * time.Hour)
	for round := range 8 {
		at = at.Add(time.Minute)
		var mergeID string
		status, err := race(func() int {
			response := request(t, api, http.MethodPost, "/api/v1/contacts/"+kept+"/merges", map[string]any{"contact_id": duplicate}, admin, nil)
			var merge struct {
				ID string `json:"id"`
			}
			if response.Code == http.StatusCreated {
				decode(t, response, &merge)
			}
			mergeID = merge.ID
			return response.Code
		}, "incoming", at)
		if status != http.StatusCreated || err != nil {
			t.Fatalf("round %d merge: status=%d, mail error=%v", round, status, err)
		}
		expect(round, kept, "incoming")

		at = at.Add(time.Minute)
		status, err = race(func() int {
			return request(t, api, http.MethodDelete, "/api/v1/contacts/"+kept+"/merges/"+mergeID, nil, admin, nil).Code
		}, "outgoing", at)
		if status != http.StatusOK || err != nil {
			t.Fatalf("round %d undo: status=%d, mail error=%v", round, status, err)
		}
		expect(round, duplicate, "outgoing")
	}
}
