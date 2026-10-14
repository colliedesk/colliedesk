package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RefreshStatistics runs ANALYZE on the given tables as the owner. Use it before large joins over
// rows written moments ago (an undo right after an import): until autovacuum analyzes them, the
// planner believes the tables are nearly empty and picks nested loops that take minutes. ANALYZE
// returns no rows to the caller and the statistics are per table, so it needs no workspace scope.
func (d *DB) RefreshStatistics(ctx context.Context, tables ...string) error {
	names := make([]string, len(tables))
	for index, table := range tables {
		names[index] = pgx.Identifier{table}.Sanitize()
	}
	if _, err := d.pool.Exec(ctx, "ANALYZE "+strings.Join(names, ", ")); err != nil {
		return fmt.Errorf("refresh table statistics: %w", err)
	}
	return nil
}
