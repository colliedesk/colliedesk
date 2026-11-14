// Package milestones appends a workspace's lifecycle milestones to workspace_events, inside the
// transaction that stores the fact itself, so a fact and its milestone commit or roll back together.
// The log is append-only (a trigger refuses UPDATE, DELETE and TRUNCATE) and outlives its workspace;
// workspace_milestones turns it into one row per workspace. Decisions: docs/internals/workspace-milestones.md.
// detail holds ids and counts only, never a name, an address or message text.
package milestones

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Kind is one milestone. The database checks the list (workspace_events.kind).
type Kind string

const (
	SignedUp             Kind = "signed_up"
	FirstCustomerMessage Kind = "first_customer_message"
	ImportFinished       Kind = "import_finished"
	FirstReply           Kind = "first_reply"
	TeammateInvited      Kind = "teammate_invited"
	TrialEnded           Kind = "trial_ended"
	// Paid and Cancelled are recorded every time billing changes that way; every other kind once.
	Paid      Kind = "paid"
	Cancelled Kind = "cancelled"
)

// notRecordedYet is true for a kind that may repeat, and for a first-time kind the workspace has not
// recorded. Checked before the insert, it keeps the usual case (already recorded) to an index lookup that
// writes nothing; ON CONFLICT settles two transactions that race for the first one.
const notRecordedYet = `($2::text IN ('paid','cancelled') OR NOT EXISTS (SELECT 1 FROM workspace_events WHERE workspace_id=$1 AND kind=$2))`

const onConflict = ` ON CONFLICT (workspace_id, kind) WHERE kind NOT IN ('paid','cancelled') DO NOTHING`

// Record appends kind for workspaceID at the time the fact happened, inside tx. A first-time kind the
// workspace already has is left as it is: the first occurrence wins.
func Record(ctx context.Context, tx pgx.Tx, workspaceID string, kind Kind, at time.Time, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source,detail)
		SELECT $1::uuid,$2::text,$3::timestamptz,'app',$4::jsonb WHERE `+notRecordedYet+onConflict, workspaceID, string(kind), at.UTC(), detail); err != nil {
		return fmt.Errorf("record milestone %s: %w", kind, err)
	}
	return nil
}

// liveCustomerMessage is true for a message m that a customer wrote in through a channel: not imported,
// not refused by the sender check, not Gmail's forwarding confirmation, not from one of the workspace's own
// agents (the test message a person sends themselves comes from their own address), and not in a
// conversation marked spam. The migration that backfilled the log uses the same rule.
const liveCustomerMessage = `m.kind='incoming' AND m.origin IS NULL AND m.channel_id IS NOT NULL
	AND m.email_meta->>'authentication' IS DISTINCT FROM 'fail'
	AND lower(m.email_meta->>'from') IS DISTINCT FROM 'forwarding-noreply@google.com'
	AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id=m.workspace_id AND a.email=lower(m.email_meta->>'from'))
	AND NOT EXISTS (SELECT 1 FROM conversations c WHERE c.workspace_id=m.workspace_id AND c.id=m.conversation_id AND c.status='spam')`

// teamReply is true for a message m that a person or an agent token wrote to a customer in Colliedesk:
// not imported, not a note, not the channel's own notice, and not in a conversation whose customer is
// one of the workspace's agents (the answer to a test message).
const teamReply = `m.kind='outgoing' AND m.origin IS NULL AND m.notice_kind IS NULL
	AND (m.author_agent_id IS NOT NULL OR m.agent_token_id IS NOT NULL)
	AND NOT EXISTS (SELECT 1 FROM conversations c
		JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id
		JOIN agents a ON a.workspace_id=c.workspace_id AND a.email=lower(k.email)
		WHERE c.workspace_id=m.workspace_id AND c.id=m.conversation_id)`

// RecordCustomerMessage records the workspace's first live customer message when messageID, just stored
// in tx, is one (liveCustomerMessage), at the time the message carries.
func RecordCustomerMessage(ctx context.Context, tx pgx.Tx, workspaceID, messageID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source,detail)
		SELECT m.workspace_id,$2::text,m.created_at,'app',jsonb_build_object('message_id',m.id,'conversation_id',m.conversation_id,'channel_id',m.channel_id,'channel_type',ch.type)
		FROM messages m JOIN channels ch ON ch.workspace_id=m.workspace_id AND ch.id=m.channel_id
		WHERE m.workspace_id=$1::uuid AND m.id=$3::uuid AND `+liveCustomerMessage+` AND `+notRecordedYet+onConflict,
		workspaceID, string(FirstCustomerMessage), messageID); err != nil {
		return fmt.Errorf("record milestone %s: %w", FirstCustomerMessage, err)
	}
	return nil
}

// RecordReply records the workspace's first reply when messageID, just stored in tx, is one (teamReply),
// at the time the message carries.
func RecordReply(ctx context.Context, tx pgx.Tx, workspaceID, messageID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source,detail)
		SELECT m.workspace_id,$2::text,m.created_at,'app',jsonb_build_object('message_id',m.id,'conversation_id',m.conversation_id)
		FROM messages m WHERE m.workspace_id=$1::uuid AND m.id=$3::uuid AND `+teamReply+` AND `+notRecordedYet+onConflict,
		workspaceID, string(FirstReply), messageID); err != nil {
		return fmt.Errorf("record milestone %s: %w", FirstReply, err)
	}
	return nil
}
