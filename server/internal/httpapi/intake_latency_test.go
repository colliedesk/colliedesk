package httpapi_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// What a page of the intake queue (50 cards) may take in the store, like the other screens' lists, and
// a card, like the other reads of one item (docs/internals/performance.md).
const (
	intakeQueueBudget = 100 * time.Millisecond
	intakeCardBudget  = 20 * time.Millisecond
)

// TestIntakeQueueLatencyBudget reads the review page, the decided page, a filtered page and one card in a
// workspace with 20,000 gatherings of three items each (5,000 waiting, 15,000 decided), beside another
// workspace as large.
func TestIntakeQueueLatencyBudget(t *testing.T) {
	d := newLevelDesk(t)
	ctx := context.Background()
	for _, w := range []struct{ workspace, channel string }{{d.workspaceA, d.chatA}, {d.workspaceB, d.chatB}} {
		if _, err := d.pool.Exec(ctx, `WITH th AS (INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,opens_conversations)
				SELECT $1,$2,'group','group:'||g,true FROM generate_series(1,200) g RETURNING id,external_id),
			g AS (INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,state,review_reason,rules_action,rules_rule,decided_by,decided_at,opened_at,closed_at)
				SELECT $1,$2,'group','thread:'||th.id||':u'||s,th.id,CASE WHEN s%4=0 THEN 'review' ELSE 'created' END,CASE WHEN s%4=0 THEN 'no_command' END,
					'create','new_thread',CASE WHEN s%4<>0 THEN 'rules' END,CASE WHEN s%4<>0 THEN now()-s*interval '1 minute' END,
					now()-s*interval '1 minute'-interval '5 minutes',now()-s*interval '1 minute'
				FROM generate_series(1,20000) s JOIN th ON th.external_id='group:'||(1+s%200) RETURNING id,thread_id)
			INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,sender_kind,sender_value,sender_name,body,gathering_id,disposition,occurred_at)
				SELECT $1,$2,g.thread_id,'in','customer','loopback_user','u','Minh',repeat('The stock export fails again. ',20),g.id,'pending_review',now()-i*interval '1 second'
				FROM g, generate_series(1,3) i`, w.workspace, w.channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.pool.Exec(ctx, `ANALYZE intake_gatherings; ANALYZE channel_thread_items`); err != nil {
		t.Fatal(err)
	}
	st := store.New(d.pool)
	var card string
	if err := d.pool.QueryRow(ctx, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 AND state='review' ORDER BY closed_at DESC LIMIT 1`, d.workspaceA).Scan(&card); err != nil {
		t.Fatal(err)
	}
	// The card's group has a conversation for each of its 300 items, the candidates it offers.
	if _, err := d.pool.Exec(ctx, `WITH p AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Minh','minh@example.com') RETURNING id),
			i AS (SELECT i.id,row_number() OVER (ORDER BY i.id) n FROM channel_thread_items i JOIN intake_gatherings g ON g.workspace_id=i.workspace_id AND g.id=$2::uuid
				WHERE i.workspace_id=$1 AND i.thread_id=g.thread_id),
			c AS (INSERT INTO conversations(workspace_id,contact_id,subject,status,updated_at,channel_id)
				SELECT $1,p.id,'Report '||n,CASE WHEN n%3=0 THEN 'resolved' ELSE 'open' END,now()-n*interval '1 minute',$3 FROM p, i RETURNING id,subject)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT $1,c.id,i.id,'opened' FROM c JOIN i ON c.subject='Report '||i.n`,
		d.workspaceA, card, d.chatA); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `ANALYZE conversations; ANALYZE conversation_thread_items`); err != nil {
		t.Fatal(err)
	}
	measure := func(name string, budget time.Duration, read func() error) {
		for range 5 {
			if err := read(); err != nil {
				t.Fatal(err)
			}
		}
		durations := make([]time.Duration, 40)
		for i := range durations {
			started := time.Now()
			if err := read(); err != nil {
				t.Fatal(err)
			}
			durations[i] = time.Since(started)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95 := durations[len(durations)*95/100]
		t.Logf("%s p95 %s", name, p95)
		if p95 > budget {
			t.Fatalf("%s p95 %s exceeds %s", name, p95, budget)
		}
	}
	page := func(list, channel, scope string) func() error {
		return func() error {
			got, err := st.IntakeGatherings(ctx, d.workspaceA, list, channel, scope, "", "", store.MaxIntakePage)
			if err == nil && len(got.Items) != store.MaxIntakePage {
				t.Fatalf("%s page has %d cards", list, len(got.Items))
			}
			return err
		}
	}
	measure("review page", intakeQueueBudget, page(store.IntakeListReview, "", ""))
	measure("decided page", intakeQueueBudget, page(store.IntakeListDecided, "", ""))
	measure("review page of a channel and scope", intakeQueueBudget, page(store.IntakeListReview, d.chatA, "group"))
	measure("decided page of the rules", intakeQueueBudget, func() error {
		got, err := st.IntakeGatherings(ctx, d.workspaceA, store.IntakeListDecided, "", "", store.DecidedByRules, "", store.MaxIntakePage)
		if err == nil && len(got.Items) != store.MaxIntakePage {
			t.Fatalf("decided page of the rules has %d cards", len(got.Items))
		}
		return err
	})
	measure("card", intakeCardBudget, func() error {
		got, err := st.IntakeGatheringByID(ctx, d.workspaceA, card, time.Now())
		if err == nil && len(got.Candidates) != 30 {
			t.Fatalf("card has %d candidates", len(got.Candidates))
		}
		return err
	})
	measure("summary", intakeCardBudget, func() error {
		_, err := st.IntakeSummaryOf(ctx, d.workspaceA)
		return err
	})
}
