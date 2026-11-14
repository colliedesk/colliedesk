package importjobs_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

func (e *env) importFinishedLog(t *testing.T) string {
	t.Helper()
	var list []string
	rows, err := e.pool.Query(context.Background(), `SELECT detail::text FROM workspace_events WHERE workspace_id=$1 AND kind='import_finished'`, e.a.id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		list = append(list, detail)
	}
	return strings.Join(list, "|")
}

func (e *env) stubOK(t *testing.T, w *importjobs.Worker) *int {
	runs := new(int)
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		if !o.MilestoneByCaller {
			// The in-app worker writes the milestone with the end of the job; the importer must leave it.
			t.Error("the worker did not ask the importer to leave the import_finished milestone to it")
		}
		if o.Mode == freshdesk.ModeFull {
			*runs++
		}
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	return runs
}

// The milestone commits with the end of the job: one transaction, so a job that ended OK has its milestone.
func TestAnInAppImportRecordsItsFinishWithTheEndOfTheJob(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	e.stubOK(t, w)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished || got.Status == nil || *got.Status != "OK" {
		t.Fatalf("job = %+v, want finished OK", got)
	}
	if got := e.importFinishedLog(t); !strings.Contains(got, `"importer": "freshdesk"`) || strings.Contains(got, "|") {
		t.Fatalf("import_finished = %q, want one freshdesk row", got)
	}
}

// A refused milestone never ends the job as failed and never discards what the run did: the end rolls back with
// the milestone, the job waits to retry (not failed), and the next run ends it again.
func TestARefusedMilestoneDoesNotFailTheImportAndTheNextRunEndsIt(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `CREATE FUNCTION refuse_import_finished() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'import_finished refused for the test'; END $$`)
	e.exec(t, `CREATE TRIGGER refuse_import_finished BEFORE INSERT ON workspace_events FOR EACH ROW WHEN (NEW.kind = 'import_finished') EXECUTE FUNCTION refuse_import_finished()`)
	dropped := false
	drop := func() {
		if !dropped {
			dropped = true
			e.exec(t, `DROP TRIGGER refuse_import_finished ON workspace_events`)
			e.exec(t, `DROP FUNCTION refuse_import_finished()`)
		}
	}
	t.Cleanup(drop)
	w := e.worker(t, e.keys)
	runs := e.stubOK(t, w)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State == importjobs.StateFailed || got.State == importjobs.StateFinished {
		t.Fatalf("job = %+v after a refused milestone, want neither failed nor finished: the end rolled back", got)
	}
	if e.importFinishedLog(t) != "" {
		t.Fatal("a milestone was written although its insert was refused")
	}

	drop()
	e.due(t, job.ID)
	tick(t, w)
	got = e.get(t, job.ID)
	if got.State != importjobs.StateFinished || got.Status == nil || *got.Status != "OK" || !got.HasReport || got.FinishedAt == nil {
		t.Fatalf("job = %+v after the next run, want finished OK with its report", got)
	}
	if *runs != 2 || !strings.Contains(e.importFinishedLog(t), `"importer": "freshdesk"`) {
		t.Fatalf("runs=%d log=%q, want the full pass run twice (the ledger skips what is done) and one milestone", *runs, e.importFinishedLog(t))
	}
}
