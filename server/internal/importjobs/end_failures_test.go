package importjobs_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

// refuseMilestone makes every import_finished insert fail, as a bug or a database fault would; the returned
// func lifts it (also on cleanup).
func (e *env) refuseMilestone(t *testing.T) func() {
	t.Helper()
	e.exec(t, `CREATE FUNCTION refuse_import_finished() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'import_finished refused for the test'; END $$`)
	e.exec(t, `CREATE TRIGGER refuse_import_finished BEFORE INSERT ON workspace_events FOR EACH ROW WHEN (NEW.kind = 'import_finished') EXECUTE FUNCTION refuse_import_finished()`)
	lifted := false
	lift := func() {
		if !lifted {
			lifted = true
			e.exec(t, `DROP TRIGGER refuse_import_finished ON workspace_events`)
			e.exec(t, `DROP FUNCTION refuse_import_finished()`)
		}
	}
	t.Cleanup(lift)
	return lift
}

// due makes a job that is not over runnable now: its lease and its wait are both past.
func (e *env) due(t *testing.T, id string) {
	t.Helper()
	e.exec(t, `UPDATE import_jobs SET lease_until=now()-interval '1 second', next_run_at=now()-interval '1 second'
		WHERE id=$1 AND state NOT IN ('finished','failed','cancelled')`, id)
}

// An end transaction that keeps failing is not retried for ever: each rerun ticks progress for the tickets it
// skips, which clears the crash count, so the end failures are counted on their own. After three the job fails
// with a stated error; the report and what was imported stay, and the key is erased.
func TestAnEndThatKeepsFailingFailsTheJobAndKeepsWhatWasImported(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.refuseMilestone(t)
	e.exec(t, `INSERT INTO import_items (workspace_id, source, entity, source_id, status) VALUES ($1, $2, 'ticket', '1001', 'done')`,
		e.a.id, *job.AccountSource)
	w := e.worker(t, e.keys)
	runs := 0
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		if o.Mode == freshdesk.ModeFull {
			runs++
			// The rerun walks the account again: every skipped ticket is progress.
			o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 0, Total: 2})
			o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 2, Total: 2})
		}
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)

	for range 10 {
		tick(t, w)
		if s := e.get(t, job.ID).State; s == importjobs.StateFailed || s == importjobs.StateFinished {
			break
		}
		e.due(t, job.ID)
	}

	got := e.get(t, job.ID)
	if got.State != importjobs.StateFailed || code(got) != importjobs.CodeEndFailed {
		t.Fatalf("job = %+v after %d runs whose end failed, want failed with %q", got, runs, importjobs.CodeEndFailed)
	}
	if runs != 3 {
		t.Fatalf("the full pass ran %d times, want 3: the end failures are bounded", runs)
	}
	if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "import_finished refused for the test") {
		t.Fatalf("error message = %v, want it to say what refused the end", got.ErrorMessage)
	}
	if !got.HasReport || got.ImportedConversations == nil || *got.ImportedConversations != 1 || !e.keyErased(t, job.ID) {
		t.Fatalf("job = %+v, want the report kept, the imported conversation counted and the key erased", got)
	}
	if n := e.column(t, job.ID, "(SELECT count(*) FROM import_items i WHERE i.workspace_id=import_jobs.workspace_id AND i.source=import_jobs.account_source)").(int64); n != 1 {
		t.Fatalf("import_items = %d after the failed end, want the imported ticket kept", n)
	}
}

// A refused end waits before it runs again and says so; it is not a Freshdesk outage.
func TestARefusedEndWaitsToRetryAndSaysWhy(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.refuseMilestone(t)
	w := e.worker(t, e.keys)
	e.stubOK(t, w)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeEndFailed {
		t.Fatalf("job = %+v after one refused end, want retry_wait with %q", got, importjobs.CodeEndFailed)
	}
	if e.column(t, job.ID, "claim_token IS NULL AND lease_until IS NULL AND next_run_at > now()").(bool) != true {
		t.Fatal("a refused end kept its claim or runs again at once")
	}
}

// Without a key ring the key lives in this process's memory only. An end that rolled back has not ended the
// job, so the key stays and the next run ends it without asking the person for the key again.
func TestWithoutAKeyRingARefusedEndKeepsTheKey(t *testing.T) {
	e := newEnv(t, false)
	job := e.started(t)
	lift := e.refuseMilestone(t)
	w := e.worker(t, e.keys)
	e.stubOK(t, w)
	tick(t, w)
	lift()
	e.due(t, job.ID)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished || got.Status == nil || *got.Status != "OK" {
		t.Fatalf("job = %+v after the refusal was lifted, want finished OK (not waiting for its key)", got)
	}
}
