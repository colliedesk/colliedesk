package trialreminder_test

import (
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/trialreminder"
	"github.com/jackc/pgx/v5"
)

func at(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

type sentMail struct{ To, Raw string }

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []sentMail
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
	m.sent = append(m.sent, sentMail{To: to[0], Raw: string(message)})
	return nil
}

func (m *recordingMailer) to() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var addresses []string
	for _, mail := range m.sent {
		addresses = append(addresses, mail.To)
	}
	return addresses
}

// A trial that ends on Friday 13 November 2026 at 09:00 in Ho Chi Minh City (02:00 UTC).
const trialEnd = "2026-11-13T02:00:00Z"

type env struct {
	db      *database.DB
	store   *store.Store
	mailer  *recordingMailer
	now     time.Time
	service *trialreminder.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := databasetest.Pool(t)
	e := &env{db: database.New(pool), store: store.New(pool).WithChannels(registry.New(registry.Config{})), mailer: &recordingMailer{}}
	e.service = e.newService()
	return e
}

// newService is one server process: a second one is a second replica, or the same server after a restart.
func (e *env) newService() *trialreminder.Service {
	return trialreminder.New(trialreminder.Config{DB: e.db, Mailer: e.mailer, From: "noreply@mail.colliedesk.example", ReplyTo: "support@colliedesk.example",
		Origin: "https://app.colliedesk.example", Now: func() time.Time { return e.now }})
}

type workspace struct{ ID, Slug string }

// workspace creates a workspace in a timezone whose trial ends at end, with the admins given, one plain
// agent and one admin who left (deactivated).
func (e *env) workspace(t *testing.T, name, timezone, end string, admins ...string) workspace {
	t.Helper()
	ctx := context.Background()
	created, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, name, admins[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range admins[1:] {
		e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'admin')`, created.ID, extra)
	}
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'agent')`, created.ID, "agent@"+created.Slug+".example")
	e.exec(t, `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,$2,'admin',$3)`, created.ID, "left@"+created.Slug+".example", at("2026-10-20T00:00:00Z"))
	e.exec(t, `UPDATE workspaces SET timezone=$2 WHERE id=$1`, created.ID, timezone)
	e.setTrialEnd(t, workspace{ID: created.ID}, end)
	return workspace{ID: created.ID, Slug: created.Slug}
}

func (e *env) setTrialEnd(t *testing.T, w workspace, end string) {
	t.Helper()
	if _, err := e.store.SetTrialEnd(context.Background(), w.ID, at(end), "test", at("2026-10-15T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) pay(t *testing.T, w workspace) {
	t.Helper()
	period := at("2027-02-13T00:00:00Z")
	_, err := e.store.ApplySubscription(context.Background(), w.ID, "hook-paid", "subscription.updated", domain.Subscription{
		Plan: domain.PlanTeam, SubscriptionID: "sub_1", CustomerID: "cus_1", Status: "active", CurrentPeriodEnd: &period, ModifiedAt: at("2026-10-16T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
}

// run looks for due reminders at now and says how many emails went out in that pass.
func (e *env) run(t *testing.T, now string) int {
	t.Helper()
	e.now = at(now)
	before := len(e.mailer.to())
	if _, err := e.service.RunOnce(context.Background()); err != nil {
		t.Fatalf("run at %s: %v", now, err)
	}
	return len(e.mailer.to()) - before
}

type parsed struct{ Subject, From, ReplyTo, Body string }

func parse(t *testing.T, raw string) parsed {
	t.Helper()
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	return parsed{Subject: message.Header.Get("Subject"), From: message.Header.Get("From"), ReplyTo: message.Header.Get("Reply-To"), Body: string(body)}
}

func TestNothingIsSentFourDaysBeforeTheTrialEnds(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	if sent := e.run(t, "2026-11-09T02:00:00Z"); sent != 0 {
		t.Fatalf("4 days before the end: %d emails, want 0", sent)
	}
	if sent := e.run(t, "2026-11-10T01:59:59Z"); sent != 0 {
		t.Fatalf("one second short of 3 days: %d emails, want 0", sent)
	}
}

func TestThreeDaysBeforeEveryActiveAdminGetsTheReminderOnce(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example", "bob@alpha.example")
	e.workspace(t, "Birch Desk", "Asia/Ho_Chi_Minh", "2027-03-01T00:00:00Z", "carl@birch.example")

	if sent := e.run(t, "2026-11-10T02:00:00Z"); sent != 2 {
		t.Fatalf("3 days before the end: %d emails, want 2 (one per active admin)", sent)
	}
	if got := strings.Join(e.mailer.to(), ","); got != "ann@alpha.example,bob@alpha.example" {
		t.Fatalf("recipients = %s, want only Alpha's two active admins (not the agent, the admin who left, or Birch's)", got)
	}
	first := parse(t, e.mailer.sent[0].Raw)
	if first.Subject != "Your Colliedesk trial for Alpha Support ends on November 13" {
		t.Errorf("subject = %q", first.Subject)
	}
	if !strings.Contains(first.From, "<noreply@mail.colliedesk.example>") || !strings.Contains(first.ReplyTo, "<support@colliedesk.example>") {
		t.Errorf("from %q, reply-to %q", first.From, first.ReplyTo)
	}
	for _, want := range []string{
		`The free trial of your Colliedesk workspace "Alpha Support" ends on Friday, November 13, 2026 at 9:00 AM (Asia/Ho_Chi_Minh).`,
		"The workspace locks.", "Nothing is deleted.", "You can still export all your data.",
		"https://app.colliedesk.example/settings/billing", "Chinh, Colliedesk founder",
	} {
		if !strings.Contains(first.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, first.Body)
		}
	}

	// The same pass again, a pass an hour later, a pass on a restarted server and one on a second replica send nothing more.
	e.service = e.newService()
	for _, now := range []string{"2026-11-10T02:00:00Z", "2026-11-10T03:00:00Z", "2026-11-11T02:00:00Z"} {
		if sent := e.run(t, now); sent != 0 {
			t.Fatalf("pass at %s: %d emails, want 0", now, sent)
		}
	}
}

func TestOnTheDayItEndsAdminsGetTheFinalReminderOnceAtLocalMidnight(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	e.run(t, "2026-11-10T02:00:00Z")
	// 23:59:59 on 12 November in Ho Chi Minh City is still the day before the end.
	if sent := e.run(t, "2026-11-12T16:59:59Z"); sent != 0 {
		t.Fatalf("the evening before: %d emails, want 0", sent)
	}
	if sent := e.run(t, "2026-11-12T17:00:00Z"); sent != 1 {
		t.Fatalf("midnight of the end day: %d emails, want 1", sent)
	}
	final := parse(t, e.mailer.sent[1].Raw)
	if final.Subject != "Your Colliedesk trial for Alpha Support ends today" {
		t.Errorf("subject = %q", final.Subject)
	}
	for _, want := range []string{
		`The free trial of your Colliedesk workspace "Alpha Support" ends today, Friday, November 13, 2026, at 9:00 AM (Asia/Ho_Chi_Minh).`,
		"The workspace locks.", "Nothing is deleted.", "You can still export all your data.", "https://app.colliedesk.example/settings/billing", "Chinh, Colliedesk founder",
	} {
		if !strings.Contains(final.Body, want) {
			t.Errorf("final body lacks %q:\n%s", want, final.Body)
		}
	}
	for _, now := range []string{"2026-11-12T17:00:00Z", "2026-11-12T23:00:00Z", "2026-11-13T01:59:59Z"} {
		if sent := e.run(t, now); sent != 0 {
			t.Fatalf("pass at %s: %d emails, want 0 (the final reminder goes once)", now, sent)
		}
	}
}

func TestTheEndDayIsTheDayInTheWorkspaceTimezone(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Utc Desk", "UTC", trialEnd, "ann@utc.example")
	e.run(t, "2026-11-10T02:00:00Z")
	// 17:00 UTC on 12 November is already 13 November in Ho Chi Minh City, but not in this workspace.
	if sent := e.run(t, "2026-11-12T23:59:59Z"); sent != 0 {
		t.Fatalf("23:59:59 UTC the day before: %d emails, want 0", sent)
	}
	if sent := e.run(t, "2026-11-13T00:00:00Z"); sent != 1 {
		t.Fatalf("midnight UTC of the end day: %d emails, want 1", sent)
	}
	if body := parse(t, e.mailer.sent[1].Raw).Body; !strings.Contains(body, "ends today, Friday, November 13, 2026, at 2:00 AM (UTC).") {
		t.Errorf("body = %s", body)
	}
}

func TestAWorkspaceFirstSeenOnTheEndDayGetsOnlyTheFinalReminder(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	if sent := e.run(t, "2026-11-13T00:30:00Z"); sent != 1 {
		t.Fatalf("first pass on the end day: %d emails, want 1", sent)
	}
	if subject := parse(t, e.mailer.sent[0].Raw).Subject; !strings.HasSuffix(subject, "ends today") {
		t.Fatalf("subject = %q, want the final reminder", subject)
	}
	if sent := e.run(t, "2026-11-13T01:00:00Z"); sent != 0 {
		t.Fatalf("later that day: %d emails, want 0", sent)
	}
}

func TestAPayingWorkspaceGetsNothing(t *testing.T) {
	e := newEnv(t)
	paid := e.workspace(t, "Paid Desk", "Asia/Ho_Chi_Minh", trialEnd, "ann@paid.example")
	e.pay(t, paid)
	for _, now := range []string{"2026-11-10T02:00:00Z", "2026-11-12T17:00:00Z", "2026-11-13T01:00:00Z"} {
		if sent := e.run(t, now); sent != 0 {
			t.Fatalf("paid workspace at %s: %d emails, want 0", now, sent)
		}
	}
}

func TestAWorkspaceThatPaysAfterTheFirstReminderGetsNoFinalOne(t *testing.T) {
	e := newEnv(t)
	w := e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	e.run(t, "2026-11-10T02:00:00Z")
	e.pay(t, w)
	if sent := e.run(t, "2026-11-12T17:00:00Z"); sent != 0 {
		t.Fatalf("paid since the first reminder: %d emails, want 0", sent)
	}
}

func TestALockedWorkspaceGetsNothing(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	for _, now := range []string{trialEnd, "2026-11-13T09:00:00Z", "2026-12-01T00:00:00Z"} {
		if sent := e.run(t, now); sent != 0 {
			t.Fatalf("locked workspace at %s: %d emails, want 0", now, sent)
		}
	}
}

func TestAnExtendedTrialGetsRemindersForItsNewEnd(t *testing.T) {
	e := newEnv(t)
	w := e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example")
	e.run(t, "2026-11-10T02:00:00Z")
	// An operator, or a Freshdesk import, moves the end two weeks on: the old reminder no longer tells the truth.
	e.setTrialEnd(t, w, "2026-11-27T02:00:00Z")
	if sent := e.run(t, "2026-11-12T02:00:00Z"); sent != 0 {
		t.Fatalf("15 days before the new end: %d emails, want 0", sent)
	}
	if sent := e.run(t, "2026-11-24T02:00:00Z"); sent != 1 {
		t.Fatalf("3 days before the new end: %d emails, want 1", sent)
	}
	if body := parse(t, e.mailer.sent[1].Raw).Body; !strings.Contains(body, "ends on Friday, November 27, 2026 at 9:00 AM") {
		t.Errorf("body = %s", body)
	}
}

func TestATransientFailureIsRetriedAndNobodyGetsTwoEmails(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example", "bob@alpha.example")
	failing := true
	e.mailer.fail = func(to string) error {
		if failing && to == "ann@alpha.example" {
			return errors.New("451 try again later")
		}
		return nil
	}
	if sent := e.run(t, "2026-11-10T02:00:00Z"); sent != 1 {
		t.Fatalf("first pass: %d emails, want 1 (Bob's)", sent)
	}
	failing = false
	if sent := e.run(t, "2026-11-10T02:05:00Z"); sent != 1 {
		t.Fatalf("second pass: %d emails, want 1 (Ann's retry)", sent)
	}
	if got := strings.Join(e.mailer.to(), ","); got != "bob@alpha.example,ann@alpha.example" {
		t.Fatalf("recipients = %s, want Bob once and then Ann once", got)
	}
	if sent := e.run(t, "2026-11-10T02:10:00Z"); sent != 0 {
		t.Fatalf("third pass: %d emails, want 0", sent)
	}
}

func TestAnAddressTheRelayRefusesForGoodIsNotTriedAgain(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@alpha.example", "bob@alpha.example")
	attempts := 0
	e.mailer.fail = func(to string) error {
		if to == "ann@alpha.example" {
			attempts++
			return &textproto.Error{Code: 550, Msg: "no such mailbox"}
		}
		return nil
	}
	e.run(t, "2026-11-10T02:00:00Z")
	e.run(t, "2026-11-10T02:05:00Z")
	if attempts != 1 {
		t.Fatalf("attempts at the refused address = %d, want 1", attempts)
	}
	if got := strings.Join(e.mailer.to(), ","); got != "bob@alpha.example" {
		t.Fatalf("recipients = %s, want Bob only", got)
	}
}

func TestTwoServersAtTheSameTimeSendEachReminderOnce(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"Alpha", "Birch", "Gamma", "Delta"} {
		e.workspace(t, name+" Support", "Asia/Ho_Chi_Minh", trialEnd, "ann@"+strings.ToLower(name)+".example", "bob@"+strings.ToLower(name)+".example")
	}
	e.now = at("2026-11-10T02:00:00Z")
	var wg sync.WaitGroup
	for range 3 {
		service := e.newService()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.RunOnce(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	seen := map[string]int{}
	for _, address := range e.mailer.to() {
		seen[address]++
	}
	if len(seen) != 8 {
		t.Fatalf("recipients = %v, want the 8 admins", seen)
	}
	for address, count := range seen {
		if count != 1 {
			t.Errorf("%s got %d emails, want 1", address, count)
		}
	}
}

func TestWithoutAMailerARunFailsLoudly(t *testing.T) {
	e := newEnv(t)
	service := trialreminder.New(trialreminder.Config{DB: e.db, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example", Now: func() time.Time { return e.now }})
	e.now = at("2026-11-10T02:00:00Z")
	if _, err := service.RunOnce(context.Background()); !errors.Is(err, trialreminder.ErrNoMailer) {
		t.Fatalf("error = %v, want ErrNoMailer", err)
	}
}
