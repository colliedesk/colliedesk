package importjobs_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

func TestImportNoticeRetriesAfterRestartWithoutMailingAnotherWorkspace(t *testing.T) {
	e := newEnv(t, true)
	e.started(t)
	mailer := &fakeMailer{err: errors.New("relay unavailable")}
	w := e.mailingWorker(t, mailer)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, w)
	if len(mailer.sent) != 0 {
		t.Fatal("relay accepted mail while unavailable")
	}
	mailer.err = nil
	w = importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: e.keys, Mailer: mailer, MailFrom: "noreply@mail.example.com",
		Origin: "https://desk.example.com", Now: func() time.Time { return time.Now().Add(2 * time.Minute) }})
	tick(t, w)
	if len(mailer.sent) != 1 || strings.Join(mailer.sent[0].to, ",") != "admin@acmesupport.example" {
		t.Fatalf("restarted worker deliveries=%+v", mailer.sent)
	}
	tick(t, w)
	if len(mailer.sent) != 1 {
		t.Fatalf("retry duplicated notice: %d", len(mailer.sent))
	}
}

func TestIncompleteImportNoticeListsMissingItemsAndRecovery(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	// The worker has already used its automatic retries; this is the final partial result.
	e.exec(t, `UPDATE import_jobs SET full_retries=2 WHERE id=$1`, job.ID)
	mailer := &fakeMailer{}
	w := e.mailingWorker(t, mailer)
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		if o.Mode == freshdesk.ModeRecent {
			return &freshdesk.Report{Status: freshdesk.StatusInterim, Interim: true}, nil
		}
		return &freshdesk.Report{Status: freshdesk.StatusFailed, Tickets: freshdesk.TicketCount{EntityCount: freshdesk.EntityCount{Source: 3, Imported: 2, Failed: 1}},
			Missing: []string{"ticket 42"}, Failures: []freshdesk.Failure{{Entity: "attachment", FreshdeskID: "84"}}}, nil
	}, nil, nil)
	tick(t, w)
	if len(mailer.sent) != 1 {
		t.Fatalf("notices=%d", len(mailer.sent))
	}
	for _, want := range []string{"Freshdesk import incomplete", "2 of 3 tickets", "Not imported: ticket 42", "Not imported: attachment 84", "start a new import to retry", "Nothing is copied twice"} {
		if !strings.Contains(mailer.sent[0].message, want) {
			t.Errorf("mail lacks %q: %s", want, mailer.sent[0].message)
		}
	}
}

func TestUndoDoesNotSendAnImportNotice(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	mailer := &fakeMailer{}
	w := e.mailingWorker(t, mailer)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, w)
	if _, err := e.service.Undo(t.Context(), e.a.id, e.a.agentID, job.ID); err != nil {
		t.Fatal(err)
	}
	importjobs.StubImporter(w, nil, nil, func(context.Context, freshdesk.UndoOptions) (freshdesk.UndoResult, error) {
		return freshdesk.UndoResult{}, nil
	})
	tick(t, w)
	if len(mailer.sent) != 1 {
		t.Fatalf("undo sent another notice: %d", len(mailer.sent))
	}
}

func TestMoveImportNoticeIncludesTheSameNextActionAndOptionalHelpCenter(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			e := newEnv(t, true)
			e.exec(t, `INSERT INTO workspace_onboarding(workspace_id,goal) VALUES($1,'leave_helpdesk')`, e.a.id)
			if enabled {
				e.exec(t, `INSERT INTO help_centers(workspace_id,enabled) VALUES($1,true) ON CONFLICT(workspace_id) DO UPDATE SET enabled=true`, e.a.id)
				e.exec(t, `WITH collection AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Guides','guides') RETURNING id)
					INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
					SELECT $1,id,1,'Welcome','welcome','Welcome.','published',now() FROM collection`, e.a.id)
			}
			e.started(t)
			mailer := &fakeMailer{}
			w := e.mailingWorker(t, mailer)
			importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
				return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
			}, nil, nil)
			tick(t, w)
			if len(mailer.sent) != 1 {
				t.Fatalf("notices=%d", len(mailer.sent))
			}
			body := mailer.sent[0].message
			want := "Next step: Turn on the help center"
			if enabled {
				want = "Next step: Verify your domain by TXT"
				if !strings.Contains(body, "View help center: https://desk.example.com/help/"+e.a.slug) {
					t.Fatalf("no optional view link: %s", body)
				}
			}
			if !strings.Contains(body, want) || strings.Contains(body, "I checked the preview") {
				t.Fatalf("next action: %s", body)
			}
		})
	}
}

func TestConcurrentImportNoticeWorkersSendOnce(t *testing.T) {
	e := newEnv(t, true)
	e.started(t)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, w)
	mailer := &fakeMailer{}
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			worker := e.mailingWorker(t, mailer)
			if err := worker.Tick(t.Context()); err != nil {
				t.Error(err)
			}
			worker.Wait()
		}()
	}
	workers.Wait()
	if len(mailer.sent) != 1 {
		t.Fatalf("concurrent deliveries=%d", len(mailer.sent))
	}
}
