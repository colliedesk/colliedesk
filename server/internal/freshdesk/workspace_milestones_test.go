package freshdesk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5"
)

func (e *importEnv) milestones(t *testing.T) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT kind||' '||to_char(at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')||' '||detail::text
		FROM workspace_events WHERE workspace_id=$1 AND kind<>'signed_up' ORDER BY id`, e.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Imported history is no customer writing in and no team answering in Colliedesk; only the import that
// finished OK is a milestone, at the time its run finished.
func TestAnImportRecordsOnlyItsSuccessfulFinish(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failAttachment = "9002"
	failedAt := time.Date(2026, time.October, 2, 8, 0, 0, 0, time.UTC)
	report, err := env.run(t, context.Background(), func(options *freshdesk.Options) { options.Now = func() time.Time { return failedAt } })
	if err != nil || report.Status != freshdesk.StatusFailed {
		t.Fatalf("first run: %v %+v, want FAILED", err, report)
	}
	if env.count(t, "messages") == 0 {
		t.Fatal("the fixture imported no messages")
	}
	if got := env.milestones(t); len(got) != 0 {
		t.Fatalf("milestones after a FAILED import = %v, want none", got)
	}

	env.fake.failAttachment = ""
	finishedAt := time.Date(2026, time.October, 2, 9, 30, 0, 0, time.UTC)
	report, err = env.run(t, context.Background(), func(options *freshdesk.Options) { options.Now = func() time.Time { return finishedAt } })
	if err != nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("retry: %v %+v, want OK", err, report)
	}
	rerunAt := finishedAt.Add(time.Hour)
	if _, err := env.run(t, context.Background(), func(options *freshdesk.Options) { options.Now = func() time.Time { return rerunAt } }); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(env.milestones(t), "\n"); got != `import_finished 2026-10-02T09:30:00Z {"importer": "freshdesk"}` {
		t.Fatalf("milestones = %q, want only the first OK finish", got)
	}
}

// A run whose finish marker cannot be written must say so: the milestone is written with the marker, in one
// transaction, and a rerun writes both. A log line would leave the workspace without its milestone unseen.
func TestAnImportWhoseFinishCannotBeRecordedReturnsTheError(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	for _, statement := range []string{
		`CREATE FUNCTION refuse_import_finished() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'import_finished refused for the test'; END $$`,
		`CREATE TRIGGER refuse_import_finished BEFORE INSERT ON workspace_events FOR EACH ROW WHEN (NEW.kind = 'import_finished') EXECUTE FUNCTION refuse_import_finished()`,
	} {
		if _, err := env.pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		for _, statement := range []string{`DROP TRIGGER refuse_import_finished ON workspace_events`, `DROP FUNCTION refuse_import_finished()`} {
			if _, err := env.pool.Exec(ctx, statement); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(drop)

	report, err := env.run(t, ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "import_finished refused") {
		t.Fatalf("run with the milestone refused: err=%v, want the error returned", err)
	}
	if report == nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("report = %+v, want the OK report kept next to the error", report)
	}
	if got := env.milestones(t); len(got) != 0 {
		t.Fatalf("milestones = %v, want none: the marker and the milestone commit together", got)
	}

	drop()
	if report, err = env.run(t, ctx, nil); err != nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("rerun: %v %+v", err, report)
	}
	if got := env.milestones(t); len(got) != 1 || !strings.HasPrefix(got[0], "import_finished ") {
		t.Fatalf("milestones after the rerun = %v, want one import_finished", got)
	}
}
