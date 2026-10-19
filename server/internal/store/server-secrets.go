package store

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// ServerSecret returns the named secret every server process shares, making it first (32 random bytes)
// when no process has yet. Two processes starting together end up with the same value: the insert is
// ON CONFLICT DO NOTHING, and both then read the row that won. The table has no workspace and no grant to
// the app role, so only this owner-connection path reads it.
func (s *Store) ServerSecret(ctx context.Context, name string) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("make server secret: %w", err)
	}
	var value []byte
	err := s.db.Privileged(ctx, database.PrivilegeServerSecret, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO server_secrets(name,value) VALUES($1,$2) ON CONFLICT (name) DO NOTHING`, name, fresh); err != nil {
			return fmt.Errorf("store server secret: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT value FROM server_secrets WHERE name=$1`, name).Scan(&value); err != nil {
			return fmt.Errorf("read server secret: %w", err)
		}
		return nil
	})
	return value, err
}
