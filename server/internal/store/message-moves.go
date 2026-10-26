package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Moving customer messages between conversations (docs/internals/message-moves.md):
// a person or an AI agent moves messages added to the wrong conversation, splits them into a new one, or
// closes a conversation that was no new request. Only customer messages move, with their files, chat
// items and the thread keys they recorded; every move is a message_moves row its undo reverses.

// MaxMovedMessages is the most messages one move takes.
const MaxMovedMessages = 100

// Errors of moving messages.
var (
	// ErrMoveSourceHasActivity refuses to close a conversation someone replied in, wrote a note in or
	// assigned: it is a request someone works on.
	ErrMoveSourceHasActivity = errors.New("someone replied, wrote a note or assigned this conversation")
	// ErrMoveSuperseded refuses to undo a move a later move of one of its messages followed.
	ErrMoveSuperseded = errors.New("a later move took one of these messages")
	// ErrMoveDecided refuses to undo a move to the intake queue whose card was decided since.
	ErrMoveDecided = errors.New("the card these messages went to was decided since")
	// ErrNoIntakeQueue refuses to take messages that came through no chat scope with an intake queue
	// back to it.
	ErrNoIntakeQueue = errors.New("these messages have no intake queue to go back to")
)

// MoveInvalidError is a move that cannot be made as asked, with why.
type MoveInvalidError struct{ Reason string }

func (e *MoveInvalidError) Error() string { return e.Reason }

// MoveTarget is where moved messages go: an existing conversation, a new one with Subject, or the queue.
type MoveTarget struct {
	ConversationID string
	NewSubject     string
	Queue          bool
}

// MoveInput is a move of a conversation's messages.
type MoveInput struct {
	MessageIDs []string
	To         MoveTarget
	// CloseSource takes every customer message of the source to To.ConversationID and closes the source.
	CloseSource bool
}

// MessageMove is a move as the API shows it.
type MessageMove struct {
	ID                  string     `json:"id"`
	MessageIDs          []string   `json:"message_ids"`
	FromConversationID  string     `json:"from_conversation_id"`
	ToConversationID    *string    `json:"to_conversation_id"`
	CreatedConversation bool       `json:"created_conversation"`
	ClosedSource        bool       `json:"closed_source"`
	GatheringID         *string    `json:"gathering_id"`
	DecisionID          *string    `json:"decision_id"`
	MovedByAgentID      *string    `json:"moved_by_agent_id"`
	ViaToken            bool       `json:"via_token"`
	MovedAt             time.Time  `json:"moved_at"`
	UndoneAt            *time.Time `json:"undone_at"`
	UndoneByAgentID     *string    `json:"undone_by_agent_id"`
	Undoable            bool       `json:"undoable"`
}

// MoveResult is a move and what a person should know of it.
type MoveResult struct {
	Move MessageMove `json:"move"`
	// Warnings: other_contact (a message now sits in a conversation of another contact), other_channel
	// (a message came through another channel than the conversation's).
	Warnings []string `json:"warnings"`
}

// Warnings of a move.
const (
	MoveWarningOtherContact = "other_contact"
	MoveWarningOtherChannel = "other_channel"
)

// moveConversation is a conversation a move locks.
type moveConversation struct {
	id, contactID, status, channel   string
	channelID, scope, replyChannelID *string
	// mergedInto is the conversation this one was merged into, when it was.
	mergedInto *string
}

// lockMoveConversations locks the conversations of a move in id order (two moves between the same two
// conversations never wait on each other the other way round) and returns them by id.
func lockMoveConversations(ctx context.Context, tx pgx.Tx, workspaceID string, ids ...string) (map[string]moveConversation, error) {
	sorted := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == "" })
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	rows, err := tx.Query(ctx, `SELECT id::text,contact_id::text,status,channel,channel_id::text,channel_scope,reply_channel_id::text,merged_into_id::text FROM conversations
		WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR UPDATE`, workspaceID, sorted)
	if err != nil {
		return nil, fmt.Errorf("lock conversations: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (moveConversation, error) {
		var c moveConversation
		return c, row.Scan(&c.id, &c.contactID, &c.status, &c.channel, &c.channelID, &c.scope, &c.replyChannelID, &c.mergedInto)
	})
	if err != nil {
		return nil, fmt.Errorf("lock conversations: %w", err)
	}
	byID := make(map[string]moveConversation, len(found))
	for _, c := range found {
		byID[c.id] = c
	}
	return byID, nil
}

// MoveMessages moves customer messages of conversation fromID as in says, for actor.
func (s *Store) MoveMessages(ctx context.Context, workspaceID, fromID string, in MoveInput, actor Actor, now time.Time) (MoveResult, error) {
	if !isID(fromID) {
		return MoveResult{}, pgx.ErrNoRows
	}
	ids, err := moveMessageIDs(in.MessageIDs)
	if err != nil {
		return MoveResult{}, err
	}
	targets := 0
	for _, set := range []bool{in.To.ConversationID != "", in.To.NewSubject != "", in.To.Queue} {
		if set {
			targets++
		}
	}
	switch {
	case targets != 1:
		return MoveResult{}, &MoveInvalidError{"Send exactly one of to.conversation_id, to.new or to.queue."}
	case in.To.ConversationID != "" && !isID(in.To.ConversationID):
		return MoveResult{}, pgx.ErrNoRows
	case in.To.ConversationID == fromID:
		return MoveResult{}, &MoveInvalidError{"The messages are in this conversation already."}
	case in.CloseSource && in.To.ConversationID == "":
		return MoveResult{}, &MoveInvalidError{"close_source moves the messages to another conversation (to.conversation_id)."}
	case len([]rune(in.To.NewSubject)) > 200:
		return MoveResult{}, &MoveInvalidError{"A new conversation's subject is at most 200 characters."}
	}
	if in.To.Queue {
		return s.detachMessages(ctx, workspaceID, fromID, ids, actor, now)
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MoveResult, error) {
		locked, err := lockMoveConversations(ctx, tx, workspaceID, fromID, in.To.ConversationID)
		if err != nil {
			return MoveResult{}, err
		}
		source, ok := locked[fromID]
		if !ok {
			return MoveResult{}, pgx.ErrNoRows
		}
		messages, err := lockMovedMessages(ctx, tx, workspaceID, fromID, ids)
		if err != nil {
			return MoveResult{}, err
		}
		move := messageMoveRow{fromID: fromID, ids: ids, actor: actor, now: now}
		if in.CloseSource {
			if err := checkCloseSource(ctx, tx, workspaceID, fromID, len(ids)); err != nil {
				return MoveResult{}, err
			}
		}
		var target moveConversation
		if in.To.ConversationID != "" {
			if target, ok = locked[in.To.ConversationID]; !ok {
				return MoveResult{}, pgx.ErrNoRows
			}
			// Its messages went on to another conversation; opening it again would split them.
			if target.mergedInto != nil {
				return MoveResult{}, &MoveInvalidError{"That conversation was merged into another (" + *target.mergedInto + "); move the messages there."}
			}
		} else {
			if target, err = splitConversation(ctx, tx, workspaceID, source, messages, in.To.NewSubject, now); err != nil {
				return MoveResult{}, err
			}
			move.created = true
		}
		move.toID = target.id
		warnings := moveWarnings(source, target, messages)
		if err := moveIntoConversation(ctx, tx, workspaceID, source, target, &move); err != nil {
			return MoveResult{}, err
		}
		if in.CloseSource {
			if err := setStatusActor(ctx, tx, actor.AgentID); err != nil {
				return MoveResult{}, err
			}
			if _, err := tx.Exec(ctx, `UPDATE conversations SET status='closed',merged_into_id=$3,updated_at=$4 WHERE workspace_id=$1 AND id=$2`,
				workspaceID, fromID, target.id, now); err != nil {
				return MoveResult{}, fmt.Errorf("close the source conversation: %w", err)
			}
			move.closed, move.sourceStatus = true, source.status
		}
		if move.decisionID, err = recordMoveCorrections(ctx, tx, workspaceID, ids, target.id, move.created, actor, now); err != nil {
			return MoveResult{}, err
		}
		recorded, err := move.insert(ctx, tx, workspaceID)
		if err != nil {
			return MoveResult{}, err
		}
		recorded.Undoable = true
		return MoveResult{Move: recorded, Warnings: warnings}, nil
	})
}

// moveMessageIDs checks the ids of a move: one to MaxMovedMessages uuids, each once.
func moveMessageIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > MaxMovedMessages {
		return nil, &MoveInvalidError{fmt.Sprintf("Move 1 to %d messages.", MaxMovedMessages)}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.ToLower(id)
		if !isID(id) {
			return nil, &MoveInvalidError{"message_ids are ids of messages of this conversation."}
		}
		if slices.Contains(out, id) {
			return nil, &MoveInvalidError{"Name each message once."}
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

// movedMessage is a customer message a move takes.
type movedMessage struct {
	id                        string
	otherContactID, channelID *string
}

// lockMovedMessages locks the messages of a move: customer messages of fromID, in its timeline.
func lockMovedMessages(ctx context.Context, tx pgx.Tx, workspaceID, fromID string, ids []string) ([]movedMessage, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,kind,detached_at IS NOT NULL,other_contact_id::text,channel_id::text FROM messages
		WHERE workspace_id=$1 AND conversation_id=$2 AND id=ANY($3::uuid[]) ORDER BY id FOR UPDATE`, workspaceID, fromID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock messages to move: %w", err)
	}
	defer rows.Close()
	var messages []movedMessage
	for rows.Next() {
		var m movedMessage
		var kind string
		var detached bool
		if err := rows.Scan(&m.id, &kind, &detached, &m.otherContactID, &m.channelID); err != nil {
			return nil, fmt.Errorf("lock messages to move: %w", err)
		}
		switch {
		case kind != "incoming":
			return nil, &MoveInvalidError{"Only customer messages move; replies and notes of the team stay."}
		case detached:
			return nil, &MoveInvalidError{"A message taken back to the intake queue is decided there."}
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(messages) != len(ids) {
		return nil, &MoveInvalidError{"message_ids are ids of messages of this conversation."}
	}
	return messages, nil
}

// checkCloseSource refuses to close a conversation the move does not empty of customer messages, or one
// a person worked on: a reply or note by someone, or an assignment someone made.
func checkCloseSource(ctx context.Context, tx pgx.Tx, workspaceID, fromID string, moving int) error {
	var incoming int
	var worked bool
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming' AND detached_at IS NULL),
			EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind IN ('outgoing','note')
				AND (author_agent_id IS NOT NULL OR agent_token_id IS NOT NULL))
			OR EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2 AND assignee_id IS NOT NULL AND assigned_by_agent_id IS NOT NULL)`,
		workspaceID, fromID).Scan(&incoming, &worked); err != nil {
		return fmt.Errorf("check the source conversation: %w", err)
	}
	if incoming != moving {
		return &MoveInvalidError{"close_source moves every customer message of the conversation."}
	}
	if worked {
		return ErrMoveSourceHasActivity
	}
	return nil
}

// splitConversation opens the conversation a split moves messages into: on the source's channel, for the
// customer who wrote them when they are all one other contact's, else the source's.
func splitConversation(ctx context.Context, tx pgx.Tx, workspaceID string, source moveConversation, messages []movedMessage, subject string, now time.Time) (moveConversation, error) {
	contactID := source.contactID
	if writer := messages[0].otherContactID; writer != nil && slices.IndexFunc(messages, func(m movedMessage) bool {
		return m.otherContactID == nil || *m.otherContactID != *writer
	}) < 0 {
		contactID = *writer
	}
	created := moveConversation{contactID: contactID, status: "open", channel: source.channel, channelID: source.channelID, scope: source.scope,
		replyChannelID: source.replyChannelID}
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel,channel_id,channel_scope,reply_channel_id)
		VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8) RETURNING id::text`, workspaceID, contactID, subject, now, source.channel, source.channelID, source.scope,
		source.replyChannelID).Scan(&created.id); err != nil {
		return created, fmt.Errorf("open the split conversation: %w", err)
	}
	return created, nil
}

// moveWarnings says what a person should know of messages landing in target.
func moveWarnings(source, target moveConversation, messages []movedMessage) []string {
	warnings := []string{}
	for _, m := range messages {
		writer := source.contactID
		if m.otherContactID != nil {
			writer = *m.otherContactID
		}
		if writer != target.contactID && !slices.Contains(warnings, MoveWarningOtherContact) {
			warnings = append(warnings, MoveWarningOtherContact)
		}
		if m.channelID != nil && (target.channelID == nil || *target.channelID != *m.channelID) && !slices.Contains(warnings, MoveWarningOtherChannel) {
			warnings = append(warnings, MoveWarningOtherChannel)
		}
	}
	return warnings
}

// messageMoveRow is what a move writes of itself.
type messageMoveRow struct {
	fromID, toID            string
	ids                     []string
	created, closed         bool
	sourceStatus            string
	targetStatus            string
	keys                    []movedKey
	gatheringID, decisionID string
	actor                   Actor
	now                     time.Time
}

// movedKey is a thread key a move took with its message.
type movedKey struct {
	ChannelType string `json:"channel_type"`
	Key         string `json:"key"`
}

// moveIntoConversation puts the messages of move in target: their writer kept when it is not target's
// contact, the thread keys they recorded, their chat items linked there, and target reopened when it was
// resolved or closed.
func moveIntoConversation(ctx context.Context, tx pgx.Tx, workspaceID string, source, target moveConversation, move *messageMoveRow) error {
	if _, err := tx.Exec(ctx, `UPDATE messages SET conversation_id=$3,other_contact_id=NULLIF(COALESCE(other_contact_id,$4::uuid),$5::uuid)
		WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, move.ids, target.id, source.contactID, target.contactID); err != nil {
		return fmt.Errorf("move messages: %w", err)
	}
	rows, err := tx.Query(ctx, `UPDATE conversation_thread_keys k SET conversation_id=$4
		WHERE k.workspace_id=$1 AND k.message_id=ANY($2::uuid[]) AND k.conversation_id=$3
			AND NOT EXISTS(SELECT 1 FROM conversation_thread_keys t WHERE t.workspace_id=k.workspace_id AND t.channel_type=k.channel_type AND t.key=k.key
				AND t.conversation_id=$4)
		RETURNING k.channel_type,k.key`, workspaceID, move.ids, source.id, target.id)
	if err != nil {
		return fmt.Errorf("move thread keys: %w", err)
	}
	move.keys, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (movedKey, error) {
		var k movedKey
		return k, row.Scan(&k.ChannelType, &k.Key)
	})
	if err != nil {
		return fmt.Errorf("move thread keys: %w", err)
	}
	if err := relinkItems(ctx, tx, workspaceID, move.ids, source.id, target.id, "agent", move.actor.AgentID, move.now); err != nil {
		return err
	}
	if !move.created && (target.status == "resolved" || target.status == "closed") {
		if err := setStatusActor(ctx, tx, move.actor.AgentID); err != nil {
			return err
		}
		move.targetStatus = target.status
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN id=$3 AND status IN ('resolved','closed') THEN 'open' ELSE status END,updated_at=$4
		WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, []string{source.id, target.id}, target.id, move.now); err != nil {
		return fmt.Errorf("touch moved conversations: %w", err)
	}
	return nil
}

// relinkItems moves the chat items of messages from one conversation to another: the old links end, new
// live links start (a link is never rewritten, so a conversation's chat keeps what it once held).
func relinkItems(ctx context.Context, tx pgx.Tx, workspaceID string, messageIDs []string, fromID, toID, reason, agentID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE conversation_thread_items l SET unlinked_at=$4,unlinked_by_agent_id=NULLIF($5,'')::uuid
		FROM channel_thread_items i WHERE l.workspace_id=$1 AND i.workspace_id=l.workspace_id AND i.id=l.item_id AND i.message_id=ANY($2::uuid[])
			AND l.conversation_id=$3 AND l.unlinked_at IS NULL`, workspaceID, messageIDs, fromID, now, agentID); err != nil {
		return fmt.Errorf("unlink moved chat items: %w", err)
	}
	if toID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason,linked_by_agent_id,linked_at)
		SELECT $1,$3,i.id,$4,NULLIF($5,'')::uuid,$6 FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.message_id=ANY($2::uuid[])
		ON CONFLICT DO NOTHING`, workspaceID, messageIDs, toID, reason, agentID, now); err != nil {
		return fmt.Errorf("link moved chat items: %w", err)
	}
	return nil
}

// recordMoveCorrections records, for each intake card whose messages AI applied, a person's decision
// that corrects it: the label evaluation and the "AI corrected" count read. It returns the first.
func recordMoveCorrections(ctx context.Context, tx pgx.Tx, workspaceID string, messageIDs []string, toID string, created bool, actor Actor, now time.Time) (string, error) {
	action := "attach"
	if created {
		action = "create"
	}
	rows, err := tx.Query(ctx, `INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,target_conversation_id,applied,items_seen,
			corrects_decision_id,agent_id,token_id,created_at)
		SELECT $1,d.gathering_id,'agent','person',$3,NULLIF($4,'')::uuid,true,
			ARRAY(SELECT e.id FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.gathering_id=d.gathering_id AND e.message_id=ANY($2::uuid[]) ORDER BY e.id),
			d.id,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7
		FROM (SELECT DISTINCT ON (d.gathering_id) d.id,d.gathering_id,d.decider,d.applied FROM intake_decisions d
			WHERE d.workspace_id=$1 AND d.gathering_id IN (SELECT e.gathering_id FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.message_id=ANY($2::uuid[]))
			ORDER BY d.gathering_id,d.created_at DESC,d.id DESC) d
		WHERE d.decider='ai' AND d.applied
		ORDER BY d.gathering_id RETURNING id::text`, workspaceID, messageIDs, action, toID, actor.AgentID, actor.TokenID, now)
	if err != nil {
		return "", fmt.Errorf("record corrections of AI: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

func (m messageMoveRow) insert(ctx context.Context, tx pgx.Tx, workspaceID string) (MessageMove, error) {
	keys := m.keys
	if keys == nil {
		keys = []movedKey{}
	}
	rawKeys, err := json.Marshal(keys)
	if err != nil {
		return MessageMove{}, err
	}
	row := tx.QueryRow(ctx, `INSERT INTO message_moves(workspace_id,message_ids,from_conversation_id,to_conversation_id,created_conversation,closed_source,
			source_status_before,target_status_before,thread_keys_moved,gathering_id,decision_id,moved_by_agent_id,moved_by_token_id,moved_at)
		VALUES($1,$2::uuid[],$3,NULLIF($4,'')::uuid,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,'')::uuid,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,'')::uuid,$14)
		RETURNING `+messageMoveColumns, workspaceID, m.ids, m.fromID, m.toID, m.created, m.closed, m.sourceStatus, m.targetStatus, rawKeys, m.gatheringID,
		m.decisionID, m.actor.AgentID, m.actor.TokenID, m.now)
	move, err := scanMessageMove(row)
	if err != nil {
		return move, fmt.Errorf("record message move: %w", err)
	}
	return move, nil
}

const messageMoveColumns = `id::text,message_ids::text[],from_conversation_id::text,to_conversation_id::text,created_conversation,closed_source,gathering_id::text,
	decision_id::text,moved_by_agent_id::text,moved_by_token_id IS NOT NULL,moved_at,undone_at,undone_by_agent_id::text`

func scanMessageMove(row pgx.Row) (MessageMove, error) {
	var m MessageMove
	err := row.Scan(&m.ID, &m.MessageIDs, &m.FromConversationID, &m.ToConversationID, &m.CreatedConversation, &m.ClosedSource, &m.GatheringID, &m.DecisionID,
		&m.MovedByAgentID, &m.ViaToken, &m.MovedAt, &m.UndoneAt, &m.UndoneByAgentID)
	m.MovedAt, m.UndoneAt = domain.UTC(m.MovedAt), domain.UTCOptional(m.UndoneAt)
	return m, err
}

// moveUndoable is whether a move m (the alias) can still be undone: not undone, no later live move of one
// of its messages, and a move to the queue whose card still waits in it.
const moveUndoable = `(m.undone_at IS NULL
	AND NOT EXISTS(SELECT 1 FROM message_moves l WHERE l.workspace_id=m.workspace_id AND l.message_ids && m.message_ids AND l.seq>m.seq AND l.undone_at IS NULL)
	AND (m.to_conversation_id IS NOT NULL OR EXISTS(SELECT 1 FROM intake_gatherings g WHERE g.workspace_id=m.workspace_id AND g.id=m.gathering_id
		AND g.state='review')))`

// MaxMessageMoves is the most moves a conversation's list holds.
const MaxMessageMoves = 100

// MessageMovesOf lists the newest moves out of or into a conversation, newest first; pgx.ErrNoRows for a
// conversation of another workspace.
func (s *Store) MessageMovesOf(ctx context.Context, workspaceID, conversationID string) ([]MessageMove, error) {
	if !isID(conversationID) {
		return nil, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]MessageMove, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `SELECT `+prefixColumns(messageMoveColumns, "m.")+`,`+moveUndoable+` FROM message_moves m
			WHERE m.workspace_id=$1 AND (m.from_conversation_id=$2::uuid OR m.to_conversation_id=$2::uuid) ORDER BY m.seq DESC LIMIT $3`,
			workspaceID, conversationID, MaxMessageMoves)
		if err != nil {
			return nil, fmt.Errorf("list message moves: %w", err)
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MessageMove, error) {
			var m MessageMove
			err := row.Scan(&m.ID, &m.MessageIDs, &m.FromConversationID, &m.ToConversationID, &m.CreatedConversation, &m.ClosedSource, &m.GatheringID,
				&m.DecisionID, &m.MovedByAgentID, &m.ViaToken, &m.MovedAt, &m.UndoneAt, &m.UndoneByAgentID, &m.Undoable)
			m.MovedAt, m.UndoneAt = domain.UTC(m.MovedAt), domain.UTCOptional(m.UndoneAt)
			return m, err
		})
	})
}

// prefixColumns qualifies each column of a list with a table alias.
func prefixColumns(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + strings.TrimSpace(part)
	}
	return strings.Join(parts, ",")
}

// UndoMessageMove puts the messages of a move back: pgx.ErrNoRows for a move of another workspace,
// ErrMoveSuperseded when a later move took one of them, ErrMoveDecided for a move to the queue whose card
// was decided. Undoing it again returns it as it is.
func (s *Store) UndoMessageMove(ctx context.Context, workspaceID, moveID string, actor Actor, now time.Time) (MessageMove, error) {
	if !isID(moveID) {
		return MessageMove{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MessageMove, error) {
		var m struct {
			fromID, toID, sourceStatus, targetStatus, gatheringID string
			ids                                                   []string
			created, closed, undone, superseded                   bool
			keys                                                  []byte
			movedAt                                               time.Time
		}
		err := tx.QueryRow(ctx, `SELECT m.from_conversation_id::text,COALESCE(m.to_conversation_id::text,''),COALESCE(m.source_status_before,''),
				COALESCE(m.target_status_before,''),COALESCE(m.gathering_id::text,''),m.message_ids::text[],m.created_conversation,m.closed_source,m.undone_at IS NOT NULL,
				EXISTS(SELECT 1 FROM message_moves l WHERE l.workspace_id=m.workspace_id AND l.message_ids && m.message_ids AND l.seq>m.seq AND l.undone_at IS NULL),
				m.thread_keys_moved,m.moved_at
			FROM message_moves m WHERE m.workspace_id=$1 AND m.id=$2::uuid FOR UPDATE OF m`, workspaceID, moveID).
			Scan(&m.fromID, &m.toID, &m.sourceStatus, &m.targetStatus, &m.gatheringID, &m.ids, &m.created, &m.closed, &m.undone, &m.superseded, &m.keys, &m.movedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return MessageMove{}, pgx.ErrNoRows
		}
		if err != nil {
			return MessageMove{}, fmt.Errorf("lock message move: %w", err)
		}
		if !m.undone {
			if m.superseded {
				return MessageMove{}, ErrMoveSuperseded
			}
			if m.toID == "" {
				err = undoDetach(ctx, tx, workspaceID, m.fromID, m.gatheringID, m.ids, actor, now)
			} else {
				var keys []movedKey
				if err := json.Unmarshal(m.keys, &keys); err != nil {
					return MessageMove{}, fmt.Errorf("read moved thread keys: %w", err)
				}
				err = undoMove(ctx, tx, workspaceID, undoInput{fromID: m.fromID, toID: m.toID, ids: m.ids, keys: keys, created: m.created, closed: m.closed,
					sourceStatus: m.sourceStatus, targetStatus: m.targetStatus, movedAt: m.movedAt}, actor, now)
			}
			if err != nil {
				return MessageMove{}, err
			}
			if _, err := tx.Exec(ctx, `UPDATE message_moves SET undone_at=$3,undone_by_agent_id=NULLIF($4,'')::uuid,undone_by_token_id=NULLIF($5,'')::uuid
				WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, moveID, now, actor.AgentID, actor.TokenID); err != nil {
				return MessageMove{}, fmt.Errorf("record undo of message move: %w", err)
			}
		}
		return scanMessageMove(tx.QueryRow(ctx, `SELECT `+messageMoveColumns+` FROM message_moves WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, moveID))
	})
}

type undoInput struct {
	fromID, toID, sourceStatus, targetStatus string
	ids                                      []string
	keys                                     []movedKey
	created, closed                          bool
	movedAt                                  time.Time
}

// undoMove puts moved messages back in their source, with their thread keys and chat links, and gives
// back the statuses the move changed while nobody changed them since.
func undoMove(ctx context.Context, tx pgx.Tx, workspaceID string, in undoInput, actor Actor, now time.Time) error {
	locked, err := lockMoveConversations(ctx, tx, workspaceID, in.fromID, in.toID)
	if err != nil {
		return err
	}
	source, target := locked[in.fromID], locked[in.toID]
	var away int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND (conversation_id<>$3 OR detached_at IS NOT NULL)`,
		workspaceID, in.ids, in.toID).Scan(&away); err != nil {
		return fmt.Errorf("check moved messages: %w", err)
	}
	if away > 0 {
		return ErrMoveSuperseded
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET conversation_id=$3,other_contact_id=NULLIF(COALESCE(other_contact_id,$4::uuid),$5::uuid)
		WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, in.ids, in.fromID, target.contactID, source.contactID); err != nil {
		return fmt.Errorf("move messages back: %w", err)
	}
	for _, key := range in.keys {
		if _, err := tx.Exec(ctx, `UPDATE conversation_thread_keys SET conversation_id=$5 WHERE workspace_id=$1 AND channel_type=$2 AND key=$3 AND conversation_id=$4
			AND message_id=ANY($6::uuid[])`, workspaceID, key.ChannelType, key.Key, in.toID, in.fromID, in.ids); err != nil {
			return fmt.Errorf("move thread keys back: %w", err)
		}
	}
	if err := relinkItems(ctx, tx, workspaceID, in.ids, in.toID, in.fromID, "agent", actor.AgentID, now); err != nil {
		return err
	}
	if err := setStatusActor(ctx, tx, actor.AgentID); err != nil {
		return err
	}
	// A status the move set comes back while nobody changed it since: the target is still open (its reopen
	// is the move's only change to open) and no customer wrote in it since; the source is still closed
	// pointing where its messages went.
	if in.targetStatus != "" {
		if _, err := tx.Exec(ctx, `UPDATE conversations c SET status=$3 WHERE c.workspace_id=$1 AND c.id=$2 AND c.status='open'
			AND NOT EXISTS(SELECT 1 FROM conversation_status_changes s WHERE s.workspace_id=c.workspace_id AND s.conversation_id=c.id AND s.changed_at>$4
				AND s.to_status<>'open')
			AND NOT EXISTS(SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming' AND m.created_at>$4)`,
			workspaceID, in.toID, in.targetStatus, in.movedAt); err != nil {
			return fmt.Errorf("give the target its status back: %w", err)
		}
	}
	if in.closed {
		if _, err := tx.Exec(ctx, `UPDATE conversations c SET status=$3,merged_into_id=NULL WHERE c.workspace_id=$1 AND c.id=$2 AND c.status='closed'
			AND c.merged_into_id=$4`, workspaceID, in.fromID, in.sourceStatus, in.toID); err != nil {
			return fmt.Errorf("reopen the source conversation: %w", err)
		}
	}
	if in.created {
		// The conversation the split opened is left without customer messages: closed, pointing back.
		if _, err := tx.Exec(ctx, `UPDATE conversations c SET status='closed',merged_into_id=$3 WHERE c.workspace_id=$1 AND c.id=$2
			AND NOT EXISTS(SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming')`,
			workspaceID, in.toID, in.fromID); err != nil {
			return fmt.Errorf("close the split conversation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=$3 WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, []string{in.fromID, in.toID}, now); err != nil {
		return fmt.Errorf("touch conversations: %w", err)
	}
	return nil
}
