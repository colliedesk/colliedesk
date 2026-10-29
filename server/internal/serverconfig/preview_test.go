package serverconfig_test

import (
	"context"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/jackc/pgx/v5"
)

// The billing preview counts the workspaces that lose access when billing turns on: the trial is over and
// nothing paying or complimentary is in force. It answers a number and a yes or no, never a row.
func TestPreviewCountsWhatBillingWouldLock(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	db := database.New(pool)
	now := time.Now().UTC()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(tx pgx.Tx, slug string, trialEnds time.Time) string {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name,trial_ends_at) VALUES($1,$1,$2) RETURNING id::text`, slug, trialEnds).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.privileged','on',true)`); err != nil {
		t.Fatal(err)
	}
	insert(tx, "expired", now.Add(-48*time.Hour))
	insert(tx, "on-trial", now.Add(48*time.Hour))
	paying := insert(tx, "paying", now.Add(-48*time.Hour))
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,source_modified_at)
		VALUES($1::uuid,'sub_p','team','cus_p','active',now())`, paying); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	preview := serverconfig.NewPreview(db)
	if count, err := preview.LockedWithBilling(ctx, now); err != nil || count != 1 {
		t.Fatalf("locked with billing = %d %v, want 1 (only the expired trial without a plan)", count, err)
	}
	for slug, want := range map[string]bool{"paying": true, "nobody": false} {
		if exists, err := preview.WorkspaceExists(ctx, slug); err != nil || exists != want {
			t.Fatalf("exists(%s) = %v %v, want %v", slug, exists, err, want)
		}
	}
}

// Products are added before billing turns on (the console's order): until the token is set they are not
// billing, so the billing parser reads billing off, never "partly configured".
func TestProductsBeforeBillingLeaveBillingOff(t *testing.T) {
	products := []serverconfig.ProductRow{{ProductID: "prod_tm", Plan: "team", Period: "monthly", OnSale: true},
		{ProductID: "prod_ty", Plan: "team", Period: "yearly", OnSale: true}, {ProductID: "prod_f", Plan: "founding", Period: "single", OnSale: true}}
	off := serverconfig.NewSnapshot(1, map[string]string{}, products, "", func(string) string { return "" }, true)
	if config, err := polar.ConfigFromEnv(off.Getenv); err != nil || config.Enabled() {
		t.Fatalf("products without a token: %+v %v, want billing off", config, err)
	}
	on := off.With(map[string]string{"polar.access_token": "tok", "polar.webhook_secret": "whsec"}, nil)
	config, err := polar.ConfigFromEnv(on.Getenv)
	if err != nil || !config.Enabled() || config.Products.TeamMonthly != "prod_tm" || config.Products.Founding != "prod_f" {
		t.Fatalf("billing on with products: %+v %v", config, err)
	}
}
