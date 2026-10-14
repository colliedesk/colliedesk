package freshdesk

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// verifyAttachments re-reads every stored file and compares its SHA-256 and
// size with what was recorded at download time.
func (r *run) verifyAttachments(ctx context.Context, report *Report, ticketIDs, _ []string) error {
	rows, err := r.pool.Query(ctx, `SELECT i.source_id,a.size,a.sha256,a.storage_key FROM import_items i
		JOIN attachments a ON a.workspace_id=i.workspace_id AND a.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='attachment' AND i.status='done' AND i.parent_source_id=ANY($3)`,
		r.workspaceID, r.ledger.source, ticketIDs)
	if err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	type stored struct {
		id, sha, key string
		size         int64
	}
	var files []stored
	for rows.Next() {
		var file stored
		if err := rows.Scan(&file.id, &file.size, &file.sha, &file.key); err != nil {
			rows.Close()
			return fmt.Errorf("reconcile attachments: %w", err)
		}
		files = append(files, file)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	report.Attachments.ChecksumMismatches = []string{}
	for _, file := range files {
		report.Attachments.ImportedBytes += file.size
		size, sum, err := r.rehash(ctx, file.key)
		if err != nil || size != file.size || sum != file.sha {
			report.Attachments.ChecksumMismatches = append(report.Attachments.ChecksumMismatches, file.id)
			continue
		}
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
