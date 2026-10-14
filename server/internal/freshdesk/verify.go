package freshdesk

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// verifyAttachments re-reads every stored file and compares its SHA-256 and
// size with what was recorded at download time. A lost or changed file marks
// the attachment and its ticket not done, so the next run downloads it again.
func (r *run) verifyAttachments(ctx context.Context, report *Report, ticketIDs, _ []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // also covers a panic; a second rollback is a no-op
	rows, err := tx.Query(ctx, `SELECT i.source_id,i.parent_source_id,a.size,a.sha256,a.storage_key FROM import_items i
		JOIN attachments a ON a.workspace_id=i.workspace_id AND a.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='attachment' AND i.status='done' AND i.parent_source_id=ANY($3)`,
		r.workspaceID, r.ledger.source, ticketIDs)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	type stored struct {
		id, ticket, sha, key string
		size                 int64
	}
	var files []stored
	for rows.Next() {
		var file stored
		if err := rows.Scan(&file.id, &file.ticket, &file.size, &file.sha, &file.key); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return fmt.Errorf("reconcile attachments: %w", err)
		}
		files = append(files, file)
	}
	rows.Close()
	_ = tx.Rollback(ctx) // read only; end it before re-reading stored files
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	report.Attachments.ChecksumMismatches = []string{}
	for _, file := range files {
		size, sum, err := r.rehash(ctx, file.key)
		if err != nil || size != file.size || sum != file.sha {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			report.Attachments.ChecksumMismatches = append(report.Attachments.ChecksumMismatches, file.id)
			if err := r.markForRedownload(ctx, file.id, file.ticket); err != nil {
				return err
			}
			continue
		}
		report.Attachments.ImportedBytes += file.size
		report.Attachments.ChecksumVerified++
	}
	report.Attachments.ChecksumMismatches = uniqueSorted(report.Attachments.ChecksumMismatches)
	return nil
}

func (r *run) rehash(ctx context.Context, key string) (int64, string, error) {
	body, err := r.opts.Storage.Open(ctx, key)
	if err != nil {
		return 0, "", err
	}
	defer body.Close()
	return attachments.Checksum(body)
}

func (r *run) markForRedownload(ctx context.Context, attachmentID, ticketID string) error {
	cause := "stored file is missing or no longer matches its SHA-256; it will be downloaded again on the next run"
	if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entityAttachment, sourceID: attachmentID, parentID: ticketID, status: ledgerFailed, errText: cause}); err != nil {
		return err
	}
	return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: ticketID, status: ledgerFailed, errText: "attachment " + attachmentID + " must be downloaded again"})
}
