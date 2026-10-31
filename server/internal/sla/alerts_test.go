package sla_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/sla"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	subject := ""
	for _, line := range strings.Split(string(message), "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			subject = strings.TrimPrefix(line, "Subject: ")
		}
	}
	m.sent = append(m.sent, to[0]+" "+subject)
	return nil
}

func (m *recordingMailer) take() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	sent := strings.Join(m.sent, "|")
	m.sent = nil
	return sent
}

func every(minutes int) []domain.BusinessDay {
	var hours []domain.BusinessDay
	for _, day := range domain.Weekdays {
		hours = append(hours, domain.BusinessDay{Day: day, Open: true, From: "00:00", To: "23:59"})
	}
	return hours
}

// A running clock sends its reminder when it warns and its overdue email once due, each once however
// many servers run: to the assignee, or the admins while nobody is assigned, each unless they turned these
// emails off. A threshold from before the conversation came in sends nothing, nor a workspace whose emails
// or targets are off.
func TestRemindersAndOverdueEmailsGoOnce(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	db := database.New(pool)
	st := store.New(pool).WithChannels(registry.New(registry.Config{}))
	workspace, _, err := auth.NewWithDB(db, "https://app.colliedesk.example").CreateWorkspace(ctx, "Alert Desk", "admin@alert.example.com")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) string {
		t.Helper()
		var out string
		if err := db.Privileged(ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, sql, args...).Scan(&out)
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return out
	}
	exec(`INSERT INTO agents(workspace_id,email,role) VALUES($1,'quiet@alert.example.com','admin') RETURNING id::text`, workspace.ID)
	exec(`UPDATE agents SET sla_alert_email=false WHERE email='quiet@alert.example.com' RETURNING id::text`)
	exec(`INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,'left@alert.example.com','admin',now()) RETURNING id::text`, workspace.ID)
	assignee := exec(`INSERT INTO agents(workspace_id,email,role) VALUES($1,'ann@alert.example.com','agent') RETURNING id::text`, workspace.ID)
	hour := 60
	if _, err := st.SetResponseTargets(ctx, workspace.ID, "", store.ResponseTargets{Enabled: true, Timezone: ptr("UTC"), Hours: every(0), WarnBeforeMinutes: 10, AlertEmail: true,
		Targets: map[string]store.TargetMinutes{"normal": {FirstResponse: &hour}}}); err != nil {
		t.Fatal(err)
	}
	contact := exec(`INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@alert.example.com') RETURNING id::text`, workspace.ID)
	opened := func(subject string, ago time.Duration, since time.Duration, assigned bool) string {
		t.Helper()
		var who any
		if assigned {
			who = assignee
		}
		id := exec(`INSERT INTO conversations(workspace_id,contact_id,subject,channel,assignee_id) VALUES($1,$2,$3,'agent',$4::uuid) RETURNING id::text`, workspace.ID, contact, subject, who)
		exec(`UPDATE conversations SET created_at=now()-make_interval(secs => $2),sla_alert_since=now()-make_interval(secs => $3),sla_stale=true WHERE id=$1 RETURNING id::text`,
			id, ago.Seconds(), since.Seconds())
		return id
	}
	opened("Assigned", 55*time.Minute, 55*time.Minute, true)
	opened("Nobody's", 55*time.Minute, 55*time.Minute, false)
	opened("Came in late", 3*time.Hour, 0, false)
	if _, err := (sla.Worker{Store: st}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	mailer := &recordingMailer{}
	now := time.Now().UTC()
	alerts := func() sla.Alerts {
		return sla.Alerts{Store: st, Mailer: mailer, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example", Now: func() time.Time { return now }}
	}
	// Two servers at once: each email goes once.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := alerts().RunOnce(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := mailer.take(); !sameSet(got, "admin@alert.example.com Reply due soon: #2 Nobody's|ann@alert.example.com Reply due soon: #1 Assigned") {
		t.Fatalf("reminders = %s", got)
	}
	if _, err := alerts().RunOnce(ctx); err != nil || mailer.take() != "" {
		t.Fatalf("sent again: %v", err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := alerts().RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mailer.take(); !sameSet(got, "admin@alert.example.com Reply overdue: #2 Nobody's|ann@alert.example.com Reply overdue: #1 Assigned") {
		t.Fatalf("overdue = %s", got)
	}
	// The conversation's own history shows what went.
	one, err := st.ListConversations(ctx, workspace.ID, store.ConversationFilter{Page: 1, PageSize: 10, Search: "Assigned"})
	if err != nil || len(one.Items) != 1 || one.Items[0].SLA == nil || len(one.Items[0].SLA.Alerts) != 2 {
		t.Fatalf("history = %+v %v", one.Items, err)
	}
	// Emails off for the workspace: nothing, even for a new threshold.
	opened("Off", 55*time.Minute, 55*time.Minute, true)
	if _, err := (sla.Worker{Store: st}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	targets, _ := st.GetResponseTargets(ctx, workspace.ID)
	targets.AlertEmail = false
	if _, err := st.SetResponseTargets(ctx, workspace.ID, "", targets); err != nil {
		t.Fatal(err)
	}
	if _, err := (sla.Worker{Store: st}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := alerts().RunOnce(ctx); err != nil || mailer.take() != "" {
		t.Fatalf("sent with emails off: %v", err)
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

func ptr[T any](value T) *T { return &value }
