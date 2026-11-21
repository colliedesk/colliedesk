package database

import (
	"context"
	"fmt"
)

// MinPostgresMajor is the oldest PostgreSQL major version Colliedesk runs on. Every migration and query
// must work on it; CI runs the Go tests on it.
const MinPostgresMajor = 16

// CheckPostgresVersion refuses a server older than PostgreSQL minMajor before anything else runs, so an
// install on an old server stops with a message that says how to upgrade instead of failing later on
// syntax that server does not know.
func (d *DB) CheckPostgresVersion(ctx context.Context, minMajor int) error {
	var number int
	var shown string
	err := d.pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int, current_setting('server_version')`).
		Scan(&number, &shown)
	if err != nil {
		return fmt.Errorf("read the PostgreSQL version: %w", err)
	}
	if number/10000 >= minMajor {
		return nil
	}
	return fmt.Errorf("PostgreSQL %d or newer is required; this database is PostgreSQL %s. "+
		"Move the bundled database with sh upgrade-postgres.sh; upgrade an external database the way its provider says. "+
		"See https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/upgrade.md#postgresql-versions",
		minMajor, shown)
}
