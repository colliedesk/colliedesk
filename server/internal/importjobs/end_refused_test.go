package importjobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

// refuse makes the first n writes of event on table that match when fail, as a database fault would. A sequence
// counts them (as the owner: the app's role has no use of it), since a refused write rolls back everything else its
// transaction did.
func (e *env) refuse(t *testing.T, event, table, when string, n int) {
	t.Helper()
	e.exec(t, `CREATE SEQUENCE refused_writes`)
	e.exec(t, fmt.Sprintf(`CREATE FUNCTION refuse_write() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$ BEGIN
		IF nextval('refused_writes') <= %d THEN RAISE EXCEPTION 'write refused for the test'; END IF;
		RETURN COALESCE(NEW, OLD); END $$`, n))
	if when != "" {
		when = "WHEN (" + when + ")"
	}
	e.exec(t, fmt.Sprintf(`CREATE TRIGGER refuse_write BEFORE %s ON %s FOR EACH ROW %s EXECUTE FUNCTION refuse_write()`, event, table, when))
}

// refuseEnds refuses the first n writes that move a job to a state it ends in.
func (e *env) refuseEnds(t *testing.T, n int) {
	t.Helper()
	e.refuse(t, "UPDATE", "import_jobs", `NEW.state IS DISTINCT FROM OLD.state AND NEW.state IN ('finished','failed','cancelled')`, n)
}

// An undo commits what it removes before the job ends. A rerun after a refused end does not undo again (the
// ledger is gone, so it would fail with "no import"): it ends the job with the report of the run that undid,
// and the Freshdesk agents of the account go with it.
func TestAnUndoWhoseEndWasRefusedOnceEndsOnTheNextRunWithItsReport(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewWithAgents(t)
	e.exec(t, `UPDATE import_jobs SET state='finished', status='OK', started_at=now(), sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, job.ID)
	undo, err := e.service.Undo(context.Background(), e.a.id, e.a.agentID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.refuse(t, "DELETE", "import_people", "", 1)
	w := e.worker(t, e.keys)
	var undos atomic.Int32
	importjobs.StubImporter(w, nil, nil, func(_ context.Context, o freshdesk.UndoOptions) (freshdesk.UndoResult, error) {
		if undos.Add(1) > 1 {
			// What freshdesk.Undo answers once its ledger is gone.
			return freshdesk.UndoResult{}, freshdesk.ErrNoImport
		}
		return freshdesk.UndoResult{Source: o.Source, Conversations: 5}, nil
	})
	tick(t, w)
	if got := e.get(t, undo.ID); got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeEndFailed {
		t.Fatalf("undo = %s %s %v after its end was refused, want retry_wait with %q", got.State, code(got), *got.ErrorMessage, importjobs.CodeEndFailed)
	}
	e.due(t, undo.ID)
	tick(t, w)

	got := e.get(t, undo.ID)
	if got.State != importjobs.StateFinished || got.ErrorCode != nil || !got.HasReport {
		t.Fatalf("undo = %+v (code %s), want finished with its report", got, code(got))
	}
	if n := undos.Load(); n != 1 {
		t.Fatalf("the undo ran %d times, want once: what it removed was committed", n)
	}
	raw, markdown, ok, err := e.service.Report(context.Background(), e.a.id, undo.ID)
	var report freshdesk.UndoResult
	if err != nil || !ok || json.Unmarshal(raw, &report) != nil || report.Conversations != 5 || markdown == nil || !strings.Contains(*markdown, "Conversations removed: 5") {
		t.Fatalf("report = %s markdown=%v ok=%v err=%v, want the report of the run that undid", raw, markdown, ok, err)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM import_people WHERE workspace_id=$1`, e.a.id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the undo left %d Freshdesk agents (%v)", n, err)
	}
}

// A preview whose end is refused does not wait to retry: it would take the slot of the one active import, or,
// with an import active, stay previewing and run again for ever. It ends with its own reason.
func TestAPreviewWhoseEndIsRefusedFailsWithItsOwnReason(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprintf("another import active %v", active), func(t *testing.T) {
			e := newEnv(t, true)
			var other importjobs.Job
			if active {
				other = e.started(t)
				e.exec(t, `UPDATE import_jobs SET state='retry_wait', next_run_at=now()+interval '1 hour' WHERE id=$1`, other.ID)
			}
			job := e.preview(t, e.a)
			e.refuseEnds(t, 1)
			w := e.worker(t, e.keys)
			var previews atomic.Int32
			importjobs.StubImporter(w, nil, func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) {
				previews.Add(1)
				return nil, freshdesk.ErrUnavailable
			}, nil)
			tick(t, w)
			e.due(t, job.ID)
			tick(t, w)

			got := e.get(t, job.ID)
			if got.State != importjobs.StateFailed || code(got) != importjobs.CodeUnavailable || !e.keyErased(t, job.ID) {
				t.Fatalf("preview = %+v (code %s), want failed with %q and its key erased", got, code(got), importjobs.CodeUnavailable)
			}
			if n := previews.Load(); n != 1 {
				t.Fatalf("the preview ran %d times, want once", n)
			}
			if active {
				if o := e.get(t, other.ID); o.State != importjobs.StateRetryWait {
					t.Fatalf("the active import = %+v, want it left waiting", o)
				}
			} else if a, err := e.service.Active(context.Background(), e.a.id); err != nil || a != nil {
				t.Fatalf("active = %+v (%v), want no active import", a, err)
			}
		})
	}
}

// An end of its own (a code that says why, or a stop the person asked for) whose write is refused keeps that
// state and code: end_failed is for the end's other writes.
func TestAFailureWithItsOwnCodeKeepsItWhenItsEndIsRefused(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.refuseEnds(t, 1)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return nil, freshdesk.ErrKeyRejected
	}, nil, nil)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateFailed || code(got) != importjobs.CodeKeyRejected || !e.keyErased(t, job.ID) {
		t.Fatalf("job = %+v (code %s), want failed with %q", got, code(got), importjobs.CodeKeyRejected)
	}
}

// A stop whose end is refused, even twice, ends cancelled; the run after it ends the job without asking
// Freshdesk again.
func TestACancelWhoseEndIsRefusedStaysACancelAndDoesNotRunAgain(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.refuseEnds(t, 2)
	w := e.worker(t, e.keys)
	var runs atomic.Int32
	running := make(chan struct{}, 1)
	importjobs.StubImporter(w, func(ctx context.Context, _ freshdesk.Options) (*freshdesk.Report, error) {
		runs.Add(1)
		running <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-running
	if _, err := e.service.Cancel(context.Background(), e.a.id, job.ID); err != nil {
		t.Fatal(err)
	}
	w.Wait()
	e.due(t, job.ID)
	tick(t, w)

	got := e.get(t, job.ID)
	if got.State != importjobs.StateCancelled || got.ErrorCode != nil || got.CancelRequested || !e.keyErased(t, job.ID) {
		t.Fatalf("job = %+v (code %s), want cancelled", got, code(got))
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("Freshdesk was asked %d times, want once: the stop was asked for", n)
	}
}

// A preview whose last write (the job moving to previewed, with the Freshdesk agents it read) keeps being refused
// fails at once with end_failed: a preview is not retried in the background, so it neither takes the slot of the
// one active import nor reads Freshdesk again. The person sees why and can ask again.
func TestAPreviewWhoseLastWriteKeepsBeingRefusedFailsWithoutReadingFreshdeskAgain(t *testing.T) {
	for name, refuse := range map[string]func(e *env, t *testing.T){
		"the job":              func(e *env, t *testing.T) { e.refuse(t, "UPDATE", "import_jobs", `NEW.state = 'previewed'`, 1000) },
		"the Freshdesk agents": func(e *env, t *testing.T) { e.refuse(t, "INSERT", "import_people", "", 1000) },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, true)
			job := e.preview(t, e.a)
			refuse(e, t)
			w := e.worker(t, e.keys)
			var previews atomic.Int32
			importjobs.StubImporter(w, nil, func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) {
				previews.Add(1)
				return &freshdesk.PreviewResult{Domain: "acme.freshdesk.com", AccountID: 777, Source: "freshdesk:account:777",
					Agents: []freshdesk.PreviewAgent{{ID: 101, Name: "Alice", Email: "alice@old.example", Active: true}}}, nil
			}, nil)
			for range 5 {
				tick(t, w)
				e.due(t, job.ID)
			}

			got := e.get(t, job.ID)
			if got.State != importjobs.StateFailed || code(got) != importjobs.CodeEndFailed || !e.keyErased(t, job.ID) {
				t.Fatalf("preview = %+v (code %s), want failed with %q and its key erased", got, code(got), importjobs.CodeEndFailed)
			}
			if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "write refused for the test") {
				t.Fatalf("error message = %v, want it to say what refused the write", got.ErrorMessage)
			}
			if n := previews.Load(); n != 1 {
				t.Fatalf("Freshdesk was read %d times, want once", n)
			}
			if a, err := e.service.Active(context.Background(), e.a.id); err != nil || a != nil {
				t.Fatalf("active = %+v (%v), want no active import", a, err)
			}
		})
	}
}
