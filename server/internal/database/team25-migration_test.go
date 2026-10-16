package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const team25Migration = "20261016032139-allow-team25-plan.up.sql"

func TestTeam25DownRefusesWhileASubscriptionUsesThePlan(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	err := New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		var workspace string
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('t25','Team 25 Co') RETURNING id::text`).Scan(&workspace); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,source_modified_at)
			VALUES($1::uuid,'sub_t25','team25','cus_t25','active','2026-10-01T09:00:00Z')`, workspace)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = rollbackThrough(ctx, pool, team25Migration)
	if err == nil || !strings.Contains(err.Error(), "team25") || !strings.Contains(err.Error(), "1") {
		t.Fatalf("down with a team25 subscription: err=%v, want a refusal naming team25 and the count", err)
	}
	var plan string
	if err := pool.QueryRow(ctx, `SELECT plan FROM workspace_billing`).Scan(&plan); err != nil || plan != "team25" {
		t.Fatalf("subscription after the refused down: plan=%q err=%v, want it untouched", plan, err)
	}
}

func TestTeam25DownThenUpWorksWhenNoSubscriptionUsesThePlan(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := rollbackThrough(ctx, pool, team25Migration); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,source_modified_at)
		VALUES(gen_random_uuid(),'x','team25','c','active',now())`); err == nil {
		t.Fatal("team25 accepted after the down migration")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
