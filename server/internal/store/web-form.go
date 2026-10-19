package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// WebFormTag marks every conversation opened by the public web form.
const WebFormTag = "web-form"

// WebFormNewConversationLimit caps the conversations the public form may open in one workspace per
// InboundRateWindow. It is counted apart from email's ceiling, so form spam cannot block mail.
const WebFormNewConversationLimit = 50

// WebFormSubmission is a validated, normalized submission of the public form.
type WebFormSubmission struct {
	Email   string
	Message string
	Source  string
}

// GetWebForm returns the workspace's web form settings; a workspace that never set it up has it off.
func (s *Store) GetWebForm(ctx context.Context, workspaceID string) (domain.WebForm, error) {
	form, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.WebForm, error) {
		return readWebForm(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("get web form: %w", err)
	}
	return form, nil
}

func readWebForm(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.WebForm, error) {
	form := domain.WebForm{AllowedOrigins: []string{}}
	err := tx.QueryRow(ctx, `SELECT enabled,allowed_origins FROM web_form_channels WHERE workspace_id=$1`, workspaceID).
		Scan(&form.Enabled, &form.AllowedOrigins)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.WebForm{}, err
	}
	return form, nil
}

// SetWebForm turns the form on or off and replaces its allowed origins. Turning it off keeps the origins.
func (s *Store) SetWebForm(ctx context.Context, workspaceID string, enabled bool, origins []string) (domain.WebForm, error) {
	if origins == nil {
		origins = []string{}
	}
	form, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.WebForm, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO web_form_channels(workspace_id,enabled,allowed_origins) VALUES($1,$2,$3)
			ON CONFLICT(workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled,allowed_origins=EXCLUDED.allowed_origins,updated_at=now()`,
			workspaceID, enabled, origins); err != nil {
			return domain.WebForm{}, err
		}
		return readWebForm(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("save web form: %w", err)
	}
	return form, nil
}

// OpenWebForm resolves the workspace of a public form by its slug. ok is false when the slug is
// unknown, the form is off or the workspace is locked, which callers must answer identically.
func (s *Store) OpenWebForm(ctx context.Context, slug string) (workspaceID string, origins []string, ok bool, err error) {
	workspaceID, found, err := s.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !found {
		return "", nil, false, err
	}
	type state struct {
		form   domain.WebForm
		locked bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		form, err := readWebForm(ctx, tx, workspaceID)
		if err != nil || !form.Enabled || !s.limits {
			return state{form: form}, err
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		return state{form: form, locked: access.Locked}, err
	})
	if err != nil {
		return "", nil, false, fmt.Errorf("open web form: %w", err)
	}
	if !got.form.Enabled || got.locked {
		return "", nil, false, nil
	}
	return workspaceID, got.form.AllowedOrigins, true, nil
}

// SubmitWebForm opens a conversation from a public form submission: the sender becomes (or is
// found as) a contact, the text is an incoming message and the conversation carries the web-form
// tag. It returns domain.ErrWebFormClosed when the form was turned off or the workspace locked since
// OpenWebForm, and ErrRateLimited at the workspace's ceiling on new conversations.
func (s *Store) SubmitWebForm(ctx context.Context, workspaceID string, in WebFormSubmission) error {
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("begin web form submission: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	form, err := readWebForm(ctx, tx, workspaceID)
	if err != nil {
		return fmt.Errorf("load web form: %w", err)
	}
	if !form.Enabled {
		return domain.ErrWebFormClosed
	}
	if s.limits {
		access, err := workspaceAccess(ctx, tx, workspaceID, now)
		if err != nil {
			return err
		}
		if access.Locked {
			return domain.ErrWebFormClosed
		}
	}
	// Serializes submissions of one workspace so the ceiling count sees the others.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "web-form:"+workspaceID); err != nil {
		return fmt.Errorf("lock web form: %w", err)
	}
	var created int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND kind='incoming' AND created_at>$2 AND email_meta->>'via'=$3`,
		workspaceID, now.Add(-InboundRateWindow), domain.ViaWebForm).Scan(&created); err != nil {
		return fmt.Errorf("check web form ceiling: %w", err)
	}
	if created >= WebFormNewConversationLimit {
		return ErrRateLimited
	}
	var contactID, tagID, conversationID string
	if err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$3)
		ON CONFLICT(workspace_id,email) DO UPDATE SET email=contacts.email RETURNING id::text`,
		workspaceID, in.Email[:strings.LastIndex(in.Email, "@")], in.Email).Scan(&contactID); err != nil {
		return fmt.Errorf("find or create contact: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
		ON CONFLICT(workspace_id,name) DO UPDATE SET name=tags.name RETURNING id::text`, workspaceID, WebFormTag).Scan(&tagID); err != nil {
		return fmt.Errorf("find or create tag: %w", err)
	}
	subject := "Web form"
	if in.Source != "" {
		subject += ": " + in.Source
	}
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel) VALUES($1,$2,$3,$4,$4,$5) RETURNING id::text`,
		workspaceID, contactID, subject, now, domain.ChannelWebForm).Scan(&conversationID); err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3)`, workspaceID, conversationID, tagID); err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	body := in.Message
	if body == "" {
		body = "(no message)"
	}
	// The sender typed this address and nothing checked it. The mark stays on the message when an agent removes the tag.
	meta, err := json.Marshal(domain.IncomingEmail{From: in.Email, Authentication: "none", Via: domain.ViaWebForm, Attachments: []domain.EmailAttachment{}})
	if err != nil {
		return fmt.Errorf("encode web form metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,created_at) VALUES($1,$2,'incoming',$3,$4,$5)`,
		workspaceID, conversationID, body, meta, now); err != nil {
		return fmt.Errorf("store web form message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit web form submission: %w", err)
	}
	return nil
}
