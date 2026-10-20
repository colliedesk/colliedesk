package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const soloSmallMigration = "20261020051726-allow-solo-and-small-plans.up.sql"

func insertSubscription(ctx context.Context, tx pgx.Tx, slug, plan string) error {
	var workspace string
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(&workspace); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,source_modified_at)
		VALUES($1::uuid,'sub_'||$2,$3,'cus_'||$2,'active','2026-10-20T09:00:00Z')`, workspace, slug, plan)
	return err
}

func TestSoloAndSmallSubscriptionsCanBeStored(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	err := New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		for _, plan := range []string{"solo", "small", "team", "team25", "founding"} {
			if err := insertSubscription(ctx, tx, "plan-"+plan, plan); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("storing a subscription on every sold plan: %v", err)
	}
}

func TestSoloSmallDownRefusesWhileASubscriptionUsesThePlans(t *testing.T) {
	for _, plan := range []string{"solo", "small"} {
		t.Run(plan, func(t *testing.T) {
			pool := testPool(t)
			ctx := context.Background()
			if err := Migrate(ctx, pool); err != nil {
				t.Fatalf("up: %v", err)
			}
			if err := New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
				return insertSubscription(ctx, tx, "keep-"+plan, plan)
			}); err != nil {
				t.Fatal(err)
			}
			err := rollbackThrough(ctx, pool, soloSmallMigration)
			if err == nil || !strings.Contains(err.Error(), "solo or small") || !strings.Contains(err.Error(), "1 subscription") {
				t.Fatalf("down with a %s subscription: err=%v, want a refusal naming the plans and the count", plan, err)
			}
			var stored string
			if err := pool.QueryRow(ctx, `SELECT plan FROM workspace_billing`).Scan(&stored); err != nil || stored != plan {
				t.Fatalf("subscription after the refused down: plan=%q err=%v, want it untouched", stored, err)
			}
		})
	}
}

func TestSoloSmallDownThenUpWorksWhenNoSubscriptionUsesThePlans(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := rollbackThrough(ctx, pool, soloSmallMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	store := func(plan string) error {
		return New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
			return insertSubscription(ctx, tx, "after-down-"+plan, plan)
		})
	}
	for _, plan := range []string{"solo", "small"} {
		if err := store(plan); err == nil || !strings.Contains(err.Error(), "workspace_billing_plan_check") {
			t.Fatalf("%s after the down migration: err=%v, want the plan check to refuse it", plan, err)
		}
	}
	if err := store("team25"); err != nil {
		t.Fatalf("team25 after the down migration: %v, want the older plans kept", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
