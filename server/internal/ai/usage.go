package ai

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// UsageRow sums a month's calls of one feature on one provider. Cost is what the server's provider
// spent, in micro-dollars; a workspace key's calls are billed by that provider and have none.
type UsageRow struct {
	Feature       string `json:"feature"`
	BilledTo      string `json:"billed_to"`
	ProviderLabel string `json:"provider_label"`
	Calls         int64  `json:"calls"`
	Failed        int64  `json:"failed"`
	Automatic     int64  `json:"automatic"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	CostMicroUSD  *int64 `json:"cost_micro_usd"`
	// ProviderCost sums what the provider said the calls cost, in its own unit (OpenRouter: credits),
	// never converted; nil when it said nothing.
	ProviderCost *float64 `json:"provider_cost"`
}

// Usage is one UTC calendar month of a workspace's AI calls.
type Usage struct {
	Month     string         `json:"month"`
	Allowance *AllowanceView `json:"allowance"`
	Rows      []UsageRow     `json:"rows"`
}

// ParseMonth reads YYYY-MM; empty is the current UTC month.
func (s *Service) ParseMonth(value string) (time.Time, error) {
	if value == "" {
		return monthStart(s.now()), nil
	}
	month, err := time.Parse("2006-01", value)
	if err != nil {
		return time.Time{}, invalid("month is written YYYY-MM, for example 2026-10.")
	}
	return month.UTC(), nil
}

// Usage sums the workspace's calls of the month starting at month, with the allowance when it is the
// current month and the server sets one.
func (s *Service) Usage(ctx context.Context, workspaceID string, month time.Time) (Usage, error) {
	usage := Usage{Month: month.Format("2006-01"), Rows: []UsageRow{}}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT feature,billed_to,provider_label,count(*),count(*) FILTER (WHERE status='error'),count(*) FILTER (WHERE automatic),
				COALESCE(sum(input_tokens),0)::bigint,COALESCE(sum(output_tokens),0)::bigint,
				CASE WHEN billed_to='server' THEN COALESCE(sum(cost_micro_usd),0)::bigint END,sum(provider_cost)::float8
			FROM ai_usage WHERE workspace_id=$1 AND started_at>=$2 AND started_at<$3
			GROUP BY feature,billed_to,provider_label ORDER BY feature,billed_to,provider_label`, workspaceID, month, month.AddDate(0, 1, 0))
		if err != nil {
			return err
		}
		usage.Rows, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageRow, error) {
			var r UsageRow
			err := row.Scan(&r.Feature, &r.BilledTo, &r.ProviderLabel, &r.Calls, &r.Failed, &r.Automatic, &r.InputTokens, &r.OutputTokens, &r.CostMicroUSD, &r.ProviderCost)
			return r, err
		})
		if err != nil {
			return err
		}
		if !month.Equal(monthStart(s.now())) {
			return nil
		}
		left, limit, limited, err := s.allowanceLeft(ctx, tx, workspaceID)
		if err != nil || !limited {
			return err
		}
		usage.Allowance, err = s.allowanceView(ctx, tx, workspaceID, left, limit, month.AddDate(0, 1, 0))
		return err
	})
	if usage.Rows == nil {
		usage.Rows = []UsageRow{}
	}
	return usage, err
}
