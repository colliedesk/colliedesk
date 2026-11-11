package results

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	workspace string
	contact   string
}

func newFixture(t *testing.T, pool *pgxpool.Pool, name string) *fixture {
	t.Helper()
	f := &fixture{t: t, pool: pool}
	f.workspace = f.id(`INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, name)
	f.exec(`UPDATE workspaces SET status_history_since=$2 WHERE id=$1`, f.workspace, ago(60))
	f.contact = f.id(`INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey',$2) RETURNING id::text`, f.workspace, "casey@"+name+".example")
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *fixture) id(sql string, args ...any) string {
	f.t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return id
}

// conversation adds a conversation created at created with messages of the given kinds, each after
// the offset from created (an empty kind skips).
func (f *fixture) conversation(channel string, created time.Time, origin any, messages ...message) string {
	f.t.Helper()
	id := f.id(`INSERT INTO conversations(workspace_id,contact_id,subject,channel,created_at,updated_at,origin) VALUES($1,$2,'Help',$3,$4,$4,$5) RETURNING id::text`,
		f.workspace, f.contact, channel, created, origin)
	for _, m := range messages {
		f.exec(`INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,$3,'text',$4)`, f.workspace, id, m.kind, created.Add(m.after))
	}
	return id
}

type message struct {
	kind  string
	after time.Duration
}

func in(after time.Duration) message  { return message{"incoming", after} }
func out(after time.Duration) message { return message{"outgoing", after} }

func (f *fixture) snapshot(kind string) (Data, map[string][]string) {
	f.t.Helper()
	var id string
	var raw []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT id::text,data FROM results_snapshots WHERE workspace_id=$1 AND kind=$2 ORDER BY computed_at DESC LIMIT 1`, f.workspace, kind).Scan(&id, &raw); err != nil {
		f.t.Fatalf("read %s snapshot: %v", kind, err)
	}
	var data Data
	if err := json.Unmarshal(raw, &data); err != nil {
		f.t.Fatal(err)
	}
	rows, err := f.pool.Query(context.Background(), `SELECT metric,conversation_id::text FROM results_members WHERE workspace_id=$1 AND snapshot_id=$2`, f.workspace, id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	members := map[string][]string{}
	for rows.Next() {
		var metric, conversation string
		if err := rows.Scan(&metric, &conversation); err != nil {
			f.t.Fatal(err)
		}
		members[metric] = append(members[metric], conversation)
	}
	return data, members
}

// The worker reads only what the rules count: customer-started conversations of Colliedesk (not
// imported, not problem reports, not started by the team), replies (not notes) up to the moment
// measured, Colliedesk's status changes (not an import's), and for the baseline only the baseline
// import's tickets created by its anchor. Expected values are worked out by hand.
func TestWorkerStoresTheNumbersAndTheConversationsBehindThem(t *testing.T) {
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, pool, "acme")
	other := newFixture(t, pool, "beta")

	l1 := f.conversation("email", ago(3), nil, in(0), message{"note", 5 * time.Minute}, out(20*time.Minute))
	l2 := f.conversation("email", ago(4), nil, in(0), out(time.Hour))
	f.conversation("agent", ago(5), nil, out(0), in(time.Hour))
	f.conversation("imported", ago(2), nil, in(0), out(time.Minute))
	f.conversation("problem_report", ago(2), nil, in(0), out(time.Minute))
	l6 := f.conversation("email", ago(10), nil, in(0), out(2*time.Hour))
	f.exec(`INSERT INTO conversation_status_changes(workspace_id,conversation_id,from_status,to_status,changed_at,source) VALUES
		($1,$2,'open','resolved',$3,'colliedesk'),($1,$2,'resolved','open',$4,'import')`, f.workspace, l6, ago(9), ago(8.5))
	l7 := f.conversation("email", ago(6), nil, in(0), out(7*Day))
	l8 := f.conversation("web_form", ago(1), nil, in(0), out(10*time.Minute))
	l9 := f.conversation("email", ago(2.5), nil, in(0), out(30*time.Minute))
	// The other workspace's conversation never counts here.
	other.conversation("email", ago(3), nil, in(0), out(time.Minute))

	anchor := ago(20)
	ticket := func(id string, created time.Time, stats any, minutes int, reply time.Duration) string {
		origin, _ := json.Marshal(map[string]any{"system": "freshdesk", "external_id": id, "stats": stats})
		if stats == nil {
			origin, _ = json.Marshal(map[string]any{"system": "freshdesk", "external_id": id})
		}
		conversation := f.conversation("imported", created, origin, in(0), out(reply))
		f.exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status) VALUES($1,'freshdesk:account:9','ticket',$2,$3,'done')`, f.workspace, id, conversation)
		if minutes > 0 {
			f.exec(`INSERT INTO time_entries(workspace_id,conversation_id,minutes,billable,executed_at) VALUES($1,$2,$3,false,$4)`, f.workspace, conversation, minutes, created)
		}
		return conversation
	}
	stats := func(status string, resolved time.Time) map[string]any {
		return map[string]any{"status": status, "resolved_at": resolved, "first_responded_at": nil, "closed_at": nil}
	}
	b1Created := anchor.Add(-2 * Day)
	b1 := ticket("1", b1Created, stats("resolved", b1Created.Add(time.Hour)), 5, 10*time.Minute)
	b2 := ticket("2", anchor.Add(-9*Day), stats("closed", anchor.Add(-9*Day+2*time.Hour)), 15, 20*time.Minute)
	b3 := ticket("3", anchor.Add(-10*Day), stats("resolved", anchor.Add(-10*Day+3*time.Hour)), 25, 30*time.Minute)
	b4 := ticket("4", anchor.Add(-11*Day), nil, 0, 40*time.Minute)
	b5 := ticket("5", anchor.Add(-12*Day), stats("open", anchor.Add(-12*Day+time.Hour)), 0, 50*time.Minute)
	// A ticket the team started after b1 (outbound) does not move where the windows end.
	ticketOutbound := f.conversation("imported", anchor.Add(-Day), []byte(`{"system":"freshdesk","external_id":"7"}`), out(0), in(time.Hour))
	f.exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status) VALUES($1,'freshdesk:account:9','ticket','7',$2,'done')`, f.workspace, ticketOutbound)
	// Created after the anchor (Freshdesk kept running), and a ticket of another account: neither counts.
	ticket("6", anchor.Add(Day), stats("resolved", anchor.Add(Day+time.Hour)), 0, time.Minute)
	f.exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status) VALUES($1,'freshdesk:account:10','ticket','1',$2,'done')`,
		f.workspace, f.conversation("imported", anchor.Add(-3*Day), nil, in(0), out(time.Minute)))
	f.exec(`INSERT INTO results_state(workspace_id,baseline_source,baseline_anchor_at,baseline_import_status,baseline_dirty) VALUES($1,'freshdesk:account:9',$2,'OK',true)`, f.workspace, anchor)

	worker := NewWorker(database.New(pool))
	worker.now = func() time.Time { return measured }
	if computed, err := worker.RunOnce(context.Background()); err != nil || computed != 2 {
		t.Fatalf("computed %d workspaces, err %v; want both", computed, err)
	}

	current, members := f.snapshot("current")
	month := current.Windows["30d"]
	// Replies 600 (l8) 1200 (l1) 1800 (l9) 3600 (l2) 7200 (l6) s; l7's reply comes after the moment measured.
	if want := (FirstResponse{MedianSeconds: ptr[int64](1800), P90Seconds: ptr[int64](7200), Answered: 5, Waiting: 1}); !reflect.DeepEqual(month.FirstResponse, want) {
		t.Errorf("30d first response = %+v, want %+v", month.FirstResponse, want)
	}
	if month.Volume.Count != 6 {
		t.Errorf("30d volume = %+v, want 6 customer-started conversations", month.Volume)
	}
	// l6 resolved a day after it came in; the import's reopen is not Colliedesk's.
	if want := (&Resolution{Resolved: 1}); !reflect.DeepEqual(month.Resolution, want) {
		t.Errorf("30d resolution = %+v, want %+v", month.Resolution, want)
	}
	for key, want := range map[string][]string{
		"30d.first_response":         {l1, l2, l6, l8, l9},
		"30d.first_response_waiting": {l7},
		"30d.resolution":             {l6},
	} {
		if got := sorted(members[key]); !reflect.DeepEqual(got, sorted(want)) {
			t.Errorf("members %s = %v, want %v", key, got, want)
		}
	}

	baseline, baselineMembers := f.snapshot("baseline")
	if baseline.Source != "freshdesk:account:9" || baseline.ImportStatus != "OK" || baseline.End == nil || !baseline.End.Equal(b1Created) || baseline.StatsRead != 4 {
		t.Errorf("baseline = source %q status %q end %v stats read %d", baseline.Source, baseline.ImportStatus, baseline.End, baseline.StatsRead)
	}
	month = baseline.Windows["30d"]
	// Replies 600 1200 1800 2400 3000 s from b1..b5.
	if want := (FirstResponse{MedianSeconds: ptr[int64](1800), P90Seconds: ptr[int64](3000), Answered: 5}); !reflect.DeepEqual(month.FirstResponse, want) {
		t.Errorf("baseline 30d first response = %+v, want %+v", month.FirstResponse, want)
	}
	// Created 37 to 7 days before b1: b2 b3 resolved, b5 open, b4 without stats.
	if want := (&Resolution{Resolved: 2, Unresolved: 1, WithoutResolveTime: 1}); !reflect.DeepEqual(month.Resolution, want) {
		t.Errorf("baseline 30d resolution = %+v, want %+v", month.Resolution, want)
	}
	if want := (&Handle{Conversations: 2}); !reflect.DeepEqual(month.Handle, want) {
		t.Errorf("baseline 30d handle = %+v, want %+v", month.Handle, want)
	}
	for key, want := range map[string][]string{
		"30d.first_response":       {b1, b2, b3, b4, b5},
		"30d.resolution":           {b2, b3},
		"30d.without_resolve_time": {b4},
		"30d.unresolved":           {b5},
		"30d.handle":               {b2, b3},
	} {
		if got := sorted(baselineMembers[key]); !reflect.DeepEqual(got, sorted(want)) {
			t.Errorf("baseline members %s = %v, want %v", key, got, want)
		}
	}

	// Nothing changed: the next look within the hour computes nothing.
	if computed, err := worker.RunOnce(context.Background()); err != nil || computed != 0 {
		t.Fatalf("second run computed %d, err %v; want none", computed, err)
	}
}

// A workspace that imported before results existed gets that import as its baseline on its first
// compute, with no reconcile status; its tickets have no stats, so resolve times are missing.
func TestAnEarlierImportBecomesTheBaselineOnTheFirstCompute(t *testing.T) {
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, pool, "early")
	conversation := f.conversation("imported", ago(40), []byte(`{"system":"freshdesk","external_id":"1"}`), in(0), out(time.Hour))
	f.exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status,updated_at) VALUES($1,'freshdesk:account:5','ticket','1',$2,'done',$3)`,
		f.workspace, conversation, ago(35))
	worker := NewWorker(database.New(pool))
	worker.now = func() time.Time { return measured }
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var source string
	var anchor time.Time
	var status *string
	if err := pool.QueryRow(context.Background(), `SELECT baseline_source,baseline_anchor_at,baseline_import_status FROM results_state WHERE workspace_id=$1`, f.workspace).Scan(&source, &anchor, &status); err != nil {
		t.Fatal(err)
	}
	if source != "freshdesk:account:5" || !anchor.Equal(ago(35)) || status != nil {
		t.Fatalf("baseline = %s at %s status %v", source, anchor, status)
	}
	baseline, _ := f.snapshot("baseline")
	if baseline.StatsRead != 0 || baseline.Windows["30d"].FirstResponse.Answered != 1 || baseline.Windows["30d"].Resolution.WithoutResolveTime != 0 {
		t.Fatalf("baseline = %+v", baseline.Windows["30d"])
	}
}

// An import still running is not an earlier import: the first compute of a workspace that is importing
// leaves the baseline alone, so it is never measured from half the tickets (the finished run names it).
func TestAnImportStillRunningIsNotTakenAsTheBaseline(t *testing.T) {
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, pool, "importing")
	conversation := f.conversation("imported", ago(40), []byte(`{"system":"freshdesk","external_id":"1"}`), in(0), out(time.Hour))
	f.exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status,updated_at) VALUES($1,'freshdesk:account:5','ticket','1',$2,'done',$3)`,
		f.workspace, conversation, ago(35))
	f.exec(`INSERT INTO import_jobs(workspace_id,kind,source,domain,state,phase) VALUES($1,'import','freshdesk','acme.freshdesk.com','running','full')`, f.workspace)
	worker := NewWorker(database.New(pool))
	worker.now = func() time.Time { return measured }
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var source *string
	if err := pool.QueryRow(context.Background(), `SELECT baseline_source FROM results_state WHERE workspace_id=$1`, f.workspace).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != nil {
		t.Fatalf("baseline = %s while its import is still running, want none until the run finishes", *source)
	}
	var baselines int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM results_snapshots WHERE workspace_id=$1 AND kind='baseline'`, f.workspace).Scan(&baselines); err != nil || baselines != 0 {
		t.Fatalf("a baseline was measured from a running import: %d snapshots, err %v", baselines, err)
	}
}

// A received notice the channel sent by itself is no first reply: the conversation it alone answered is
// still waiting, and one the team answered later counts the team's reply.
func TestTheChannelsReceivedNoticeIsNoFirstReply(t *testing.T) {
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, pool, "acme")
	notice := func(conversation string, at time.Time) {
		f.exec(`INSERT INTO messages(workspace_id,conversation_id,kind,body,notice_kind,created_at) VALUES($1,$2,'outgoing','We got it','received',$3)`, f.workspace, conversation, at)
	}
	answered := f.conversation("email", ago(2), nil, in(0), out(20*time.Minute))
	notice(answered, ago(2).Add(time.Second))
	waiting := f.conversation("email", ago(3), nil, in(0))
	notice(waiting, ago(3).Add(time.Second))
	worker := NewWorker(database.New(pool))
	worker.now = func() time.Time { return measured }
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, members := f.snapshot("current")
	if got := current.Windows["30d"].FirstResponse; got.Answered != 1 || got.Waiting != 1 {
		t.Fatalf("first response = %+v, want 1 answered and 1 waiting", got)
	}
	if got := members["30d."+MetricFirstResponseWaiting]; len(got) != 1 || got[0] != waiting {
		t.Fatalf("waiting = %v, want %s", got, waiting)
	}
	if got := members["30d."+MetricFirstResponse]; len(got) != 1 || got[0] != answered {
		t.Fatalf("answered = %v, want %s", got, answered)
	}
}
