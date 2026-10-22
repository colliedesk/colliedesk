package importjobs_test

import (
	"context"
	"io"
	"mime/quotedprintable"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

type sentMail struct {
	from    string
	to      []string
	message string
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

// Send keeps the message with its body decoded from quoted-printable, as a reader sees it.
func (f *fakeMailer) Send(_ context.Context, from string, to []string, message []byte) error {
	head, body, _ := strings.Cut(string(message), "\r\n\r\n")
	decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMail{from: from, to: to, message: head + "\n\n" + string(decoded)})
	return nil
}

func (e *env) mailingWorker(t *testing.T, mailer *fakeMailer) *importjobs.Worker {
	t.Helper()
	return importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: e.keys, Interval: time.Hour, Heartbeat: 20 * time.Millisecond, Flush: 10 * time.Millisecond,
		Lease: time.Minute, Mailer: mailer, MailFrom: "noreply@mail.example.com", Origin: "https://desk.example.com"})
}

func TestTheAdminsHearWhenAnImportEnds(t *testing.T) {
	e := newEnv(t, true)
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'second-admin@acme.example','admin'),($1,'agent@acme.example','agent')`, e.a.id)
	e.exec(t, `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,'gone@acme.example','admin',now())`, e.a.id)
	job := e.started(t)
	mailer := &fakeMailer{}
	w := e.mailingWorker(t, mailer)
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		if o.Mode == freshdesk.ModeRecent {
			return &freshdesk.Report{Status: freshdesk.StatusInterim, Interim: true}, nil
		}
		report := &freshdesk.Report{Status: freshdesk.StatusOK}
		report.Tickets.Source, report.Tickets.Imported = 300, 300
		return report, nil
	}, nil, nil)
	tick(t, w)
	var to []string
	for _, sent := range mailer.sent {
		to = append(to, sent.to...)
	}
	if strings.Join(to, ",") != "admin@acmesupport.example,second-admin@acme.example" || mailer.sent[0].from != "noreply@mail.example.com" {
		t.Fatalf("to=%v from=%s", to, mailer.sent[0].from)
	}
	mail := mailer.sent[0]
	for _, want := range []string{"Subject: Freshdesk import complete: acme.freshdesk.com", "300 of 300 tickets", "https://desk.example.com/settings/import-freshdesk", "Auto-Submitted: auto-generated"} {
		if !strings.Contains(mail.message, want) {
			t.Errorf("mail lacks %q:\n%s", want, mail.message)
		}
	}
	if strings.Contains(mail.message, testKey) {
		t.Fatal("the mail holds the key")
	}
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished {
		t.Fatalf("job = %+v", got)
	}
}

func TestAStoppedImportIsMailedButAPreviewOrACancelIsNot(t *testing.T) {
	e := newEnv(t, true)
	mailer := &fakeMailer{}
	// A preview: the person is on the page.
	e.preview(t, e.a)
	w := e.mailingWorker(t, mailer)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return nil, freshdesk.ErrPlanNoAPI
	},
		func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) {
			return nil, freshdesk.ErrKeyRejected
		}, nil)
	tick(t, w)
	if len(mailer.sent) != 0 {
		t.Fatalf("a failed preview was mailed: %v", mailer.sent)
	}
	job := e.started(t)
	tick(t, w)
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].message, "Subject: Freshdesk import stopped: acme.freshdesk.com") ||
		!strings.Contains(mailer.sent[0].message, "14-day trial") {
		t.Fatalf("mails = %+v", mailer.sent)
	}
	if got := e.get(t, job.ID); got.ErrorCode == nil || *got.ErrorCode != importjobs.CodePlanNoAPI {
		t.Fatalf("job = %+v", got)
	}
}

func TestWithoutMailTheEndOfAnImportIsOnlyOnThePage(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished {
		t.Fatalf("job = %+v", got)
	}
}
