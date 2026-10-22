package importjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5"
)

// Service is what the API does with import jobs. Every call names the workspace; a job of another
// workspace is ErrNotFound. Nothing here talks to Freshdesk: the worker does.
type Service struct {
	db   *database.DB
	keys *Keys
	now  func() time.Time
}

// NewService uses db (request pool) and the process's key store, shared with the worker.
func NewService(db *database.DB, keys *Keys) *Service {
	return &Service{db: db, keys: keys, now: time.Now}
}

// SetClock replaces the clock; tests use it.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Service) inWorkspace(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	return s.db.InWorkspace(ctx, workspaceID, fn)
}

// withKey marks a job whose key only this process's memory holds.
func (s *Service) withKey(job Job) Job {
	if !job.HasKey && s.keys.held(job.ID) {
		job.HasKey = true
	}
	return job
}

// known refuses an id that is not a UUID as not found, before it reaches a query.
func known(id string) error {
	if !uuidPattern.MatchString(id) {
		return ErrNotFound
	}
	return nil
}

// Preview creates a job that reads the Freshdesk account in the background and writes nothing. It
// discards the workspace's earlier previews nobody started. agentID is the person or token's agent.
func (s *Service) Preview(ctx context.Context, workspaceID, agentID, domain, apiKey string, options Options) (Job, error) {
	host, err := freshdesk.AccountHost(domain)
	if err != nil {
		return Job{}, err
	}
	if apiKey == "" {
		return Job{}, errors.New("the Freshdesk API key is empty")
	}
	if !options.valid() {
		return Job{}, ErrOptions
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return Job{}, err
	}
	now := s.now().UTC()
	var job Job
	var discarded []string
	err = s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE import_jobs SET state='discarded', sealed_key=NULL, sealed_key_id=NULL, claim_token=NULL, lease_until=NULL,
				finished_at=$2, updated_at=$2
			WHERE workspace_id=$1 AND state IN ('previewing','previewed') RETURNING id::text, account_source`, workspaceID, now)
		if err != nil {
			return fmt.Errorf("discard earlier previews: %w", err)
		}
		type earlier struct {
			id     string
			source *string
		}
		previews, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (earlier, error) {
			var e earlier
			return e, row.Scan(&e.id, &e.source)
		})
		if err != nil {
			return err
		}
		for _, preview := range previews {
			discarded = append(discarded, preview.id)
			if preview.source != nil {
				if err := forgetPeople(ctx, tx, workspaceID, *preview.source); err != nil {
					return err
				}
			}
		}
		var id string
		if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			return err
		}
		sealed, keyID, err := s.keys.seal(workspaceID, id, apiKey)
		if err != nil {
			return fmt.Errorf("seal the Freshdesk API key: %w", err)
		}
		job, err = scanJob(tx.QueryRow(ctx, `INSERT INTO import_jobs(id,workspace_id,kind,source,domain,state,phase,options,sealed_key,sealed_key_id,owner_instance,
				created_by,created_at,updated_at,next_run_at)
			VALUES($1,$2,'import','freshdesk',$3,'previewing','preview',$4,$5,$6,$7,$8,$9,$9,$9) RETURNING `+jobColumns,
			id, workspaceID, host, encoded, sealed, keyID, s.keys.instance, nullable(agentID), now))
		return err
	})
	s.keys.forget(discarded...)
	if err == nil {
		s.keys.keep(job.ID, apiKey)
	}
	return s.withKey(job), err
}

// Get returns one job.
func (s *Service) Get(ctx context.Context, workspaceID, id string) (Job, error) {
	if err := known(id); err != nil {
		return Job{}, err
	}
	var job Job
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		job, err = scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM import_jobs WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
		return err
	})
	return s.withKey(job), err
}

// List returns the workspace's jobs, newest first.
func (s *Service) List(ctx context.Context, workspaceID string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	jobs := []Job{}
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM import_jobs WHERE workspace_id=$1 ORDER BY created_at DESC, id LIMIT $2`, workspaceID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, s.withKey(job))
		}
		return rows.Err()
	})
	return jobs, err
}

// Active returns the job queued, running or waiting in the workspace, if any.
func (s *Service) Active(ctx context.Context, workspaceID string) (*Job, error) {
	var job *Job
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		found, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM import_jobs
			WHERE workspace_id=$1 AND state IN ('queued','running','retry_wait','waiting_key')`, workspaceID))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		found = s.withKey(found)
		job = &found
		return err
	})
	return job, err
}

// Report returns the report of a job as JSON and Markdown: the final one, else the interim report of
// the recent pass (Markdown nil). ok is false when there is none yet.
func (s *Service) Report(ctx context.Context, workspaceID, id string) (report json.RawMessage, markdown *string, ok bool, err error) {
	if err := known(id); err != nil {
		return nil, nil, false, err
	}
	err = s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var final, interim []byte
		if err := tx.QueryRow(ctx, `SELECT report, report_markdown, interim_report FROM import_jobs WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
			Scan(&final, &markdown, &interim); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		switch {
		case final != nil:
			report, ok = final, true
		case interim != nil:
			report, markdown, ok = interim, nil, true
		}
		return nil
	})
	return report, markdown, ok, err
}

// Start queues a previewed import. It runs the recent pass first unless recent_days is 0.
func (s *Service) Start(ctx context.Context, workspaceID, id string) (Job, error) {
	now := s.now().UTC()
	return s.transition(ctx, workspaceID, id, func(tx pgx.Tx) (pgx.Row, error) {
		return tx.QueryRow(ctx, `UPDATE import_jobs SET state='queued',
				phase=CASE WHEN COALESCE((options->>'recent_days')::int, $3)>0 THEN 'recent' ELSE 'full' END,
				started_at=$4, next_run_at=$4, updated_at=$4
			WHERE workspace_id=$1 AND id=$2 AND state='previewed' AND updated_at>$5 RETURNING `+jobColumns,
			workspaceID, id, DefaultRecentDays, now, now.Add(-PreviewTTL)), nil
	})
}

// Cancel stops a job. A preview is discarded, a job not running ends at once, a running one is asked
// to stop and ends within one heartbeat. Imported data stays; undo removes it.
func (s *Service) Cancel(ctx context.Context, workspaceID, id string) (Job, error) {
	now := s.now().UTC()
	job, err := s.transition(ctx, workspaceID, id, func(tx pgx.Tx) (pgx.Row, error) {
		return tx.QueryRow(ctx, `UPDATE import_jobs SET
				state=CASE WHEN state IN ('previewing','previewed') THEN 'discarded' WHEN state='running' THEN 'running' ELSE 'cancelled' END,
				cancel_requested_at=CASE WHEN state='running' THEN $3::timestamptz END,
				finished_at=CASE WHEN state='running' THEN NULL ELSE $3::timestamptz END,
				sealed_key=CASE WHEN state='running' THEN sealed_key END, sealed_key_id=CASE WHEN state='running' THEN sealed_key_id END,
				claim_token=CASE WHEN state='running' THEN claim_token END, lease_until=CASE WHEN state='running' THEN lease_until END,
				updated_at=$3
			WHERE workspace_id=$1 AND id=$2 AND state IN ('previewing','previewed','queued','running','retry_wait','waiting_key') RETURNING `+jobColumns,
			workspaceID, id, now), nil
	})
	if err == nil && job.State != StateRunning {
		s.keys.forget(id)
	}
	if err == nil && job.State == StateDiscarded && job.AccountSource != nil {
		err = s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error { return forgetPeople(ctx, tx, workspaceID, *job.AccountSource) })
	}
	return job, err
}

// SetKey gives a job waiting for its key a new one and queues it again; it goes on with its phase.
func (s *Service) SetKey(ctx context.Context, workspaceID, id, apiKey string) (Job, error) {
	if err := known(id); err != nil {
		return Job{}, err
	}
	if apiKey == "" {
		return Job{}, errors.New("the Freshdesk API key is empty")
	}
	sealed, keyID, err := s.keys.seal(workspaceID, id, apiKey)
	if err != nil {
		return Job{}, fmt.Errorf("seal the Freshdesk API key: %w", err)
	}
	now := s.now().UTC()
	job, err := s.transition(ctx, workspaceID, id, func(tx pgx.Tx) (pgx.Row, error) {
		return tx.QueryRow(ctx, `UPDATE import_jobs SET state='queued', sealed_key=$3, sealed_key_id=$4, owner_instance=$5, error_code=NULL, error_message=NULL,
				attempts_without_progress=0, next_run_at=$6, updated_at=$6
			WHERE workspace_id=$1 AND id=$2 AND state='waiting_key' RETURNING `+jobColumns, workspaceID, id, sealed, keyID, s.keys.instance, now), nil
	})
	if err != nil {
		return Job{}, err
	}
	s.keys.keep(id, apiKey)
	return s.withKey(job), nil
}

// Undo queues an undo of an import that has ended: it removes what that Freshdesk account brought in,
// as `collie import undo` does.
func (s *Service) Undo(ctx context.Context, workspaceID, agentID, id string) (Job, error) {
	now := s.now().UTC()
	return s.transition(ctx, workspaceID, id, func(tx pgx.Tx) (pgx.Row, error) {
		return tx.QueryRow(ctx, `INSERT INTO import_jobs(workspace_id,kind,source,domain,account_source,undo_of,state,phase,created_by,created_at,updated_at,next_run_at,started_at)
			SELECT workspace_id,'undo',source,domain,account_source,id,'queued','undo',$3,$4,$4,$4,$4 FROM import_jobs
			WHERE workspace_id=$1 AND id=$2 AND kind='import' AND account_source IS NOT NULL AND state IN ('finished','failed','cancelled')
			RETURNING `+jobColumns, workspaceID, id, nullable(agentID), now), nil
	})
}

// transition runs one state change; no row means the job is missing (ErrNotFound) or in another state
// (ErrState), and the unique index means another job is active (ErrActive).
func (s *Service) transition(ctx context.Context, workspaceID, id string, query func(pgx.Tx) (pgx.Row, error)) (Job, error) {
	if err := known(id); err != nil {
		return Job{}, err
	}
	var job Job
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		row, err := query(tx)
		if err != nil {
			return err
		}
		job, err = scanJob(row)
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM import_jobs WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrState
		}
		return ErrNotFound
	})
	if uniqueViolation(err) {
		return Job{}, ErrActive
	}
	return s.withKey(job), err
}
