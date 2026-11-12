package mentionmail_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/mentionmail"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
	fail error
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.sent = append(m.sent, to[0]+"\n"+string(message))
	return nil
}

type env struct {
	ctx                  context.Context
	db                   *database.DB
	st                   *store.Store
	workspace, writer    string
	ann, quiet           string
	conversation, mailer string
	mail                 *recordingMailer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := databasetest.Pool(t)
	e := &env{ctx: ctx, db: database.New(pool), st: store.New(pool).WithChannels(registry.New(registry.Config{})), mail: &recordingMailer{}}
	workspace, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, "Northwind Help", "tu@northwind.example.com")
	if err != nil {
		t.Fatal(err)
	}
	e.workspace = workspace.ID
	e.writer = e.exec(t, `UPDATE agents SET display_name='Hoang Tu' WHERE workspace_id=$1 RETURNING id::text`, e.workspace)
	e.ann = e.exec(t, `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'long@northwind.example.com','Long Bui','agent') RETURNING id::text`, e.workspace)
	e.quiet = e.exec(t, `INSERT INTO agents(workspace_id,email,role,mention_email) VALUES($1,'quiet@northwind.example.com','agent',false) RETURNING id::text`, e.workspace)
	contact := e.exec(t, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Hana','hana@kumo.example.com') RETURNING id::text`, e.workspace)
	e.conversation = e.exec(t, `INSERT INTO conversations(workspace_id,contact_id,subject,channel) VALUES($1,$2,'Refund still not on my card','agent') RETURNING id::text`, e.workspace, contact)
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

func (e *env) sender() mentionmail.Sender {
	return mentionmail.Sender{Store: e.st, Mailer: e.mail, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example"}
}

func (e *env) run(t *testing.T) []string {
	t.Helper()
	if _, err := e.sender().RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.mail.mu.Lock()
	defer e.mail.mu.Unlock()
	sent := e.mail.sent
	e.mail.sent = nil
	return sent
}

// Each mention is emailed once, with the note's start and a link to it, however many rounds run; a person
// who turned these emails off gets none and still has the mention in their list.
func TestOneEmailPerMentionWithTheExcerptAndALink(t *testing.T) {
	e := newEnv(t)
	note, err := e.st.CreateMessage(e.ctx, e.workspace, e.conversation, store.Actor{AgentID: e.writer, Mentions: []string{e.ann, e.quiet}},
		"note", "Bank confirmed RF-2291-08 lands by Thursday. @Long Bui can you check the provider shows it as settled?")
	if err != nil {
		t.Fatal(err)
	}
	sent := e.run(t)
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1: %q", len(sent), sent)
	}
	mail := sent[0]
	for _, want := range []string{"long@northwind.example.com\n", "Subject: Hoang Tu mentioned you: Refund still not on my card",
		"Bank confirmed RF-2291-08 lands by Thursday. @Long Bui can you check the provider shows it as settled?",
		"https://app.colliedesk.example/conversations/" + e.conversation + "#message-" + note.ID, "Account, Preferences"} {
		if !strings.Contains(strings.ReplaceAll(mail, "=\r\n", ""), want) {
			t.Errorf("email lacks %q:\n%s", want, mail)
		}
	}
	if again := e.run(t); len(again) != 0 {
		t.Fatalf("a second round sent again: %q", again)
	}
	// Adding the same person again tells nobody; a new person gets their own email.
	other := e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'loan@northwind.example.com','agent') RETURNING id::text`, e.workspace)
	if _, err := e.st.AddMentions(e.ctx, e.workspace, e.conversation, note.ID, e.writer, []string{e.ann, other}); err != nil {
		t.Fatal(err)
	}
	if added := e.run(t); len(added) != 1 || !strings.HasPrefix(added[0], "loan@northwind.example.com\n") {
		t.Fatalf("after adding loan: %q", added)
	}
}

// A send that fails for now goes again in a later round; a mention whose person left gets no email.
func TestAFailedMentionEmailIsSentLaterAndALeaverGetsNone(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.CreateMessage(e.ctx, e.workspace, e.conversation, store.Actor{AgentID: e.writer, Mentions: []string{e.ann}}, "note", "@Long Bui look"); err != nil {
		t.Fatal(err)
	}
	e.mail.fail = errors.New("relay down")
	if sent := e.run(t); len(sent) != 0 {
		t.Fatalf("sent while the relay was down: %q", sent)
	}
	e.mail.fail = nil
	if sent := e.run(t); len(sent) != 1 {
		t.Fatalf("after the relay came back: %q", sent)
	}
	leaver := e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'gone@northwind.example.com','agent') RETURNING id::text`, e.workspace)
	if _, err := e.st.CreateMessage(e.ctx, e.workspace, e.conversation, store.Actor{AgentID: e.writer, Mentions: []string{leaver}}, "note", "@gone"); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE agents SET deactivated_at=now() WHERE id=$1 RETURNING id::text`, leaver)
	if sent := e.run(t); len(sent) != 0 {
		t.Fatalf("a deactivated person was emailed: %q", sent)
	}
}
