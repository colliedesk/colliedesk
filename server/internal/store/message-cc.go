package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// checkTrialCC refuses a Cc list that would reach someone the customer never named. A trial workspace
// may write only to customers who wrote first; a Cc address is another recipient, so it must already
// be on mail the customer sent (the sender or a Cc) or on imported history. The mail counted is the
// same the first-contact rule trusts: mail from the platform forwarder that did not fail verification,
// or imported tickets. A verified sending domain lifts the rule, as it does for the contact.
func (s *Store) checkTrialCC(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, domainVerified bool, cc []string) error {
	if len(cc) == 0 || domainVerified {
		return nil
	}
	if trial, err := s.onTrial(ctx, tx, workspaceID, s.now()); err != nil || !trial {
		return err
	}
	known, err := customerAddresses(ctx, tx, workspaceID, conversationID)
	if err != nil {
		return err
	}
	var refused []string
	for _, address := range cc {
		if !known[strings.ToLower(address)] {
			refused = append(refused, address)
		}
	}
	if len(refused) > 0 {
		return &domain.TrialOutboundError{RefusedCC: refused}
	}
	return nil
}

// customerAddresses collects the sender and Cc addresses of the customer mail that proves the customer
// wrote first (see customerWroteFirst).
func customerAddresses(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT email_meta,origin FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming'
		AND (origin IS NOT NULL OR (email_meta->>'via'='platform' AND COALESCE(email_meta->>'authentication','none') <> 'fail'))`,
		workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("load customer addresses: %w", err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var meta, origin []byte
		if err := rows.Scan(&meta, &origin); err != nil {
			return nil, fmt.Errorf("scan customer addresses: %w", err)
		}
		var envelope domain.IncomingEmail
		var imported domain.MessageOrigin
		if meta != nil && json.Unmarshal(meta, &envelope) == nil {
			known[strings.ToLower(envelope.From)] = true
			for _, address := range envelope.CC {
				known[strings.ToLower(address)] = true
			}
		}
		if origin != nil && json.Unmarshal(origin, &imported) == nil {
			if imported.From != nil {
				known[strings.ToLower(*imported.From)] = true
			}
			for _, address := range imported.CC {
				known[strings.ToLower(address)] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate customer addresses: %w", err)
	}
	return known, nil
}

// withoutContact drops the conversation contact from a Cc list: the contact already receives the reply
// in To. An empty result is nil, which is stored as no Cc.
func withoutContact(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, cc []string) ([]string, error) {
	if len(cc) == 0 {
		return nil, nil
	}
	var contact string
	err := tx.QueryRow(ctx, `SELECT COALESCE(lower(ct.email),'') FROM conversations c JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
		WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, conversationID).Scan(&contact)
	if err != nil {
		return nil, fmt.Errorf("find conversation contact: %w", err)
	}
	kept := make([]string, 0, len(cc))
	for _, address := range cc {
		if strings.ToLower(address) != contact {
			kept = append(kept, address)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return kept, nil
}
