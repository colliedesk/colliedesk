package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The files a written reply draft carries (ai_draft_files). Their bytes are attachment objects like a
// message's; a row here keeps them (DiscardObject reads it). Sending the draft turns the files a person
// kept into attachments of the reply and removes these rows in the same transaction.

var (
	// ErrDraftFileUnknown means ai_draft_file_ids named a file that is not one of that draft's.
	ErrDraftFileUnknown = errors.New("ai_draft_file_ids names a file that is not one of this draft's files")
	// ErrDraftFileNotFound means no file with that id on that draft of this workspace.
	ErrDraftFileNotFound = errors.New("no file with that id on this draft")
	// ErrReplyTooManyFiles and ErrReplyFilesTooLarge refuse a reply whose draft files and uploads
	// together are more than one message carries.
	ErrReplyTooManyFiles  = errors.New("the draft's files and the files sent with the reply are more than a message carries")
	ErrReplyFilesTooLarge = errors.New("the draft's files and the files sent with the reply are larger than a message carries")
)

// DraftFile is the stored file of a draft, with where its bytes are.
type DraftFile struct {
	domain.AIDraftFile
	Key, SHA256 string
}

// InsertDraftFiles records files already written to attachment storage on a draft, inside the
// transaction that writes the draft: the plan's storage cap is checked, and every object is locked and
// checked to exist, so the row never points at bytes a cleanup removed.
func (s *Store) InsertDraftFiles(ctx context.Context, tx pgx.Tx, workspaceID, draftID string, files []NewAttachment) error {
	if len(files) == 0 {
		return nil
	}
	var total int64
	keys := make([]string, 0, len(files))
	for _, file := range files {
		total += file.Object.Size
		keys = append(keys, file.Object.Key)
	}
	if err := s.CheckStorage(ctx, tx, workspaceID, total, s.now()); err != nil {
		return err
	}
	if err := VerifyObjects(ctx, tx, s.objects, keys...); err != nil {
		return err
	}
	for _, file := range files {
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		// clock_timestamp keeps the files of one draft in the order they came.
		if _, err := tx.Exec(ctx, `INSERT INTO ai_draft_files(workspace_id,draft_id,filename,content_type,size,sha256,storage_key,created_at)
			SELECT $1,d.id,$3,$4,$5,$6,$7,clock_timestamp() FROM ai_drafts d WHERE d.workspace_id=$1 AND d.id=$2`,
			workspaceID, draftID, file.Filename, contentType, file.Object.Size, file.Object.SHA256, file.Object.Key); err != nil {
			return fmt.Errorf("record draft file: %w", err)
		}
	}
	return nil
}

// DraftFiles reads the files of the given drafts of the workspace, in the order they came, by draft.
func DraftFiles(ctx context.Context, tx pgx.Tx, workspaceID string, draftIDs []string) (map[string][]domain.AIDraftFile, error) {
	files := map[string][]domain.AIDraftFile{}
	if len(draftIDs) == 0 {
		return files, nil
	}
	rows, err := tx.Query(ctx, `SELECT draft_id::text,id::text,filename,content_type,size,created_at FROM ai_draft_files
		WHERE workspace_id=$1 AND draft_id=ANY($2::uuid[]) ORDER BY created_at,id`, workspaceID, draftIDs)
	if err != nil {
		return nil, fmt.Errorf("list draft files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var draftID string
		var file domain.AIDraftFile
		if err := rows.Scan(&draftID, &file.ID, &file.Filename, &file.ContentType, &file.Size, &file.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan draft file: %w", err)
		}
		file.CreatedAt = domain.UTC(file.CreatedAt)
		files[draftID] = append(files[draftID], file)
	}
	return files, rows.Err()
}

// GetDraftFile reads one file of a draft of the conversation, with its storage key.
func (s *Store) GetDraftFile(ctx context.Context, workspaceID, conversationID, draftID, fileID string) (DraftFile, error) {
	if !savedReplyID.MatchString(conversationID) || !savedReplyID.MatchString(draftID) || !savedReplyID.MatchString(fileID) {
		return DraftFile{}, ErrDraftFileNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (DraftFile, error) {
		var file DraftFile
		err := tx.QueryRow(ctx, `SELECT f.id::text,f.filename,f.content_type,f.size,f.created_at,f.storage_key,f.sha256
			FROM ai_draft_files f JOIN ai_drafts d ON d.workspace_id=f.workspace_id AND d.id=f.draft_id
			WHERE f.workspace_id=$1 AND d.conversation_id=$2 AND f.draft_id=$3 AND f.id=$4`, workspaceID, conversationID, draftID, fileID).
			Scan(&file.ID, &file.Filename, &file.ContentType, &file.Size, &file.CreatedAt, &file.Key, &file.SHA256)
		if errors.Is(err, pgx.ErrNoRows) {
			return DraftFile{}, ErrDraftFileNotFound
		}
		file.CreatedAt = domain.UTC(file.CreatedAt)
		return file, err
	})
}

// draftFilesForReply reads the files of the draft a reply is sent from (this conversation's, in this
// workspace) that go with it: keep names them, nil keeps all. It returns them as new attachments and the
// storage keys of the ones left behind. A kept id that is not a file of the draft is ErrDraftFileUnknown.
// uploaded are the files sent with the reply itself: together they stay within one message's limits.
func draftFilesForReply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, draftID string, keep *[]string, uploaded []NewAttachment) ([]NewAttachment, []string, error) {
	if draftID == "" {
		if keep != nil && len(*keep) > 0 {
			return nil, nil, ErrDraftFileUnknown
		}
		return nil, nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT f.id::text,f.filename,f.content_type,f.size,f.sha256,f.storage_key
		FROM ai_draft_files f JOIN ai_drafts d ON d.workspace_id=f.workspace_id AND d.id=f.draft_id
		WHERE f.workspace_id=$1 AND d.conversation_id::text=$2 AND f.draft_id::text=$3 ORDER BY f.created_at,f.id`, workspaceID, conversationID, draftID)
	if err != nil {
		return nil, nil, fmt.Errorf("read draft files: %w", err)
	}
	type stored struct {
		id   string
		file NewAttachment
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (stored, error) {
		var item stored
		err := row.Scan(&item.id, &item.file.Filename, &item.file.ContentType, &item.file.Object.Size, &item.file.Object.SHA256, &item.file.Object.Key)
		return item, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read draft files: %w", err)
	}
	if keep != nil {
		for _, id := range *keep {
			if !slices.ContainsFunc(all, func(item stored) bool { return item.id == id }) {
				return nil, nil, ErrDraftFileUnknown
			}
		}
	}
	var kept []NewAttachment
	var left []string
	for _, item := range all {
		if keep == nil || slices.Contains(*keep, item.id) {
			kept = append(kept, item.file)
		} else {
			left = append(left, item.file.Object.Key)
		}
	}
	total := int64(0)
	for _, file := range append(slices.Clone(uploaded), kept...) {
		total += file.Object.Size
	}
	if len(uploaded)+len(kept) > attachments.MaxOutgoingFiles {
		return nil, nil, ErrReplyTooManyFiles
	}
	if total > attachments.MaxOutgoingBytes {
		return nil, nil, ErrReplyFilesTooLarge
	}
	return kept, left, nil
}

// SweepDraftFiles removes, in every workspace, the files of drafts that can no longer be used: sent
// (its kept files are the reply's attachments now), stopped before it was written, or ended long enough
// ago that its text was erased. An earlier draft (replaced, expired, discarded) keeps its files until
// then, so it can be restored. The bytes go unless another row still uses them. It returns how many
// files it removed.
func (s *Store) SweepDraftFiles(ctx context.Context) (int, error) {
	type removed struct{ workspaceID, key string }
	var gone []removed
	err := s.db.Privileged(ctx, database.PrivilegeDraftFileSweep, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `DELETE FROM ai_draft_files f USING ai_drafts d
			WHERE d.workspace_id=f.workspace_id AND d.id=f.draft_id
				AND (d.outcome IN ('sent_unchanged','sent_edited') OR d.status='cancelled' OR d.text_cleared_at IS NOT NULL)
			RETURNING f.workspace_id::text, f.storage_key`)
		if err != nil {
			return fmt.Errorf("sweep draft files: %w", err)
		}
		gone, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (removed, error) {
			var item removed
			err := row.Scan(&item.workspaceID, &item.key)
			return item, err
		})
		return err
	})
	if err != nil || s.objects == nil {
		return len(gone), err
	}
	seen := map[removed]bool{}
	for _, item := range gone {
		if seen[item] {
			continue
		}
		seen[item] = true
		if err := s.DiscardObject(ctx, item.workspaceID, item.key); err != nil {
			return len(gone), fmt.Errorf("delete a swept draft file: %w", err)
		}
	}
	return len(gone), nil
}
