package importjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// jobColumns are what a Job shows; never the key or the reports.
const jobColumns = `id::text,kind,source,domain,account_source,undo_of::text,state,phase,options,preview,progress,status,error_code,error_message,
	imported_conversations,sealed_key IS NOT NULL,interim_report IS NOT NULL,report IS NOT NULL,cancel_requested_at IS NOT NULL,
	CASE WHEN state='retry_wait' THEN next_run_at END,created_by::text,created_at,started_at,finished_at,updated_at`

func scanJob(row pgx.Row) (Job, error) {
	var job Job
	var options, preview, progress []byte
	err := row.Scan(&job.ID, &job.Kind, &job.Source, &job.Domain, &job.AccountSource, &job.UndoOf, &job.State, &job.Phase, &options, &preview, &progress,
		&job.Status, &job.ErrorCode, &job.ErrorMessage, &job.ImportedConversations, &job.HasKey, &job.HasInterimReport, &job.HasReport, &job.CancelRequested,
		&job.NextRunAt, &job.CreatedBy, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	// The API answers in UTC whatever the session time zone.
	job.CreatedAt, job.UpdatedAt = domain.UTC(job.CreatedAt), domain.UTC(job.UpdatedAt)
	job.NextRunAt, job.StartedAt, job.FinishedAt = domain.UTCOptional(job.NextRunAt), domain.UTCOptional(job.StartedAt), domain.UTCOptional(job.FinishedAt)
	if err := json.Unmarshal(options, &job.Options); err != nil {
		return Job{}, fmt.Errorf("decode import options: %w", err)
	}
	job.Preview = preview
	if progress != nil {
		job.Progress = &Progress{}
		if err := json.Unmarshal(progress, job.Progress); err != nil {
			return Job{}, fmt.Errorf("decode import progress: %w", err)
		}
	}
	return job, nil
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// claimed is a job a worker leased: its id, workspace and token.
type claimed struct {
	id, workspaceID, token string
}

// claimDue leases up to limit due jobs of every workspace that this process can open the key of: sealed
// with a key id of its ring, or held in its memory. A job no process can open (another server's memory,
// a key that left every ring) is taken once nobody leased or saved it for one lease, and then waits for
// its key; a retry counts from when it came due. Only a job found running counts toward the
// crash-loop guard.
func claimDue(ctx context.Context, db *database.DB, instance string, keyIDs []string, now time.Time, limit int, lease time.Duration) ([]claimed, error) {
	var out []claimed
	err := db.Privileged(ctx, database.PrivilegeImportJobClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
				SELECT id FROM import_jobs
				WHERE state IN ('previewing','queued','running','retry_wait') AND next_run_at<=$1 AND (lease_until IS NULL OR lease_until<=$1)
					AND (kind='undo' OR sealed_key_id=ANY($5) OR (sealed_key IS NULL AND owner_instance=$2) OR COALESCE(lease_until, GREATEST(updated_at, next_run_at)+$4::interval)<=$1)
				ORDER BY next_run_at LIMIT $3 FOR UPDATE SKIP LOCKED)
			UPDATE import_jobs j SET claim_token=gen_random_uuid(), lease_until=$1+$4::interval, attempts=j.attempts+1,
				attempts_without_progress=j.attempts_without_progress+CASE WHEN j.state='running' THEN 1 ELSE 0 END,
				state=CASE WHEN j.state='previewing' THEN 'previewing' ELSE 'running' END, updated_at=$1
			FROM due WHERE j.id=due.id
			RETURNING j.id::text, j.workspace_id::text, j.claim_token::text`, now, instance, limit, lease.String(), keyIDs)
		if err != nil {
			return fmt.Errorf("claim import jobs: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimed, error) {
			var item claimed
			return item, row.Scan(&item.id, &item.workspaceID, &item.token)
		})
		return err
	})
	return out, err
}

// expirePreviews discards previews nobody started within PreviewTTL and returns their ids.
func expirePreviews(ctx context.Context, db *database.DB, now time.Time) ([]string, error) {
	var ids []string
	err := db.Privileged(ctx, database.PrivilegeImportJobClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE import_jobs SET state='discarded', sealed_key=NULL, sealed_key_id=NULL, finished_at=$1, updated_at=$1
			WHERE state='previewed' AND updated_at<=$2 RETURNING id::text`, now, now.Add(-PreviewTTL))
		if err != nil {
			return fmt.Errorf("expire import previews: %w", err)
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(ids) == 0 {
			return err
		}
		// The Freshdesk agents those previews read go with them, unless an import of the account keeps them.
		_, err = tx.Exec(ctx, `DELETE FROM import_people p USING import_jobs e
			WHERE e.id=ANY($1::uuid[]) AND p.workspace_id=e.workspace_id AND p.source=e.account_source AND NOT EXISTS (
				SELECT 1 FROM import_jobs j WHERE j.workspace_id=p.workspace_id AND j.account_source=p.source AND j.kind='import'
				AND (j.state IN ('previewing','previewed') OR (j.started_at IS NOT NULL AND NOT EXISTS (
					SELECT 1 FROM import_jobs u WHERE u.workspace_id=j.workspace_id AND u.undo_of=j.id AND u.state='finished'))))`, ids)
		return err
	})
	return ids, err
}

// runRow is everything a worker needs to run a claimed job.
type runRow struct {
	Job
	workspaceID, slug               string
	sealedKey                       []byte
	sealedKeyID                     *string
	ownerInstance                   *string
	report                          []byte
	attemptsNoProgress, fullRetries int
	retryingSince                   *time.Time
}

func loadRun(ctx context.Context, db *database.DB, item claimed) (runRow, error) {
	var row runRow
	err := db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM import_jobs WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`,
			item.workspaceID, item.id, item.token))
		if err != nil {
			return err
		}
		row.Job = job
		return tx.QueryRow(ctx, `SELECT w.slug, j.sealed_key, j.sealed_key_id, j.owner_instance, j.report, j.attempts_without_progress, j.full_retries, j.retrying_since
			FROM import_jobs j JOIN workspaces w ON w.id=j.workspace_id WHERE j.workspace_id=$1 AND j.id=$2`, item.workspaceID, item.id).
			Scan(&row.slug, &row.sealedKey, &row.sealedKeyID, &row.ownerInstance, &row.report, &row.attemptsNoProgress, &row.fullRetries, &row.retryingSince)
	})
	row.workspaceID = item.workspaceID
	return row, err
}

// errLeaseLost means the job's claim token changed: another worker holds it now.
var errLeaseLost = errors.New("import job lease was lost")

// update runs one write of a claimed job; it fails with errLeaseLost when the claim is gone.
func update(ctx context.Context, db *database.DB, item claimed, sql string, args ...any) error {
	return updateAnd(ctx, db, item, nil, sql, args...)
}

// updateAnd is update with more writes in the same transaction, made only while the claim holds.
func updateAnd(ctx context.Context, db *database.DB, item claimed, also func(pgx.Tx) error, sql string, args ...any) error {
	return db.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE import_jobs SET `+sql+` WHERE workspace_id=$1 AND id=$2 AND claim_token=$3`,
			append([]any{item.workspaceID, item.id, item.token}, args...)...)
		if err != nil {
			return fmt.Errorf("update import job: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errLeaseLost
		}
		if also != nil {
			return also(tx)
		}
		return nil
	})
}

// endSQL ends a claimed job: terminal state, key erased, lease released. Arguments from $4: state,
// status, error code, error message, now.
const endSQL = `state=$4, status=$5, error_code=$6, error_message=$7, sealed_key=NULL, sealed_key_id=NULL,
	claim_token=NULL, lease_until=NULL, cancel_requested_at=NULL, finished_at=$8, updated_at=$8,
	imported_conversations=CASE WHEN kind='import' AND account_source IS NOT NULL THEN (SELECT count(*) FROM import_items i
		WHERE i.workspace_id=import_jobs.workspace_id AND i.source=import_jobs.account_source AND i.entity='ticket' AND i.status='done') END`

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
