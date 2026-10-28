package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// setStatusActor names who changes a conversation's status in the rest of tx, for the status
// history trigger (migration 20261023110000-conversation-status-changes). An empty actor is no one:
// a customer reply that reopens. Set it before every status write, so a change in the same
// transaction never carries the previous writer's name.
func setStatusActor(ctx context.Context, tx pgx.Tx, actorID string) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.actor_agent_id',$1,true)`, actorID); err != nil {
		return fmt.Errorf("name the status change actor: %w", err)
	}
	return nil
}

// MarkImportWrites marks tx as an importer's: status changes it makes are recorded as source
// 'import', which the results page leaves out (they are the source system's work, not Colliedesk's).
func MarkImportWrites(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.status_source','import',true),set_config('app.actor_agent_id','',true)`); err != nil {
		return fmt.Errorf("mark import transaction: %w", err)
	}
	return nil
}
