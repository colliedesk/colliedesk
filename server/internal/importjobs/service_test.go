package importjobs_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

func (e *env) preview(t *testing.T, w workspace) importjobs.Job {
	t.Helper()
	job, err := e.service.Preview(context.Background(), w.id, w.agentID, "https://Acme.freshdesk.com/", testKey, importjobs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// previewed arranges what the worker does after a preview.
func (e *env) previewed(t *testing.T, w workspace) importjobs.Job {
	t.Helper()
	job := e.preview(t, w)
	e.exec(t, `UPDATE import_jobs SET state='previewed', account_source='freshdesk:account:777', preview='{}' WHERE id=$1`, job.ID)
	return job
}

func TestPreviewSealsTheKeyForItsWorkspaceAndJobOnly(t *testing.T) {
	e := newEnv(t, true)
	job := e.preview(t, e.a)
	if job.State != importjobs.StatePreviewing || job.Phase != importjobs.PhasePreview || job.Domain != "acme.freshdesk.com" || !job.HasKey {
		t.Fatalf("job = %+v", job)
	}
	sealed := e.column(t, job.ID, "sealed_key").([]byte)
	if len(sealed) == 0 || bytes.Contains(sealed, []byte(testKey)) {
		t.Fatalf("sealed key = %q", sealed)
	}
	key, err := importjobs.OpenKeyForTest(e.keys, e.a.id, job.ID, sealed, "k1")
	if err != nil || key != testKey {
		t.Fatalf("open = %q, %v", key, err)
	}
	// Copied to another job or workspace, the ciphertext does not open.
	other := e.preview(t, e.b)
	if _, err := importjobs.OpenKeyForTest(e.keys, e.b.id, other.ID, sealed, "k1"); err == nil {
		t.Fatal("a key sealed for another workspace and job opened")
	}
	if _, err := importjobs.OpenKeyForTest(e.keys, e.a.id, other.ID, sealed, "k1"); err == nil {
		t.Fatal("a key sealed for another job opened")
	}
}

func TestPreviewWithoutAKeyRingKeepsTheKeyOnlyInMemory(t *testing.T) {
	e := newEnv(t, false)
	job := e.preview(t, e.a)
	if job.HasKey || !e.keyErased(t, job.ID) {
		t.Fatalf("a server without a key ring stored the key: %+v", job)
	}
	key, err := importjobs.OpenKeyForTest(e.keys, e.a.id, job.ID, nil, "")
	if err != nil || key != testKey {
		t.Fatalf("memory key = %q, %v", key, err)
	}
	if _, err := e.service.Cancel(context.Background(), e.a.id, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := importjobs.OpenKeyForTest(e.keys, e.a.id, job.ID, nil, ""); err == nil {
		t.Fatal("the in-memory key outlived the cancelled preview")
	}
}

func TestPreviewRefusesWhatIsNotAFreshdeskAccount(t *testing.T) {
	e := newEnv(t, true)
	for _, domain := range []string{"acme.zendesk.com", "evil.com/acme.freshdesk.com", ""} {
		if _, err := e.service.Preview(context.Background(), e.a.id, e.a.agentID, domain, testKey, importjobs.Options{}); !errors.Is(err, freshdesk.ErrAccountHost) {
			t.Errorf("%q: err = %v", domain, err)
		}
	}
	days := 400
	if _, err := e.service.Preview(context.Background(), e.a.id, e.a.agentID, "acme.freshdesk.com", testKey, importjobs.Options{RecentDays: &days}); !errors.Is(err, importjobs.ErrOptions) {
		t.Fatalf("recent_days 400: err = %v", err)
	}
}

func TestANewPreviewDiscardsTheOneNobodyStarted(t *testing.T) {
	e := newEnv(t, true)
	first := e.previewed(t, e.a)
	second := e.preview(t, e.a)
	got, err := e.service.Get(context.Background(), e.a.id, first.ID)
	if err != nil || got.State != importjobs.StateDiscarded || !e.keyErased(t, first.ID) {
		t.Fatalf("first = %+v, %v; erased=%v", got, err, e.keyErased(t, first.ID))
	}
	if second.State != importjobs.StatePreviewing {
		t.Fatalf("second = %+v", second)
	}
}

func TestStartQueuesTheRecentPassAndOnlyOneImportRunsPerWorkspace(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	job := e.preview(t, e.a)
	if _, err := e.service.Start(ctx, e.a.id, job.ID); !errors.Is(err, importjobs.ErrState) {
		t.Fatalf("start while previewing: %v", err)
	}
	e.exec(t, `UPDATE import_jobs SET state='previewed', account_source='freshdesk:account:777' WHERE id=$1`, job.ID)
	started, err := e.service.Start(ctx, e.a.id, job.ID)
	if err != nil || started.State != importjobs.StateQueued || started.Phase != importjobs.PhaseRecent || started.StartedAt == nil {
		t.Fatalf("started = %+v, %v", started, err)
	}
	active, err := e.service.Active(ctx, e.a.id)
	if err != nil || active == nil || active.ID != job.ID {
		t.Fatalf("active = %+v, %v", active, err)
	}
	second := e.previewed(t, e.a)
	if _, err := e.service.Start(ctx, e.a.id, second.ID); !errors.Is(err, importjobs.ErrActive) {
		t.Fatalf("second start: %v, want ErrActive", err)
	}
	// Another workspace is not blocked.
	other := e.previewed(t, e.b)
	if _, err := e.service.Start(ctx, e.b.id, other.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStartWithoutARecentWindowGoesStraightToTheFullPass(t *testing.T) {
	e := newEnv(t, true)
	none := 0
	job, err := e.service.Preview(context.Background(), e.a.id, e.a.agentID, "acme.freshdesk.com", testKey, importjobs.Options{RecentDays: &none})
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE import_jobs SET state='previewed' WHERE id=$1`, job.ID)
	started, err := e.service.Start(context.Background(), e.a.id, job.ID)
	if err != nil || started.Phase != importjobs.PhaseFull {
		t.Fatalf("started = %+v, %v", started, err)
	}
}

func TestAPreviewOlderThanAnHourCannotStart(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewed(t, e.a)
	e.exec(t, `UPDATE import_jobs SET updated_at=now()-interval '61 minutes' WHERE id=$1`, job.ID)
	if _, err := e.service.Start(context.Background(), e.a.id, job.ID); !errors.Is(err, importjobs.ErrState) {
		t.Fatalf("err = %v, want ErrState", err)
	}
}

func TestCancelErasesTheKeyOfEveryJobItEnds(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	for _, state := range []string{"previewing", "previewed", "queued", "retry_wait"} {
		job := e.preview(t, e.a)
		e.exec(t, `UPDATE import_jobs SET state=$2, account_source='freshdesk:account:777' WHERE id=$1`, job.ID, state)
		got, err := e.service.Cancel(ctx, e.a.id, job.ID)
		want := importjobs.StateCancelled
		if state == "previewing" || state == "previewed" {
			want = importjobs.StateDiscarded
		}
		if err != nil || got.State != want || got.HasKey || !e.keyErased(t, job.ID) || got.FinishedAt == nil {
			t.Fatalf("cancel %s: %+v, %v", state, got, err)
		}
	}
	running := e.preview(t, e.a)
	e.exec(t, `UPDATE import_jobs SET state='running', phase='full', claim_token=gen_random_uuid(), lease_until=now()+interval '2 minutes' WHERE id=$1`, running.ID)
	got, err := e.service.Cancel(ctx, e.a.id, running.ID)
	if err != nil || got.State != importjobs.StateRunning || !got.CancelRequested || !got.HasKey {
		t.Fatalf("cancel running: %+v, %v (the worker ends it and erases the key)", got, err)
	}
	if _, err := e.service.Cancel(ctx, e.a.id, running.ID); err != nil {
		t.Fatalf("a second cancel of a running job: %v", err)
	}
	e.exec(t, `UPDATE import_jobs SET state='finished', sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, running.ID)
	if _, err := e.service.Cancel(ctx, e.a.id, running.ID); !errors.Is(err, importjobs.ErrState) {
		t.Fatalf("cancel finished: %v", err)
	}
}

func TestSetKeyResumesAJobWaitingForItsKey(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	job := e.preview(t, e.a)
	e.exec(t, `UPDATE import_jobs SET state='waiting_key', phase='full', sealed_key=NULL, sealed_key_id=NULL, error_code='key_unavailable' WHERE id=$1`, job.ID)
	got, err := e.service.SetKey(ctx, e.a.id, job.ID, "new-key")
	if err != nil || got.State != importjobs.StateQueued || got.Phase != importjobs.PhaseFull || !got.HasKey || got.ErrorCode != nil {
		t.Fatalf("set key: %+v, %v", got, err)
	}
	if _, err := e.service.SetKey(ctx, e.a.id, job.ID, "again"); !errors.Is(err, importjobs.ErrState) {
		t.Fatalf("set key on a queued job: %v", err)
	}
}

func TestUndoQueuesAJobForAnImportThatEnded(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	job := e.previewed(t, e.a)
	if _, err := e.service.Undo(ctx, e.a.id, e.a.agentID, job.ID); !errors.Is(err, importjobs.ErrState) {
		t.Fatalf("undo of a preview: %v", err)
	}
	e.exec(t, `UPDATE import_jobs SET state='finished', status='OK', sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, job.ID)
	undo, err := e.service.Undo(ctx, e.a.id, e.a.agentID, job.ID)
	if err != nil || undo.Kind != importjobs.KindUndo || undo.State != importjobs.StateQueued || undo.UndoOf == nil || *undo.UndoOf != job.ID ||
		undo.AccountSource == nil || *undo.AccountSource != "freshdesk:account:777" || undo.HasKey {
		t.Fatalf("undo = %+v, %v", undo, err)
	}
	if _, err := e.service.Undo(ctx, e.a.id, e.a.agentID, job.ID); !errors.Is(err, importjobs.ErrActive) {
		t.Fatalf("second undo while the first is queued: %v", err)
	}
}

func TestAnotherWorkspaceCannotSeeOrTouchAJob(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	job := e.previewed(t, e.a)
	b := e.b.id
	if _, err := e.service.Get(ctx, b, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, _, _, err := e.service.Report(ctx, b, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("report: %v", err)
	}
	if _, err := e.service.Start(ctx, b, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("start: %v", err)
	}
	if _, err := e.service.Cancel(ctx, b, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("cancel: %v", err)
	}
	e.exec(t, `UPDATE import_jobs SET state='waiting_key', sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, job.ID)
	if _, err := e.service.SetKey(ctx, b, job.ID, "k"); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("set key: %v", err)
	}
	e.exec(t, `UPDATE import_jobs SET state='finished' WHERE id=$1`, job.ID)
	if _, err := e.service.Undo(ctx, b, e.b.agentID, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("undo: %v", err)
	}
	if list, err := e.service.List(ctx, b, 0); err != nil || len(list) != 0 {
		t.Errorf("list = %+v, %v", list, err)
	}
	if active, err := e.service.Active(ctx, b); err != nil || active != nil {
		t.Errorf("active = %+v, %v", active, err)
	}
	if got, err := e.service.Get(ctx, e.a.id, job.ID); err != nil || got.State != importjobs.StateFinished {
		t.Fatalf("the job changed: %+v, %v", got, err)
	}
	if _, err := e.service.Get(ctx, e.a.id, "not-a-uuid"); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("malformed id: %v", err)
	}
}
