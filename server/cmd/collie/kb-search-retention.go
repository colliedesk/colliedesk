package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/configerr"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	defaultKBSearchRetentionDays = 180
	maxKBSearchRetentionDays     = 3650
)

// kbSearchRetentionFromEnv reads kb_searches.retention_days: how long help center searches are kept
// for the search report (docs/internals/knowledge-base.md, "Searches").
func kbSearchRetentionFromEnv(getenv func(string) string) (time.Duration, error) {
	days := defaultKBSearchRetentionDays
	if value := strings.TrimSpace(getenv("kb_searches.retention_days")); value != "" {
		var err error
		days, err = strconv.Atoi(value)
		if err != nil || days < 1 || days > maxKBSearchRetentionDays {
			return 0, configerr.Say("kb_search_retention_invalid", configerr.Params{"max": maxKBSearchRetentionDays})
		}
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// deleteOldKBSearches deletes the help center searches older than retention, read at each pass.
func deleteOldKBSearches(ctx context.Context, data *store.Store, retention time.Duration) {
	if retention <= 0 {
		return
	}
	if deleted, err := data.DeleteOldKBSearches(ctx, retention); err != nil && ctx.Err() == nil {
		slog.Error("delete old help center searches", "error", err)
	} else if deleted > 0 {
		slog.Info("deleted help center searches older than the retention", "count", deleted, "days", int(retention.Hours()/24))
	}
}
