package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PresenceCaller is the session announcing itself: a person's sign-in (one row per tab) or an agent
// token.
type PresenceCaller struct {
	AgentID   string
	SessionID string
	Token     bool
}

// PresenceUpdate is one announce of a tab: whether its reply box holds a reply not sent yet, whether
// that reply changed since the last announce, and how long the row lives without another.
type PresenceUpdate struct {
	ClientID string
	Writing  bool
	Edited   bool
	TTL      time.Duration
}

// AnnouncePresence records that the caller's tab has the conversation open (moving the tab's row
// from any conversation it had open before) and returns everyone else who has it open. It also
// removes the workspace's expired rows and the caller's person's oldest rows past
// domain.PresenceMaxPerAgent, so the table holds only live tabs. A tab that announced the same
// conversation less than domain.PresenceMinInterval ago gets domain.ErrPresenceTooSoon and changes
// nothing, unless the announce starts or ends a reply and the last one is domain.PresenceMinChangeInterval old.
func (s *Store) AnnouncePresence(ctx context.Context, workspaceID, conversationID string, who PresenceCaller, update PresenceUpdate) ([]domain.ConversationViewer, error) {
	now := s.now()
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.ConversationViewer, error) {
		batch := &pgx.Batch{}
		batch.Queue(`DELETE FROM conversation_presence WHERE workspace_id=$1 AND expires_at<=$2`, workspaceID, now)
		// A reply starts (writing_since) when the box stops being empty, and on another conversation.
		batch.Queue(`INSERT INTO conversation_presence AS p
				(workspace_id,session_id,client_id,agent_id,conversation_id,writing_since,edited_at,seen_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,CASE WHEN $6 THEN $7::timestamptz END,CASE WHEN $6 THEN $7::timestamptz END,$7,$8)
			ON CONFLICT (workspace_id,session_id,client_id) DO UPDATE SET
				conversation_id=EXCLUDED.conversation_id,
				writing_since=CASE WHEN NOT $6 THEN NULL
					WHEN p.writing_since IS NULL OR p.conversation_id<>EXCLUDED.conversation_id THEN $7 ELSE p.writing_since END,
				edited_at=CASE WHEN NOT $6 THEN NULL
					WHEN p.writing_since IS NULL OR p.conversation_id<>EXCLUDED.conversation_id OR $9 THEN $7 ELSE p.edited_at END,
				seen_at=$7,expires_at=$8
			WHERE p.conversation_id<>EXCLUDED.conversation_id OR p.seen_at<=$10
				OR ((p.writing_since IS NOT NULL)<>$6 AND p.seen_at<=$11)`,
			workspaceID, who.SessionID, update.ClientID, who.AgentID, conversationID, update.Writing, now, now.Add(update.TTL), update.Edited,
			now.Add(-domain.PresenceMinInterval), now.Add(-domain.PresenceMinChangeInterval))
		batch.Queue(`DELETE FROM conversation_presence WHERE workspace_id=$1 AND agent_id=$2 AND (session_id,client_id) NOT IN (
				SELECT session_id,client_id FROM conversation_presence WHERE workspace_id=$1 AND agent_id=$2
				ORDER BY seen_at DESC,session_id,client_id LIMIT $3)`, workspaceID, who.AgentID, domain.PresenceMaxPerAgent)
		queueViewers(batch, workspaceID, conversationID, who, now)
		results := tx.SendBatch(ctx, batch)
		defer results.Close()
		for _, step := range []string{"remove expired presence", "announce presence", "cap presence"} {
			tag, err := results.Exec()
			if err != nil {
				var database *pgconn.PgError
				if errors.As(err, &database) && database.Code == "23503" {
					return nil, domain.ErrPresenceConversationNotFound
				}
				return nil, fmt.Errorf("%s: %w", step, err)
			}
			// The row exists, on this conversation, and was renewed less than the minimum interval ago.
			if step == "announce presence" && tag.RowsAffected() == 0 {
				return nil, domain.ErrPresenceTooSoon
			}
		}
		rows, err := results.Query()
		if err != nil {
			return nil, fmt.Errorf("list viewers: %w", err)
		}
		return collectViewers(rows)
	})
}

// LeavePresence removes the caller's row for one tab on one conversation; another person's row, or a
// tab that has moved on to another conversation, is left alone. It reports
// domain.ErrPresenceConversationNotFound when the conversation is not in the workspace.
func (s *Store) LeavePresence(ctx context.Context, workspaceID, conversationID, sessionID, clientID string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `WITH gone AS (
				DELETE FROM conversation_presence WHERE workspace_id=$1 AND conversation_id=$2 AND session_id=$3 AND client_id=$4 RETURNING 1)
			SELECT EXISTS (SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID, sessionID, clientID).Scan(&exists); err != nil {
			return struct{}{}, fmt.Errorf("leave presence: %w", err)
		}
		if !exists {
			return struct{}{}, domain.ErrPresenceConversationNotFound
		}
		return struct{}{}, nil
	})
	return err
}

// queueViewers lists who has the conversation open, once per person and once per agent token, people
// first. The caller's own rows are left out, and so are a person's other tabs when the person asks;
// an agent token does see the tabs of the person it acts as, who may be writing a reply. Deactivated
// people and signed-out sessions are not listed even before their rows expire.
func queueViewers(batch *pgx.Batch, workspaceID, conversationID string, who PresenceCaller, now time.Time) {
	batch.Queue(`SELECT p.agent_id::text,COALESCE(NULLIF(a.display_name,''),a.email),
			min(p.writing_since),max(p.edited_at),t.id::text,t.label,t.scope
		FROM conversation_presence p
		JOIN agents a ON a.workspace_id=p.workspace_id AND a.id=p.agent_id AND a.deactivated_at IS NULL
		JOIN sessions s ON s.workspace_id=p.workspace_id AND s.id=p.session_id AND s.revoked_at IS NULL AND s.expires_at>$5
		LEFT JOIN sessions t ON t.workspace_id=s.workspace_id AND t.id=s.id AND s.scope IS NOT NULL
		WHERE p.workspace_id=$1 AND p.conversation_id=$2 AND p.expires_at>$5 AND p.session_id<>$3
			AND NOT (NOT $6 AND p.agent_id=$4 AND s.scope IS NULL)
		GROUP BY p.agent_id,a.display_name,a.email,t.id,t.label,t.scope
		ORDER BY t.id IS NOT NULL,2,t.label,p.agent_id,t.id`, workspaceID, conversationID, who.SessionID, who.AgentID, now, who.Token)
}

func collectViewers(rows pgx.Rows) ([]domain.ConversationViewer, error) {
	defer rows.Close()
	viewers := []domain.ConversationViewer{}
	for rows.Next() {
		var viewer domain.ConversationViewer
		var tokenID, label, scope *string
		if err := rows.Scan(&viewer.AgentID, &viewer.Name, &viewer.WritingSince, &viewer.EditedAt, &tokenID, &label, &scope); err != nil {
			return nil, fmt.Errorf("scan viewer: %w", err)
		}
		viewer.Writing = viewer.WritingSince != nil
		if viewer.Writing {
			since, edited := domain.UTC(*viewer.WritingSince), domain.UTC(*viewer.EditedAt)
			viewer.WritingSince, viewer.EditedAt = &since, &edited
		}
		if tokenID != nil {
			viewer.AgentToken = &domain.MessageToken{ID: *tokenID, Label: *label, Scope: *scope, AgentName: viewer.Name}
		}
		viewers = append(viewers, viewer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list viewers: %w", err)
	}
	return viewers, nil
}
