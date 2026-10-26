package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// IntakeSnapshot is what the AI decider reads of a card, at one moment: the card, the channel scope's
// level and options, what the workspace agreed AI may read, the card's messages and those before it in
// the same place, the conversations it may join and the channel's commands. Building the prompt from it
// reads nothing else.
type IntakeSnapshot struct {
	Gathering IntakeGathering
	// Level is the scope's intake level now: off, suggest or auto.
	Level    string
	Options  json.RawMessage
	Consents []string
	Timezone string
	Items    []IntakeSnapshotItem
	// Before are the place's messages before the card, oldest first.
	Before []IntakeSnapshotItem
	// Quoted are the messages the card's and the earlier ones reply to, by their id on the channel.
	Quoted     map[string]IntakeSnapshotItem
	Candidates []IntakeSnapshotCandidate
	Contact    *IntakeSnapshotContact
	Commands   []string
	// EventIDs are the journal events the card holds now; a decision applies only while it holds them.
	EventIDs []string
}

// IntakeSnapshotItem is a message of the place with the conversation it is part of.
type IntakeSnapshotItem struct {
	ChatItem
	ConversationID string
}

// IntakeSnapshotCandidate is a conversation the card may join, with its last public message.
type IntakeSnapshotCandidate struct {
	IntakeCandidate
	LastMessage string
}

// IntakeSnapshotContact is the customer the card is from.
type IntakeSnapshotContact struct {
	ID, Name, Organization string
	OpenConversations      int
}

// snapshotBefore is how many messages before the card are read: the window the prompt shows and,
// before it, enough for a login there to hide the password that follows.
const snapshotBefore = 40

// IntakeSnapshotOf reads what the AI decider needs of a card. ErrGatheringNotFound for none.
func (s *Store) IntakeSnapshotOf(ctx context.Context, workspaceID, gatheringID string, now time.Time) (IntakeSnapshot, error) {
	if !isID(gatheringID) {
		return IntakeSnapshot{}, ErrGatheringNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeSnapshot, error) {
		var snap IntakeSnapshot
		g, err := scanGathering(tx.QueryRow(ctx, `SELECT `+gatheringColumns+` `+gatheringFrom+` WHERE g.workspace_id=$1 AND g.id=$2::uuid`, workspaceID, gatheringID))
		if errors.Is(err, pgx.ErrNoRows) {
			return snap, ErrGatheringNotFound
		}
		if err != nil {
			return snap, err
		}
		snap.Gathering = g
		var options []byte
		if err := tx.QueryRow(ctx, `SELECT ai_level($1,$2::uuid,$3,'intake'),
				COALESCE((SELECT options FROM channel_ai_levels WHERE workspace_id=$1 AND channel_id=$2::uuid AND slot='intake' AND scope IN ($3,'')
					ORDER BY scope=$3 DESC LIMIT 1),'{}'),
				(SELECT timezone FROM workspaces WHERE id=$1),
				COALESCE((SELECT array_agg(source_class) FROM ai_source_consents WHERE workspace_id=$1),'{}'),
				COALESCE((SELECT array_agg(e.id::text ORDER BY e.id) FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.gathering_id=$4::uuid),'{}')`,
			workspaceID, g.ChannelID, g.Scope, gatheringID).Scan(&snap.Level, &options, &snap.Timezone, &snap.Consents, &snap.EventIDs); err != nil {
			return snap, fmt.Errorf("read intake settings: %w", err)
		}
		snap.Options = options
		if snap.Items, err = snapshotItems(ctx, tx, `i.gathering_id=$2::uuid ORDER BY i.occurred_at,i.id`, workspaceID, gatheringID); err != nil {
			return snap, err
		}
		known := map[string]string{}
		if err := knownContacts(ctx, tx, workspaceID, g.ChannelID, snap.Items, known); err != nil {
			return snap, err
		}
		var sender string
		for _, item := range snap.Items {
			if item.Role == "customer" && item.ContactID != nil {
				sender = *item.ContactID
				break
			}
		}
		if len(snap.Items) > 0 {
			first := snap.Items[0]
			before, err := snapshotItems(ctx, tx, `i.thread_id=$2::uuid AND i.gathering_id IS DISTINCT FROM $3::uuid AND (i.occurred_at,i.id)<($4,$5::uuid)
				ORDER BY i.occurred_at DESC,i.id DESC LIMIT $6`, workspaceID, first.ThreadID, gatheringID, first.OccurredAt, first.ID, snapshotBefore)
			if err != nil {
				return snap, err
			}
			slices.Reverse(before)
			if err := knownContacts(ctx, tx, workspaceID, g.ChannelID, before, known); err != nil {
				return snap, err
			}
			snap.Before = before
			if snap.Quoted, err = snapshotQuoted(ctx, tx, workspaceID, first.ThreadID, slices.Concat(snap.Before, snap.Items)); err != nil {
				return snap, err
			}
			for id, quoted := range snap.Quoted {
				one := []IntakeSnapshotItem{quoted}
				if err := knownContacts(ctx, tx, workspaceID, g.ChannelID, one, known); err != nil {
					return snap, err
				}
				snap.Quoted[id] = one[0]
			}
		}
		candidates, err := gatheringCandidates(ctx, tx, workspaceID, g, now)
		if err != nil {
			return snap, err
		}
		for _, candidate := range candidates {
			c := IntakeSnapshotCandidate{IntakeCandidate: candidate}
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT body FROM messages WHERE workspace_id=$1 AND conversation_id=$2::uuid AND kind IN ('incoming','outgoing')
					AND detached_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 1),'')`, workspaceID, candidate.ConversationID).Scan(&c.LastMessage); err != nil {
				return snap, fmt.Errorf("read candidate's last message: %w", err)
			}
			snap.Candidates = append(snap.Candidates, c)
		}
		var contact IntakeSnapshotContact
		err = tx.QueryRow(ctx, `SELECT p.id::text,p.name,COALESCE(p.organization,''),
				(SELECT count(*) FROM conversations c WHERE c.workspace_id=p.workspace_id AND c.contact_id=p.id AND c.status IN ('open','pending'))
			FROM intake_gatherings g LEFT JOIN channel_threads t ON t.workspace_id=g.workspace_id AND t.id=g.thread_id
			JOIN contacts p ON p.workspace_id=g.workspace_id AND p.id=COALESCE(g.contact_id,
				(SELECT i.contact_id FROM channel_thread_items i WHERE i.workspace_id=g.workspace_id AND i.gathering_id=g.id AND i.contact_id IS NOT NULL LIMIT 1),
				NULLIF($3,'')::uuid, CASE WHEN t.kind<>'group' THEN t.contact_id END)
			WHERE g.workspace_id=$1 AND g.id=$2::uuid`, workspaceID, gatheringID, sender).Scan(&contact.ID, &contact.Name, &contact.Organization, &contact.OpenConversations)
		switch {
		case err == nil:
			snap.Contact = &contact
		case !errors.Is(err, pgx.ErrNoRows):
			return snap, fmt.Errorf("read card's contact: %w", err)
		}
		snap.Commands, err = s.openCommands(ctx, tx, workspaceID, g.ChannelID)
		return snap, err
	})
}

// snapshotItems reads items of the workspace that where (with $2 onward) selects, with the conversation
// each is part of.
func snapshotItems(ctx context.Context, tx pgx.Tx, where, workspaceID string, args ...any) ([]IntakeSnapshotItem, error) {
	rows, err := tx.Query(ctx, `SELECT `+chatItemColumns+`,COALESCE((SELECT l.conversation_id::text FROM conversation_thread_items l
			WHERE l.workspace_id=i.workspace_id AND l.item_id=i.id AND l.unlinked_at IS NULL ORDER BY l.linked_at DESC LIMIT 1),'')
		FROM channel_thread_items i WHERE i.workspace_id=$1 AND `+where, append([]any{workspaceID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("read intake items: %w", err)
	}
	defer rows.Close()
	var items []IntakeSnapshotItem
	for rows.Next() {
		var item IntakeSnapshotItem
		if item.ChatItem, err = scanChatItem(rows, &item.ConversationID); err != nil {
			return nil, fmt.Errorf("read intake items: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// knownContacts gives the customers' items that have no contact yet (a held batch) the contact their
// sender's identity already belongs to, so a customer is the same person in the card, before it and in the
// conversations they wrote in. It creates none; known caches the lookups by identity.
func knownContacts(ctx context.Context, tx pgx.Tx, workspaceID, channelID string, items []IntakeSnapshotItem, known map[string]string) error {
	for i, item := range items {
		if item.ContactID != nil || item.Role != "customer" || item.Sender.Kind == nil || item.Sender.Value == nil {
			continue
		}
		key := *item.Sender.Kind + ":" + *item.Sender.Value
		contactID, seen := known[key]
		if !seen {
			var err error
			contactID, err = knownContact(ctx, tx, workspaceID, channelID, *item.Sender.Kind, *item.Sender.Value)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("find sender's contact: %w", err)
			}
			known[key] = contactID
		}
		if contactID != "" {
			items[i].ContactID = &contactID
		}
	}
	return nil
}

// snapshotQuoted reads the messages of the place that items reply to.
func snapshotQuoted(ctx context.Context, tx pgx.Tx, workspaceID, threadID string, items []IntakeSnapshotItem) (map[string]IntakeSnapshotItem, error) {
	var ids []string
	for _, item := range items {
		if item.QuoteExternalID != nil {
			ids = append(ids, *item.QuoteExternalID)
		}
	}
	quoted := map[string]IntakeSnapshotItem{}
	if len(ids) == 0 {
		return quoted, nil
	}
	found, err := snapshotItems(ctx, tx, `i.thread_id=$2::uuid AND i.external_id=ANY($3)`, workspaceID, threadID, ids)
	if err != nil {
		return nil, err
	}
	for _, item := range found {
		quoted[*item.ExternalID] = item
	}
	return quoted, nil
}
