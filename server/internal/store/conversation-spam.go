package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrConversationSpam refuses a status change of a conversation in Spam: Not spam (MarkSpam false) moves it.
var ErrConversationSpam = errors.New("conversation is in spam")

// SpamSender is a customer whose later mail a spam change files in Spam, or lets reach the inbox again.
type SpamSender struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SpamResult is how many conversations moved into Spam (or out of it) and whose mail now goes there (or not).
type SpamResult struct {
	Updated int          `json:"updated"`
	Senders []SpamSender `json:"senders"`
}

// MarkSpam files the conversations in ids in Spam (spam true) or takes them out (false), with their senders,
// in one transaction. In Spam, a conversation has the status spam: no view but Spam lists it, its clocks stop
// and Results leave it out; its sender's later mail goes there too (contacts.spam_since, read by
// ingestInbound). Not spam puts back the status it had before (its status history; open for one that arrived
// in Spam) and clears the sender's rule. Nobody is told either way. An id that is missing or another
// workspace's fails the whole call with "not found" and changes nothing.
func (s *Store) MarkSpam(ctx context.Context, workspaceID string, ids []string, spam bool, actorID, tokenID string) (SpamResult, error) {
	if len(ids) > MaxBulkConversations {
		return SpamResult{}, fmt.Errorf("mark spam: too many conversations")
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (SpamResult, error) {
		result := SpamResult{Senders: []SpamSender{}}
		var contacts []string
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(array_agg(DISTINCT contact_id::text),'{}') FROM conversations WHERE workspace_id=$1 AND id=ANY($2::uuid[])`,
			workspaceID, ids).Scan(&result.Updated, &contacts); err != nil {
			return result, fmt.Errorf("find conversations: %w", err)
		}
		if result.Updated != len(ids) {
			return SpamResult{}, fmt.Errorf("mark spam: not found")
		}
		// Lock order, as inbound mail takes them: the senders, then their conversations (ORDER BY id).
		if _, err := tx.Exec(ctx, `SELECT 1 FROM contacts WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR NO KEY UPDATE`, workspaceID, contacts); err != nil {
			return result, fmt.Errorf("lock senders: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR NO KEY UPDATE`, workspaceID, ids); err != nil {
			return result, fmt.Errorf("lock conversations: %w", err)
		}
		// Quiet: a spam change never tells the customer.
		if err := setStatusCause(ctx, tx, statusChange{ActorID: actorID, TokenID: tokenID, Cause: tokenCause(tokenID), Quiet: true}); err != nil {
			return result, err
		}
		move := `UPDATE conversations SET status='spam',updated_at=now() WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND status<>'spam'`
		rule := `UPDATE contacts SET spam_since=COALESCE(spam_since,now()) WHERE workspace_id=$1 AND id=ANY($2::uuid[])
			RETURNING id::text,name,COALESCE(email,'')`
		if !spam {
			move = `UPDATE conversations c SET status=COALESCE((SELECT h.from_status FROM conversation_status_changes h
					WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.to_status='spam' AND h.from_status<>'spam'
					ORDER BY h.changed_at DESC,h.id DESC LIMIT 1),'open'),updated_at=now()
				WHERE c.workspace_id=$1 AND c.id=ANY($2::uuid[]) AND c.status='spam'`
			rule = `UPDATE contacts SET spam_since=NULL WHERE workspace_id=$1 AND id=ANY($2::uuid[]) RETURNING id::text,name,COALESCE(email,'')`
		}
		tag, err := tx.Exec(ctx, move, workspaceID, ids)
		if err != nil {
			return result, fmt.Errorf("move conversations: %w", err)
		}
		result.Updated = int(tag.RowsAffected())
		rows, err := tx.Query(ctx, rule, workspaceID, contacts)
		if err != nil {
			return result, fmt.Errorf("mark senders: %w", err)
		}
		senders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SpamSender, error) {
			var sender SpamSender
			return sender, row.Scan(&sender.ID, &sender.Name, &sender.Email)
		})
		if err != nil {
			return result, fmt.Errorf("mark senders: %w", err)
		}
		result.Senders = senders
		return result, nil
	})
}

// refuseSpamStatus answers ErrConversationSpam when one of ids is in Spam, locking them for the change.
func refuseSpamStatus(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) error {
	var spam bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_or(status='spam'),false) FROM (SELECT status FROM conversations
		WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR NO KEY UPDATE) c`, workspaceID, ids).Scan(&spam); err != nil {
		return fmt.Errorf("read conversation status: %w", err)
	}
	if spam {
		return ErrConversationSpam
	}
	return nil
}
