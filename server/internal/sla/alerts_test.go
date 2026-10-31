package sla_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/sla"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
	raw  []string
	fail func(to string) error
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		if err := m.fail(to[0]); err != nil {
			return err
		}
	}
	subject := ""
	for _, line := range strings.Split(string(message), "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			subject = strings.TrimPrefix(line, "Subject: ")
		}
	}
	m.sent = append(m.sent, to[0]+" "+subject)
	m.raw = append(m.raw, string(message))
	return nil
}

func (m *recordingMailer) take() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	sent := strings.Join(m.sent, "|")
	m.sent = nil
	return sent
}

func every() []domain.BusinessDay {
	var hours []domain.BusinessDay
	for _, day := range domain.Weekdays {
		hours = append(hours, domain.BusinessDay{Day: day, Open: true, From: "00:00", To: "24:00"})
	}
	return hours
}

type env struct {
	ctx       context.Context
	db        *database.DB
	st        *store.Store
	workspace string
	assignee  string
	contact   string
	mailer    *recordingMailer
	now       time.Time
}

func newEnv(t *testing.T, timezone string) *env {
	t.Helper()
	ctx := context.Background()
	pool := databasetest.Pool(t)
	e := &env{ctx: ctx, db: database.New(pool), st: store.New(pool).WithChannels(registry.New(registry.Config{})), mailer: &recordingMailer{}, now: time.Now().UTC()}
	workspace, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, "Alert Desk", "admin@alert.example.com")
	if err != nil {
		t.Fatal(err)
	}
	e.workspace = workspace.ID
	e.exec(t, `INSERT INTO agents(workspace_id,email,role,sla_alert_email) VALUES($1,'quiet@alert.example.com','admin',false) RETURNING id::text`, e.workspace)
	e.exec(t, `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,'left@alert.example.com','admin',now()) RETURNING id::text`, e.workspace)
	e.assignee = e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'ann@alert.example.com','agent') RETURNING id::text`, e.workspace)
	hour := 60
	if _, err := e.st.SetResponseTargets(ctx, e.workspace, "", store.ResponseTargets{Enabled: true, Timezone: &timezone, Hours: every(), WarnBeforeMinutes: 10, AlertEmail: true,
		Targets: map[string]store.TargetMinutes{"normal": {FirstResponse: &hour}}}); err != nil {
		t.Fatal(err)
	}
	e.contact = e.exec(t, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@alert.example.com') RETURNING id::text`, e.workspace)
	return e
}

func (e *env) exec(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := e.db.Privileged(e.ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, sql, args...).Scan(&out)
	}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return out
}

// opened opens a conversation ago before now, which came in since before now.
func (e *env) opened(t *testing.T, subject string, ago, since time.Duration, assigned bool) string {
	t.Helper()
	var who any
	if assigned {
		who = e.assignee
	}
	id := e.exec(t, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,assignee_id) VALUES($1,$2,$3,'agent',$4::uuid) RETURNING id::text`, e.workspace, e.contact, subject, who)
	e.exec(t, `UPDATE conversations SET created_at=now()-make_interval(secs => $2),sla_alert_since=now()-make_interval(secs => $3),sla_stale=true WHERE id=$1 RETURNING id::text`,
		id, ago.Seconds(), since.Seconds())
	if _, err := (sla.Worker{Store: e.st}).RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) alerts() sla.Alerts {
	return sla.Alerts{Store: e.st, Mailer: e.mailer, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example", Now: func() time.Time { return e.now }}
}

func (e *env) run(t *testing.T) string {
	t.Helper()
	if _, err := e.alerts().RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	return e.mailer.take()
}

// A running clock sends its reminder when it warns and its overdue email once due, each once however
// many servers run: to the assignee, or the admins while nobody is assigned, each unless they turned these
// emails off. A threshold from before the conversation came in sends nothing, nor a workspace whose emails
// are off.
func TestRemindersAndOverdueEmailsGoOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	e.opened(t, "Assigned", 55*time.Minute, 55*time.Minute, true)
	e.opened(t, "Nobody's", 55*time.Minute, 55*time.Minute, false)
	e.opened(t, "Came in late", 3*time.Hour, 0, false)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.alerts().RunOnce(e.ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := e.mailer.take(); !sameSet(got, "admin@alert.example.com Reply due soon: #2 Nobody's|ann@alert.example.com Reply due soon: #1 Assigned") {
		t.Fatalf("reminders = %s", got)
	}
	if got := e.run(t); got != "" {
		t.Fatalf("sent again: %s", got)
	}
	e.now = e.now.Add(10 * time.Minute)
	if got := e.run(t); !sameSet(got, "admin@alert.example.com Reply overdue: #2 Nobody's|ann@alert.example.com Reply overdue: #1 Assigned") {
		t.Fatalf("overdue = %s", got)
	}
	one, err := e.st.ListConversations(e.ctx, e.workspace, store.ConversationFilter{Page: 1, PageSize: 10, Search: "Assigned"})
	if err != nil || len(one.Items) != 1 || one.Items[0].SLA == nil || len(one.Items[0].SLA.Alerts) != 2 || one.Items[0].SLA.Alerts[0].Status != "sent" {
		t.Fatalf("history = %+v %v", one.Items, err)
	}
	e.opened(t, "Off", 55*time.Minute, 55*time.Minute, true)
	targets, _ := e.st.GetResponseTargets(e.ctx, e.workspace)
	targets.AlertEmail = false
	if _, err := e.st.SetResponseTargets(e.ctx, e.workspace, "", targets); err != nil {
		t.Fatal(err)
	}
	if got := e.run(t); got != "" {
		t.Fatalf("sent with emails off: %s", got)
	}
}

// Each recipient gets an email once: one that failed for now gets it in a later round, without the
// others getting it again. A claim left by a server that stopped mid-send is taken again.
func TestEachRecipientGetsItsEmailOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'two@alert.example.com','admin') RETURNING id::text`, e.workspace)
	failed := false
	e.mailer.fail = func(to string) error {
		if to == "two@alert.example.com" && !failed {
			failed = true
			return errors.New("the relay is busy")
		}
		return nil
	}
	e.opened(t, "Nobody's", 55*time.Minute, 55*time.Minute, false)
	if got := e.run(t); got != "admin@alert.example.com Reply due soon: #1 Nobody's" {
		t.Fatalf("first round = %s", got)
	}
	if got := e.run(t); got != "two@alert.example.com Reply due soon: #1 Nobody's" {
		t.Fatalf("second round = %s", got)
	}
	if got := e.run(t); got != "" {
		t.Fatalf("third round = %s", got)
	}
	// A server stopped between its claim and its send: after a while the claim is taken again.
	stuck := e.opened(t, "Stuck", 55*time.Minute, 55*time.Minute, true)
	warn := e.exec(t, `SELECT to_char(sla_first_due_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM conversations WHERE id=$1`, stuck)
	e.exec(t, `INSERT INTO conversation_sla_alerts(workspace_id,conversation_id,clock,threshold,due_at,recipient,status,created_at)
		VALUES($1,$2,'first_response','reminder',$3::timestamptz,'ann@alert.example.com','sending',now()-interval '1 minute') RETURNING id::text`, e.workspace, stuck, warn)
	if got := e.run(t); got != "" {
		t.Fatalf("a claim still being sent was taken: %s", got)
	}
	e.exec(t, `UPDATE conversation_sla_alerts SET created_at=now()-interval '11 minutes' WHERE conversation_id=$1 RETURNING id::text`, stuck)
	if got := e.run(t); got != "ann@alert.example.com Reply due soon: #2 Stuck" {
		t.Fatalf("a stopped claim = %s", got)
	}
	// Nobody to tell is recorded once.
	e.exec(t, `UPDATE agents SET sla_alert_email=false WHERE email='ann@alert.example.com' RETURNING id::text`)
	quiet := e.opened(t, "Quiet", 55*time.Minute, 55*time.Minute, true)
	for range 2 {
		if got := e.run(t); got != "" {
			t.Fatalf("an opted-out assignee got %s", got)
		}
	}
	if got := e.exec(t, `SELECT count(*)::text||':'||string_agg(status,',') FROM conversation_sla_alerts WHERE conversation_id=$1`, quiet); got != "1:nobody" {
		t.Fatalf("nobody = %s", got)
	}
}

// The email gives the time in the timezone the targets count in.
func TestTheEmailGivesTheTimeOfTheTargets(t *testing.T) {
	e := newEnv(t, "Asia/Tokyo")
	e.opened(t, "Zoned", 55*time.Minute, 55*time.Minute, true)
	e.run(t)
	if len(e.mailer.raw) != 1 || !strings.Contains(e.mailer.raw[0], "JST") {
		t.Fatalf("email = %v", e.mailer.raw)
	}
}

// A claim is taken again only after any send could have finished: longer than the relay's timeout.
func TestAStoppedClaimOutlastsASlowSend(t *testing.T) {
	if sla.AbandonedAfter <= email.SMTPTimeout+email.SMTPDialTimeout {
		t.Fatalf("a claim is taken again after %s, while a send may take %s", sla.AbandonedAfter, email.SMTPTimeout+email.SMTPDialTimeout)
	}
}

func sameSet(got, want string) bool {
	g, w := strings.Split(got, "|"), strings.Split(want, "|")
	if len(g) != len(w) {
		return false
	}
	seen := map[string]int{}
	for _, item := range g {
		seen[item]++
	}
	for _, item := range w {
		if seen[item] == 0 {
			return false
		}
		seen[item]--
	}
	return true
}

// The recipients shrink after some got the email (one turned theirs off before a retry): the threshold is
// done with those who got it, not listed again every round.
func TestAThresholdIsDoneWhenFewerAreLeftToTell(t *testing.T) {
	e := newEnv(t, "UTC")
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'two@alert.example.com','admin') RETURNING id::text`, e.workspace)
	e.mailer.fail = func(to string) error {
		if to == "two@alert.example.com" {
			return errors.New("the relay is busy")
		}
		return nil
	}
	e.opened(t, "Nobody's", 55*time.Minute, 55*time.Minute, false)
	if got := e.run(t); got != "admin@alert.example.com Reply due soon: #1 Nobody's" {
		t.Fatalf("first round = %s", got)
	}
	e.exec(t, `UPDATE agents SET sla_alert_email=false WHERE email='two@alert.example.com' RETURNING id::text`)
	for range 2 {
		if got := e.run(t); got != "" {
			t.Fatalf("again = %s", got)
		}
	}
	if due, err := e.st.DueSLAAlerts(e.ctx, e.now, 10); err != nil || len(due) != 0 {
		t.Fatalf("still due = %+v %v", due, err)
	}
}

// Each recipient is claimed just before their email goes: a slow relay never leaves the next ones' claims
// old enough for another server to take them again.
func TestEachRecipientIsClaimedJustBeforeTheirEmail(t *testing.T) {
	e := newEnv(t, "UTC")
	for _, extra := range []string{"two@alert.example.com", "three@alert.example.com"} {
		e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'admin') RETURNING id::text`, e.workspace, extra)
	}
	var claims []string
	e.mailer.fail = func(string) error {
		claims = append(claims, e.exec(t, `SELECT count(*)::text FROM conversation_sla_alerts WHERE status='sending'`))
		return nil
	}
	e.opened(t, "Nobody's", 55*time.Minute, 55*time.Minute, false)
	e.run(t)
	if strings.Join(claims, ",") != "1,1,1" {
		t.Fatalf("claims held at each send = %v, want only the one being sent", claims)
	}
}
