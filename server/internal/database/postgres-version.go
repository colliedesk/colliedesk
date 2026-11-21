package database

import (
	"context"
	"fmt"
	"strings"
)

// MinPostgresMajor is the oldest PostgreSQL major version Colliedesk runs on. Every migration and query
// must work on it; CI runs every test on it.
const MinPostgresMajor = 19

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
	// The release alone: Debian builds append "(Debian 17.11-1.pgdg12+2)".
	release, _, _ := strings.Cut(shown, " ")
	return fmt.Errorf("PostgreSQL %d or newer is required; this database is PostgreSQL %s. "+
		"sh upgrade-postgres.sh moves a bundled database on PostgreSQL 16, 17 or 18 to 19; upgrade any other server the way its provider says. "+
		"See https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/upgrade.md#postgresql-versions",
		minMajor, release)
}
