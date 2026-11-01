package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// setStatusActor names who changes a conversation's status in the rest of tx, for the status
// history trigger (migration 20261023110000-conversation-status-changes): the person, and the token
// when an AI agent's token makes it. An empty actor is no one: a customer reply that reopens. Set it
// before every status write, so a change in the same transaction never carries the previous writer's
// name or cause.
func setStatusActor(ctx context.Context, tx pgx.Tx, actor Actor, quiet bool) error {
	return setStatusCause(ctx, tx, statusChange{ActorID: actor.AgentID, TokenID: actor.TokenID, Cause: tokenCause(actor.TokenID), Quiet: quiet})
}

// movedStatusQuiet is how a status a move of messages sets is recorded. A move never tells the customer, so
// it is quiet, except a resolved status it gives back: that keeps how the team resolved it (told or not),
// which decides whether a close by itself tells the customer later.
func movedStatusQuiet(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, status string) (bool, error) {
	if status != "resolved" {
		return true, nil
	}
	var quiet bool
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT quiet FROM conversation_status_changes WHERE workspace_id=$1 AND conversation_id=$2 AND to_status='resolved'
		ORDER BY changed_at DESC,id DESC LIMIT 1),false)`, workspaceID, conversationID).Scan(&quiet)
	if err != nil {
		return false, fmt.Errorf("read how the conversation was resolved: %w", err)
	}
	return quiet, nil
}

// statusChange is who changes a conversation's status, and how: the person, the token when an AI agent's
// token makes it, the cause (empty: the one the trigger reads from the actor, a person or the customer
// writing again), and whether the customer is not told.
type statusChange struct {
	ActorID, TokenID, Cause string
	Quiet                   bool
}

// Status change causes (migration 20261031170000-status-change-cause): who within Colliedesk changed it.
const (
	CauseAgent     = "agent"
	CauseToken     = "token"
	CauseCustomer  = "customer"
	CauseCommand   = "command"
	CauseAutoClose = "auto_close"
)

// setStatusCause names who changes a conversation's status in the rest of tx, and how, for the status
// history trigger (migration 20261031170000-status-change-cause).
func setStatusCause(ctx context.Context, tx pgx.Tx, change statusChange) error {
	quiet := ""
	if change.Quiet {
		quiet = "on"
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.actor_agent_id',$1,true),set_config('app.actor_token_id',$2,true),set_config('app.status_cause',$3,true),set_config('app.status_quiet',$4,true)`,
		change.ActorID, change.TokenID, change.Cause, quiet); err != nil {
		return fmt.Errorf("name the status change actor: %w", err)
	}
	return nil
}

// tokenCause is the cause of a change a person or, with tokenID, a token makes.
func tokenCause(tokenID string) string {
	if tokenID != "" {
		return CauseToken
	}
	return ""
}

// MarkImportWrites marks tx as an importer's: status changes it makes are recorded as source
// 'import', which the results page leaves out (they are the source system's work, not Colliedesk's).
func MarkImportWrites(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.status_source','import',true),set_config('app.actor_agent_id','',true),set_config('app.actor_token_id','',true),set_config('app.status_cause','',true),set_config('app.status_quiet','',true)`); err != nil {
		return fmt.Errorf("mark import transaction: %w", err)
	}
	return nil
}
