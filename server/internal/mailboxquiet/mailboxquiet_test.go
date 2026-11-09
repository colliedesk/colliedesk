package mailboxquiet_test

import (
	"context"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/mailboxquiet"
	"github.com/colliedesk/colliedesk/server/internal/store"
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
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, address := range to {
		m.sent = append(m.sent, sentMail{To: address, Raw: string(message)})
	}
	return nil
}

type env struct {
	db     *database.DB
	store  *store.Store
	mailer *recordingMailer
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := databasetest.Pool(t)
	// The hosted cloud: mail is forwarded to <local>@<slug>.mail.colliedesk.example, and replies can leave.
	email := channelemail.Config{HostedMailDomain: "mail.colliedesk.example", PlatformForwarding: true, Outbound: store.OutboundPolicy{Enabled: true}}
	return &env{db: database.New(pool), store: store.New(pool).WithChannels(registry.New(registry.Config{Email: email})), mailer: &recordingMailer{}}
}

func (e *env) worker(mailer mailboxquiet.Mailer) mailboxquiet.Worker {
	return mailboxquiet.Worker{Store: e.store, Mailer: mailer, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example", HelpCenterDomain: "colliedesk.example",
		HostedMailDomain: "mail.colliedesk.example", Now: func() time.Time { return e.now }}
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

type mailbox struct{ WorkspaceID, ChannelID string }

// mailbox makes a workspace in Ho Chi Minh City with its admin, a teammate without the right to change
// channels and an admin who left, and its support address.
func (e *env) mailbox(t *testing.T, name, admin, address string) mailbox {
	t.Helper()
	ctx := context.Background()
	created, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, name, admin)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE workspaces SET timezone='Asia/Ho_Chi_Minh' WHERE id=$1`, created.ID)
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'agent')`, created.ID, "agent@"+created.Slug+".example")
	e.exec(t, `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,$2,'admin',$3)`, created.ID, "left@"+created.Slug+".example", at("2026-10-01T00:00:00Z"))
	channel, err := e.store.UpsertEmailChannel(ctx, created.ID, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	return mailbox{WorkspaceID: created.ID, ChannelID: channel.ChannelID}
}

// arrives records an email received on the mailbox at the given time, as the inbound path does.
func (e *env) arrives(t *testing.T, m mailbox, when time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,received_at) VALUES($1,$2,'processed','{}',$3)`, m.WorkspaceID, m.ChannelID, when)
}

// run is one pass of the worker at now; it returns how many emails went out.
func (e *env) run(t *testing.T, worker mailboxquiet.Worker, now time.Time) int {
	t.Helper()
	e.now = now
	sent, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("pass at %s: %v", now.Format(time.RFC3339), err)
	}
	return sent
}

// status is what the channel API shows of the mailbox.
func (e *env) status(t *testing.T, m mailbox) channels.Status {
	t.Helper()
	list, err := e.store.ListChannels(context.Background(), m.WorkspaceID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, public := range list {
		if public.Channel.ID == m.ChannelID {
			return public.Status
		}
	}
	t.Fatalf("channel %s not listed", m.ChannelID)
	return channels.Status{}
}

func body(t *testing.T, raw string) (string, string) {
	t.Helper()
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	text, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	return message.Header.Get("Subject"), string(text)
}

// The last email before the silence: Thursday 5 November 2026, 09:12 in Ho Chi Minh City.
var lastEmail = at("2026-11-05T02:12:00Z")

// everyDayFor7 is mail on each of the 7 days up to lastEmail.
func everyDayFor7(t *testing.T, e *env, m mailbox) {
	for day := 6; day >= 0; day-- {
		e.arrives(t, m, lastEmail.Add(-time.Duration(day)*24*time.Hour))
	}
}

const wantWarning = "No email has arrived at support@northwind.example since Thu 5 Nov 09:12 +07. " +
	"Check that forwarding to support@northwind-help.mail.colliedesk.example is still on. This clears by itself when the next email arrives."

func TestABusyMailboxQuietFor25HoursWarnsOnceAndTheNextEmailClearsIt(t *testing.T) {
	e := newEnv(t)
	quiet := e.mailbox(t, "Northwind Help", "ann@northwind.example", "support@northwind.example")
	everyDayFor7(t, e, quiet)
	// Another workspace, busy and still receiving: its admin hears nothing.
	other := e.mailbox(t, "Birch Desk", "carl@birch.example", "help@birch.example")
	for hour := 0; hour <= 8*24; hour += 6 {
		e.arrives(t, other, lastEmail.Add(-7*24*time.Hour+time.Duration(hour)*time.Hour))
	}
	worker := e.worker(e.mailer)

	if sent := e.run(t, worker, lastEmail.Add(23*time.Hour)); sent != 0 {
		t.Fatalf("23 hours of silence: %d emails, want 0", sent)
	}
	if got := e.status(t, quiet); got.State != channels.StateWorking {
		t.Fatalf("23 hours of silence: status %+v, want working", got)
	}

	if sent := e.run(t, worker, lastEmail.Add(25*time.Hour)); sent != 1 {
		t.Fatalf("25 hours of silence: %d emails, want 1", sent)
	}
	if e.mailer.sent[0].To != "ann@northwind.example" {
		t.Fatalf("recipient = %s, want only the active admin of the quiet workspace", e.mailer.sent[0].To)
	}
	got := e.status(t, quiet)
	if got.State != channels.StateNeedsAttention || got.ReasonCode != "no_recent_mail" || got.Message != wantWarning {
		t.Fatalf("status = %+v, want needs_attention no_recent_mail %q", got, wantWarning)
	}
	if other := e.status(t, other); other.State != channels.StateWorking {
		t.Fatalf("the busy workspace's channel = %+v, want working", other)
	}
	subject, text := body(t, e.mailer.sent[0].Raw)
	if subject != "No email has reached support@northwind.example since Thu 5 Nov 09:12 +07" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{wantWarning, "https://northwind-help.colliedesk.example/settings/channels/" + quiet.ChannelID} {
		if !strings.Contains(strings.ReplaceAll(text, "\r\n", "\n"), want) {
			t.Errorf("email lacks %q:\n%s", want, text)
		}
	}

	// The same quiet spell: no second email, the warning stays.
	if sent := e.run(t, worker, lastEmail.Add(30*time.Hour)); sent != 0 {
		t.Fatalf("a later pass of the same silence: %d emails, want 0", sent)
	}
	if got := e.status(t, quiet); got.ReasonCode != "no_recent_mail" {
		t.Fatalf("a later pass: status %+v, want the warning to stay", got)
	}

	// The next email clears it by itself.
	e.arrives(t, quiet, lastEmail.Add(31*time.Hour))
	if got := e.status(t, quiet); got.State != channels.StateWorking {
		t.Fatalf("after the next email: status %+v, want working", got)
	}
	if sent := e.run(t, worker, lastEmail.Add(32*time.Hour)); sent != 0 {
		t.Fatalf("after the next email: %d emails, want 0", sent)
	}
}

func TestAMailboxWithMailOnThreeOfSevenDaysNeverWarns(t *testing.T) {
	e := newEnv(t)
	m := e.mailbox(t, "Northwind Help", "ann@northwind.example", "support@northwind.example")
	for _, day := range []int{6, 3, 0} {
		e.arrives(t, m, lastEmail.Add(-time.Duration(day)*24*time.Hour))
	}
	worker := e.worker(e.mailer)
	for _, after := range []time.Duration{25 * time.Hour, 49 * time.Hour, 10 * 24 * time.Hour} {
		if sent := e.run(t, worker, lastEmail.Add(after)); sent != 0 {
			t.Fatalf("%s after the last email: %d emails, want 0", after, sent)
		}
		if got := e.status(t, m); got.State != channels.StateWorking {
			t.Fatalf("%s after the last email: status %+v, want working", after, got)
		}
	}
}

func TestAServerThatCannotSendMailStillShowsTheWarning(t *testing.T) {
	e := newEnv(t)
	m := e.mailbox(t, "Northwind Help", "ann@northwind.example", "support@northwind.example")
	everyDayFor7(t, e, m)
	if sent := e.run(t, e.worker(nil), lastEmail.Add(25*time.Hour)); sent != 0 {
		t.Fatalf("without a mailer: %d emails, want 0", sent)
	}
	if got := e.status(t, m); got.ReasonCode != "no_recent_mail" {
		t.Fatalf("without a mailer: status %+v, want the warning", got)
	}
	// Once the server can send mail, the admins of a spell still quiet hear of it, once.
	if sent := e.run(t, e.worker(e.mailer), lastEmail.Add(26*time.Hour)); sent != 1 {
		t.Fatalf("with a mailer later in the spell: %d emails, want 1", sent)
	}
}
