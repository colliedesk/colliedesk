package results

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/jackc/pgx/v5"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent map[string][]string
}

func (m *fakeMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = map[string][]string{}
	}
	m.sent[to[0]] = append(m.sent[to[0]], string(message))
	return nil
}

func TestWeekStartAndNextWeeklyFollowTheWorkspaceTimezone(t *testing.T) {
	// Monday Oct 19, 2026, 07:30 in Berlin (05:30 UTC): this week's email is still to come at 8:00.
	now := time.Date(2026, 10, 19, 5, 30, 0, 0, time.UTC)
	if got, want := WeekStart(now, "Europe/Berlin"), time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("week start = %s, want %s", got, want)
	}
	if got, want := NextWeekly(now, "Europe/Berlin"), time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("next weekly = %s, want %s", got, want)
	}
	// Sunday evening in Los Angeles is already Monday in UTC: the week has not started there.
	sunday := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	if got, want := WeekStart(sunday, "America/Los_Angeles"), time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("LA week start = %s, want %s", got, want)
	}
	if got := WeekStart(now, "Not/AZone"); !got.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("unknown zone week start = %s, want UTC", got)
	}
}

// The email goes on Monday at 8:00 workspace time to every admin who kept it on, once per week however
// many times the worker looks; a workspace seen for the first time waits for the next Monday; a locked
// workspace gets none. It holds totals only.
func TestWeeklyEmailGoesOnceToAdminsWhoKeptItOn(t *testing.T) {
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, pool, "weekly")
	f.exec(`UPDATE workspaces SET timezone='Europe/Berlin' WHERE id=$1`, f.workspace)
	for _, agent := range []struct {
		email, role string
		weekly      bool
	}{{"ada@weekly.example", "admin", true}, {"bob@weekly.example", "admin", false}, {"cy@weekly.example", "agent", true}} {
		f.exec(`INSERT INTO agents(workspace_id,email,role,results_email) VALUES($1,$2,$3,$4)`, f.workspace, agent.email, agent.role, agent.weekly)
	}
	monday := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC) // 9:00 in Berlin
	conversation := f.conversation("email", monday.Add(-2*Day), nil, in(0), out(30*time.Minute))
	_ = conversation
	mailer := &fakeMailer{}
	locked := false
	worker := NewWorker(database.New(pool)).WithMail(Mail{Mailer: mailer, From: "noreply@mail.example", Origin: "https://desk.example/",
		Locked: func(context.Context, pgx.Tx, string, time.Time) (bool, error) { return locked, nil }})
	run := func(at time.Time) {
		t.Helper()
		worker.now = func() time.Time { return at }
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	run(monday)
	if len(mailer.sent) != 0 {
		t.Fatalf("a workspace seen for the first time got an email mid-week: %v", mailer.sent)
	}
	nextMonday := monday.Add(7 * Day)
	run(nextMonday.Add(-90 * time.Minute)) // 7:30 in Berlin
	if len(mailer.sent) != 0 {
		t.Fatalf("an email went before 8:00: %v", mailer.sent)
	}
	run(nextMonday)
	run(nextMonday.Add(2 * time.Hour))
	if len(mailer.sent) != 1 || len(mailer.sent["ada@weekly.example"]) != 1 {
		t.Fatalf("sent = %v, want one email to ada", mailer.sent)
	}
	message := mailer.sent["ada@weekly.example"][0]
	for _, want := range []string{"Subject: weekly weekly results: first reply", "List-Unsubscribe: <https://desk.example/results#weekly-email>",
		"Auto-Submitted: auto-generated", "Weekly results for weekly", "Every number, and the conversations behind it: https://desk.example/results"} {
		if !strings.Contains(message, want) {
			t.Errorf("email lacks %q:\n%s", want, message)
		}
	}
	for _, secret := range []string{"Casey", "casey@", "Help"} {
		if strings.Contains(message, secret) {
			t.Errorf("email contains %q:\n%s", secret, message)
		}
	}

	locked = true
	run(nextMonday.Add(7 * Day))
	if len(mailer.sent["ada@weekly.example"]) != 1 {
		t.Fatalf("a locked workspace got an email")
	}
}
