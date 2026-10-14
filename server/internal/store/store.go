package store

import "github.com/jackc/pgx/v5/pgxpool"

type Store struct {
	pool     *pgxpool.Pool
	outbound OutboundPolicy
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
