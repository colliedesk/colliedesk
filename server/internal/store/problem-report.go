package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// ProblemReportTag marks every conversation opened by an agent's "Report a problem".
const ProblemReportTag = "problem-report"

// ProblemReport is a validated report, ready to become a conversation of the support desk. The reporter is
// a signed-in agent, so the address is verified.
type ProblemReport struct {
	Subject       string
	Body          string
	ReporterEmail string
	ReporterName  string
	// Files are screenshots already written to attachment storage under the support desk's workspace.
	Files []EmailFile
}

// CreateProblemReport opens a conversation in workspaceID (the support desk): the reporter becomes (or is
// found as) a contact, the report is the incoming message, the screenshots are its attachments and the
// conversation carries the problem-report tag. Nothing is read from any other workspace.
func (s *Store) CreateProblemReport(ctx context.Context, workspaceID string, in ProblemReport) (string, error) {
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("begin problem report: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	var contactID, tagID, conversationID, messageID string
	if contactID, err = contactForEmail(ctx, tx, workspaceID, in.ReporterName, in.ReporterEmail); err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
		ON CONFLICT(workspace_id,name) DO UPDATE SET name=tags.name RETURNING id::text`, workspaceID, ProblemReportTag).Scan(&tagID); err != nil {
		return "", fmt.Errorf("find or create tag: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at) VALUES($1,$2,$3,$4,$4) RETURNING id::text`,
		workspaceID, contactID, in.Subject, now).Scan(&conversationID); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3)`, workspaceID, conversationID, tagID); err != nil {
		return "", fmt.Errorf("tag conversation: %w", err)
	}
	named := make([]domain.EmailAttachment, 0, len(in.Files))
	for _, file := range in.Files {
		named = append(named, domain.EmailAttachment{Filename: file.Filename, ContentType: file.ContentType})
	}
	meta, err := json.Marshal(domain.IncomingEmail{From: in.ReporterEmail, Authentication: "pass", Via: domain.ViaProblemReport, Attachments: named})
	if err != nil {
		return "", fmt.Errorf("encode problem report metadata: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,created_at) VALUES($1,$2,'incoming',$3,$4,$5) RETURNING id::text`,
		workspaceID, conversationID, in.Body, meta, now).Scan(&messageID); err != nil {
		return "", fmt.Errorf("store problem report message: %w", err)
	}
	if err := linkFiles(ctx, tx, s.objects, workspaceID, messageID, in.Files); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit problem report: %w", err)
	}
	return conversationID, nil
}
