package helpscout

import (
	"context"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// verifyAttachments re-reads every stored file (attachments and inline images) and compares its
// SHA-256 and size with what was recorded when it was stored. A lost or changed file marks the item
// and its conversation not done, so the next run reads it again.
func (r *run) verifyAttachments(ctx context.Context, report *Report, conversationIDs, _ []string) error {
	type stored struct {
		entity, id, conversation, sha, key string
		size                               int64
	}
	var files []stored
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT i.entity,i.source_id,i.parent_source_id,a.size,a.sha256,a.storage_key FROM import_items i
		JOIN attachments a ON a.workspace_id=i.workspace_id AND a.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity IN ('attachment','inline_image') AND i.status='done' AND i.parent_source_id=ANY($3)`,
		r.workspaceID, r.ledger.source, conversationIDs)
	if err == nil {
		for rows.Next() {
			var file stored
			if err = rows.Scan(&file.entity, &file.id, &file.conversation, &file.size, &file.sha, &file.key); err != nil {
				break
			}
			files = append(files, file)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
	}
	_ = tx.Rollback(ctx) // read only; end it before re-reading stored files
	if err != nil {
		return fmt.Errorf("reconcile attachments: %w", err)
	}
	for _, file := range files {
		mismatches, bytesStored, verified := &report.Attachments.ChecksumMismatches, &report.Attachments.ImportedBytes, &report.Attachments.ChecksumVerified
		if file.entity == entityInlineImage {
			mismatches, bytesStored, verified = &report.InlineImages.ChecksumMismatches, &report.InlineImages.ImportedBytes, &report.InlineImages.ChecksumVerified
		}
		size, sum, err := r.rehash(ctx, file.key)
		if err != nil || size != file.size || sum != file.sha {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			*mismatches = append(*mismatches, file.id)
			if err := r.markForRedownload(ctx, file.entity, file.id, file.conversation); err != nil {
				return err
			}
			continue
		}
		*bytesStored += file.size
		*verified++
	}
	report.Attachments.ChecksumMismatches = uniqueSorted(report.Attachments.ChecksumMismatches)
	report.InlineImages.ChecksumMismatches = uniqueSorted(report.InlineImages.ChecksumMismatches)
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

func (r *run) markForRedownload(ctx context.Context, entity, itemID, conversationID string) error {
	cause := "stored file is missing or no longer matches its SHA-256; it will be downloaded again on the next run"
	if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entity, sourceID: itemID, parentID: conversationID, status: ledgerFailed, errText: cause}); err != nil {
		return err
	}
	return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: conversationID, status: ledgerFailed, errText: entity + " " + itemID + " must be downloaded again"})
}

// collectFailures lists every failed ledger row of the run's conversations and customers.
func (r *run) collectFailures(ctx context.Context, report *Report, conversationIDs, contactIDs []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile failures: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,source_id,COALESCE(parent_source_id,''),error FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND status='failed' AND ((entity='ticket' AND source_id=ANY($3))
		OR (entity IN ('thread','attachment','inline_image') AND parent_source_id=ANY($3)) OR (entity='contact' AND source_id=ANY($4)))
		ORDER BY entity,length(source_id),source_id`, r.workspaceID, r.ledger.source, conversationIDs, contactIDs)
	if err != nil {
		return fmt.Errorf("reconcile failures: %w", err)
	}
	defer rows.Close()
	report.Failures = []Failure{}
	for rows.Next() {
		var failure Failure
		if err := rows.Scan(&failure.Entity, &failure.HelpScoutID, &failure.ConversationID, &failure.Error); err != nil {
			return fmt.Errorf("reconcile failures: %w", err)
		}
		if failure.Entity == entityTicket {
			failure.Entity, failure.ConversationID = "conversation", failure.HelpScoutID
		}
		report.Failures = append(report.Failures, failure)
	}
	return rows.Err()
}

// collectGoneImages lists the images Help Scout answered 404 or 410 for. They are not failures: a
// rerun cannot bring them over. The message keeps its link.
func (r *run) collectGoneImages(ctx context.Context, report *Report, conversationIDs, _ []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile gone images: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT source_id,parent_source_id,error FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND entity='inline_image' AND status='skipped' AND parent_source_id=ANY($3)
		ORDER BY length(parent_source_id),parent_source_id,source_id`, r.workspaceID, r.ledger.source, conversationIDs)
	if err != nil {
		return fmt.Errorf("reconcile gone images: %w", err)
	}
	defer rows.Close()
	report.InlineImages.GoneImages = []GoneImage{}
	for rows.Next() {
		var id, conversation, detail string
		if err := rows.Scan(&id, &conversation, &detail); err != nil {
			return fmt.Errorf("reconcile gone images: %w", err)
		}
		thread := ""
		if parts := strings.SplitN(id, ":", 3); len(parts) == 3 {
			thread = parts[1]
		}
		report.InlineImages.GoneImages = append(report.InlineImages.GoneImages, GoneImage{ConversationID: conversation, ThreadID: thread, Detail: detail})
	}
	report.InlineImages.Gone = len(report.InlineImages.GoneImages)
	return rows.Err()
}
